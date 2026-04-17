package main

import (
	"context"
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

	switch call.Name {
	case "generate_image":
		target := d.singleTargetPath(call, payloads[0])
		return d.downloadToPath(ctx, payloads[0].DownloadURL, target)
	case "generate_batch_images", "batch_generate_from_markdown":
		for _, payload := range payloads {
			target := d.batchTargetPath(call, payload)
			if err := d.downloadToPath(ctx, payload.DownloadURL, target); err != nil {
				return err
			}
		}
	}

	return nil
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
	return filepath.Base(payload.DownloadURL)
}

func (d *Downloader) batchTargetPath(call trackedToolCall, payload downloadPayload) string {
	outputDir, _ := call.Input["output_dir"].(string)
	outputDir = strings.TrimSpace(outputDir)
	fileName := filepath.Base(payload.FilePath)
	if fileName == "." || fileName == "" || fileName == string(filepath.Separator) {
		fileName = filepath.Base(payload.DownloadURL)
	}
	if fileName == "" {
		fileName = fmt.Sprintf("generated-%d.png", payload.Index)
	}
	if outputDir == "" {
		return fileName
	}
	return filepath.Join(outputDir, fileName)
}

func (d *Downloader) resolveWorkspacePath(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("target path is empty")
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(d.cfg.Workspace, target)
	}
	target = filepath.Clean(target)

	workspace := filepath.Clean(d.cfg.Workspace)
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
	downloadURL, err := d.resolveDownloadURL(rawURL)
	if err != nil {
		return err
	}

	targetPath, err := d.resolveWorkspacePath(target)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+d.cfg.APIKey)

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
