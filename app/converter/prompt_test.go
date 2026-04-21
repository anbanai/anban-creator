package converter

import (
	"strings"
	"testing"
)

func TestParseMarkdownTitle_H1(t *testing.T) {
	title := ParseMarkdownTitle("# My Title\nSome content")
	if title != "My Title" {
		t.Errorf("got %q, want %q", title, "My Title")
	}
}

func TestParseMarkdownTitle_H2(t *testing.T) {
	title := ParseMarkdownTitle("## Subtitle\nContent")
	if title != "Subtitle" {
		t.Errorf("got %q, want %q", title, "Subtitle")
	}
}

func TestParseMarkdownTitle_NoHeading(t *testing.T) {
	// First non-empty, non-image, non-quote line is returned as title
	title := ParseMarkdownTitle("Just plain text\nNo heading")
	if title != "Just plain text" {
		t.Errorf("got %q, want %q", title, "Just plain text")
	}
}

func TestParseMarkdownTitle_Empty(t *testing.T) {
	title := ParseMarkdownTitle("")
	if title != "未命名文章" {
		t.Errorf("got %q, want %q", title, "未命名文章")
	}
}

func TestEstimateTokenCount(t *testing.T) {
	tests := []struct {
		name string
		text string
		min  int // minimum expected
		max  int // maximum expected
	}{
		{"empty", "", 0, 0},
		{"ascii", "hello world", 2, 4},
		{"chinese", "你好世界", 4, 4},
		{"mixed", "hello 你好", 3, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := EstimateTokenCount(tt.text)
			if count < tt.min || count > tt.max {
				t.Errorf("EstimateTokenCount(%q) = %d, want between %d and %d", tt.text, count, tt.min, tt.max)
			}
		})
	}
}

func TestValidatePromptContent_Safe(t *testing.T) {
	result := ValidatePromptContent("Convert this markdown to HTML with proper styling.")
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}

func TestValidatePromptContent_DangerousScript(t *testing.T) {
	result := ValidatePromptContent("Use <script>alert('xss')</script> in output")
	if result.Valid {
		t.Error("expected invalid for <script>")
	}
}

func TestValidatePromptContent_DangerousOnload(t *testing.T) {
	result := ValidatePromptContent("Add onload=alert() to body")
	if result.Valid {
		t.Error("expected invalid for onload=")
	}
}

func TestValidatePromptContent_JavascriptURL(t *testing.T) {
	result := ValidatePromptContent("Link to javascript:void(0)")
	if result.Valid {
		t.Error("expected invalid for javascript:")
	}
}

func TestPromptBuilder_AddTemplate(t *testing.T) {
	pb := NewPromptBuilder()
	err := pb.AddTemplate(&PromptTemplate{
		Name:        "test",
		Description: "Test template",
		Template:    "Hello {{NAME}}, welcome to {{PLACE}}!",
	})
	if err != nil {
		t.Fatalf("AddTemplate: %v", err)
	}

	templates := pb.ListTemplates()
	found := false
	for _, name := range templates {
		if name == "test" {
			found = true
		}
	}
	if !found {
		t.Error("template 'test' not found in list")
	}
}

func TestPromptBuilder_BuildPrompt(t *testing.T) {
	pb := NewPromptBuilder()
	pb.AddTemplate(&PromptTemplate{
		Name:     "greet",
		Template: "Hello {{NAME}}!",
	})

	result, err := pb.BuildPrompt("greet", map[string]string{"NAME": "World"})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if result != "Hello World!" {
		t.Errorf("got %q, want %q", result, "Hello World!")
	}
}

func TestPromptBuilder_BuildPrompt_MissingTemplate(t *testing.T) {
	pb := NewPromptBuilder()
	_, err := pb.BuildPrompt("nonexistent", nil)
	if err == nil {
		t.Error("expected error for missing template")
	}
}

func TestPromptBuilder_GetVariable(t *testing.T) {
	pb := NewPromptBuilder()
	vars := pb.ListVariables()
	if len(vars) == 0 {
		t.Error("expected built-in variables")
	}

	v, err := pb.GetVariable("{{THEME_NAME}}")
	if err != nil {
		t.Fatalf("GetVariable: %v", err)
	}
	if v.DefaultValue != "default" {
		t.Errorf("THEME_NAME default = %q, want %q", v.DefaultValue, "default")
	}
}

func TestBuildCustomAIPrompt(t *testing.T) {
	prompt := BuildCustomAIPrompt("My custom style")
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "My custom style") {
		t.Error("expected custom prompt to be included")
	}
}

func TestBuildCustomAIPrompt_Empty(t *testing.T) {
	prompt := BuildCustomAIPrompt("")
	if prompt != "" {
		t.Errorf("expected empty for empty input, got %q", prompt)
	}
}
