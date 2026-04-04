package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"go.uber.org/zap"

	"github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/server/model"

	srvconfig "github.com/royalrick/anbanwriter/server/config"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

// safePath resolves path relative to workDir and ensures it does not escape workDir.
func safePath(workDir, path string) (string, error) {
	absWorkDir, _ := filepath.Abs(workDir)
	fullPath, _ := filepath.Abs(filepath.Join(workDir, path))
	if !strings.HasPrefix(fullPath, absWorkDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("access denied: path escapes workspace")
	}
	return fullPath, nil
}

// BuildAppConfig constructs an app/config.Config from a Channel DB record.
// This bridges the multi-user server config to the single-account app config.
func BuildAppConfig(ch *model.Channel, imageAPICfg *srvconfig.ImageAPIConfig) (*config.Config, error) {
	cfg := &config.Config{
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

	// WeChat credentials.
	cfg.Wechat.AppID = ch.WechatAppID
	cfg.Wechat.Secret = ch.WechatSecret

	// Platform-specific fields.
	switch ch.Platform {
	case model.ScopeArticle:
		cfg.Wechat.Article.Author = ch.Author
		cfg.Wechat.Article.Style = ch.Style
		cfg.Wechat.Article.Theme = ch.Theme
	case model.ScopeXls:
		cfg.Wechat.Xls.Style = ch.Style
	case model.ScopeRednote:
		if cfg.Rednote == nil {
			cfg.Rednote = &config.RednoteConfig{}
		}
		cfg.Rednote.Style = ch.Style
	}

	// Apply global image API config from server config.
	if imageAPICfg != nil {
		if imageAPICfg.Cover != nil {
			switch ch.Platform {
			case model.ScopeArticle:
				cfg.Wechat.Article.Cover.Image = *imageAPICfg.Cover
			case model.ScopeXls:
				cfg.Wechat.Xls.Cover.Image = *imageAPICfg.Cover
			case model.ScopeRednote:
				if cfg.Rednote == nil {
					cfg.Rednote = &config.RednoteConfig{}
				}
				cfg.Rednote.Cover.Image = *imageAPICfg.Cover
			}
		}
		if imageAPICfg.Content != nil {
			switch ch.Platform {
			case model.ScopeArticle:
				cfg.Wechat.Article.Content.Image = *imageAPICfg.Content
			case model.ScopeXls:
				cfg.Wechat.Xls.Content.Image = *imageAPICfg.Content
			case model.ScopeRednote:
				if cfg.Rednote == nil {
					cfg.Rednote = &config.RednoteConfig{}
				}
				cfg.Rednote.Content.Image = *imageAPICfg.Content
			}
		}
	}

	return cfg, nil
}

// toZapLogger creates a zap.Logger from a zerolog.Logger.
func toZapLogger(zlog *zerolog.Logger) *zap.Logger {
	// Use zap.NewNop as base; the zerolog logger is the real logger.
	// The app packages require zap.Logger so we provide a minimal wrapper.
	return zap.NewNop()
}

// getImageAPI returns the appropriate ImageAPI config for the given scope.
func getImageAPI(cfg *config.Config, scope string) *config.ImageAPI {
	switch scope {
	case model.ScopeArticle:
		return &cfg.Wechat.Article.Content.Image
	case model.ScopeXls:
		return &cfg.Wechat.Xls.Content.Image
	case model.ScopeRednote:
		if cfg.Rednote != nil {
			return &cfg.Rednote.Content.Image
		}
		return &cfg.Wechat.Xls.Content.Image
	default:
		return &cfg.Wechat.Article.Content.Image
	}
}

// getCoverImageAPI returns the cover ImageAPI config for the given scope.
func getCoverImageAPI(cfg *config.Config, scope string) *config.ImageAPI {
	switch scope {
	case model.ScopeArticle:
		return &cfg.Wechat.Article.Cover.Image
	case model.ScopeXls:
		return &cfg.Wechat.Xls.Cover.Image
	case model.ScopeRednote:
		if cfg.Rednote != nil {
			return &cfg.Rednote.Cover.Image
		}
		return &cfg.Wechat.Xls.Cover.Image
	default:
		return &cfg.Wechat.Article.Cover.Image
	}
}

// toolEnv holds the shared environment passed to tool-creation helpers.
type toolEnv struct {
	workDir     string
	cfg         *config.Config
	scope       string
	apiCfg      *config.ImageAPI
	coverApiCfg *config.ImageAPI
	zapLog      *zap.Logger
}

// CreateMCPTools creates an SDK MCP server with tools that wrap the app/ packages.
func CreateMCPTools(workDir string, channel *model.Channel, imageAPICfg *srvconfig.ImageAPIConfig, logger *zerolog.Logger) (*claudecode.McpSdkServerConfig, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is required")
	}

	cfg, err := BuildAppConfig(channel, imageAPICfg)
	if err != nil {
		return nil, fmt.Errorf("build app config: %w", err)
	}

	scope := channel.Platform
	env := &toolEnv{
		workDir:     workDir,
		cfg:         cfg,
		scope:       scope,
		apiCfg:      getImageAPI(cfg, scope),
		coverApiCfg: getCoverImageAPI(cfg, scope),
		zapLog:      toZapLogger(logger),
	}

	// Build tool groups.
	fileTools := createFileTools(env)
	draftTools := createDraftTools(env)

	// Register all tools.
	return claudecode.CreateSDKMcpServer(
		"anbanwriter",
		"1.0.0",
		append(fileTools, draftTools...)...,
	), nil
}
