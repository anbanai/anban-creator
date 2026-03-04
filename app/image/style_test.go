package image

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestPreset(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("write test preset: %v", err)
	}
}

const validPresetYAML = `
name: "测试扁平"
english_name: "test-flat"
description: "测试用扁平风格"
category: "测试"
design:
  style: "扁平"
  mood: "清新"
colors:
  primary: "蓝 #0000FF"
  background: "白 #FFFFFF"
border:
  style: "圆角"
  radius: "8px"
background:
  type: "纯色"
  description: "纯白背景"
typography:
  heading: "粗体"
  body: "常规"
prompt: "扁平风格，蓝色调，圆角设计。"
`

const missingEnglishNameYAML = `
name: "缺少英文名"
prompt: "一些提示词"
`

const missingPromptYAML = `
name: "缺少提示词"
english_name: "no-prompt"
`

func TestStylePresetManager_LoadPresets(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		m := &StylePresetManager{presets: make(map[string]*StylePreset), stylesDir: dir}
		if err := m.LoadPresets(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.Count() != 0 {
			t.Errorf("expected 0 presets, got %d", m.Count())
		}
	})

	t.Run("directory does not exist", func(t *testing.T) {
		m := &StylePresetManager{presets: make(map[string]*StylePreset), stylesDir: "/nonexistent/path/styles"}
		if err := m.LoadPresets(); err != nil {
			t.Fatalf("unexpected error for missing dir: %v", err)
		}
		if m.Count() != 0 {
			t.Errorf("expected 0 presets, got %d", m.Count())
		}
	})

	t.Run("loads valid preset", func(t *testing.T) {
		dir := t.TempDir()
		writeTestPreset(t, dir, "test-flat.yaml", validPresetYAML)

		m := &StylePresetManager{presets: make(map[string]*StylePreset), stylesDir: dir}
		if err := m.LoadPresets(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.Count() != 1 {
			t.Errorf("expected 1 preset, got %d", m.Count())
		}
	})

	t.Run("skips invalid preset, continues loading", func(t *testing.T) {
		dir := t.TempDir()
		writeTestPreset(t, dir, "valid.yaml", validPresetYAML)
		writeTestPreset(t, dir, "no-english.yaml", missingEnglishNameYAML)
		writeTestPreset(t, dir, "no-prompt.yaml", missingPromptYAML)

		m := &StylePresetManager{presets: make(map[string]*StylePreset), stylesDir: dir}
		if err := m.LoadPresets(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Only the valid one should be loaded
		if m.Count() != 1 {
			t.Errorf("expected 1 preset, got %d", m.Count())
		}
	})

	t.Run("skips non-yaml files", func(t *testing.T) {
		dir := t.TempDir()
		writeTestPreset(t, dir, "test-flat.yaml", validPresetYAML)
		writeTestPreset(t, dir, "readme.md", "# README")
		writeTestPreset(t, dir, "data.json", `{"key":"value"}`)

		m := &StylePresetManager{presets: make(map[string]*StylePreset), stylesDir: dir}
		if err := m.LoadPresets(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.Count() != 1 {
			t.Errorf("expected 1 preset, got %d", m.Count())
		}
	})
}

func TestStylePresetManager_GetPreset(t *testing.T) {
	dir := t.TempDir()
	writeTestPreset(t, dir, "test-flat.yaml", validPresetYAML)

	m := &StylePresetManager{presets: make(map[string]*StylePreset), stylesDir: dir}

	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantKey string
	}{
		{"by english_name", "test-flat", false, "test-flat"},
		{"by chinese name", "测试扁平", false, "test-flat"},
		{"not found", "nonexistent", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preset, err := m.GetPreset(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if preset.EnglishName != tt.wantKey {
				t.Errorf("expected english_name %q, got %q", tt.wantKey, preset.EnglishName)
			}
		})
	}
}

func TestStylePresetManager_GetPrompt(t *testing.T) {
	dir := t.TempDir()
	writeTestPreset(t, dir, "test-flat.yaml", validPresetYAML)

	m := &StylePresetManager{presets: make(map[string]*StylePreset), stylesDir: dir}

	t.Run("returns prompt for known preset", func(t *testing.T) {
		prompt, err := m.GetPrompt("test-flat")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prompt == "" {
			t.Error("expected non-empty prompt")
		}
	})

	t.Run("returns error for unknown preset", func(t *testing.T) {
		_, err := m.GetPrompt("unknown-preset")
		if err == nil {
			t.Error("expected error but got nil")
		}
	})
}

func TestStylePresetManager_ListPresets(t *testing.T) {
	dir := t.TempDir()
	writeTestPreset(t, dir, "test-flat.yaml", validPresetYAML)
	writeTestPreset(t, dir, "test-flat2.yaml", `
name: "测试暗色"
english_name: "test-dark"
prompt: "暗色风格。"
`)

	m := &StylePresetManager{presets: make(map[string]*StylePreset), stylesDir: dir}
	presets := m.ListPresets()

	if len(presets) != 2 {
		t.Errorf("expected 2 presets, got %d", len(presets))
	}
}
