package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	maxTurns := map[string]int{
		"article": 100,
		"xls":     50,
		"rednote": 60,
	}
	tests := []struct {
		taskType string
		want     int
	}{
		{model.ScopeArticle, 100},
		{model.ScopeXls, 50},
		{model.ScopeRednote, 60},
		{"unknown", 40},
	}

	for _, tt := range tests {
		t.Run(tt.taskType, func(t *testing.T) {
			got := DefaultMaxTurns(tt.taskType, maxTurns)
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

func TestWriteMCPConfig(t *testing.T) {
	tmpDir := t.TempDir()

	wantURL := "http://host.docker.internal:18060"
	wantKey := "test-api-key-12345"

	if err := writeMCPConfig(tmpDir, wantURL, wantKey); err != nil {
		t.Fatalf("writeMCPConfig error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, ".claude", ".mcp.json"))
	if err != nil {
		t.Fatalf("failed to read .mcp.json: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse .mcp.json: %v", err)
	}

	servers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers not found or wrong type")
	}

	server, ok := servers["anbanwriter"].(map[string]any)
	if !ok {
		t.Fatal("abwriter server not found")
	}

	// Verify URL.
	if got := server["url"]; got != wantURL+"/mcp" {
		t.Errorf("url = %v, want %v", got, wantURL+"/mcp")
	}

	// Verify Authorization: Bearer header (NOT X-API-Key).
	headers, ok := server["headers"].(map[string]any)
	if !ok {
		t.Fatal("headers not found or wrong type")
	}

	authVal, ok := headers["Authorization"]
	if !ok {
		t.Error("Authorization header not found")
	}
	if authVal != "Bearer "+wantKey {
		t.Errorf("Authorization = %v, want %v", authVal, "Bearer "+wantKey)
	}

	// Verify X-API-Key is NOT present.
	if _, hasXAPIKey := headers["X-API-Key"]; hasXAPIKey {
		t.Error("X-API-Key header should not be present; use Authorization: Bearer instead")
	}
}

func TestCountMeaningfulFiles(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string)
		want    int
	}{
		{
			name:  "empty directory",
			setup: func(t *testing.T, dir string) {},
			want:  0,
		},
		{
			name: "only dotfiles",
			setup: func(t *testing.T, dir string) {
				os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("bin"), 0644)
				os.WriteFile(filepath.Join(dir, ".hidden"), []byte("hidden"), 0644)
			},
			want: 0,
		},
		{
			name: "only excluded dirs",
			setup: func(t *testing.T, dir string) {
				os.MkdirAll(filepath.Join(dir, ".anbanwriter"), 0755)
				os.MkdirAll(filepath.Join(dir, ".claude"), 0755)
				os.WriteFile(filepath.Join(dir, ".anbanwriter", "settings.json"), []byte("{}"), 0644)
				os.WriteFile(filepath.Join(dir, ".claude", ".mcp.json"), []byte("{}"), 0644)
			},
			want: 0,
		},
		{
			name: "single file",
			setup: func(t *testing.T, dir string) {
				os.WriteFile(filepath.Join(dir, "article.html"), []byte("<p>hi</p>"), 0644)
			},
			want: 1,
		},
		{
			name: "nested files",
			setup: func(t *testing.T, dir string) {
				os.MkdirAll(filepath.Join(dir, "output", "images"), 0755)
				os.WriteFile(filepath.Join(dir, "output", "article.md"), []byte("# Hi"), 0644)
				os.WriteFile(filepath.Join(dir, "output", "images", "cover.png"), []byte("pngdata"), 0644)
			},
			want: 2,
		},
		{
			name: "mixed with excluded",
			setup: func(t *testing.T, dir string) {
				os.MkdirAll(filepath.Join(dir, ".anbanwriter"), 0755)
				os.WriteFile(filepath.Join(dir, ".anbanwriter", "settings.json"), []byte("{}"), 0644)
				os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>hi</p>"), 0644)
				os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("bin"), 0644)
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)
			got := CountMeaningfulFiles(dir)
			if got != tt.want {
				t.Errorf("CountMeaningfulFiles = %d, want %d", got, tt.want)
			}
		})
	}
}
