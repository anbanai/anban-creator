package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	serveragent "github.com/anbanai/anban-creator/server/agent"
)

func TestMaterializeMontageRuntimeCopiesCompleteWritableWorkspace(t *testing.T) {
	template := filepath.Join(t.TempDir(), "template")
	nestedTemplate := filepath.Join(template, "nested")
	if err := os.MkdirAll(nestedTemplate, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedTemplate, "pipeline.yaml"), []byte("version: one\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nested/pipeline.yaml", filepath.Join(template, "pipeline.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(template, ".git"), []byte("gitdir: ../../.git/modules/OpenMontage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(serveragent.MontageTemplateEnvName, template)

	workspace := t.TempDir()
	runtimePath, err := materializeMontageRuntime(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(workspace, "openmontage"); runtimePath != want {
		t.Fatalf("runtime path = %q, want %q", runtimePath, want)
	}
	if body, err := os.ReadFile(filepath.Join(runtimePath, "nested", "pipeline.yaml")); err != nil || string(body) != "version: one\n" {
		t.Fatalf("copied pipeline = %q, err=%v", body, err)
	}
	if target, err := os.Readlink(filepath.Join(runtimePath, "pipeline.yaml")); err != nil || target != "nested/pipeline.yaml" {
		t.Fatalf("copied symlink = %q, err=%v", target, err)
	}
	if _, err := os.Lstat(filepath.Join(runtimePath, ".git")); !os.IsNotExist(err) {
		t.Fatalf("materialized runtime must exclude template Git metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimePath, "nested", "checkpoint.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("Montage workspace must be writable: %v", err)
	}

	templatePipeline := filepath.Join(template, "nested", "pipeline.yaml")
	if err := os.Chmod(templatePipeline, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(templatePipeline, []byte("version: two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := materializeMontageRuntime(workspace); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(runtimePath, "nested", "pipeline.yaml")); err != nil || string(body) != "version: one\n" {
		t.Fatalf("resume replaced existing runtime: %q, err=%v", body, err)
	}
	if _, err := os.Stat(filepath.Join(runtimePath, "nested", "checkpoint.json")); err != nil {
		t.Fatalf("resume lost Montage checkpoint: %v", err)
	}
}

func TestMaterializeMontageRuntimeRejectsNonDirectoryRuntime(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "openmontage"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := materializeMontageRuntime(workspace); err == nil {
		t.Fatal("materializeMontageRuntime accepted a non-directory runtime path")
	}
}

func TestSyncMontageTaskInputsMergesManagedWorkspaceFiles(t *testing.T) {
	workspace := t.TempDir()
	runtimePath := filepath.Join(workspace, "openmontage")
	if err := os.MkdirAll(filepath.Join(workspace, ".anban-creator", "resume"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(runtimePath, ".anban-creator"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(workspace, "montage-input.json"):                    "{}",
		filepath.Join(workspace, ".anban-creator", "resume", "latest.md"): "continue",
		filepath.Join(workspace, "CLAUDE.md"):                             "project rules",
		filepath.Join(runtimePath, "CLAUDE.md"):                           "upstream rules",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := syncMontageTaskInputs(workspace, runtimePath); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(runtimePath, "montage-input.json"),
		filepath.Join(runtimePath, ".anban-creator", "resume", "latest.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Montage runtime missing task input %s: %v", path, err)
		}
	}
	claudeBody, err := os.ReadFile(filepath.Join(runtimePath, "CLAUDE.md"))
	if err != nil || !strings.Contains(string(claudeBody), "upstream rules") || !strings.Contains(string(claudeBody), "project rules") {
		t.Fatalf("merged CLAUDE.md = %q, err=%v", claudeBody, err)
	}
	for _, name := range []string{"montage-input.json", "CLAUDE.md", ".anban-creator"} {
		if _, err := os.Stat(filepath.Join(workspace, name)); !os.IsNotExist(err) {
			t.Fatalf("migrated input %s remains outside Montage runtime: %v", name, err)
		}
	}
}

func TestMontageRuntimeLinksCanonicalOutput(t *testing.T) {
	template := filepath.Join(t.TempDir(), "template")
	if err := os.MkdirAll(template, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(serveragent.MontageTemplateEnvName, template)
	workspace := t.TempDir()

	runtimePath, err := materializeMontageRuntime(workspace)
	if err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(filepath.Join(runtimePath, "output"))
	if err != nil || link != filepath.Join(workspace, "output") {
		t.Fatalf("Montage output link = %q, err=%v", link, err)
	}
}
