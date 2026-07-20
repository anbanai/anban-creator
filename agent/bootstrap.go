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
	"strings"
	"time"
	"unicode"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

const (
	maxWorkloadTokenBytes    = 16 << 10
	maxBootstrapResponse     = 2 << 20
	maxBootstrapFileBytes    = 64 << 20
	maxBootstrapTotalBytes   = 512 << 20
	maxBootstrapFiles        = 256
	maxBootstrapTurns        = 1000
	maxBootstrapModelBytes   = 256
	maxBootstrapPromptBytes  = 1 << 20
	maxExecutionTokenBytes   = 16 << 10
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
	return bootstrapJobWithPolicy(ctx, cfg, bootstrapRequestPolicy{})
}

type bootstrapRequestPolicy struct {
	allowHTTPLoopback bool
	client            *http.Client
}

func bootstrapJobWithPolicy(ctx context.Context, cfg JobConfig, policy bootstrapRequestPolicy) (*BootstrapResponse, error) {
	if strings.TrimSpace(cfg.ServerURL) == "" || strings.TrimSpace(cfg.ExecutionID) == "" || strings.TrimSpace(cfg.Workspace) == "" || strings.TrimSpace(cfg.WorkloadTokenFile) == "" {
		return nil, fmt.Errorf("server-url, execution-id, workspace, and workload-token-file are required")
	}
	serverURL, err := validateBootstrapServerURL(cfg.ServerURL, policy.allowHTTPLoopback)
	if err != nil {
		return nil, err
	}
	token, err := readProjectedToken(cfg.WorkloadTokenFile)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]string{"execution_id": cfg.ExecutionID})
	if err != nil {
		return nil, fmt.Errorf("marshal bootstrap request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL.String()+"/api/v1/agent/bootstrap", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create bootstrap request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := policy.client
	if client == nil {
		client = &http.Client{Timeout: bootstrapRequestTimeout}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
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
	if err := validateBootstrapRuntime(cfg.ExecutionID, &envelope.Data); err != nil {
		return &envelope.Data, err
	}
	if err := materializeBootstrap(ctx, cfg.Workspace, envelope.Data.Files, bootstrapDownloadClient()); err != nil {
		return &envelope.Data, fmt.Errorf("materialize bootstrap workspace: %w", err)
	}
	return &envelope.Data, nil
}

func validateBootstrapServerURL(raw string, allowHTTPLoopback bool) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || trimmed == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("bootstrap server URL is invalid")
	}
	if _, err := net.LookupPort("tcp", parsed.Port()); parsed.Port() != "" && err != nil {
		return nil, fmt.Errorf("bootstrap server URL is invalid")
	}
	if parsed.Scheme != "https" {
		ip := net.ParseIP(parsed.Hostname())
		if !allowHTTPLoopback || parsed.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("bootstrap server URL must use HTTPS")
		}
	}
	parsed.Path = ""
	return parsed, nil
}

