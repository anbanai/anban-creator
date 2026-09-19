package agent

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
)

func TestManagedAgentsOwnDynamicLifecycleReporting(t *testing.T) {
	root := repositoryRoot(t)
	pluginRoot := filepath.Join(root, "harness")
	catalog, err := agentpack.LoadCatalog(pluginRoot)
	if err != nil {
		t.Fatalf("load Agent Pack catalog: %v", err)
	}

	for _, pack := range catalog.Packs {
		if pack.Kind != agentpack.KindManaged {
			continue
		}
		pack := pack
		t.Run(pack.ID, func(t *testing.T) {
			if pack.Version != "2.0.1" {
				t.Fatalf("Pack version = %q, want 2.0.1", pack.Version)
			}

			claudePaths := []string{
				filepath.Join(pluginRoot, "packs", pack.ID, pack.Agent.ClaudeSource),
				filepath.Join(pluginRoot, "agents", pack.Agent.Name+".md"),
			}
			for _, path := range claudePaths {
				assertClaudeLifecycleContract(t, path)
			}

			codexPaths := []string{
				filepath.Join(pluginRoot, "packs", pack.ID, pack.Agent.CodexSource),
				filepath.Join(pluginRoot, "agents", pack.Agent.Name+".toml"),
			}
			for _, path := range codexPaths {
				assertDirectLifecycleContract(t, path)
			}
			if pack.Agent.DSHSource != "" {
				assertDirectLifecycleContract(t, filepath.Join(pluginRoot, "packs", pack.ID, pack.Agent.DSHSource))
			}
		})
	}
}

func assertClaudeLifecycleContract(t *testing.T, path string) {
	t.Helper()
	body := readRepoFile(t, path)
	for _, want := range []string{"set_task_progress_plan", "TaskCreate", "TaskUpdate", "anban_stage_id"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s missing dynamic lifecycle rule %q", path, want)
		}
	}
	for _, forbidden := range []string{
		"anban_progress_stage",
		"progress_percent",
		"active_percent",
		"complete_percent",
		"三个阶段 Task",
		"三个可追踪阶段 Task",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("%s contains removed lifecycle token %q", path, forbidden)
		}
	}
}

func assertDirectLifecycleContract(t *testing.T, path string) {
	t.Helper()
	body := readRepoFile(t, path)
	for _, want := range []string{"set_task_progress_plan", "update_task_progress"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s missing direct lifecycle tool %q", path, want)
		}
	}
	for _, forbidden := range []string{"progress_percent", "active_percent", "complete_percent"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("%s contains removed lifecycle argument %q", path, forbidden)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "update_task_progress") && strings.Contains(line, "title=") {
			t.Errorf("%s sends a client-owned title in lifecycle update: %s", path, strings.TrimSpace(line))
		}
	}
}

func TestSkillsDoNotOwnTaskLifecycle(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "harness", "skills")
	forbidden := []string{"set_task_progress_plan", "update_task_progress", "anban_stage_id", "anban_progress_stage"}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}
		body := readRepoFile(t, path)
		for _, token := range forbidden {
			if strings.Contains(body, token) {
				t.Errorf("%s must remain host-neutral and contains %q", path, token)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
