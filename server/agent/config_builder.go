package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"

	appconfig "github.com/anbanai/anban-creator/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/resolver"
	"github.com/anbanai/anban-creator/server/storage"
)

// maxReferenceImageBytes caps downloaded reference image size to prevent
// unbounded memory/disk usage. Mirrors the upload limit in handler/file.go.
const maxReferenceImageBytes int64 = 10 << 20 // 10 MB

// EffectiveProject returns the task snapshot view when a task carries one,
// otherwise the live project. Runtime config generation should use this so old
// tasks remain reproducible after project edits.
func EffectiveProject(ch *model.Project, task *model.Task) *model.Project {
	if ch == nil || task == nil {
		return ch
	}
	return model.ProjectFromSnapshot(ch, task.ProjectSnapshot.Data())
}

// BuildAppConfig constructs an app/config.Config from a Project DB record plus the
// resolved style dimensions. For new tasks this project is the frozen task
// snapshot; old rows without a snapshot fall back through legacy task overrides.
// This bridges the multi-user server config to the single-account app config used
// by the Anban Creator agent runtime.
//
// resolved carries the effective values (resolver.ResolveStyle); only
// the dimensions each platform's settings.json slot consumes are read here:
// Article.Writer / Article.Author / Article.Theme and Seednote.VisualStyle,
// each driven by the resolved value (not the raw project column).
func BuildAppConfig(ch *model.Project, resolved resolver.Resolved, imageAPICfg *srvconfig.ImageAPIConfig, taskImageRatio string, skipRefImage bool, taskReferenceImageURL string) (*appconfig.Config, error) {
	cfg := &appconfig.Config{
		Name:        ch.Name,
		Positioning: ch.Instructions,
	}

	// Parse keywords (comma or space separated).
	if ch.Keywords != "" {
		for _, kw := range strings.Split(ch.Keywords, ",") {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				cfg.Keywords = append(cfg.Keywords, kw)
			}
		}
	}

	// WeChat credentials from Project.
	cfg.Wechat.AppID = ch.GetWechatAppID()
	cfg.Wechat.Secret = ch.GetWechatSecret()

	// Platform-specific fields. The style/author/theme values come from the
	// resolved set. For current tasks, the raw project already represents the
	// frozen snapshot; legacy task overrides are applied only for old rows.
	switch ch.Platform {
	case model.ScopeArticle:
		// Author is the publish署名 (goes to draft.json's author key at publish).
		cfg.Wechat.Article.Author = resolved.Author
		// Writer is the writer RESOURCE key (e.g. "dan-koe") — NOT the image
		// visual style. The article visual style is orthogonal and is read by the
		// agent solely from get_project_profile (MCP); it never enters the user
		// prompt nor settings.json.
		cfg.Wechat.Article.Writer = resolved.Writer
		cfg.Wechat.Article.Theme = resolved.Theme
	case model.ScopeSeednote:
		cfg.Seednote = &appconfig.SeednoteConfig{}
		// Seednote visual style is an image description (no separate writer dimension).
		cfg.Seednote.VisualStyle = resolved.VisualStyle
	}

	// Apply global image API config from server config.
	if imageAPICfg != nil {
		if imageAPICfg.Cover != nil {
			switch ch.Platform {
			case model.ScopeArticle:
				cfg.Wechat.Article.Cover.Image = *imageAPICfg.Cover
			case model.ScopeSeednote:
				cfg.Seednote.Cover.Image = *imageAPICfg.Cover
			}
		}
		if imageAPICfg.Content != nil {
			switch ch.Platform {
			case model.ScopeArticle:
				cfg.Wechat.Article.Content.Image = *imageAPICfg.Content
			case model.ScopeSeednote:
				cfg.Seednote.Content.Image = *imageAPICfg.Content
			}
		}

		// Set platform-specific default sizes on the ImageAPI configs.
		// These are used by generate_image (which reads apiCfg.Size).
		// Note: Size is set even when Cover/Content config is nil, so the default
		// is available if the user configures provider/key later.
		switch ch.Platform {
		case model.ScopeArticle:
			if cfg.Wechat.Article.Cover.Image.Size == "" {
				cfg.Wechat.Article.Cover.Image.Size = imageAPICfg.Sizes.ArticleCover
			}
			if cfg.Wechat.Article.Content.Image.Size == "" {
				cfg.Wechat.Article.Content.Image.Size = imageAPICfg.Sizes.ArticleContent
			}
		case model.ScopeSeednote:
			if cfg.Seednote.Cover.Image.Size == "" {
				cfg.Seednote.Cover.Image.Size = imageAPICfg.Sizes.SeednoteCover
			}
			if cfg.Seednote.Content.Image.Size == "" {
				cfg.Seednote.Content.Image.Size = imageAPICfg.Sizes.SeednoteContent
			}
		}
	}

	// Apply image ratio override: task-level > project-level > server YAML defaults.
	effectiveRatio := taskImageRatio
	if effectiveRatio == "" {
		effectiveRatio = ch.ImageRatio
	}
	if effectiveRatio != "" {
		switch ch.Platform {
		case model.ScopeArticle:
			cfg.Wechat.Article.Cover.Image.Size = effectiveRatio
			cfg.Wechat.Article.Content.Image.Size = effectiveRatio
		case model.ScopeSeednote:
			cfg.Seednote.Cover.Image.Size = effectiveRatio
			cfg.Seednote.Content.Image.Size = effectiveRatio
		}
	}

	// Set reference image path for image generation (downloaded by executor).
	// Task-level reference image takes priority over project brand image.
	effectiveReferURL := taskReferenceImageURL
	if effectiveReferURL == "" && !skipRefImage {
		effectiveReferURL = ch.ReferenceImageURL
	}
	if effectiveReferURL != "" {
		referPath := filepath.Join(appconfig.ConfigDir, "reference.png")
		switch ch.Platform {
		case model.ScopeArticle:
			cfg.Wechat.Article.Cover.Image.Refer = referPath
			cfg.Wechat.Article.Content.Image.Refer = referPath
		case model.ScopeSeednote:
			cfg.Seednote.Cover.Image.Refer = referPath
			cfg.Seednote.Content.Image.Refer = referPath
		}
	}

	return cfg, nil
}

