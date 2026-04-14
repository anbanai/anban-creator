package agent

import (
	"encoding/json"
	"testing"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
)

func TestBuildAppConfig(t *testing.T) {
	tests := []struct {
		name    string
		ch      *model.Channel
		wantErr bool
		check   func(t *testing.T, cfg map[string]any)
	}{
		{
			name: "basic article config",
			ch: &model.Channel{
				Platform:    model.ScopeArticle,
				Name:        "Test Account",
				Keywords:    "写作,效率",
				Positioning: "个人成长",
				Config: model.ChannelConfig{
					WechatAppID:  "test_appid",
					WechatSecret: "test_secret",
				},
				Author: "TestAuthor",
				Style:  "dan-koe",
				Theme:  "default",
			},
			wantErr: false,
			check: func(t *testing.T, cfg map[string]any) {
				if cfg["name"] != "Test Account" {
					t.Errorf("name = %v, want Test Account", cfg["name"])
				}
				kwRaw := cfg["keywords"].([]any)
				if len(kwRaw) != 2 || kwRaw[0] != "写作" || kwRaw[1] != "效率" {
					t.Errorf("keywords = %v, want [写作 效率]", kwRaw)
				}
			},
		},
		{
			name: "rednote config with image API",
			ch: &model.Channel{
				Platform: model.ScopeRednote,
				Name:     "RedNote Account",
				Style:    "cute-doodle",
			},
			wantErr: false,
			check: func(t *testing.T, cfg map[string]any) {
				if cfg["name"] != "RedNote Account" {
					t.Errorf("name = %v, want RedNote Account", cfg["name"])
				}
			},
		},
		{
			name: "empty keywords",
			ch: &model.Channel{
				Platform: model.ScopeXls,
				Name:     "XLS Account",
			},
			wantErr: false,
			check: func(t *testing.T, cfg map[string]any) {
				if cfg["name"] != "XLS Account" {
					t.Errorf("name = %v, want XLS Account", cfg["name"])
				}
				// Empty keywords should be nil or empty in JSON.
				kwRaw, ok := cfg["keywords"].([]any)
				if ok && len(kwRaw) != 0 {
					t.Errorf("keywords = %v, want empty", kwRaw)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := BuildAppConfig(tt.ch, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Serialize to map for easier field checking.
			data, _ := json.Marshal(cfg)
			var m map[string]any
			json.Unmarshal(data, &m)

			if tt.check != nil {
				tt.check(t, m)
			}
		})
	}
}

func TestBuildAppConfig_PlatformSizes(t *testing.T) {
	tests := []struct {
		name            string
		platform        string
		imageAPICfg     *srvconfig.ImageAPIConfig
		wantCoverSize   string
		wantContentSize string
	}{
		{
			name:     "article defaults",
			platform: model.ScopeArticle,
			imageAPICfg: &srvconfig.ImageAPIConfig{
				Sizes: srvconfig.SizesConfig{
					ArticleCover:   "16:9",
					ArticleContent: "16:9",
				},
			},
			wantCoverSize:   "16:9",
			wantContentSize: "16:9",
		},
		{
			name:     "xls defaults",
			platform: model.ScopeXls,
			imageAPICfg: &srvconfig.ImageAPIConfig{
				Sizes: srvconfig.SizesConfig{
					XlsCover:   "3:4",
					XlsContent: "3:4",
				},
			},
			wantCoverSize:   "3:4",
			wantContentSize: "3:4",
		},
		{
			name:     "rednote defaults",
			platform: model.ScopeRednote,
			imageAPICfg: &srvconfig.ImageAPIConfig{
				Sizes: srvconfig.SizesConfig{
					RednoteCover:   "3:4",
					RednoteContent: "3:4",
				},
			},
			wantCoverSize:   "3:4",
			wantContentSize: "3:4",
		},
		{
			name:     "explicit cover size overrides platform default",
			platform: model.ScopeArticle,
			imageAPICfg: &srvconfig.ImageAPIConfig{
				Cover: &appconfig.ImageAPI{
					Provider: "openrouter",
					Key:      "test-key",
					Size:     "9:16",
				},
				Sizes: srvconfig.SizesConfig{
					ArticleCover:   "16:9",
					ArticleContent: "16:9",
				},
			},
			wantCoverSize:   "9:16",
			wantContentSize: "16:9",
		},
		{
			name:     "nil imageAPICfg does not panic",
			platform: model.ScopeArticle,
			imageAPICfg:     nil,
			wantCoverSize:   "",
			wantContentSize: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &model.Channel{
				Platform: tt.platform,
				Name:     "Test",
			}
			cfg, err := BuildAppConfig(ch, tt.imageAPICfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			coverSize, contentSize := getPlatformSizes(cfg, tt.platform)
			if coverSize != tt.wantCoverSize {
				t.Errorf("cover size = %q, want %q", coverSize, tt.wantCoverSize)
			}
			if contentSize != tt.wantContentSize {
				t.Errorf("content size = %q, want %q", contentSize, tt.wantContentSize)
			}
		})
	}
}

// getPlatformSizes extracts cover and content sizes for a given platform.
func getPlatformSizes(cfg *appconfig.Config, platform string) (cover, content string) {
	switch platform {
	case model.ScopeArticle:
		return cfg.Wechat.Article.Cover.Image.Size, cfg.Wechat.Article.Content.Image.Size
	case model.ScopeXls:
		return cfg.Wechat.Xls.Cover.Image.Size, cfg.Wechat.Xls.Content.Image.Size
	case model.ScopeRednote:
		if cfg.Rednote != nil {
			return cfg.Rednote.Cover.Image.Size, cfg.Rednote.Content.Image.Size
		}
	}
	return "", ""
}

func TestTaskTypeToAgent(t *testing.T) {
	tests := []struct {
		taskType string
		want     string
	}{
		{model.ScopeArticle, "wechatarticle"},
		{model.ScopeXls, "wechatxls"},
		{model.ScopeRednote, "rednote"},
		{"unknown", "rednote"},
	}

	for _, tt := range tests {
		t.Run(tt.taskType, func(t *testing.T) {
			got := taskTypeToAgent(tt.taskType)
			if got != tt.want {
				t.Errorf("taskTypeToAgent(%q) = %q, want %q", tt.taskType, got, tt.want)
			}
		})
	}
}

func TestDefaultMaxTurns(t *testing.T) {
	tests := []struct {
		taskType string
		want     int
	}{
		{model.ScopeArticle, 50},
		{model.ScopeXls, 25},
		{model.ScopeRednote, 20},
		{"unknown", 20},
	}

	for _, tt := range tests {
		t.Run(tt.taskType, func(t *testing.T) {
			got := DefaultMaxTurns(tt.taskType)
			if got != tt.want {
				t.Errorf("DefaultMaxTurns(%q) = %d, want %d", tt.taskType, got, tt.want)
			}
		})
	}
}

func TestMarshalResultJSON(t *testing.T) {
	r := &ExecutionResult{
		Success: true,
		WorkDir: "/tmp/test",
		LogText: "test output",
	}
	data, err := MarshalResultJSON(r)
	if err != nil {
		t.Fatalf("MarshalResultJSON error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if parsed["success"] != true {
		t.Errorf("success = %v, want true", parsed["success"])
	}
	if parsed["work_dir"] != "/tmp/test" {
		t.Errorf("work_dir = %v, want /tmp/test", parsed["work_dir"])
	}
}
