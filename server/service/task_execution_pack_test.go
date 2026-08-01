package service

import (
	"testing"

	"github.com/anbanai/anban-creator/server/model"
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
}

func TestApplyAgentPackIdentityRejectsUnknownManagedTaskType(t *testing.T) {
	if err := applyAgentPackIdentity(&model.TaskExecution{}, "unknown"); err == nil {
		t.Fatal("unknown task type accepted")
	}
}

func TestInheritAgentPackIdentityPreservesRuntimeProfile(t *testing.T) {
	source := &model.TaskExecution{
		AgentPackID: "article", AgentPackVersion: "1.0.0", AgentPackDigest: "digest",
		RuntimeAdapter: "standard", RuntimeProfile: "article",
	}
	target := &model.TaskExecution{}
	if !inheritAgentPackIdentity(target, source) {
		t.Fatal("expected frozen Agent Pack identity to be inherited")
	}
	if target.RuntimeProfile != source.RuntimeProfile {
		t.Fatalf("RuntimeProfile = %q, want %q", target.RuntimeProfile, source.RuntimeProfile)
	}
}

func TestInheritAgentPackIdentityRejectsIncompleteRuntimeIdentity(t *testing.T) {
	source := &model.TaskExecution{
		AgentPackID: "article", AgentPackVersion: "1.0.0", AgentPackDigest: "digest",
		RuntimeAdapter: "standard",
	}
	if inheritAgentPackIdentity(&model.TaskExecution{}, source) {
		t.Fatal("incomplete frozen Agent Pack identity was inherited")
	}
}
