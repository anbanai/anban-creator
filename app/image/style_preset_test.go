package image

import (
	"os"
	"path/filepath"
	"testing"
)

const validPresetYAML = `name: test-preset
english_name: Test Preset
description: 测试风格预设
category: 测试
prompt: |
  测试风格提示词
  第二行
`

func TestStylePresetManager_LoadPresets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test-preset.yaml"), []byte(validPresetYAML), 0644); err != nil {
		t.Fatal(err)
	}

	spm := &StylePresetManager{
		presets:   make(map[string]*StylePreset),
		stylesDir: dir,
	}
	if err := spm.LoadPresets(); err != nil {
		t.Fatalf("LoadPresets() error = %v", err)
	}
	if len(spm.presets) != 1 {
		t.Errorf("expected 1 preset, got %d", len(spm.presets))
	}
}

func TestStylePresetManager_GetPreset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test-preset.yaml"), []byte(validPresetYAML), 0644); err != nil {
		t.Fatal(err)
	}

	spm := &StylePresetManager{
		presets:   make(map[string]*StylePreset),
		stylesDir: dir,
	}

	preset, err := spm.GetPreset("test-preset")
	if err != nil {
		t.Fatalf("GetPreset() error = %v", err)
	}
	if preset.Name != "test-preset" {
		t.Errorf("preset.Name = %q, want %q", preset.Name, "test-preset")
	}
	if preset.EnglishName != "Test Preset" {
		t.Errorf("preset.EnglishName = %q, want %q", preset.EnglishName, "Test Preset")
	}
	if preset.Prompt == "" {
		t.Error("preset.Prompt should not be empty")
	}
}

func TestStylePresetManager_NotFound(t *testing.T) {
	dir := t.TempDir()

	spm := &StylePresetManager{
		presets:   make(map[string]*StylePreset),
		stylesDir: dir,
	}

	_, err := spm.GetPreset("nonexistent")
	if err == nil {
		t.Error("GetPreset() should return error for unknown preset name")
	}
}

func TestStylePresetManager_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	spm := &StylePresetManager{
		presets:   make(map[string]*StylePreset),
		stylesDir: dir,
	}
	if err := spm.LoadPresets(); err != nil {
		t.Errorf("LoadPresets() on empty dir should not error, got %v", err)
	}
	if len(spm.presets) != 0 {
		t.Errorf("expected 0 presets in empty dir, got %d", len(spm.presets))
	}
}

func TestStylePresetManager_NonExistentDir(t *testing.T) {
	spm := &StylePresetManager{
		presets:   make(map[string]*StylePreset),
		stylesDir: "/nonexistent/path/that/does/not/exist",
	}
	if err := spm.LoadPresets(); err != nil {
		t.Errorf("LoadPresets() on non-existent dir should not error, got %v", err)
	}
}

func TestStylePresetManager_ListPresetNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test-preset.yaml"), []byte(validPresetYAML), 0644); err != nil {
		t.Fatal(err)
	}

	spm := &StylePresetManager{
		presets:   make(map[string]*StylePreset),
		stylesDir: dir,
	}

	names := spm.ListPresetNames()
	if len(names) != 1 || names[0] != "test-preset" {
		t.Errorf("ListPresetNames() = %v, want [test-preset]", names)
	}
}

func TestStylePresetManager_HasPreset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test-preset.yaml"), []byte(validPresetYAML), 0644); err != nil {
		t.Fatal(err)
	}

	spm := &StylePresetManager{
		presets:   make(map[string]*StylePreset),
		stylesDir: dir,
	}

	if !spm.HasPreset("test-preset") {
		t.Error("HasPreset() should return true for existing preset")
	}
	if spm.HasPreset("missing") {
		t.Error("HasPreset() should return false for missing preset")
	}
}
