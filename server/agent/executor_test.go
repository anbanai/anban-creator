package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
)

func TestFilterAgentEnvPreservesClaudeConfig(t *testing.T) {
	env := map[string]string{
		"ANTHROPIC_AUTH_TOKEN":                     "token",
		"ANTHROPIC_BASE_URL":                       "https://open.bigmodel.cn/api/anthropic",
		"ANTHROPIC_MODEL":                          "opusplan",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":            "glm-4.5-air",
		"ANTHROPIC_DEFAULT_SONNET_MODEL":           "glm-5-turbo",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":             "glm-5.1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	}

	got := filterAgentEnv(env)
	if !reflect.DeepEqual(got, env) {
		t.Fatalf("filterAgentEnv() = %#v, want %#v", got, env)
	}

	got["ANTHROPIC_MODEL"] = "changed"
	if env["ANTHROPIC_MODEL"] == "changed" {
		t.Fatal("filterAgentEnv returned the input map instead of a copy")
	}
}

func TestExecutionOptionsDoesNotExposeModelOverride(t *testing.T) {
	optsType := reflect.TypeOf(ExecutionOptions{})
	if _, ok := optsType.FieldByName("Model"); ok {
		t.Fatal("ExecutionOptions must not expose a per-task Claude model override")
	}
}

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
			cfg, err := BuildAppConfig(tt.ch, nil, "")
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
					Provider: "gemini",
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
			name:            "nil imageAPICfg does not panic",
			platform:        model.ScopeArticle,
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
			cfg, err := BuildAppConfig(ch, tt.imageAPICfg, "")
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

func TestBuildAppConfig_ImageRatioOverride(t *testing.T) {
	yamlCfg := &srvconfig.ImageAPIConfig{
		Sizes: srvconfig.SizesConfig{
			ArticleCover:   "16:9",
			ArticleContent: "16:9",
			RednoteCover:   "3:4",
			RednoteContent: "3:4",
		},
	}

	tests := []struct {
		name            string
		channel         *model.Channel
		imageAPICfg     *srvconfig.ImageAPIConfig
		taskImageRatio  string
		wantCoverSize   string
		wantContentSize string
	}{
		{
			name: "both empty falls back to YAML defaults",
			channel: &model.Channel{
				Platform:   model.ScopeArticle,
				Name:       "Test",
				ImageRatio: "",
			},
			imageAPICfg:     yamlCfg,
			taskImageRatio:  "",
			wantCoverSize:   "16:9",
			wantContentSize: "16:9",
		},
		{
			name: "channel ratio overrides YAML defaults",
			channel: &model.Channel{
				Platform:   model.ScopeArticle,
				Name:       "Test",
				ImageRatio: "1:1",
			},
			imageAPICfg:     yamlCfg,
			taskImageRatio:  "",
			wantCoverSize:   "1:1",
			wantContentSize: "1:1",
		},
		{
			name: "task ratio overrides channel ratio",
			channel: &model.Channel{
				Platform:   model.ScopeArticle,
				Name:       "Test",
				ImageRatio: "1:1",
			},
			imageAPICfg:     yamlCfg,
			taskImageRatio:  "4:3",
			wantCoverSize:   "4:3",
			wantContentSize: "4:3",
		},
		{
			name: "task ratio without channel ratio overrides YAML",
			channel: &model.Channel{
				Platform:   model.ScopeRednote,
				Name:       "Test",
				ImageRatio: "",
			},
			imageAPICfg:     yamlCfg,
			taskImageRatio:  "16:9",
			wantCoverSize:   "16:9",
			wantContentSize: "16:9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := BuildAppConfig(tt.channel, tt.imageAPICfg, tt.taskImageRatio)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			coverSize, contentSize := getPlatformSizes(cfg, tt.channel.Platform)
			if coverSize != tt.wantCoverSize {
				t.Errorf("cover size = %q, want %q", coverSize, tt.wantCoverSize)
			}
			if contentSize != tt.wantContentSize {
				t.Errorf("content size = %q, want %q", contentSize, tt.wantContentSize)
			}
		})
	}
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
			got := TaskTypeToAgent(tt.taskType)
			if got != tt.want {
				t.Errorf("TaskTypeToAgent(%q) = %q, want %q", tt.taskType, got, tt.want)
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

func TestCountMeaningfulFiles(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  int
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
			name: "rednote archived files",
			setup: func(t *testing.T, dir string) {
				archiveDir := filepath.Join(dir, "output", "rednote", "测试标题")
				os.MkdirAll(archiveDir, 0755)
				os.WriteFile(filepath.Join(archiveDir, "content.md"), []byte("# 测试标题"), 0644)
				os.WriteFile(filepath.Join(archiveDir, "image-plan.md"), []byte("# 图片内容规划"), 0644)
				os.WriteFile(filepath.Join(archiveDir, "cover.png"), []byte("pngdata"), 0644)
				os.WriteFile(filepath.Join(archiveDir, "tail.png"), []byte("pngdata"), 0644)
			},
			want: 4,
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

func TestExecutionResultSerializesToolErrors(t *testing.T) {
	result := &ExecutionResult{
		Success:           true,
		ToolUseCount:      3,
		ToolErrorCount:    1,
		LastToolErrorTool: "generate_images",
		LastToolError:     "image provider rejected model",
	}

	raw, err := MarshalResultJSON(result)
	if err != nil {
		t.Fatalf("MarshalResultJSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got["tool_error_count"] != float64(1) {
		t.Fatalf("tool_error_count = %v, want 1", got["tool_error_count"])
	}
	if got["last_tool_error_tool"] != "generate_images" {
		t.Fatalf("last_tool_error_tool = %v, want generate_images", got["last_tool_error_tool"])
	}
	if got["last_tool_error"] != "image provider rejected model" {
		t.Fatalf("last_tool_error = %v, want image provider rejected model", got["last_tool_error"])
	}
}

func TestCompactToolResultContent(t *testing.T) {
	tests := []struct {
		name    string
		content any
		want    string
	}{
		{
			name:    "string content",
			content: "  generate image failed\nbecause model is invalid  ",
			want:    "generate image failed because model is invalid",
		},
		{
			name: "text content array",
			content: []any{
				map[string]any{"type": "text", "text": "batch generate: provider rejected model"},
			},
			want: "batch generate: provider rejected model",
		},
		{
			name:    "nil content",
			content: nil,
			want:    "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compactToolResultContent(tt.content); got != tt.want {
				t.Fatalf("compactToolResultContent() = %q, want %q", got, tt.want)
			}
		})
	}
}