func readProjectedToken(path string) (string, error) {
	resolved, info, err := resolveProjectedTokenPath(path)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0o022 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return "", fmt.Errorf("projected token has unsafe permissions")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", fmt.Errorf("open projected token: %w", err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) || opened.Mode().Perm()&0o022 != 0 || opened.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
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

func resolveProjectedTokenPath(path string) (string, os.FileInfo, error) {
	visible, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil || strings.TrimSpace(path) == "" {
		return "", nil, fmt.Errorf("resolve projected token path")
	}
	root := filepath.Dir(visible)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve projected token root: %w", err)
	}
	rootInfo, err := os.Lstat(canonicalRoot)
	if err != nil || !rootInfo.IsDir() {
		return "", nil, fmt.Errorf("projected token root must be a directory")
	}
	rel, err := filepath.Rel(root, visible)
	if err != nil || !pathWithinRoot(rel) {
		return "", nil, fmt.Errorf("projected token path escapes volume root")
	}
	components := splitPathComponents(rel)
	current := canonicalRoot
	for links := 0; ; {
		if len(components) == 0 {
			return "", nil, fmt.Errorf("projected token target is invalid")
		}
		component := components[0]
		components = components[1:]
		candidate := filepath.Join(current, component)
		info, err := os.Lstat(candidate)
		if err != nil {
			return "", nil, fmt.Errorf("resolve projected token target: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > 32 {
				return "", nil, fmt.Errorf("projected token has too many symlinks")
			}
			target, err := os.Readlink(candidate)
			if err != nil {
				return "", nil, fmt.Errorf("read projected token symlink: %w", err)
			}
			if filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
				return "", nil, fmt.Errorf("projected token symlink must be relative")
			}
			expanded := filepath.Clean(filepath.Join(current, target))
			expandedRel, err := filepath.Rel(canonicalRoot, expanded)
			if err != nil || !pathWithinRoot(expandedRel) {
				return "", nil, fmt.Errorf("projected token symlink escapes volume root")
			}
			components = append(splitPathComponents(expandedRel), components...)
			current = canonicalRoot
			continue
		}
		if len(components) > 0 {
			if !info.IsDir() {
				return "", nil, fmt.Errorf("projected token parent is not a directory")
			}
			current = candidate
			continue
		}
		if !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("projected token target must be a regular file")
		}
		return candidate, info, nil
	}
}

func pathWithinRoot(rel string) bool {
	return rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func splitPathComponents(path string) []string {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" && part != "." {
			result = append(result, part)
		}
	}
	return result
}

