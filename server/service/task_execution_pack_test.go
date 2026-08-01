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
