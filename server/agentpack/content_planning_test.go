package agentpack

import (
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestContentPlanningTaskContracts(t *testing.T) {
	for _, taskType := range []string{model.TaskTypeContentResearch, model.TaskTypeContentValidation, model.TaskTypeContentScript} {
		p, ok := Default().ForTaskType(taskType)
		if !ok {
			t.Fatalf("missing typed planning Pack: %s", taskType)
		}
		if p.Runtime.Profile != "content-planning" || p.Channel != "content-planning" {
			t.Fatalf("wrong planning runtime: %+v", p)
		}
		kind := model.TaskKindForTaskType(taskType)
		if op, ok := Default().BillingOperation(taskType); !ok || op != "task."+kind {
			t.Fatalf("wrong billing: %s", op)
		}
		required, _ := p.RequiredArtifactsForTaskType(taskType)
		if len(required) < 2 {
			t.Fatalf("incomplete artifact contract: %s", taskType)
		}
	}
}
