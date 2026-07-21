package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeHomeTemplateCopiesNestedStateAndSeedsOnlyMissingFiles(t *testing.T) {
	templateRoot := canonicalTempDir(t)
	homeRoot := canonicalTempDir(t)
	skillPath := filepath.Join(".claude", "skills", "example", "SKILL.md")
	pluginPath := filepath.Join(".claude", "plugins", "installed_plugins.json")
	writeHomeTemplateFile(t, templateRoot, skillPath, "skill-v1", 0o444)
	writeHomeTemplateFile(t, templateRoot, pluginPath, "image-plugin-state", 0o600)
	writeHomeTemplateFile(t, homeRoot, pluginPath, "runtime-newer-state", 0o600)

	if err := materializeHomeTemplate(templateRoot, homeRoot); err != nil {
		t.Fatalf("materializeHomeTemplate: %v", err)
	}
	if got := readHomeTemplateFile(t, homeRoot, skillPath); got != "skill-v1" {
		t.Fatalf("skill = %q, want copied template content", got)
	}
	if got := readHomeTemplateFile(t, homeRoot, pluginPath); got != "runtime-newer-state" {
		t.Fatalf("plugin state = %q, want existing runtime content preserved", got)
	}
	info, err := os.Stat(filepath.Join(homeRoot, skillPath))
	if err != nil {
		t.Fatalf("stat copied skill: %v", err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("copied skill mode = %o, want runtime-owner writable", info.Mode().Perm())
	}

	if err := os.Chmod(filepath.Join(templateRoot, skillPath), 0o644); err != nil {
		t.Fatal(err)
	}
	writeHomeTemplateFile(t, templateRoot, skillPath, "skill-v2", 0o444)
	if err := materializeHomeTemplate(templateRoot, homeRoot); err != nil {
		t.Fatalf("idempotent materializeHomeTemplate: %v", err)
	}
	if got := readHomeTemplateFile(t, homeRoot, skillPath); got != "skill-v1" {
		t.Fatalf("skill after second seed = %q, want runtime copy preserved", got)
	}
}

func TestMaterializeHomeTemplateRejectsSymlinksAndCredentials(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, templateRoot, homeRoot string)
	}{
		{name: "source symlink", setup: func(t *testing.T, templateRoot, _ string) {
			t.Helper()
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(templateRoot, "escape")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "destination symlink", setup: func(t *testing.T, templateRoot, homeRoot string) {
			t.Helper()
			writeHomeTemplateFile(t, templateRoot, filepath.Join(".claude", "settings.json"), "{}", 0o600)
			if err := os.Symlink(t.TempDir(), filepath.Join(homeRoot, ".claude")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "credential file", setup: func(t *testing.T, templateRoot, _ string) {
			t.Helper()
			writeHomeTemplateFile(t, templateRoot, filepath.Join(".claude", ".credentials.json"), "secret", 0o600)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			templateRoot := canonicalTempDir(t)
			homeRoot := canonicalTempDir(t)
			tc.setup(t, templateRoot, homeRoot)
			err := materializeHomeTemplate(templateRoot, homeRoot)
			if err == nil || (!strings.Contains(err.Error(), "symlink") && !strings.Contains(err.Error(), "credential")) {
				t.Fatalf("materializeHomeTemplate error = %v, want safe rejection", err)
			}
		})
	}
}

func TestMaterializeHomeTemplateRejectsSymlinkedParentComponents(t *testing.T) {
	base := canonicalTempDir(t)
	actualParent := filepath.Join(base, "actual")
	templateRoot := filepath.Join(actualParent, "template")
	if err := os.MkdirAll(templateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeHomeTemplateFile(t, templateRoot, filepath.Join(".claude", "settings.json"), "{}", 0o600)
	linkedParent := filepath.Join(base, "linked")
	if err := os.Symlink(actualParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	err := materializeHomeTemplate(filepath.Join(linkedParent, "template"), canonicalTempDir(t))
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("materializeHomeTemplate error = %v, want parent symlink rejection", err)
	}
}

func TestMaterializeHomeTemplateRejectsUnsafeLayouts(t *testing.T) {
	t.Run("overlapping roots", func(t *testing.T) {
		root := canonicalTempDir(t)
		if err := materializeHomeTemplate(root, filepath.Join(root, "home")); err == nil || !strings.Contains(err.Error(), "disjoint") {
			t.Fatalf("materializeHomeTemplate error = %v, want overlap rejection", err)
		}
	})

	t.Run("destination type conflict", func(t *testing.T) {
		templateRoot := canonicalTempDir(t)
		homeRoot := canonicalTempDir(t)
		writeHomeTemplateFile(t, templateRoot, filepath.Join(".claude", "settings.json"), "{}", 0o600)
		if err := os.WriteFile(filepath.Join(homeRoot, ".claude"), []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := materializeHomeTemplate(templateRoot, homeRoot); err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("materializeHomeTemplate error = %v, want type-conflict rejection", err)
		}
	})

}

func TestRunnerOptionsMaterializeConfiguredHomeTemplate(t *testing.T) {
	templateRoot := canonicalTempDir(t)
	homeRoot := canonicalTempDir(t)
	path := filepath.Join(".claude", "skills", "slideshow", "SKILL.md")
	writeHomeTemplateFile(t, templateRoot, path, "slideshow", 0o600)
	t.Setenv("ANBAN_HOME_TEMPLATE", templateRoot)
	t.Setenv("HOME", homeRoot)
	runner := NewRunner(&Config{Workspace: t.TempDir(), AgentFlag: "anban:seednote", MaxTurns: 10}, nil, nil)
	if _, err := runner.buildSDKOptions(context.Background()); err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	if got := readHomeTemplateFile(t, homeRoot, path); got != "slideshow" {
		t.Fatalf("materialized skill = %q", got)
	}
}

func TestMaterializeHomeTemplateFromEnvironmentIsNoopWhenUnset(t *testing.T) {
	t.Setenv("ANBAN_HOME_TEMPLATE", "")
	t.Setenv("HOME", t.TempDir())
	if err := materializeHomeTemplateFromEnvironment(); err != nil {
		t.Fatalf("unset template: %v", err)
	}
}

func TestMaterializeHomeTemplateFromEnvironmentRequiresHome(t *testing.T) {
	t.Setenv("ANBAN_HOME_TEMPLATE", canonicalTempDir(t))
	t.Setenv("HOME", "")
	if err := materializeHomeTemplateFromEnvironment(); err == nil || !strings.Contains(err.Error(), "HOME is required") {
		t.Fatalf("missing HOME error = %v", err)
	}
}

func writeHomeTemplateFile(t *testing.T, root, relative, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readHomeTemplateFile(t *testing.T, root, relative string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
