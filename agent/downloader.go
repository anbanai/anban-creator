package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type trackedToolCall struct {
	Name  string
	Input map[string]any
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
		},
	}
}

func (d *Downloader) HandleToolResult(ctx context.Context, call trackedToolCall, content any) error {
	payloads := collectDownloadPayloads(content)
	if len(payloads) == 0 {
		return nil
	}

	switch toolBaseName(call.Name) {
	case "generate_image":
		target := d.singleTargetPath(call, payloads[0])
		return d.downloadToPath(ctx, payloads[0].DownloadURL, target)
	}

	return nil
}

// toolBaseName extracts the base tool name from a potentially namespaced name.
// "mcp__plugin_anban_creator__generate_image" → "generate_image"
// "generate_image" → "generate_image"
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
	DownloadURL string
	FilePath    string
	Index       int
}

func collectDownloadPayloads(content any) []downloadPayload {
	var payloads []downloadPayload

	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			downloadURL, _ := v["download_url"].(string)
			filePath, _ := v["file_path"].(string)
			index := intNumber(v["index"])
			if strings.TrimSpace(downloadURL) != "" {
				payloads = append(payloads, downloadPayload{
					DownloadURL: strings.TrimSpace(downloadURL),
					FilePath:    strings.TrimSpace(filePath),
					Index:       index,
				})
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
			if text == "" {
				return
			}
			var decoded any
			if (strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")) && json.Unmarshal([]byte(text), &decoded) == nil {
				walk(decoded)
			}
		}
	}

	walk(content)
	return payloads
}

func intNumber(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

func (d *Downloader) singleTargetPath(call trackedToolCall, payload downloadPayload) string {
	if outputPath, _ := call.Input["output_path"].(string); strings.TrimSpace(outputPath) != "" {
		return outputPath
	}
	if payload.FilePath != "" {
		return filepath.Base(payload.FilePath)
	}
	// 只在非 data URL 时从 URL 提取文件名
	if !strings.HasPrefix(payload.DownloadURL, "data:") {
		if base := filepath.Base(payload.DownloadURL); base != "" && base != "." {
			return base
		}
	}
	return "generated.png"
}

func (d *Downloader) resolveWorkspacePath(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("target path is empty")
	}

	workspace := runtimeCwd(d.cfg.Workspace, d.cfg.TaskType)
	if !filepath.IsAbs(target) {
		target = filepath.Join(workspace, target)
	}
	target = filepath.Clean(target)

	workspace = filepath.Clean(workspace)
	if target != workspace && !strings.HasPrefix(target, workspace+string(filepath.Separator)) {
		return "", fmt.Errorf("target path %q escapes workspace", target)
	}
	return target, nil
}

func (d *Downloader) resolveDownloadURL(raw string) (string, error) {
	u, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse download url: %w", err)
	}
	if u.IsAbs() {
		return u.String(), nil
	}

	base, err := neturl.Parse(d.cfg.ServerURL)
	if err != nil {
		return "", fmt.Errorf("parse server url: %w", err)
	}
	return base.ResolveReference(u).String(), nil
}

func (d *Downloader) downloadToPath(ctx context.Context, rawURL, target string) error {
	// data URL 直接解码写入
	if strings.HasPrefix(rawURL, "data:") {
		return d.downloadDataURL(rawURL, target)
	}

	downloadURL, err := d.resolveDownloadURL(rawURL)
	if err != nil {
		return err
	}

	targetPath, err := d.resolveWorkspacePath(target)
	if err != nil {
		return err
	}
	// Skip if file already exists — the agent model may have already saved it.
	if info, err := os.Stat(targetPath); err == nil && info.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("download temp file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("download temp file failed: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("create target file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write downloaded file: %w", err)
	}

	return nil
}

// downloadDataURL decodes a data URL and writes the content to target path.
// Expected format: data:<mime>;base64,<encoded-data>
func (d *Downloader) downloadDataURL(dataURL, target string) error {
	if !strings.HasPrefix(dataURL, "data:") {
		return fmt.Errorf("invalid data URL")
	}

	parts := strings.SplitN(dataURL[5:], ",", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid data URL format")
	}

	if !strings.HasSuffix(parts[0], ";base64") {
		return fmt.Errorf("only base64 data URLs are supported")
	}

	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode base64: %w", err)
	}

	targetPath, err := d.resolveWorkspacePath(target)
	if err != nil {
		return err
	}
	// Skip if file already exists — the agent model may have already saved it.
	if info, err := os.Stat(targetPath); err == nil && info.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}

	return os.WriteFile(targetPath, data, 0644)
}
