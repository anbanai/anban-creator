package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
)

func TestManagedAgentProgressContracts(t *testing.T) {
	root := repositoryRoot(t)
	pluginRoot := filepath.Join(root, "plugins")
	catalog, err := agentpack.LoadCatalog(pluginRoot)
	if err != nil {
		t.Fatalf("load Agent Pack catalog: %v", err)
	}

	legacyProgressPacks := map[string]bool{
		"article":     true,
		"ecommerce":   true,
		"live-slicer": true,
		"moments":     true,
		"seednote":    true,
	}
	wantManagedPacks := map[string]bool{
		"article":     true,
		"ecommerce":   true,
		"live-slicer": true,
		"moments":     true,
		"montage":     true,
		"seednote":    true,
	}
	foundManagedPacks := make(map[string]bool, len(wantManagedPacks))

	for _, pack := range catalog.Packs {
		if pack.Kind != agentpack.KindManaged {
			continue
		}
		foundManagedPacks[pack.ID] = true
		t.Run(pack.ID, func(t *testing.T) {
			if len(pack.Progress) == 0 {
				t.Fatal("managed Pack must declare progress stages")
			}

			claudePaths := []string{
				filepath.Join(pluginRoot, "packs", pack.ID, pack.Agent.ClaudeSource),
				filepath.Join(pluginRoot, "agents", pack.Agent.Name+".md"),
			}
			for _, path := range claudePaths {
				assertClaudeTaskProgressContract(t, path, pack)
			}

			codexPaths := []string{
				filepath.Join(pluginRoot, "packs", pack.ID, pack.Agent.CodexSource),
				filepath.Join(pluginRoot, "agents", pack.Agent.Name+".toml"),
			}
			for _, path := range codexPaths {
				assertLegacyProgressContract(t, path, legacyProgressPacks[pack.ID])
			}

			if pack.Agent.DSHSource != "" {
				path := filepath.Join(pluginRoot, "packs", pack.ID, pack.Agent.DSHSource)
				assertLegacyProgressContract(t, path, legacyProgressPacks[pack.ID])
			}

			assertDeliveryProgressCoversRequiredArtifacts(t, pack)
		})
	}

	for packID := range wantManagedPacks {
		if !foundManagedPacks[packID] {
			t.Errorf("managed Pack %q disappeared from the catalog", packID)
		}
	}
	for packID := range legacyProgressPacks {
		if !foundManagedPacks[packID] {
			t.Errorf("legacy progress Pack %q disappeared from the catalog", packID)
		}
	}
}

func assertClaudeTaskProgressContract(t *testing.T, path string, pack agentpack.Manifest) {
	t.Helper()
	body := readRepoFile(t, path)
	if strings.Contains(body, "update_task_progress(") {
		t.Errorf("%s must derive managed progress from Task metadata, not update_task_progress", path)
	}
	if strings.Contains(body, "/btw") {
		t.Errorf("%s must not introduce /btw telemetry", path)
	}
	for _, want := range []string{
		"TaskCreate",
		"TaskUpdate status=in_progress",
		"TaskUpdate status=completed",
		"anban_progress_stage",
		"Runner Hooks",
		"保存每次返回的 Task id",
		"分别创建下列",
		"对同一 Task id",
		"该阶段交付完成后",
		"不得省略 TaskUpdate 的 metadata",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s missing Task metadata progress rule %q", path, want)
		}
	}
	for _, stage := range pack.Progress {
		metadata := `{"anban_progress_stage":"` + stage.ID + `"}`
		if !strings.Contains(body, metadata) {
			t.Errorf("%s does not map declared progress stage %q through Task metadata %s", path, stage.ID, metadata)
		}
	}
}

func assertLegacyProgressContract(t *testing.T, path string, requireLegacyProgress bool) {
	t.Helper()
	body := readRepoFile(t, path)
	if requireLegacyProgress && !strings.Contains(body, "update_task_progress") {
		t.Errorf("%s must retain explicit update_task_progress", path)
	}
	if !requireLegacyProgress && strings.Contains(body, "update_task_progress") {
		t.Errorf("%s must not introduce explicit update_task_progress", path)
	}
	for _, forbidden := range []string{"anban_progress_stage", "官方 Task Hook", "Runner Hook"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("%s must not claim Claude Task Hook progress support: %q", path, forbidden)
		}
	}
}

func assertDeliveryProgressCoversRequiredArtifacts(t *testing.T, pack agentpack.Manifest) {
	t.Helper()
	var delivery *agentpack.ProgressStage
	for i := range pack.Progress {
		if pack.Progress[i].ID == "delivery" || pack.Progress[i].ID == "delivery_validation" {
			delivery = &pack.Progress[i]
		}
	}
	if delivery == nil {
		t.Fatal("managed Pack must declare a delivery or delivery_validation progress stage")
	}

	covered := make(map[string]bool, len(delivery.RequiredArtifacts))
	for _, path := range delivery.RequiredArtifacts {
		covered[path] = true
	}
	required := make(map[string]bool, len(pack.Artifacts))
	for _, artifact := range pack.Artifacts {
		if artifact.Required {
			required[artifact.Path] = true
		}
		if artifact.Required && !covered[artifact.Path] {
			t.Errorf("progress stage %q required_artifacts missing required Pack artifact %q", delivery.ID, artifact.Path)
		}
	}
	for _, path := range delivery.RequiredArtifacts {
		if !required[path] {
			t.Errorf("progress stage %q required_artifacts contains optional or unknown Pack artifact %q", delivery.ID, path)
		}
	}
}