// writeSettingsJSON writes the app config to the workspace's .anban-creator/settings.json.
// The Anban Creator runtime reads config from CWD/.anban-creator/settings.json as its
// highest-priority search path.
func writeSettingsJSON(workDir string, cfg *appconfig.Config) error {
	path := filepath.Join(workDir, appconfig.ConfigDir, appconfig.ConfigFileName)
	if err := appconfig.SaveConfig(path, cfg); err != nil {
		return fmt.Errorf("write settings.json: %w", err)
	}
	return nil
}

// BuildAutoMemorySettingsJSON returns a Claude Code settings JSON document that
// points auto memory at the task-local runtime memory directory.
func BuildAutoMemorySettingsJSON(autoMemoryDir string) (string, error) {
	return buildAutoMemorySettingsJSON(autoMemoryDir)
}

func buildAutoMemorySettingsJSON(autoMemoryDir string) (string, error) {
	autoMemoryDir = strings.TrimSpace(autoMemoryDir)
	if autoMemoryDir == "" {
		return "", nil
	}
	data, err := json.Marshal(map[string]string{"autoMemoryDirectory": autoMemoryDir})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func containerMemoryDir(hostWorkDir, containerWorkDir, hostMemoryDir string) string {
	hostWorkDir = filepath.Clean(hostWorkDir)
	containerWorkDir = filepath.ToSlash(filepath.Clean(containerWorkDir))
	hostMemoryDir = filepath.Clean(hostMemoryDir)
	rel, err := filepath.Rel(hostWorkDir, hostMemoryDir)
	if err != nil || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(hostMemoryDir)
	}
	return filepath.ToSlash(filepath.Join(containerWorkDir, rel))
}

// writeProjectCLAUDEMD writes a project's positioning into a fixed CLAUDE.md
// template in the workspace root. Claude Code loads CLAUDE.md from the cwd as
// project memory, so all skills/sub-agents in the session receive the same
// positioning Studio displays. It is a no-op when the project is nil or blank.
func writeProjectCLAUDEMD(workDir string, project *model.Project) error {
	if project == nil || strings.TrimSpace(project.Instructions) == "" {
		return nil
	}
	path := filepath.Join(workDir, "CLAUDE.md")
	content := "# CLAUDE.md\n\n## 项目定位\n\n" + strings.TrimSpace(project.Instructions)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write CLAUDE.md: %w", err)
	}
	return nil
}

// TaskTypeToAgent maps server task types to Claude Code agent names.
func TaskTypeToAgent(taskType string) string {
	switch taskType {
	case model.ScopeArticle:
		return "wechatarticle"
	case model.ScopeSeednote:
		return "seednote"
	case model.ScopeEcommerce:
		return "ecommerce"
	case model.ScopeVideo:
		return "videocreator"
	default:
		return "seednote"
	}
}

// TaskToAgent maps a full task snapshot to the Claude Code agent name. Video
// tasks use video_config.workflow so generation and editing cannot drift into
// each other's workflows.
func TaskToAgent(task *model.Task) string {
	if task == nil {
		return TaskTypeToAgent("")
	}
	if task.Type == model.ScopeVideo {
		cfg := task.VideoConfig.Data()
		if model.NormalizeVideoWorkflow(cfg.Workflow) == model.VideoWorkflowEditor {
			return "videoeditor"
		}
		return "videocreator"
	}
	return TaskTypeToAgent(task.Type)
}

