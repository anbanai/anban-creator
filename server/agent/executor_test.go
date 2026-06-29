package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/resolver"
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
		name         string
		ch           *model.Project
		wantErr      bool
		skipRefImage bool
		check        func(t *testing.T, cfg map[string]any)
	}{
		{
			name: "basic article config",
			ch: &model.Project{
				Platform:    model.ScopeArticle,
				Name:        "Test Account",
				Keywords:    "写作,效率",
				Positioning: "个人成长",
				Config: model.ProjectConfig{
					WechatAppID:  "test_appid",
					WechatSecret: "test_secret",
				},
				Author:      "TestAuthor",
				VisualStyle: "dan-koe",
				Theme:       "default",
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
			name: "seednote config with image API",
			ch: &model.Project{
				Platform:    model.ScopeSeednote,
				Name:        "SeedNote Account",
				VisualStyle: "cute-doodle",
			},
			wantErr: false,
			check: func(t *testing.T, cfg map[string]any) {
				if cfg["name"] != "SeedNote Account" {
					t.Errorf("name = %v, want SeedNote Account", cfg["name"])
				}
			},
		},
		{
			name: "skip_reference_image omits refer path",
			ch: &model.Project{
				Platform:          model.ScopeSeednote,
				Name:              "SkipRef Account",
				ReferenceImageURL: "http://example.com/ref.png",
			},
			wantErr:      false,
			skipRefImage: true,
			check: func(t *testing.T, cfg map[string]any) {
				sn := cfg["seednote"].(map[string]any)
				cover := sn["cover"].(map[string]any)
				img := cover["image"].(map[string]any)
				if img["refer"] != nil {
					t.Errorf("refer should be nil when skipRefImage=true, got %v", img["refer"])
				}
			},
		},
		{
			name: "reference_image sets refer path when not skipped",
			ch: &model.Project{
				Platform:          model.ScopeSeednote,
				Name:              "WithRef Account",
				ReferenceImageURL: "http://example.com/ref.png",
			},
			wantErr:      false,
			skipRefImage: false,
			check: func(t *testing.T, cfg map[string]any) {
				sn := cfg["seednote"].(map[string]any)
				cover := sn["cover"].(map[string]any)
				img := cover["image"].(map[string]any)
				if img["refer"] == nil || img["refer"] == "" {
					t.Errorf("refer should be set when skipRefImage=false and ReferenceImageURL is set")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := BuildAppConfig(tt.ch, resolver.ResolveStyle(tt.ch, nil), nil, "", tt.skipRefImage, "")
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
			name:     "seednote defaults",
			platform: model.ScopeSeednote,
			imageAPICfg: &srvconfig.ImageAPIConfig{
				Sizes: srvconfig.SizesConfig{
					SeednoteCover:   "3:4",
					SeednoteContent: "3:4",
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
			ch := &model.Project{
				Platform: tt.platform,
				Name:     "Test",
			}
			cfg, err := BuildAppConfig(ch, resolver.ResolveStyle(ch, nil), tt.imageAPICfg, "", false, "")
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
	case model.ScopeSeednote:
		if cfg.Seednote != nil {
			return cfg.Seednote.Cover.Image.Size, cfg.Seednote.Content.Image.Size
		}
	}
	return "", ""
}

func TestBuildAppConfig_ImageRatioOverride(t *testing.T) {
	yamlCfg := &srvconfig.ImageAPIConfig{
		Sizes: srvconfig.SizesConfig{
			ArticleCover:    "16:9",
			ArticleContent:  "16:9",
			SeednoteCover:   "3:4",
			SeednoteContent: "3:4",
		},
	}

	tests := []struct {
		name            string
		project         *model.Project
		imageAPICfg     *srvconfig.ImageAPIConfig
		taskImageRatio  string
		wantCoverSize   string
		wantContentSize string
	}{
		{
			name: "both empty falls back to YAML defaults",
			project: &model.Project{
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
			name: "project ratio overrides YAML defaults",
			project: &model.Project{
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
			name: "task ratio overrides project ratio",
			project: &model.Project{
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
			name: "task ratio without project ratio overrides YAML",
			project: &model.Project{
				Platform:   model.ScopeSeednote,
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
			cfg, err := BuildAppConfig(tt.project, resolver.ResolveStyle(tt.project, nil), tt.imageAPICfg, tt.taskImageRatio, false, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			coverSize, contentSize := getPlatformSizes(cfg, tt.project.Platform)
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
		{model.ScopeSeednote, "seednote"},
		{model.ScopeEcommerce, "ecommerce"},
		{"unknown", "seednote"},
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
		"article":  100,
		"seednote": 60,
	}
	tests := []struct {
		taskType string
		want     int
	}{
		{model.ScopeArticle, 100},
		{model.ScopeSeednote, 60},
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
			name: "seednote archived files",
			setup: func(t *testing.T, dir string) {
				archiveDir := filepath.Join(dir, "output", "seednote", "测试标题")
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
		LastToolErrorTool: "generate_image",
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
	if got["last_tool_error_tool"] != "generate_image" {
		t.Fatalf("last_tool_error_tool = %v, want generate_image", got["last_tool_error_tool"])
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
			if got := CompactToolResultContent(tt.content); got != tt.want {
				t.Fatalf("CompactToolResultContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildUserPrompt(t *testing.T) {
	tests := []struct {
		name         string
		taskType     string
		topic        string
		agentName    string
		wantContains []string
		wantAbsence  []string
	}{
		{
			name:         "seednote with topic references agent",
			taskType:     "seednote",
			topic:        "春季穿搭",
			agentName:    "seednote",
			wantContains: []string{"Use the seednote agent", "春季穿搭"},
			// P2: visual style must NEVER enter the prompt (it is read via MCP
			// get_project_profile). This absence guard is a regression fence.
			wantAbsence: []string{"视觉风格要求"},
		},
		{
			name:         "article with topic references agent",
			taskType:     "article",
			topic:        "时间管理技巧",
			agentName:    "wechatarticle",
			wantContains: []string{"Use the wechatarticle agent", "时间管理技巧"},
			wantAbsence:  []string{"视觉风格要求"},
		},
		{
			name:         "unknown task type defaults to seednote agent",
			taskType:     "other",
			topic:        "随便写写",
			agentName:    "seednote",
			wantContains: []string{"Use the seednote agent", "随便写写"},
		},
		{
			name:         "no topic triggers autonomous mode",
			taskType:     "seednote",
			topic:        "",
			agentName:    "seednote",
			wantContains: []string{"Use the seednote agent", "research and create content"},
		},
		{
			name:         "empty agent name still produces prompt",
			taskType:     "seednote",
			topic:        "test topic",
			agentName:    "",
			wantContains: []string{"Use the  agent", "test topic"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildUserPrompt(UserPromptParams{
				TaskType:  tt.taskType,
				Topic:     tt.topic,
				AgentName: tt.agentName,
			})
			for _, sub := range tt.wantContains {
				if !strings.Contains(got, sub) {
					t.Errorf("BuildUserPrompt() = %q, want to contain %q", got, sub)
				}
			}
			for _, sub := range tt.wantAbsence {
				if strings.Contains(got, sub) {
					t.Errorf("BuildUserPrompt() = %q, should NOT contain %q", got, sub)
				}
			}
			if strings.Contains(got, "video") || strings.Contains(got, "Merge the generated images") {
				t.Errorf("BuildUserPrompt() = %q, should not mention video generation", got)
			}
		})
	}
}

func TestBuildUserPrompt_TopicPreClaimWording(t *testing.T) {
	// The topic-pool anti-double-consume invariant depends on this EXACT
	// wording. When the server pre-claims a topic it injects it here, and the
	// research skills pattern-match on "create content about:" to detect a
	// pre-claimed topic and skip their own claim_topic call. If this phrase
	// drifts, the skills would re-claim on every run. (Server-side
	// ClaimForTask idempotency on task_id still prevents true double-consume,
	// but the skill behavior would be wrong — pin the substring here too.)
	for _, agent := range []string{"seednote", "wechatarticle"} {
		got := BuildUserPrompt(UserPromptParams{TaskType: "seednote", Topic: "X", AgentName: agent})
		if !strings.Contains(got, "create content about: X") {
			t.Errorf("agent %q: prompt %q must contain the pre-claim marker \"create content about: X\"", agent, got)
		}
	}
}

func TestBuildUserPrompt_Goal(t *testing.T) {
	// Empty goal — no /goal prefix.
	got := BuildUserPrompt(UserPromptParams{TaskType: "seednote", Topic: "春季穿搭", AgentName: "seednote"})
	if strings.Contains(got, "/goal ") {
		t.Errorf("empty goal should not include /goal prefix; got %q", got)
	}

	// Non-empty goal — /goal prefix appears with the condition, followed by the base prompt.
	goal := "文章字数不少于 1000 字"
	got = BuildUserPrompt(UserPromptParams{TaskType: "seednote", Topic: "春季穿搭", AgentName: "seednote", Goal: goal})
	if !strings.HasPrefix(got, "/goal "+goal) {
		t.Errorf("BuildUserPrompt with goal should start with %q; got %q", "/goal "+goal, got[:min(len(got), 80)])
	}
	// Base prompt must still be present after the goal line.
	if !strings.Contains(got, "春季穿搭") {
		t.Errorf("BuildUserPrompt with goal lost the base prompt topic; got %q", got)
	}

	// Whitespace-only goal is treated as empty.
	got = BuildUserPrompt(UserPromptParams{TaskType: "seednote", Topic: "春季穿搭", AgentName: "seednote", Goal: "   \n\t  "})
	if strings.Contains(got, "/goal ") {
		t.Errorf("whitespace-only goal should not include /goal prefix; got %q", got)
	}

	// Surrounding whitespace is trimmed.
	got = BuildUserPrompt(UserPromptParams{TaskType: "seednote", Topic: "春季穿搭", AgentName: "seednote", Goal: "  含关键词 ABC  "})
	wantPrefix := "/goal 含关键词 ABC"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("goal should be trimmed; want prefix %q, got %q", wantPrefix, got[:min(len(got), 80)])
	}

	// Multi-line goal is flattened to a single /goal line (Claude Code's slash
	// parser only registers the first line as the condition).
	got = BuildUserPrompt(UserPromptParams{TaskType: "seednote", Topic: "春季穿搭", AgentName: "seednote", Goal: "字数 ≥ 1000\n包含 3 个案例\n带封面图"})
	firstLine := got
	if idx := strings.Index(got, "\n"); idx >= 0 {
		firstLine = got[:idx]
	}
	if !strings.HasPrefix(firstLine, "/goal ") {
		t.Errorf("first line should start with /goal; got %q", firstLine)
	}
	for _, frag := range []string{"字数 ≥ 1000", "包含 3 个案例", "带封面图"} {
		if !strings.Contains(firstLine, frag) {
			t.Errorf("multi-line goal fragment %q missing from /goal line %q", frag, firstLine)
		}
	}
	if strings.Contains(firstLine, "\n") {
		t.Errorf("goal line must be single-line; got %q", firstLine)
	}
}

func TestBuildUserPrompt_TaskContext(t *testing.T) {
	// Both task_id and project_id present.
	got := BuildUserPrompt(UserPromptParams{TaskType: "seednote", Topic: "春季穿搭", AgentName: "seednote", TaskID: "task-123", ProjectID: "chan-abc"})
	if !strings.Contains(got, "本任务上下文：") {
		t.Errorf("missing 任务上下文 line; got %q", got)
	}
	if !strings.Contains(got, "task_id=task-123") {
		t.Errorf("missing task_id; got %q", got)
	}
	if !strings.Contains(got, "project_id=chan-abc") {
		t.Errorf("missing project_id; got %q", got)
	}

	// Only task_id.
	got = BuildUserPrompt(UserPromptParams{TaskType: "seednote", Topic: "春季穿搭", AgentName: "seednote", TaskID: "task-123"})
	if !strings.Contains(got, "本任务上下文：task_id=task-123") {
		t.Errorf("expected only task_id in context line; got %q", got)
	}
	if strings.Contains(got, "project_id=") {
		t.Errorf("project_id= should be absent when empty; got %q", got)
	}

	// Only project_id.
	got = BuildUserPrompt(UserPromptParams{TaskType: "seednote", Topic: "春季穿搭", AgentName: "seednote", ProjectID: "chan-abc"})
	if !strings.Contains(got, "本任务上下文：project_id=chan-abc") {
		t.Errorf("expected only project_id in context line; got %q", got)
	}

	// Both empty — line omitted entirely.
	got = BuildUserPrompt(UserPromptParams{TaskType: "seednote", Topic: "春季穿搭", AgentName: "seednote"})
	if strings.Contains(got, "本任务上下文") {
		t.Errorf("context line should be omitted when both IDs empty; got %q", got)
	}
}

func TestBuildUserPrompt_SeednoteImageComposition(t *testing.T) {
	cases := []struct {
		name         string
		hasContent   bool
		hasTail      bool
		wantContains []string
		wantAbsence  []string
	}{
		{
			name:         "cover only",
			hasContent:   false,
			hasTail:      false,
			wantContains: []string{"图片构成要求", "封面图（cover.png）", "共 1 张", "写入此数字", "禁止生成尾图"},
			wantAbsence:  []string{"image_01.png", "tail.png", "1~3 张内容图"},
		},
		{
			name:         "cover and content (default)",
			hasContent:   true,
			hasTail:      false,
			wantContains: []string{"图片构成要求", "封面图（cover.png）", "1~3 张内容图", "写入实际生成的总张数", "内容图张数由信息点分组决定", "最多 3 张", "禁止生成尾图"},
			wantAbsence:  []string{"tail.png", "共 2 张"},
		},
		{
			name:         "cover and tail",
			hasContent:   false,
			hasTail:      true,
			wantContains: []string{"图片构成要求", "封面图（cover.png）", "尾图（tail.png）", "共 2 张", "写入此数字"},
			wantAbsence:  []string{"image_01.png", "1~3 张内容图", "禁止生成尾图"},
		},
		{
			name:         "cover content and tail",
			hasContent:   true,
			hasTail:      true,
			wantContains: []string{"图片构成要求", "封面图（cover.png）", "1~3 张内容图", "尾图（tail.png）", "写入实际生成的总张数", "内容图张数由信息点分组决定", "最多 3 张"},
			wantAbsence:  []string{"共 3 张", "禁止生成尾图"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildUserPrompt(UserPromptParams{
				TaskType:        "seednote",
				Topic:           "春季穿搭",
				AgentName:       "seednote",
				HasContentImage: tc.hasContent,
				HasTailImage:    tc.hasTail,
			})
			for _, sub := range tc.wantContains {
				if !strings.Contains(got, sub) {
					t.Errorf("missing %q in prompt: %q", sub, got)
				}
			}
			for _, sub := range tc.wantAbsence {
				if strings.Contains(got, sub) {
					t.Errorf("%q should be absent; got %q", sub, got)
				}
			}
			if !strings.Contains(got, "image-plan.md 必须在「计划图片数量」字段写入") {
				t.Errorf("missing image-plan.md hint; got %q", got)
			}
		})
	}

	// A default article task (both image toggles on) must NOT get the seednote
	// image-composition directive, nor any article directive (both-on = no-op).
	articleGot := BuildUserPrompt(UserPromptParams{
		TaskType:                 "article",
		Topic:                    "时间管理",
		AgentName:                "wechatarticle",
		HasContentImage:          true,
		HasTailImage:             true,
		ArticleWithCover:         true,
		ArticleWithContentImages: true,
	})
	for _, sub := range []string{"图片构成要求", "图片生成要求", "image_01.png", "tail.png"} {
		if strings.Contains(articleGot, sub) {
			t.Errorf("default article task must not get image directive %q; got %q", sub, articleGot)
		}
	}
}

func TestBuildUserPrompt_ArticleImageComposition(t *testing.T) {
	cases := []struct {
		name         string
		withCover    bool
		withContent  bool
		wantContains []string
		wantAbsence  []string
	}{
		{
			name:        "both on (default = no directive)",
			withCover:   true,
			withContent: true,
			// Byte-identical to legacy prompt: no image directive at all.
			wantAbsence: []string{"图片生成要求", "禁止生成", "纯文字文章", "图片构成要求", "image_01.png", "tail.png"},
		},
		{
			name:         "cover only (content off)",
			withCover:    true,
			withContent:  false,
			wantContains: []string{"图片生成要求", "仅生成封面", "禁止生成任何正文配图", "image_count.min 不再生效"},
			wantAbsence:  []string{"禁止生成封面", "纯文字文章", "图片构成要求", "image_01.png", "tail.png"},
		},
		{
			name:         "content only (cover off)",
			withCover:    false,
			withContent:  true,
			wantContains: []string{"图片生成要求", "禁止生成封面", "正常生成正文配图", "不带 thumb_media_id"},
			wantAbsence:  []string{"禁止生成任何正文配图", "纯文字文章", "仅生成封面", "图片构成要求", "image_01.png", "tail.png"},
		},
		{
			name:         "neither (pure text)",
			withCover:    false,
			withContent:  false,
			wantContains: []string{"图片生成要求", "纯文字文章", "禁止生成任何图片", "不带 thumb_media_id"},
			wantAbsence:  []string{"仅生成封面", "正常生成正文配图", "图片构成要求", "image_01.png", "tail.png"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildUserPrompt(UserPromptParams{
				TaskType:                 "article",
				Topic:                    "时间管理",
				AgentName:                "wechatarticle",
				ArticleWithCover:         tc.withCover,
				ArticleWithContentImages: tc.withContent,
			})
			for _, sub := range tc.wantContains {
				if !strings.Contains(got, sub) {
					t.Errorf("missing %q in prompt: %q", sub, got)
				}
			}
			for _, sub := range tc.wantAbsence {
				if strings.Contains(got, sub) {
					t.Errorf("%q should be absent; got %q", sub, got)
				}
			}
		})
	}
}

func TestLoadAgentDefinition(t *testing.T) {
	t.Run("valid agent file", func(t *testing.T) {
		dir := t.TempDir()
		agentsDir := filepath.Join(dir, "agents")
		if err := os.MkdirAll(agentsDir, 0755); err != nil {
			t.Fatal(err)
		}
		agentFile := "---\nname: testagent\ndescription: Test agent for unit testing\ntools:\n  - Read\n  - Write\n  - Bash\nmodel: inherit\n---\n\n# Test Agent\n\nYou are a test agent.\n"
		if err := os.WriteFile(filepath.Join(agentsDir, "testagent.md"), []byte(agentFile), 0644); err != nil {
			t.Fatal(err)
		}
		def, err := loadAgentDefinition(dir, "testagent")
		if err != nil {
			t.Fatalf("loadAgentDefinition() error = %v", err)
		}
		if def.Description != "Test agent for unit testing" {
			t.Errorf("Description = %q, want %q", def.Description, "Test agent for unit testing")
		}
		if len(def.Tools) != 3 {
			t.Errorf("Tools count = %d, want 3", len(def.Tools))
		}
		if string(def.Model) != "inherit" {
			t.Errorf("Model = %q, want %q", def.Model, "inherit")
		}
		if !strings.Contains(def.Prompt, "You are a test agent") {
			t.Errorf("Prompt does not contain expected content")
		}
	})

	t.Run("model mapping", func(t *testing.T) {
		tests := []struct {
			modelYAML string
			want      string
		}{
			{"sonnet", "sonnet"},
			{"haiku", "haiku"},
			{"opus", "opus"},
			{"inherit", "inherit"},
			{"", "inherit"},
			{"custom-model", "inherit"},
		}
		for _, tt := range tests {
			t.Run(tt.modelYAML, func(t *testing.T) {
				dir := t.TempDir()
				agentsDir := filepath.Join(dir, "agents")
				os.MkdirAll(agentsDir, 0755)
				yaml := "---\nname: m\ndescription: d\ntools: []\nmodel: " + tt.modelYAML + "\n---\n\n# P\n"
				if err := os.WriteFile(filepath.Join(agentsDir, "m.md"), []byte(yaml), 0644); err != nil {
					t.Fatal(err)
				}
				def, err := loadAgentDefinition(dir, "m")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if string(def.Model) != tt.want {
					t.Errorf("Model = %q, want %q", def.Model, tt.want)
				}
			})
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := loadAgentDefinition("/nonexistent", "missing")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("missing frontmatter", func(t *testing.T) {
		dir := t.TempDir()
		agentsDir := filepath.Join(dir, "agents")
		os.MkdirAll(agentsDir, 0755)
		os.WriteFile(filepath.Join(agentsDir, "bad.md"), []byte("no frontmatter"), 0644)
		_, err := loadAgentDefinition(dir, "bad")
		if err == nil {
			t.Fatal("expected error for missing frontmatter")
		}
	})

	t.Run("real agent files load", func(t *testing.T) {
		pluginDir := filepath.Join("..", "..", "claudecode")
		entries, err := os.ReadDir(filepath.Join(pluginDir, "agents"))
		if err != nil {
			t.Skip("claudecode submodule not available")
		}
		for _, a := range entries {
			name := a.Name()
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			agentName := strings.TrimSuffix(name, ".md")
			t.Run(agentName, func(t *testing.T) {
				def, err := loadAgentDefinition(pluginDir, agentName)
				if err != nil {
					t.Fatalf("load %s: %v", agentName, err)
				}
				if def.Description == "" {
					t.Error("description is empty")
				}
				if def.Prompt == "" {
					t.Error("prompt is empty")
				}
				if agentName == "designer" {
					if len(def.Tools) != 0 {
						t.Error("designer must omit tools to inherit MCP tools")
					}
				} else if len(def.Tools) != 0 {
					// All MCP-needing agents omit `tools:` to inherit the full mcp__anban__*
					// toolset (Claude Code treats `tools` as an allowlist — see
					// claudecode/CLAUDE.md). designer is asserted above; the others must
					// also omit. The legacy "must specify tools" policy was dropped when
					// agents migrated to MCP-tool inheritance.
					t.Errorf("%s agent should omit tools frontmatter to inherit MCP tools; got %v", agentName, def.Tools)
				}
			})
		}
	})
}
