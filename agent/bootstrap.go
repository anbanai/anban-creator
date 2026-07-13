package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/service"
)

const (
	maxWorkloadTokenBytes    = 16 << 10
	maxBootstrapResponse     = 2 << 20
	maxBootstrapFileBytes    = 64 << 20
	maxBootstrapTotalBytes   = 512 << 20
	bootstrapRequestTimeout  = 30 * time.Second
	bootstrapDownloadTimeout = 2 * time.Minute
)

type JobConfig struct {
	ServerURL         string
	ExecutionID       string
	Workspace         string
	WorkloadTokenFile string
}

type BootstrapFile = service.BootstrapFile
type BootstrapResponse = service.AgentBootstrapResponse

type bootstrapEnvelope struct {
	Code int               `json:"code"`
	Msg  string            `json:"msg"`
	Data BootstrapResponse `json:"data"`
}

func BootstrapJob(ctx context.Context, cfg JobConfig) (*BootstrapResponse, error) {
	if strings.TrimSpace(cfg.ServerURL) == "" || strings.TrimSpace(cfg.ExecutionID) == "" || strings.TrimSpace(cfg.Workspace) == "" || strings.TrimSpace(cfg.WorkloadTokenFile) == "" {
		return nil, fmt.Errorf("server-url, execution-id, workspace, and workload-token-file are required")
	}
	token, err := readProjectedToken(cfg.WorkloadTokenFile)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]string{"execution_id": cfg.ExecutionID})
	if err != nil {
		return nil, fmt.Errorf("marshal bootstrap request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.ServerURL, "/")+"/api/v1/agent/bootstrap", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create bootstrap request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{
		Timeout: bootstrapRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send bootstrap request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBootstrapResponse))
		return nil, fmt.Errorf("bootstrap request failed: HTTP %d", resp.StatusCode)
	}
	var envelope bootstrapEnvelope
	if err := decodeBoundedJSON(resp.Body, maxBootstrapResponse, &envelope); err != nil {
		return nil, fmt.Errorf("decode bootstrap response: %w", err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("bootstrap request rejected")
	}
	if err := validateBootstrapIdentity(cfg.ExecutionID, &envelope.Data); err != nil {
		return nil, err
	}
	if err := materializeBootstrap(ctx, cfg.Workspace, envelope.Data.Files, bootstrapDownloadClient()); err != nil {
		return &envelope.Data, fmt.Errorf("materialize bootstrap workspace: %w", err)
	}
	return &envelope.Data, nil
}

func readProjectedToken(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("lstat projected token: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("projected token must be a regular non-symlink file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open projected token: %w", err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return "", fmt.Errorf("projected token changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxWorkloadTokenBytes+1))
	if err != nil {
		return "", fmt.Errorf("read projected token: %w", err)
	}
	if len(raw) > maxWorkloadTokenBytes {
		return "", fmt.Errorf("projected token exceeds size limit")
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("projected token is empty")
	}
	return token, nil
}

