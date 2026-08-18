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
	execution := &model.TaskExecution{}
	if err := applyAgentPackIdentity(execution, model.TaskTypeViralAnalysis); err != nil {
		t.Fatalf("applyAgentPackIdentity: %v", err)
	}
	if execution.AgentPackID != "seednote" || execution.AgentPackVersion != "1.0.0" || len(execution.AgentPackDigest) != 64 {
		t.Fatalf("Pack identity = %#v", execution)
	}
	if execution.RuntimeProfile != "seednote" || execution.RuntimeAdapter != "standard" {
		t.Fatalf("runtime identity = %s/%s", execution.RuntimeProfile, execution.RuntimeAdapter)
	}
	pack, ok := agentpack.Default().Pack("seednote")
	if !ok {
		t.Fatal("embedded seednote Pack missing")
	}
	var progress []agentpack.ProgressStage
	if err := json.Unmarshal(execution.AgentPackProgressContract, &progress); err != nil || !reflect.DeepEqual(progress, pack.Progress) {
		t.Fatalf("frozen progress contract = %#v, %v", progress, err)
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
		AgentPackProgressContract: datatypes.JSON(`[{"id":"writing","title":"Writing","active_percent":20,"complete_percent":60}]`),
		RuntimeAdapter:            "standard", RuntimeProfile: "article",
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
		AgentPackProgressContract: datatypes.JSON(`[]`), RuntimeAdapter: "standard",
	}
	if inheritAgentPackIdentity(&model.TaskExecution{}, source) {
		t.Fatal("incomplete frozen Agent Pack identity was inherited")
	}
}

func TestInheritAgentPackIdentityRejectsMissingProgressSnapshot(t *testing.T) {
	source := &model.TaskExecution{
		AgentPackID: "article", AgentPackVersion: "1.0.0", AgentPackDigest: "digest",
		RuntimeAdapter: "standard", RuntimeProfile: "article",
	}
	if inheritAgentPackIdentity(&model.TaskExecution{}, source) {
		t.Fatal("Agent Pack identity without progress snapshot was inherited")
	}
}

func TestResolveFrozenExecutionPackProgressContractUsesSnapshotAfterCatalogChanges(t *testing.T) {
	contract := datatypes.JSON(`[{"id":"writing","title":"Frozen Writing","active_percent":21,"complete_percent":61}]`)
	valid := model.TaskExecution{
		AgentPackID: "removed-pack", AgentPackVersion: "99.0.0", AgentPackDigest: "changed-current-digest",
		AgentPackProgressContract: contract,
		RuntimeAdapter:            "standard", RuntimeProfile: "article",
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