// DownloadReferenceImage downloads a project's brand reference image to the
// workspace's .anban-creator directory. The image is saved as reference.png for
// use by both Claude Code visual context and image generation reference inputs.
//
// Resolution order:
//  1. If store is non-nil and imageURL is server-owned (OSS or local storage),
//     read bytes via store.Read. This works for both private OSS buckets
//     (OSSProvider.Read signs the URL internally) and local files
//     (LocalProvider.Read reads from disk), avoiding the 403 that the raw
//     public URL stored in the database would hit on a private bucket.
//  2. Otherwise (external URL, or store.Read failed), fall back to direct
//     HTTP GET. imageURL must be an absolute http(s) URL in this path.
//
// logger may be nil; when non-nil, fallbacks from path (1) are logged at warn.
func DownloadReferenceImage(ctx context.Context, store storage.Provider, logger *zerolog.Logger, workDir, imageURL string) error {
	destDir := filepath.Join(workDir, appconfig.ConfigDir)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	destPath := filepath.Join(destDir, "reference.png")

	if store != nil && store.IsOwnedURL(imageURL) {
		if key, ok := storage.StorageKeyFromURL(imageURL); ok {
			data, err := store.Read(ctx, key)
			if err == nil {
				if int64(len(data)) > maxReferenceImageBytes {
					return fmt.Errorf("download: file too large (%d bytes)", len(data))
				}
				if err := os.WriteFile(destPath, data, 0o644); err != nil {
					return fmt.Errorf("write file: %w", err)
				}
				return nil
			}
			if logger != nil {
				logger.Warn().Err(err).
					Str("url", imageURL).
					Str("key", key).
					Msg("storage.Read failed for reference image, falling back to direct HTTP")
			}
		} else if logger != nil {
			logger.Warn().
				Str("url", imageURL).
				Msg("could not extract storage key from owned URL, falling back to direct HTTP")
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxReferenceImageBytes)); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// DownloadProductImages downloads each product photo URL into the workspace's
// .anban-creator/products/ directory (used by e-commerce tasks), preserving upload
// order with 1-indexed names (product_01.<ext>, product_02.<ext>, ...). It also
// writes index.json listing the exact filenames so the agent can reference them
// deterministically (extensions vary by upload). Returns the count successfully
// materialized; per-image failures are logged and skipped (best-effort), matching
// DownloadReferenceImage's non-fatal posture.
//
// The resolution path mirrors DownloadReferenceImage (store.Read for URLs owned
// by this backend — works on private OSS buckets; direct HTTP otherwise) so it
// behaves identically under the local and docker executors.
func DownloadProductImages(ctx context.Context, store storage.Provider, logger *zerolog.Logger, workDir string, urls []string) int {
	if len(urls) == 0 {
		return 0
	}
	destDir := filepath.Join(workDir, appconfig.ConfigDir, "products")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		if logger != nil {
			logger.Warn().Err(err).Msg("create products dir failed")
		}
		return 0
	}

	names := make([]string, 0, len(urls))
	for i, imageURL := range urls {
		data, err := fetchImageBytes(ctx, store, imageURL)
		if err != nil {
			if logger != nil {
				logger.Warn().Err(err).Str("url", imageURL).Int("index", i+1).Msg("failed to download product photo, skipping")
			}
			continue
		}
		name := fmt.Sprintf("product_%02d%s", i+1, imageExtFromURL(imageURL))
		if err := os.WriteFile(filepath.Join(destDir, name), data, 0o644); err != nil {
			if logger != nil {
				logger.Warn().Err(err).Str("name", name).Msg("write product photo failed, skipping")
			}
			continue
		}
		names = append(names, name)
	}

	if len(names) > 0 {
		if indexBytes, err := json.Marshal(names); err == nil {
			if err := os.WriteFile(filepath.Join(destDir, "index.json"), indexBytes, 0o644); err != nil {
				if logger != nil {
					logger.Warn().Err(err).Msg("write products index.json failed")
				}
			}
		}
	}
	return len(names)
}

// fetchImageBytes resolves an image URL to its bytes. For URLs owned by this
// backend (OSS / local storage) it reads via the storage provider (works on
// private buckets); otherwise it downloads via HTTP. Mirrors the resolution logic
// inside DownloadReferenceImage, extracted here so the multi-file product-photo
// flow can reuse it without touching the well-tested reference-image path.
func fetchImageBytes(ctx context.Context, store storage.Provider, imageURL string) ([]byte, error) {
	if store != nil && store.IsOwnedURL(imageURL) {
		if key, ok := storage.StorageKeyFromURL(imageURL); ok {
			if data, err := store.Read(ctx, key); err == nil {
				if int64(len(data)) > maxReferenceImageBytes {
					return nil, fmt.Errorf("download: file too large (%d bytes)", len(data))
				}
				return data, nil
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxReferenceImageBytes))
}

// imageExtFromURL infers a lowercase image extension from the URL path, defaulting
// to .png when unknown. Extension is taken from the URL (the canonical source for
// /files/upload and OSS object keys) rather than sniffing bytes.
func imageExtFromURL(imageURL string) string {
	switch ext := strings.ToLower(filepath.Ext(imageURL)); ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return ext
	default:
		return ".png"
	}
}