func decodeBoundedJSON(r io.Reader, limit int64, out any) error {
	limited := &io.LimitedReader{R: r, N: limit + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	if limited.N <= 0 {
		return fmt.Errorf("response exceeds size limit")
	}
	return nil
}

func validateBootstrapIdentity(executionID string, response *BootstrapResponse) error {
	if response == nil || strings.TrimSpace(response.ExecutionToken) == "" || strings.TrimSpace(response.TaskID) == "" || strings.TrimSpace(response.ProjectID) == "" {
		return fmt.Errorf("bootstrap response identity is incomplete")
	}
	parts := strings.Split(response.ExecutionToken, ".")
	if len(parts) != 3 {
		return fmt.Errorf("bootstrap execution token is malformed")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("bootstrap execution token is malformed")
	}
	var claims struct {
		ExecutionID string `json:"execution_id"`
		TaskID      string `json:"task_id"`
		ProjectID   string `json:"project_id"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return fmt.Errorf("bootstrap execution token is malformed")
	}
	if claims.ExecutionID != executionID || claims.TaskID != response.TaskID || claims.ProjectID != response.ProjectID {
		return fmt.Errorf("bootstrap response identity mismatch")
	}
	return nil
}

func bootstrapDownloadClient() *http.Client {
	client := &http.Client{Timeout: bootstrapDownloadTimeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many bootstrap download redirects")
		}
		return validateBootstrapDownloadURL(req.URL)
	}
	return client
}

func materializeBootstrap(ctx context.Context, workspace string, files []BootstrapFile, client *http.Client) error {
	root, err := validateWorkspaceRoot(workspace)
	if err != nil {
		return err
	}
	seen := make(map[string]string, len(files))
	var total int64
	for _, file := range files {
		rel, err := cleanBootstrapPath(file.Path)
		if err != nil {
			return err
		}
		folded := strings.ToLower(filepath.ToSlash(rel))
		if previous, ok := seen[folded]; ok {
			return fmt.Errorf("duplicate bootstrap path %q conflicts with %q", rel, previous)
		}
		seen[folded] = rel
		if (file.Text == "") == (strings.TrimSpace(file.DownloadURL) == "") {
			return fmt.Errorf("bootstrap file %q must have exactly one content source", rel)
		}
		mode := os.FileMode(file.Mode)
		if file.Mode > 0o777 || mode&0o022 != 0 || mode&0o111 != 0 || file.Mode&0o7000 != 0 || (mode != 0o600 && mode != 0o644) {
			return fmt.Errorf("unsafe bootstrap file mode %#o", file.Mode)
		}
		parent := filepath.Dir(filepath.Join(root, rel))
		if err := secureMkdirAll(root, parent); err != nil {
			return err
		}
		var source io.ReadCloser
		if file.Text != "" {
			source = io.NopCloser(strings.NewReader(file.Text))
		} else {
			if client == nil {
				return fmt.Errorf("bootstrap download client is required")
			}
			parsed, err := url.Parse(strings.TrimSpace(file.DownloadURL))
			if err != nil || validateBootstrapDownloadURL(parsed) != nil {
				return fmt.Errorf("unsafe bootstrap download URL for %q", rel)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
			if err != nil {
				return fmt.Errorf("create bootstrap download %q: %w", rel, err)
			}
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("download bootstrap file %q failed", rel)
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				resp.Body.Close()
				return fmt.Errorf("download bootstrap file %q failed: HTTP %d", rel, resp.StatusCode)
			}
			if resp.ContentLength > maxBootstrapFileBytes || (resp.ContentLength >= 0 && total+resp.ContentLength > maxBootstrapTotalBytes) {
				resp.Body.Close()
				return fmt.Errorf("bootstrap file %q exceeds size limit", rel)
			}
			source = resp.Body
		}
		remaining := int64(maxBootstrapTotalBytes) - total
		fileLimit := int64(maxBootstrapFileBytes)
		if remaining < fileLimit {
			fileLimit = remaining
		}
		if fileLimit <= 0 {
			source.Close()
			return fmt.Errorf("bootstrap materialization exceeds size limit")
		}
		written, err := atomicBootstrapWrite(root, rel, mode, source, fileLimit)
		source.Close()
		if err != nil {
			return err
		}
		total += written
	}
	return nil
}

func cleanBootstrapPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" || portableDrivePath(raw) || strings.Contains(raw, "\\") {
		return "", fmt.Errorf("bootstrap path %q must be clean and relative", raw)
	}
	clean := filepath.Clean(filepath.FromSlash(raw))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.ToSlash(clean) != raw {
		return "", fmt.Errorf("bootstrap path %q escapes workspace or is not clean", raw)
	}
	return clean, nil
}

func portableDrivePath(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	first := path[0]
	return first >= 'A' && first <= 'Z' || first >= 'a' && first <= 'z'
}

func validateWorkspaceRoot(workspace string) (string, error) {
	root, err := filepath.Abs(strings.TrimSpace(workspace))
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("workspace root must already exist")
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("workspace root must be a real directory")
	}
	return root, nil
}

func secureMkdirAll(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("directory escapes workspace")
	}
	current := root
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o755); err != nil && !os.IsExist(err) {
				return fmt.Errorf("create bootstrap directory: %w", err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return fmt.Errorf("lstat bootstrap directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("bootstrap parent %q is a symlink", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("bootstrap parent %q is not a directory", current)
		}
	}
	return nil
}

func atomicBootstrapWrite(root, rel string, mode os.FileMode, source io.Reader, limit int64) (int64, error) {
	target := filepath.Join(root, rel)
	parent := filepath.Dir(target)
	if err := secureMkdirAll(root, parent); err != nil {
		return 0, err
	}
	temp, err := os.CreateTemp(parent, ".anban-bootstrap-*")
	if err != nil {
		return 0, fmt.Errorf("create bootstrap temp file: %w", err)
	}
	tempPath := temp.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(tempPath)
		}
	}()
	written, copyErr := io.Copy(temp, io.LimitReader(source, limit+1))
	if copyErr == nil && written > limit {
		copyErr = fmt.Errorf("file exceeds size limit")
	}
	if copyErr == nil {
		copyErr = temp.Chmod(mode)
	}
	if copyErr == nil {
		copyErr = temp.Sync()
	}
	closeErr := temp.Close()
	if copyErr != nil {
		return written, fmt.Errorf("write bootstrap file %q: %w", rel, copyErr)
	}
	if closeErr != nil {
		return written, fmt.Errorf("close bootstrap file %q: %w", rel, closeErr)
	}
	if err := secureMkdirAll(root, parent); err != nil {
		return written, err
	}
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return written, fmt.Errorf("bootstrap target %q is a symlink or special file", rel)
		}
		equal, err := filesEqual(target, tempPath)
		if err != nil {
			return written, err
		}
		if !equal || info.Mode().Perm() != mode {
			return written, fmt.Errorf("bootstrap target %q conflicts with existing file", rel)
		}
		return written, nil
	} else if !os.IsNotExist(err) {
		return written, fmt.Errorf("lstat bootstrap target %q: %w", rel, err)
	}
	// Hard-link publication is an atomic no-clobber operation in one directory.
	// Unlike Rename, it cannot replace a target created after the final Lstat.
	if err := os.Link(tempPath, target); err != nil {
		return written, fmt.Errorf("publish bootstrap file %q: %w", rel, err)
	}
	if err := os.Remove(tempPath); err != nil {
		return written, fmt.Errorf("remove bootstrap temp file %q: %w", rel, err)
	}
	remove = false
	if runtime.GOOS != "windows" {
		if dir, err := os.Open(parent); err == nil {
			_ = dir.Sync()
			_ = dir.Close()
		}
	}
	return written, nil
}

func filesEqual(a, b string) (bool, error) {
	aFile, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer aFile.Close()
	bFile, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer bFile.Close()
	aInfo, err := aFile.Stat()
	if err != nil {
		return false, err
	}
	bInfo, err := bFile.Stat()
	if err != nil {
		return false, err
	}
	if aInfo.Size() != bInfo.Size() || aInfo.Size() > maxBootstrapFileBytes {
		return false, nil
	}
	aHash, bHash := sha256.New(), sha256.New()
	if _, err := io.Copy(aHash, io.LimitReader(aFile, maxBootstrapFileBytes+1)); err != nil {
		return false, err
	}
	if _, err := io.Copy(bHash, io.LimitReader(bFile, maxBootstrapFileBytes+1)); err != nil {
		return false, err
	}
	return bytes.Equal(aHash.Sum(nil), bHash.Sum(nil)), nil
}

func validateBootstrapDownloadURL(u *url.URL) error {
	if u == nil || u.User != nil || u.Hostname() == "" {
		return fmt.Errorf("invalid download URL")
	}
	if strings.EqualFold(u.Scheme, "https") {
		return nil
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return fmt.Errorf("download URL must use HTTPS")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("download URL must use HTTPS")
}
