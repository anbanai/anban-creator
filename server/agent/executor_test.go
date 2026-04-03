package agent

import (
	"encoding/json"
	"strings"
	"testing"

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
				Platform:     model.ScopeArticle,
				Name:         "Test Account",
				Keywords:     "写作,效率",
				Positioning:  "个人成长",
				WechatAppID:  "test_appid",
				WechatSecret: "test_secret",
				Author:       "TestAuthor",
				Style:        "dan-koe",
				Theme:        "default",
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
				ImageAPIConfig: `{
					"cover": {"provider": "gemini", "key": "test_key", "model": "gemini-3-pro-image-preview"},
					"content": {"provider": "gemini", "key": "test_key", "model": "gemini-3-pro-image-preview"}
				}`,
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
		{
			name: "invalid image_api_config JSON",
			ch: &model.Channel{
				Platform:      model.ScopeArticle,
				ImageAPIConfig: "invalid json",
			},
			wantErr: true,
			check:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := BuildAppConfig(tt.ch)
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

func TestGetSystemPrompt(t *testing.T) {
	ch := &model.Channel{
		Platform:    model.ScopeRednote,
		Name:        "Test Name",
		Keywords:    "写作,效率",
		Positioning: "个人成长",
	}

	tests := []struct {
		name string
		ch   *model.Channel
		want string
	}{
		{
			name: "rednote prompt",
			ch:   ch,
			want: "小红书图文全自动创作引擎",
		},
		{
			name: "article prompt",
			ch: &model.Channel{
				Platform:    model.ScopeArticle,
				Name:        "Article Name",
				Author:      "Test Author",
			},
			want: "微信公众号图文文章创作引擎",
		},
		{
			name: "xls prompt",
			ch: &model.Channel{
				Platform: model.ScopeXls,
				Name:     "XLS Name",
			},
			want: "微信公众号小绿书创作引擎",
		},
		{
			name: "unknown type",
			ch: &model.Channel{
				Platform: "unknown",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetSystemPrompt(tt.ch.Platform, tt.ch)
			if tt.want == "" && got != "" {
				t.Errorf("expected empty prompt, got non-empty")
			}
			if tt.want != "" {
				if got == "" {
					t.Fatalf("expected prompt containing %q, got empty", tt.want)
				}
				if !strings.Contains(got, tt.want) {
					t.Errorf("prompt does not contain %q", tt.want)
				}
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

func TestDefaultModel(t *testing.T) {
	got := DefaultModel()
	if got != "claude-sonnet-4-6" {
		t.Errorf("DefaultModel() = %q, want claude-sonnet-4-6", got)
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
