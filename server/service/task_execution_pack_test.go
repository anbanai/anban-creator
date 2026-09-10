package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

func TestApplyAgentPackIdentityFreezesResolvedExecutionContract(t *testing.T) {
	pack, ok := agentpack.Default().Pack("seednote")
	if !ok {
		t.Fatal("embedded seednote Pack missing")
	}

	tests := []struct {
		name     string
		taskType string
		wantIDs  []string
		wantLast []string
	}{
		{name: "seednote", taskType: model.PlatformSeednote, wantIDs: []string{"research", "writing", "delivery"}, wantLast: []string{"output/content.md", "output/image-plan.md"}},
		{name: "viral analysis", taskType: model.TaskTypeViralAnalysis, wantIDs: []string{"research", "delivery"}, wantLast: []string{"output/source-analysis.md", "output/viral-template.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execution := &model.TaskExecution{}
			if err := applyAgentPackIdentity(execution, tt.taskType); err != nil {
				t.Fatalf("applyAgentPackIdentity: %v", err)
			}
			if execution.AgentPackID != "seednote" || execution.AgentPackVersion != pack.Version || len(execution.AgentPackDigest) != 64 {
				t.Fatalf("Pack identity = %#v", execution)
			}
			if execution.RuntimeProfile != "seednote" || execution.RuntimeAdapter != "standard" {
				t.Fatalf("runtime identity = %s/%s", execution.RuntimeProfile, execution.RuntimeAdapter)
			}
			var progress []agentpack.ProgressStage
			if err := json.Unmarshal(execution.AgentPackProgressContract, &progress); err != nil {
				t.Fatalf("decode frozen progress contract: %v", err)
			}
			if !reflect.DeepEqual(progress, pack.ProgressForTaskType(tt.taskType)) {
				t.Fatalf("frozen progress contract = %#v, want resolved Pack contract", progress)
			}
			gotIDs := make([]string, 0, len(progress))
			for _, stage := range progress {
				gotIDs = append(gotIDs, stage.ID)
			}
			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Fatalf("stage ids = %#v, want %#v", gotIDs, tt.wantIDs)
			}
			if !reflect.DeepEqual(progress[len(progress)-1].RequiredArtifacts, tt.wantLast) {
				t.Fatalf("delivery artifacts = %#v, want %#v", progress[len(progress)-1].RequiredArtifacts, tt.wantLast)
			}
			var required []agentpack.ArtifactSpec
			if err := json.Unmarshal(execution.AgentPackRequiredArtifactContract, &required); err != nil {
				t.Fatalf("decode frozen required artifact contract: %v", err)
			}
			if got := artifactPathsForTest(required); !reflect.DeepEqual(got, tt.wantLast) {
				t.Fatalf("required artifact contract = %#v, want %#v", got, tt.wantLast)
			}
		})
	}
}

func artifactPathsForTest(artifacts []agentpack.ArtifactSpec) []string {
	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		paths = append(paths, artifact.Path)
	}
	return paths
}

func TestInheritAgentPackIdentityPreservesResolvedViralAnalysisContract(t *testing.T) {
	source := &model.TaskExecution{}
	if err := applyAgentPackIdentity(source, model.TaskTypeViralAnalysis); err != nil {
		t.Fatalf("applyAgentPackIdentity: %v", err)
	}
	target := &model.TaskExecution{}
	if !inheritAgentPackIdentity(target, source) {
		t.Fatal("expected viral analysis Pack identity to be inherited")
	}
	if string(target.AgentPackProgressContract) != string(source.AgentPackProgressContract) {
		t.Fatalf("progress snapshot = %s, want %s", target.AgentPackProgressContract, source.AgentPackProgressContract)
	}
	if string(target.AgentPackDeliveryContract) != string(source.AgentPackDeliveryContract) {
		t.Fatalf("delivery snapshot = %s, want %s", target.AgentPackDeliveryContract, source.AgentPackDeliveryContract)
	}
	if string(target.AgentPackRequiredArtifactContract) != string(source.AgentPackRequiredArtifactContract) {
		t.Fatalf("required artifact snapshot = %s, want %s", target.AgentPackRequiredArtifactContract, source.AgentPackRequiredArtifactContract)
	}
	progress, err := resolveFrozenExecutionProgressContract(target)
	if err != nil || len(progress) != 2 || progress[0].ID != "research" || progress[1].ID != "delivery" {
		t.Fatalf("resolved inherited progress = %#v, %v", progress, err)
	}
}

func TestApplyAgentPackIdentityRejectsUnknownManagedTaskType(t *testing.T) {
	if err := applyAgentPackIdentity(&model.TaskExecution{}, "unknown"); err == nil {
		t.Fatal("unknown task type accepted")
	}
}

