package converter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestThemeManager_LoadFromTempDir(t *testing.T) {
	dir := t.TempDir()
	themeYAML := `name: "Test Theme"
description: "A test theme"
version: "1.0"
colors:
  background: "#ffffff"
  text: "#333333"
prompt: "Convert to styled HTML"
`
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(themeYAML), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tm := NewThemeManager()
	tm.LoadTheme(filepath.Join(dir, "test.yaml"))

	theme, err := tm.GetTheme("Test Theme")
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if theme.Name != "Test Theme" {
		t.Errorf("Name = %q, want %q", theme.Name, "Test Theme")
	}
	if theme.Description != "A test theme" {
		t.Errorf("Description = %q", theme.Description)
	}
	if theme.Colors["background"] != "#ffffff" {
		t.Errorf("Colors[background] = %q", theme.Colors["background"])
	}
}

func TestThemeManager_LoadTheme_NoName(t *testing.T) {
	dir := t.TempDir()
	yaml := `description: "No name theme"`
	if err := os.WriteFile(filepath.Join(dir, "noname.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tm := NewThemeManager()
	err := tm.LoadTheme(filepath.Join(dir, "noname.yaml"))
	if err == nil {
		t.Error("expected error for theme without name")
	}
}

func TestThemeManager_GetThemeDescription(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: "MyTheme"
description: "Cool theme"`
	if err := os.WriteFile(filepath.Join(dir, "mytheme.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tm := NewThemeManager()
	tm.LoadTheme(filepath.Join(dir, "mytheme.yaml"))

	desc := tm.GetThemeDescription("MyTheme")
	if desc != "Cool theme" {
		t.Errorf("got %q, want %q", desc, "Cool theme")
	}
}

func TestThemeManager_GetThemeDescription_Unknown(t *testing.T) {
	tm := NewThemeManager()
	desc := tm.GetThemeDescription("nonexistent")
	if desc != "未知主题" {
		t.Errorf("got %q, want %q", desc, "未知主题")
	}
}

func TestThemeManager_GetAIPrompt(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: "prompted"
prompt: "Convert this {{MARKDOWN}} to HTML"`
	if err := os.WriteFile(filepath.Join(dir, "prompted.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tm := NewThemeManager()
	tm.LoadTheme(filepath.Join(dir, "prompted.yaml"))

	prompt, err := tm.GetAIPrompt("prompted")
	if err != nil {
		t.Fatalf("GetAIPrompt: %v", err)
	}
	if prompt != "Convert this {{MARKDOWN}} to HTML" {
		t.Errorf("prompt = %q", prompt)
	}
}

func TestThemeManager_GetThemeColors(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: "colored"
colors:
  primary: "#ff0000"
  secondary: "#00ff00"`
	if err := os.WriteFile(filepath.Join(dir, "colored.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tm := NewThemeManager()
	tm.LoadTheme(filepath.Join(dir, "colored.yaml"))

	colors, err := tm.GetThemeColors("colored")
	if err != nil {
		t.Fatalf("GetThemeColors: %v", err)
	}
	if colors["primary"] != "#ff0000" {
		t.Errorf("colors[primary] = %q", colors["primary"])
	}
}

func TestThemeManager_ReloadThemes(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: "reloadable"
description: "v1"`
	if err := os.WriteFile(filepath.Join(dir, "reloadable.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tm := NewThemeManager()
	tm.LoadTheme(filepath.Join(dir, "reloadable.yaml"))

	if len(tm.ListThemes()) != 1 {
		t.Errorf("expected 1 theme, got %d", len(tm.ListThemes()))
	}

	tm.ReloadThemes()
	// After reload with no themes dir, themes from LoadTheme are cleared
	// (depends on theme dir existence)
}

func TestBuildCustomAIPrompt_AppendsRules(t *testing.T) {
	custom := "Use dark theme colors"
	result := BuildCustomAIPrompt(custom)
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "Use dark theme colors") {
		t.Error("expected custom prompt to be included")
	}
}
