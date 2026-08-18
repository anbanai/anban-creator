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
	wantStageIDs := map[string][]string{
		"article/article":         {"research", "writing", "delivery"},
		"ecommerce/ecommerce":     {"analysis", "production", "delivery"},
		"live-slicer/live-slicer": {"transcription", "slicing", "delivery"},
		"moments/moments":         {"project", "material_analysis", "writing", "image_generation", "quality_review", "delivery_validation", "finalize"},
		"montage/montage":         {"prepare", "production", "delivery"},
		"seednote/seednote":       {"research", "writing", "delivery"},
		"seednote/viral_analysis": {"research", "delivery"},
	}
	wantDeliveryArtifacts := map[string][]string{
		"seednote/seednote":       {"output/content.md", "output/image-plan.md"},
		"seednote/viral_analysis": {"output/source-analysis.md", "output/viral-template.json"},
	}

	for _, pack := range catalog.Packs {
		if pack.Kind != agentpack.KindManaged {
			continue
		}
		foundManagedPacks[pack.ID] = true
		t.Run(pack.ID, func(t *testing.T) {
			claudePaths := []string{
				filepath.Join(pluginRoot, "packs", pack.ID, pack.Agent.ClaudeSource),
				filepath.Join(pluginRoot, "agents", pack.Agent.Name+".md"),
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

			for _, taskType := range pack.Bindings.TaskTypes {
				contractKey := pack.ID + "/" + taskType
				progress := pack.ProgressForTaskType(taskType)
				wantIDs, ok := wantStageIDs[contractKey]
				if !ok {
					t.Fatalf("managed task contract %q is not pinned", contractKey)
				}
				gotIDs := make([]string, 0, len(progress))
				for _, stage := range progress {
					gotIDs = append(gotIDs, stage.ID)
				}
				if strings.Join(gotIDs, ",") != strings.Join(wantIDs, ",") {
					t.Errorf("%s progress stages = %v, want %v", contractKey, gotIDs, wantIDs)
				}
				for _, path := range claudePaths {
					assertClaudeTaskProgressContract(t, path, pack, taskType, progress)
				}
				if wantArtifacts, ok := wantDeliveryArtifacts[contractKey]; ok {
					assertDeliveryProgressArtifacts(t, contractKey, progress, wantArtifacts)
				}
				delete(wantStageIDs, contractKey)
			}
		})
	}
	for contractKey := range wantStageIDs {
		t.Errorf("managed task contract %q disappeared from the catalog", contractKey)
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

func TestLegacyProgressContractRejectsBTWTelemetry(t *testing.T) {
	for _, match := range legacyProgressForbiddenMatches("update_task_progress(task_id=$TASK_ID)\n/btw report progress") {
		if match == "/btw" {
			return
		}
	}
	t.Fatal("legacy progress contract did not reject /btw telemetry")
}

func assertClaudeTaskProgressContract(t *testing.T, path string, pack agentpack.Manifest, taskType string, progress []agentpack.ProgressStage) {
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
	for _, stage := range progress {
		metadata := `{"anban_progress_stage":"` + stage.ID + `"}`
		if !strings.Contains(body, metadata) {
			t.Errorf("%s does not map declared progress stage %q through Task metadata %s", path, stage.ID, metadata)
		}
	}
	switch pack.ID {
	case "article":
		for _, want := range []string{"发布前总验收、`publish_draft` 成功、最终 feedback 全部结束后才完成"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing article delivery boundary %q", path, want)
			}
		}
		for _, forbidden := range []string{"明确跳过发布"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s contains unsupported article delivery branch %q", path, forbidden)
			}
		}
	case "ecommerce":
		for _, want := range []string{"原有八个细粒度业务任务"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing ecommerce task count contract %q", path, want)
			}
		}
		for _, forbidden := range []string{"十个细粒度业务任务"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s contains inaccurate ecommerce task count %q", path, forbidden)
			}
		}
	case "seednote":
		if taskType == "viral_analysis" {
			for _, want := range []string{"viral_analysis", "只创建 `research`、`delivery`", "不得创建 `writing`"} {
				if !strings.Contains(body, want) {
					t.Errorf("%s missing viral analysis task contract %q", path, want)
				}
			}
		} else {
			for _, want := range []string{"普通 `seednote`", "`research`、`writing`、`delivery`"} {
				if !strings.Contains(body, want) {
					t.Errorf("%s missing seednote task contract %q", path, want)
				}
			}
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
	for _, forbidden := range legacyProgressForbiddenMatches(body) {
		t.Errorf("%s contains forbidden compatibility progress token: %q", path, forbidden)
	}
}

func legacyProgressForbiddenMatches(body string) []string {
	var matches []string
	for _, forbidden := range []string{"anban_progress_stage", "官方 Task Hook", "Runner Hook", "/btw"} {
		if strings.Contains(body, forbidden) {
			matches = append(matches, forbidden)
		}
	}
	return matches
}

func assertDeliveryProgressArtifacts(t *testing.T, contractKey string, progress []agentpack.ProgressStage, want []string) {
	t.Helper()
	var delivery *agentpack.ProgressStage
	for i := range progress {
		if progress[i].ID == "delivery" || progress[i].ID == "delivery_validation" {
			delivery = &progress[i]
		}
	}
	if delivery == nil {
		t.Fatal("managed Pack must declare a delivery or delivery_validation progress stage")
	}

	if strings.Join(delivery.RequiredArtifacts, ",") != strings.Join(want, ",") {
		t.Errorf("%s delivery artifacts = %v, want %v", contractKey, delivery.RequiredArtifacts, want)
	}
}