func TestInheritAgentPackIdentityPreservesRuntimeProfile(t *testing.T) {
	source := &model.TaskExecution{
		AgentPackID: "article", AgentPackVersion: "1.0.0", AgentPackDigest: "digest",
		AgentPackProgressContract:         datatypes.JSON(`[{"id":"writing","title":"Writing","active_percent":20,"complete_percent":60}]`),
		AgentPackDeliveryContract:         datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown"}]`),
		AgentPackRequiredArtifactContract: datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown","required":true}]`),
		RuntimeAdapter:                    "standard", RuntimeProfile: "article",
	}
	target := &model.TaskExecution{}
	if !inheritAgentPackIdentity(target, source) {
		t.Fatal("expected frozen Agent Pack identity to be inherited")
	}
	if target.RuntimeProfile != source.RuntimeProfile {
		t.Fatalf("RuntimeProfile = %q, want %q", target.RuntimeProfile, source.RuntimeProfile)
	}
	if string(target.AgentPackProgressContract) != string(source.AgentPackProgressContract) {
		t.Fatalf("progress snapshot = %s, want %s", target.AgentPackProgressContract, source.AgentPackProgressContract)
	}
	target.AgentPackProgressContract[0] = 'X'
	if source.AgentPackProgressContract[0] == 'X' {
		t.Fatal("inherited progress snapshot aliases source bytes")
	}
}

func TestInheritAgentPackIdentityRejectsIncompleteRuntimeIdentity(t *testing.T) {
	source := &model.TaskExecution{
		AgentPackID: "article", AgentPackVersion: "1.0.0", AgentPackDigest: "digest",
		AgentPackProgressContract: datatypes.JSON(`[]`), AgentPackDeliveryContract: datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown"}]`), RuntimeAdapter: "standard",
	}
	if inheritAgentPackIdentity(&model.TaskExecution{}, source) {
		t.Fatal("incomplete frozen Agent Pack identity was inherited")
	}
}

func TestInheritAgentPackIdentityRejectsMissingProgressSnapshot(t *testing.T) {
	source := &model.TaskExecution{
		AgentPackID: "article", AgentPackVersion: "1.0.0", AgentPackDigest: "digest",
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown"}]`), RuntimeAdapter: "standard", RuntimeProfile: "article",
	}
	if inheritAgentPackIdentity(&model.TaskExecution{}, source) {
		t.Fatal("Agent Pack identity without progress snapshot was inherited")
	}
}

func TestResolveFrozenExecutionPackProgressContractUsesSnapshotAfterCatalogChanges(t *testing.T) {
	contract := datatypes.JSON(`[{"id":"writing","title":"Frozen Writing","active_percent":21,"complete_percent":61}]`)
	valid := model.TaskExecution{
		AgentPackID: "removed-pack", AgentPackVersion: "99.0.0", AgentPackDigest: "changed-current-digest",
		AgentPackProgressContract:         contract,
		AgentPackDeliveryContract:         datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown"}]`),
		AgentPackRequiredArtifactContract: datatypes.JSON(`[{"role":"final","path":"output/final.md","mime_type":"text/markdown","required":true}]`),
		RuntimeAdapter:                    "standard", RuntimeProfile: "article",
	}
	resumed := &model.TaskExecution{}
	if !inheritAgentPackIdentity(resumed, &valid) {
		t.Fatal("resume did not inherit frozen Agent Pack contract")
	}
	resolved, err := resolveFrozenExecutionProgressContract(resumed)
	if err != nil || len(resolved) != 1 || resolved[0].Title != "Frozen Writing" || resolved[0].CompletePercent != 61 {
		t.Fatalf("resolve frozen progress = %#v, %v", resolved, err)
	}

	for _, tt := range []struct {
		name   string
		mutate func(*model.TaskExecution)
	}{
		{name: "missing id", mutate: func(execution *model.TaskExecution) { execution.AgentPackID = "" }},
		{name: "missing version", mutate: func(execution *model.TaskExecution) { execution.AgentPackVersion = "" }},
		{name: "missing digest", mutate: func(execution *model.TaskExecution) { execution.AgentPackDigest = "" }},
		{name: "missing snapshot", mutate: func(execution *model.TaskExecution) { execution.AgentPackProgressContract = nil }},
		{name: "invalid snapshot", mutate: func(execution *model.TaskExecution) {
			execution.AgentPackProgressContract = datatypes.JSON(`{"bad":true}`)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			execution := valid
			tt.mutate(&execution)
			if _, err := resolveFrozenExecutionProgressContract(&execution); !errors.Is(err, ErrAgentProgressPackMismatch) {
				t.Fatalf("resolve mismatched %s error = %v", tt.name, err)
			}
		})
	}
}
