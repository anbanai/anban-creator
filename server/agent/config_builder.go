package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
)

// BuildAppConfig constructs an app/config.Config from a Channel DB record.
// This bridges the multi-user server config to the single-account app config
// used by the abwriter CLI binary.
func BuildAppConfig(ch *model.Channel, imageAPICfg *srvconfig.ImageAPIConfig, taskImageRatio string, skipRefImage bool) (*appconfig.Config, error) {
	cfg := &appconfig.Config{
		Name:        ch.Name,
		Positioning: ch.Positioning,
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

	// WeChat credentials from Channel.
	cfg.Wechat.AppID = ch.GetWechatAppID()
	cfg.Wechat.Secret = ch.GetWechatSecret()

	// Platform-specific fields.
	switch ch.Platform {
	case model.ScopeArticle:
		cfg.Wechat.Article.Author = ch.Author
		cfg.Wechat.Article.Style = ch.Style
		cfg.Wechat.Article.Theme = ch.Theme
	case model.ScopeSeednote:
		cfg.Seednote = &appconfig.SeednoteConfig{}
		cfg.Seednote.Style = ch.Style
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

	// Apply image ratio override: task-level > channel-level > server YAML defaults.
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

	// Set brand reference image path for image generation (downloaded by executor).
	if ch.ReferenceImageURL != "" && !skipRefImage {
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

// writeSettingsJSON writes the app config to the workspace's .anbanwriter/settings.json.
// The abwriter CLI binary reads config from CWD/.anbanwriter/settings.json as its
// highest-priority search path.
func writeSettingsJSON(workDir string, cfg *appconfig.Config) error {
	path := filepath.Join(workDir, appconfig.ConfigDir, appconfig.ConfigFileName)
	if err := appconfig.SaveConfig(path, cfg); err != nil {
		return fmt.Errorf("write settings.json: %w", err)
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
	default:
		return "seednote"
	}
}

// DownloadReferenceImage downloads a channel's brand reference image to the
// workspace's .anbanwriter directory. The image is saved as reference.png for
// use by both Claude Code (visual context) and abwriter CLI (--ref flag).
func DownloadReferenceImage(ctx context.Context, workDir, imageURL string) error {
	destDir := filepath.Join(workDir, appconfig.ConfigDir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	destPath := filepath.Join(destDir, "reference.png")

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

	// Limit download size to 20MB.
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 20*1024*1024)); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}
