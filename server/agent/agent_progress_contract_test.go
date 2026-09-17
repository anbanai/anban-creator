package agent

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
)

func TestManagedAgentProgressContracts(t *testing.T) {
	root := repositoryRoot(t)
	pluginRoot := filepath.Join(root, "harness")
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
	knownStageIDs := map[string][]string{
		"article/article":         {"research", "writing", "delivery"},
		"ecommerce/ecommerce":     {"analysis", "production", "delivery"},
		"live-slicer/live-slicer": {"transcription", "slicing", "delivery"},
		"moments/moments":         {"project", "material_analysis", "writing", "image_generation", "quality_review", "delivery_validation", "finalize"},
		"montage/montage":         {"prepare", "production", "delivery"},
		"seednote/seednote":       {"research", "writing", "delivery"},
		"seednote/viral_analysis": {"research", "delivery"},
	}
	progressArtifactOverrides := map[string][]string{
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
				_, known := knownStageIDs[contractKey]
				if err := validateManagedProgressStageIDs(contractKey, progress, knownStageIDs); err != nil {
					t.Error(err)
				}
				for _, path := range claudePaths {
					assertClaudeTaskProgressContract(t, path, pack, taskType, progress)
				}
				expectedArtifacts := requiredPackArtifactPaths(pack)
				if override, ok := progressArtifactOverrides[contractKey]; ok {
					expectedArtifacts = override
				}
				if err := validateDeliveryProgressArtifacts(progress, expectedArtifacts); err != nil {
					t.Errorf("%s: %v", contractKey, err)
				}
				if known {
					delete(knownStageIDs, contractKey)
				}
			}
		})
	}
	for contractKey := range knownStageIDs {
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

func TestValidateManagedProgressStageIDsAllowsUnknownAndPinsKnownContracts(t *testing.T) {
	known := map[string][]string{"known/task": {"research", "delivery"}}
	progress := []agentpack.ProgressStage{{ID: "analysis"}, {ID: "delivery"}}
	if err := validateManagedProgressStageIDs("new-pack/new-task", progress, known); err != nil {
		t.Fatalf("unknown managed contract rejected: %v", err)
	}
	if err := validateManagedProgressStageIDs("known/task", progress, known); err == nil || !strings.Contains(err.Error(), "want [research delivery]") {
		t.Fatalf("known renamed stages error = %v", err)
	}
	if err := validateManagedProgressStageIDs("new-pack/empty", nil, known); err == nil || !strings.Contains(err.Error(), "must declare progress stages") {
		t.Fatalf("empty unknown contract error = %v", err)
	}
}

func TestValidateDeliveryProgressArtifactsRequiresExactCoverage(t *testing.T) {
	tests := []struct {
		name     string
		actual   []string
		expected []string
		wantErr  string
	}{
		{name: "exact", actual: []string{"output/a.md", "output/b.md"}, expected: []string{"output/a.md", "output/b.md"}},
		{name: "missing", actual: []string{"output/a.md"}, expected: []string{"output/a.md", "output/b.md"}, wantErr: "missing required Pack artifact"},
		{name: "extra", actual: []string{"output/a.md", "output/b.md", "output/c.md"}, expected: []string{"output/a.md", "output/b.md"}, wantErr: "optional or unknown Pack artifact"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			progress := []agentpack.ProgressStage{{ID: "delivery", RequiredArtifacts: tt.actual}}
			err := validateDeliveryProgressArtifacts(progress, tt.expected)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("validateDeliveryProgressArtifacts: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("validateDeliveryProgressArtifacts error = %v, want %q", err, tt.wantErr)
			}
		})
	}
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
		for _, want := range []string{
			"只创建 research、writing、delivery 三个正式 Task",
			"`output/04-article-final.md`、`output/05-article.html` 和 `output/draft.json`",
			"审核通过的图片仍须调用 `upload_image` 写入微信图片元数据",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing article delivery boundary %q", path, want)
			}
		}
		for _, forbidden := range []string{"十步细粒度", "十步业务任务可以另建", "`create_draft` 成功、最终 feedback"} {
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

func validateManagedProgressStageIDs(contractKey string, progress []agentpack.ProgressStage, known map[string][]string) error {
	if len(progress) == 0 {
		return fmt.Errorf("managed task contract %q must declare progress stages", contractKey)
	}
	want, ok := known[contractKey]
	if !ok {
		return nil
	}
	got := make([]string, 0, len(progress))
	for _, stage := range progress {
		got = append(got, stage.ID)
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("%s progress stages = %v, want %v", contractKey, got, want)
	}
	return nil
}

func requiredPackArtifactPaths(pack agentpack.Manifest) []string {
	var paths []string
	for _, artifact := range pack.Artifacts {
		if artifact.Required {
			paths = append(paths, artifact.Path)
		}
	}
	return paths
}

func validateDeliveryProgressArtifacts(progress []agentpack.ProgressStage, expected []string) error {
	var delivery *agentpack.ProgressStage
	for i := range progress {
		if progress[i].ID == "delivery" || progress[i].ID == "delivery_validation" {
			delivery = &progress[i]
		}
	}
	if delivery == nil {
		return fmt.Errorf("managed Pack must declare a delivery or delivery_validation progress stage")
	}

	actualSet := make(map[string]bool, len(delivery.RequiredArtifacts))
	for _, path := range delivery.RequiredArtifacts {
		actualSet[path] = true
	}
	expectedSet := make(map[string]bool, len(expected))
	for _, path := range expected {
		expectedSet[path] = true
		if !actualSet[path] {
			return fmt.Errorf("progress stage %q required_artifacts missing required Pack artifact %q", delivery.ID, path)
		}
	}
	for _, path := range delivery.RequiredArtifacts {
		if !expectedSet[path] {
			return fmt.Errorf("progress stage %q required_artifacts contains optional or unknown Pack artifact %q", delivery.ID, path)
		}
	}
	return nil
}
