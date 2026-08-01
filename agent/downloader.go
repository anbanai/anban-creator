package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	runtimeArtifactMaterializationFailureCode       = "runtime_artifact_materialization_failed"
	maxRuntimeImageArtifactBytes              int64 = 25 << 20
)

type trackedToolCall struct {
	Name  string
	Input map[string]any
}

type runtimeArtifactMaterializationError struct {
	FilePath string
	Err      error
}

func (e *runtimeArtifactMaterializationError) Error() string {
	if e.FilePath == "" {
		return fmt.Sprintf("%s: %v", runtimeArtifactMaterializationFailureCode, e.Err)
	}
	return fmt.Sprintf("%s: materialize %q: %v", runtimeArtifactMaterializationFailureCode, e.FilePath, e.Err)
}

func (e *runtimeArtifactMaterializationError) Unwrap() error { return e.Err }

func artifactMaterializationError(filePath string, err error) error {
	return &runtimeArtifactMaterializationError{FilePath: filePath, Err: err}
}

type Downloader struct {
	cfg    *Config
	client *http.Client
}

func NewDownloader(cfg *Config) *Downloader {
	return &Downloader{
		cfg: cfg,
		client: &http.Client{
			Timeout: 2 * time.Minute,
			CheckRedirect: func(req *http.Request, _ []*http.Request) error {
				if !sameURLOrigin(req.URL.String(), cfg.ServerURL) {
					req.Header.Del("Authorization")
				}
				return nil
			},
		},
	}
}

func (d *Downloader) HandleToolResult(ctx context.Context, call trackedToolCall, content any) error {
	if toolBaseName(call.Name) != "generate_image" {
		return nil
	}
	payloads := collectDownloadPayloads(content)
	if len(payloads) != 1 {
		return artifactMaterializationError("", fmt.Errorf("generate_image returned %d artifact descriptors, want exactly one", len(payloads)))
	}
	return d.materializeGeneratedImage(ctx, call, payloads[0])
}

// toolBaseName extracts the base tool name from a potentially namespaced name.
func toolBaseName(name string) string {
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(name, "__", 3)
		if len(parts) == 3 {
			return parts[2]
		}
	}
	return name
}

type downloadPayload struct {
	TaskFileID  string `json:"task_file_id"`
	FilePath    string `json:"file_path"`
	DownloadURL string `json:"download_url"`
	MimeType    string `json:"mime_type"`
	FileSize    int64  `json:"file_size"`
	ContentHash string `json:"content_hash"`
}

func collectDownloadPayloads(content any) []downloadPayload {
	var payloads []downloadPayload
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if _, ok := v["task_file_id"]; ok {
				data, err := json.Marshal(v)
				if err == nil {
					var payload downloadPayload
					if json.Unmarshal(data, &payload) == nil {
						payloads = append(payloads, payload)
					}
				}
				return
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		case string:
			text := strings.TrimSpace(v)
			if text == "" || (!strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[")) {
				return
			}
			var decoded any
			if json.Unmarshal([]byte(text), &decoded) == nil {
				walk(decoded)
			}
		}
	}
	walk(content)
	return payloads
}

