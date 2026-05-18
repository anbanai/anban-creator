package writer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStyleManager_LoadFromTempDir(t *testing.T) {
	// Create a temp writers directory with a test style.
	dir := t.TempDir()
	styleYAML := `
name: "Test Style"
english_name: "test-style"
category: "测试"
description: "A test style"
version: "1.0"
writing_prompt: "Write like this: {topic}"
`
	styleFile := filepath.Join(dir, "test-style.yaml")
	if err := os.WriteFile(styleFile, []byte(styleYAML), 0644); err != nil {
		t.Fatalf("write style file: %v", err)
	}

	sm := NewStyleManager()
	sm.writersDir = dir

	if err := sm.LoadStyles(); err != nil {
		t.Fatalf("LoadStyles: %v", err)
	}

	if count := sm.GetStyleCount(); count != 1 {
		t.Errorf("GetStyleCount() = %d, want 1", count)
	}

	style, err := sm.GetStyle("test-style")
	if err != nil {
		t.Fatalf("GetStyle(test-style): %v", err)
	}
	if style.Name != "Test Style" {
		t.Errorf("style.Name = %q, want %q", style.Name, "Test Style")
	}
	if style.Category != "测试" {
		t.Errorf("style.Category = %q, want %q", style.Category, "测试")
	}
}

func TestStyleManager_MissingEnglishName(t *testing.T) {
	dir := t.TempDir()
	badYAML := `
name: "No English Name"
category: "测试"
`
	styleFile := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(styleFile, []byte(badYAML), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sm := NewStyleManager()
	sm.writersDir = dir
	if err := sm.LoadStyles(); err != nil {
		t.Fatalf("LoadStyles: %v", err)
	}
	// Style with missing english_name should be skipped.
	if count := sm.GetStyleCount(); count != 0 {
		t.Errorf("GetStyleCount() = %d, want 0 (bad style skipped)", count)
	}
}

func TestStyleManager_StyleNotFound(t *testing.T) {
	sm := NewStyleManager()
	sm.writersDir = t.TempDir() // empty dir
	_, err := sm.GetStyle("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent style")
	}
}

func TestStyleManager_NameMapping(t *testing.T) {
	dir := t.TempDir()
	styleYAML := `
name: "Dan Koe 风格"
english_name: "dan-koe"
category: "商业"
description: "Test dan-koe"
writing_prompt: "Write like Dan"
`
	if err := os.WriteFile(filepath.Join(dir, "dan-koe.yaml"), []byte(styleYAML), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sm := NewStyleManager()
	sm.writersDir = dir
	if err := sm.LoadStyles(); err != nil {
		t.Fatalf("LoadStyles: %v", err)
	}

	// Test alias mapping
	aliases := []string{"dan-koe", "dankoe", "dan", "koe", "Dan-Koe"}
	for _, alias := range aliases {
		style, err := sm.GetStyle(alias)
		if err != nil {
			t.Errorf("GetStyle(%q): %v", alias, err)
		} else if style.EnglishName != "dan-koe" {
			t.Errorf("GetStyle(%q).EnglishName = %q, want %q", alias, style.EnglishName, "dan-koe")
		}
	}
}

func TestStyleManager_DefaultsFill(t *testing.T) {
	dir := t.TempDir()
	styleYAML := `
english_name: "minimal"
description: "Minimal style"
writing_prompt: "Write minimally"
`
	if err := os.WriteFile(filepath.Join(dir, "minimal.yaml"), []byte(styleYAML), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sm := NewStyleManager()
	sm.writersDir = dir
	if err := sm.LoadStyles(); err != nil {
		t.Fatalf("LoadStyles: %v", err)
	}

	style, err := sm.GetStyle("minimal")
	if err != nil {
		t.Fatalf("GetStyle: %v", err)
	}
	// Name should default to english_name
	if style.Name != "minimal" {
		t.Errorf("Name = %q, want %q", style.Name, "minimal")
	}
	if style.Category != "自定义" {
		t.Errorf("Category = %q, want %q", style.Category, "自定义")
	}
	if style.Version != "1.0" {
		t.Errorf("Version = %q, want %q", style.Version, "1.0")
	}
}

func TestStyleManager_ValidateStyle(t *testing.T) {
	sm := NewStyleManager()

	// Missing english_name
	if err := sm.ValidateStyle(&WriterStyle{}); err == nil {
		t.Error("expected error for missing english_name")
	}

	// Missing writing_prompt
	if err := sm.ValidateStyle(&WriterStyle{EnglishName: "test"}); err == nil {
		t.Error("expected error for missing writing_prompt")
	}

	// Valid
	if err := sm.ValidateStyle(&WriterStyle{EnglishName: "test", WritingPrompt: "prompt"}); err != nil {
		t.Errorf("ValidateStyle valid: %v", err)
	}
}

func TestStyleManager_ExportAndReload(t *testing.T) {
	dir := t.TempDir()
	style := &WriterStyle{
		Name:          "Export Test",
		EnglishName:   "export-test",
		Category:      "测试",
		Description:   "Export test",
		Version:       "2.0",
		WritingPrompt: "Write exported",
	}

	sm := NewStyleManager()
	sm.writersDir = dir
	exportPath := filepath.Join(dir, "export-test.yaml")

	if err := sm.ExportStyle(style, exportPath); err != nil {
		t.Fatalf("ExportStyle: %v", err)
	}

	// Reload and verify
	sm2 := NewStyleManager()
	sm2.writersDir = dir
	if err := sm2.LoadStyles(); err != nil {
		t.Fatalf("LoadStyles: %v", err)
	}

	loaded, err := sm2.GetStyle("export-test")
	if err != nil {
		t.Fatalf("GetStyle: %v", err)
	}
	if loaded.Name != "Export Test" {
		t.Errorf("loaded.Name = %q, want %q", loaded.Name, "Export Test")
	}
}

func TestStyleManager_GetStyleWithPrompt(t *testing.T) {
	dir := t.TempDir()
	styleYAML := `
english_name: "templated"
writing_prompt: "Write about {topic} in {tone} tone"
`
	if err := os.WriteFile(filepath.Join(dir, "templated.yaml"), []byte(styleYAML), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sm := NewStyleManager()
	sm.writersDir = dir
	if err := sm.LoadStyles(); err != nil {
		t.Fatalf("LoadStyles: %v", err)
	}

	style, err := sm.GetStyleWithPrompt("templated", map[string]string{
		"topic": "Go testing",
		"tone":  "casual",
	})
	if err != nil {
		t.Fatalf("GetStyleWithPrompt: %v", err)
	}
	want := "Write about Go testing in casual tone"
	if style.WritingPrompt != want {
		t.Errorf("WritingPrompt = %q, want %q", style.WritingPrompt, want)
	}
}

func TestStyleManager_ListStyles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"style-a", "style-b"} {
		yaml := "english_name: \"" + name + "\"\nwriting_prompt: \"prompt\"\n"
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(yaml), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	sm := NewStyleManager()
	sm.writersDir = dir
	if err := sm.LoadStyles(); err != nil {
		t.Fatalf("LoadStyles: %v", err)
	}

	list := sm.ListStyles()
	if len(list) != 2 {
		t.Errorf("ListStyles() returned %d, want 2", len(list))
	}
}

func TestWriterError(t *testing.T) {
	err := NewStyleNotFoundError("missing")
	if err.Code != ErrCodeStyleNotFound {
		t.Errorf("Code = %q, want %q", err.Code, ErrCodeStyleNotFound)
	}
	if err.Hint() == "" {
		t.Error("Hint() should not be empty")
	}
}