func decodeBoundedJSON(r io.Reader, limit int64, out any) error {
	limited := &io.LimitedReader{R: r, N: limit + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
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

func validateBootstrapResponse(executionID string, response *BootstrapResponse) error {
	if err := validateBootstrapIdentity(executionID, response); err != nil {
		return err
	}
	return validateBootstrapRuntime(executionID, response)
}

func validateBootstrapIdentity(executionID string, response *BootstrapResponse) error {
	if response == nil || strings.TrimSpace(response.ExecutionToken) == "" || strings.TrimSpace(response.TaskID) == "" || strings.TrimSpace(response.ProjectID) == "" {
		return fmt.Errorf("bootstrap response identity is incomplete")
	}
	if len(response.ExecutionToken) > maxExecutionTokenBytes || !compactExecutionToken(response.ExecutionToken) {
		return fmt.Errorf("bootstrap execution token is malformed")
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

func compactExecutionToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}

func validateBootstrapRuntime(executionID string, response *BootstrapResponse) error {
	if response == nil {
		return fmt.Errorf("bootstrap response is missing")
	}
	if !validBootstrapTaskType(response.TaskType) {
		return fmt.Errorf("bootstrap task type is invalid")
	}
	if strings.TrimSpace(response.Prompt) == "" || len(response.Prompt) > maxBootstrapPromptBytes {
		return fmt.Errorf("bootstrap prompt is invalid")
	}
	if response.MaxTurns <= 0 || response.MaxTurns > maxBootstrapTurns {
		return fmt.Errorf("bootstrap max turns is invalid")
	}
	expectedAgent := "anban:" + serveragent.TaskTypeToAgent(response.TaskType)
	if response.AgentFlag != expectedAgent {
		return fmt.Errorf("bootstrap agent flag is invalid")
	}
	if response.AutoMemoryDirectory != ".claude/memory" {
		return fmt.Errorf("bootstrap auto memory directory is invalid")
	}
	if response.ResumeSessionID != "" {
		if strings.TrimSpace(response.ResumeSessionID) != response.ResumeSessionID || len(response.ResumeSessionID) > 128 {
			return fmt.Errorf("bootstrap resume session ID is invalid")
		}
		for _, r := range response.ResumeSessionID {
			if unicode.IsControl(r) || unicode.IsSpace(r) {
				return fmt.Errorf("bootstrap resume session ID is invalid")
			}
		}
	}
	if response.ResumeContextPath != "" {
		expected, err := serveragent.ExecutionResumeContextPath(executionID)
		if err != nil || response.ResumeContextPath != expected {
			return fmt.Errorf("bootstrap resume context path is invalid")
		}
	}
	if response.ResumeSessionID != "" && response.ResumeContextPath == "" {
		return fmt.Errorf("bootstrap resume session requires resume context")
	}
	if strings.TrimSpace(response.Model) != response.Model || len(response.Model) > maxBootstrapModelBytes {
		return fmt.Errorf("bootstrap model is invalid")
	}
	if err := serveragent.ValidateClaudeRuntimeEnv(response.RuntimeEnv); err != nil {
		return fmt.Errorf("bootstrap runtime environment is invalid: %w", err)
	}
	if len(response.ModelUsageAliases) == 0 {
		return fmt.Errorf("bootstrap model usage aliases are required")
	}
	if err := serveragent.ValidateModelUsageAliases(response.ModelUsageAliases); err != nil {
		return fmt.Errorf("bootstrap model usage aliases are invalid: %w", err)
	}
	if response.TaskType != model.PlatformMontage && len(response.Env) > 0 {
		return fmt.Errorf("bootstrap environment is only valid for Montage tasks")
	}
	if len(response.Files) > maxBootstrapFiles {
		return fmt.Errorf("bootstrap file count exceeds limit")
	}
	if _, err := preflightBootstrapFiles(response.Files, false); err != nil {
		return err
	}
	return nil
}

func validBootstrapTaskType(taskType string) bool {
	switch taskType {
	case model.PlatformArticle, model.PlatformSeednote, model.PlatformMoments, model.PlatformEcommerce, model.PlatformVideoCreator, model.PlatformVideoEditor, model.PlatformMontage:
		return true
	default:
		return false
	}
}

func bootstrapDownloadClient() *http.Client {
	return newBootstrapDownloadClient(net.DefaultResolver, (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext)
}

type preparedBootstrapFile struct {
	file BootstrapFile
	rel  string
	mode os.FileMode
}

var bootstrapCommitHook func(string) error

func windowsBootstrapModeCompatible(desired, actual os.FileMode) bool {
	return (desired.Perm()&0o222 != 0) == (actual.Perm()&0o222 != 0)
}

func materializeBootstrap(ctx context.Context, workspace string, files []BootstrapFile, client *http.Client) error {
	root, err := validateWorkspaceRoot(workspace)
	if err != nil {
		return err
	}
	prepared, err := preflightBootstrapFiles(files, bootstrapClientAllowsHTTPLoopback(client))
	if err != nil {
		return err
	}
	return materializePreparedBootstrap(ctx, root, prepared, client)
}

func stageBootstrapSource(ctx context.Context, prepared preparedBootstrapFile, client *http.Client, target *os.File, total *int64) error {
	remaining := int64(maxBootstrapTotalBytes) - *total
	limit := int64(maxBootstrapFileBytes)
	if remaining < limit {
		limit = remaining
	}
	if limit <= 0 {
		return fmt.Errorf("bootstrap materialization exceeds size limit")
	}
	var source io.ReadCloser
	if prepared.file.Text != "" {
		source = io.NopCloser(strings.NewReader(prepared.file.Text))
	} else {
		if client == nil {
			return fmt.Errorf("bootstrap download client is required")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(prepared.file.DownloadURL), nil)
		if err != nil {
			return fmt.Errorf("create bootstrap download %q: %w", prepared.rel, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("download bootstrap file %q failed", prepared.rel)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return fmt.Errorf("download bootstrap file %q failed: HTTP %d", prepared.rel, resp.StatusCode)
		}
		if resp.ContentLength > limit {
			resp.Body.Close()
			return fmt.Errorf("bootstrap file %q exceeds size limit", prepared.rel)
		}
		source = resp.Body
	}
	defer source.Close()
	written, err := io.Copy(target, io.LimitReader(source, limit+1))
	if err != nil {
		return fmt.Errorf("stage bootstrap file %q: %w", prepared.rel, err)
	}
	if written > limit {
		return fmt.Errorf("bootstrap file %q exceeds size limit", prepared.rel)
	}
	*total += written
	return nil
}

func bootstrapLoopbackTestDownloadClient() *http.Client {
	return &http.Client{Timeout: bootstrapDownloadTimeout, Transport: loopbackTestTransport{base: http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func preflightBootstrapFiles(files []BootstrapFile, allowHTTPLoopback bool) ([]preparedBootstrapFile, error) {
	if len(files) > maxBootstrapFiles {
		return nil, fmt.Errorf("bootstrap file count exceeds limit")
	}
	seen := make(map[string]string, len(files))
	prepared := make([]preparedBootstrapFile, 0, len(files))
	var textTotal int64
	for _, file := range files {
		rel, err := cleanBootstrapPath(file.Path)
		if err != nil {
			return nil, err
		}
		folded := serveragent.PortableFilenameKey(filepath.ToSlash(rel))
		if previous, ok := seen[folded]; ok {
			return nil, fmt.Errorf("duplicate bootstrap path %q conflicts with %q", rel, previous)
		}
		seen[folded] = rel
		memoryKey := serveragent.PortableFilenameKey(".claude/memory")
		if folded == memoryKey || strings.HasPrefix(folded, memoryKey+"/") {
			return nil, fmt.Errorf("bootstrap path %q targets protected auto memory", rel)
		}
		if (file.Text == "") == (strings.TrimSpace(file.DownloadURL) == "") {
			return nil, fmt.Errorf("bootstrap file %q must have exactly one content source", rel)
		}
		mode := os.FileMode(file.Mode)
		if file.Mode > 0o777 || mode&0o022 != 0 || mode&0o111 != 0 || file.Mode&0o7000 != 0 || (mode != 0o600 && mode != 0o644) {
			return nil, fmt.Errorf("unsafe bootstrap file mode %#o", file.Mode)
		}
		if file.Text != "" {
			if len(file.Text) > maxBootstrapFileBytes {
				return nil, fmt.Errorf("bootstrap file %q exceeds size limit", rel)
			}
			textTotal += int64(len(file.Text))
			if textTotal > maxBootstrapTotalBytes {
				return nil, fmt.Errorf("bootstrap materialization exceeds size limit")
			}
		} else {
			parsed, err := url.Parse(strings.TrimSpace(file.DownloadURL))
			if err != nil || validateBootstrapDownloadURL(parsed, allowHTTPLoopback) != nil {
				return nil, fmt.Errorf("unsafe bootstrap download URL for %q", rel)
			}
		}
		prepared = append(prepared, preparedBootstrapFile{file: file, rel: rel, mode: mode})
	}
	for key, rel := range seen {
		ancestor := key
		for {
			index := strings.LastIndex(ancestor, "/")
			if index < 0 {
				break
			}
			ancestor = ancestor[:index]
			if conflicting, exists := seen[ancestor]; exists {
				return nil, fmt.Errorf("bootstrap path %q conflicts with file path %q", rel, conflicting)
			}
		}
	}
	return prepared, nil
}

func cleanBootstrapPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed != raw || filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" || portableDrivePath(raw) || strings.Contains(raw, "\\") {
		return "", fmt.Errorf("bootstrap path %q must be clean and relative", raw)
	}
	clean := filepath.Clean(filepath.FromSlash(raw))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.ToSlash(clean) != raw {
		return "", fmt.Errorf("bootstrap path %q escapes workspace or is not clean", raw)
	}
	for _, component := range strings.Split(filepath.ToSlash(clean), "/") {
		if err := serveragent.ValidatePortableFilenameComponent(component); err != nil {
			return "", fmt.Errorf("bootstrap path %q is not portable: %w", raw, err)
		}
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

func validateBootstrapDownloadURL(u *url.URL, allowHTTPLoopback bool) error {
	if u == nil || u.User != nil || u.Hostname() == "" || u.Fragment != "" {
		return fmt.Errorf("invalid download URL")
	}
	ip := net.ParseIP(u.Hostname())
	if allowHTTPLoopback && ip != nil && ip.IsLoopback() && (u.Scheme == "http" || u.Scheme == "https") {
		return nil
	}
	if u.Scheme != "https" {
		return fmt.Errorf("download URL must use HTTPS")
	}
	if ip != nil && !safeBootstrapIP(ip) {
		return fmt.Errorf("download URL resolves to an unsafe address")
	}
	return nil
}