func (d *Downloader) materializeGeneratedImage(ctx context.Context, call trackedToolCall, payload downloadPayload) error {
	requestedPath, _ := call.Input["output_path"].(string)
	if requestedPath == "" || strings.TrimSpace(requestedPath) != requestedPath {
		return artifactMaterializationError(payload.FilePath, errors.New("generate_image output_path is required and must not contain surrounding whitespace"))
	}
	cleanPath, err := cleanBootstrapPath(requestedPath)
	if err != nil {
		return artifactMaterializationError(payload.FilePath, err)
	}
	if filepath.ToSlash(cleanPath) != requestedPath || (requestedPath != "output" && !strings.HasPrefix(requestedPath, "output/")) {
		return artifactMaterializationError(payload.FilePath, fmt.Errorf("generate_image output_path %q must be inside output/", requestedPath))
	}
	if payload.FilePath != requestedPath {
		return artifactMaterializationError(payload.FilePath, fmt.Errorf("artifact file_path %q does not match requested output_path %q", payload.FilePath, requestedPath))
	}
	if err := validateImageArtifactDescriptor(payload); err != nil {
		return artifactMaterializationError(payload.FilePath, err)
	}

	targetPath, err := d.resolveWorkspacePath(payload.FilePath)
	if err != nil {
		return artifactMaterializationError(payload.FilePath, err)
	}
	if same, err := existingArtifactMatches(targetPath, payload); err != nil {
		return artifactMaterializationError(payload.FilePath, err)
	} else if same {
		return nil
	}
	if err := ensureArtifactParent(runtimeCwd(d.cfg.Workspace, d.cfg.RuntimeAdapter), targetPath); err != nil {
		return artifactMaterializationError(payload.FilePath, err)
	}
	if err := d.downloadVerifiedArtifact(ctx, payload, targetPath); err != nil {
		return artifactMaterializationError(payload.FilePath, err)
	}
	return nil
}

func validateImageArtifactDescriptor(payload downloadPayload) error {
	if strings.TrimSpace(payload.TaskFileID) == "" {
		return errors.New("artifact task_file_id is required")
	}
	if strings.TrimSpace(payload.DownloadURL) == "" {
		return errors.New("artifact download_url is required")
	}
	if payload.FileSize <= 0 || payload.FileSize > maxRuntimeImageArtifactBytes {
		return fmt.Errorf("artifact file_size %d is outside the allowed range", payload.FileSize)
	}
	payload.ContentHash = strings.TrimSpace(payload.ContentHash)
	decodedHash, err := hex.DecodeString(payload.ContentHash)
	if err != nil || len(decodedHash) != sha256.Size || strings.ToLower(payload.ContentHash) != payload.ContentHash {
		return errors.New("artifact content_hash must be a lowercase SHA-256 digest")
	}
	switch payload.MimeType {
	case "image/png":
		if strings.ToLower(filepath.Ext(payload.FilePath)) != ".png" {
			return errors.New("image/png artifact must use a .png path")
		}
	case "image/jpeg":
		ext := strings.ToLower(filepath.Ext(payload.FilePath))
		if ext != ".jpg" && ext != ".jpeg" {
			return errors.New("image/jpeg artifact must use a .jpg or .jpeg path")
		}
	case "image/webp":
		if strings.ToLower(filepath.Ext(payload.FilePath)) != ".webp" {
			return errors.New("image/webp artifact must use a .webp path")
		}
	default:
		return fmt.Errorf("unsupported artifact MIME %q", payload.MimeType)
	}
	return nil
}

func existingArtifactMatches(targetPath string, payload downloadPayload) (bool, error) {
	info, err := os.Lstat(targetPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect existing artifact: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("existing artifact target must be a regular file")
	}
	if info.Size() != payload.FileSize {
		return false, nil
	}
	file, err := os.Open(targetPath)
	if err != nil {
		return false, fmt.Errorf("open existing artifact: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maxRuntimeImageArtifactBytes+1))
	if err != nil {
		return false, fmt.Errorf("hash existing artifact: %w", err)
	}
	if written > maxRuntimeImageArtifactBytes {
		return false, errors.New("existing artifact exceeds size limit")
	}
	return hex.EncodeToString(hash.Sum(nil)) == payload.ContentHash, nil
}

func ensureArtifactParent(workspace, targetPath string) error {
	workspace = filepath.Clean(workspace)
	parent := filepath.Dir(targetPath)
	rel, err := filepath.Rel(workspace, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("artifact parent escapes runtime workspace")
	}
	current := workspace
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(current, 0o755); err != nil && !os.IsExist(err) {
				return fmt.Errorf("create artifact directory: %w", err)
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return fmt.Errorf("inspect artifact directory: %w", statErr)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact directory %q must be a real directory", current)
		}
	}
	return nil
}

func (d *Downloader) downloadVerifiedArtifact(ctx context.Context, payload downloadPayload, targetPath string) error {
	downloadURL, err := d.resolveDownloadURL(payload.DownloadURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create artifact download request: %w", err)
	}
	if d.cfg.APIKey != "" && sameURLOrigin(downloadURL, d.cfg.ServerURL) {
		req.Header.Set("Authorization", "Bearer "+d.cfg.APIKey)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download artifact failed: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxRuntimeImageArtifactBytes || (resp.ContentLength >= 0 && resp.ContentLength != payload.FileSize) {
		return fmt.Errorf("artifact size mismatch: response declares %d bytes, descriptor declares %d", resp.ContentLength, payload.FileSize)
	}
	if contentType, _, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Type")); parseErr == nil && strings.HasPrefix(contentType, "image/") && contentType != payload.MimeType {
		return fmt.Errorf("artifact MIME mismatch: response is %s, descriptor is %s", contentType, payload.MimeType)
	}

	temp, err := os.CreateTemp(filepath.Dir(targetPath), ".anban-artifact-*")
	if err != nil {
		return fmt.Errorf("create artifact staging file: %w", err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(resp.Body, maxRuntimeImageArtifactBytes+1))
	if err != nil {
		return fmt.Errorf("stage artifact download: %w", err)
	}
	if written > maxRuntimeImageArtifactBytes {
		return errors.New("artifact download exceeds size limit")
	}
	if written != payload.FileSize {
		return fmt.Errorf("artifact size mismatch: downloaded %d bytes, expected %d", written, payload.FileSize)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != payload.ContentHash {
		return fmt.Errorf("artifact SHA-256 mismatch: downloaded %s, expected %s", got, payload.ContentHash)
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind artifact staging file: %w", err)
	}
	header := make([]byte, 512)
	n, readErr := io.ReadFull(temp, header)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return fmt.Errorf("inspect artifact MIME: %w", readErr)
	}
	if detected := http.DetectContentType(header[:n]); detected != payload.MimeType {
		return fmt.Errorf("artifact MIME mismatch: bytes are %s, expected %s", detected, payload.MimeType)
	}
	if err := temp.Chmod(0o644); err != nil {
		return fmt.Errorf("set artifact permissions: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync artifact staging file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close artifact staging file: %w", err)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("commit artifact: %w", err)
	}
	committed = true
	return nil
}

func (d *Downloader) resolveWorkspacePath(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("target path is empty")
	}
	workspace := filepath.Clean(runtimeCwd(d.cfg.Workspace, d.cfg.RuntimeAdapter))
	if !filepath.IsAbs(target) {
		target = filepath.Join(workspace, target)
	}
	target = filepath.Clean(target)
	if target != workspace && !strings.HasPrefix(target, workspace+string(filepath.Separator)) {
		return "", fmt.Errorf("target path %q escapes workspace", target)
	}
	return target, nil
}

func (d *Downloader) resolveDownloadURL(raw string) (string, error) {
	u, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse download URL: %w", err)
	}
	if !u.IsAbs() {
		base, baseErr := neturl.Parse(d.cfg.ServerURL)
		if baseErr != nil {
			return "", fmt.Errorf("parse server URL: %w", baseErr)
		}
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("artifact download URL must use HTTP or HTTPS")
	}
	return u.String(), nil
}

func sameURLOrigin(rawURL, rawBase string) bool {
	target, targetErr := neturl.Parse(rawURL)
	base, baseErr := neturl.Parse(rawBase)
	if targetErr != nil || baseErr != nil || target.Hostname() == "" || base.Hostname() == "" {
		return false
	}
	return strings.EqualFold(target.Scheme, base.Scheme) &&
		strings.EqualFold(target.Hostname(), base.Hostname()) &&
		effectiveURLPort(target) == effectiveURLPort(base)
}

func effectiveURLPort(u *neturl.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
