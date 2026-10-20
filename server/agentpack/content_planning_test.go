package agentpack

import "testing"

func TestContentPlanningTaskContracts(t *testing.T) {
	for _, kind := range []string{"content_research", "content_validation", "content_script"} {
		p, ok := Default().ForTaskType(kind)
		if !ok {
			t.Fatalf("missing typed planning Pack: %s", kind)
		}
		if p.Runtime.Profile != "content-planning" || p.Channel != "content-planning" {
			t.Fatalf("wrong planning runtime: %+v", p)
		}
		if op, ok := Default().BillingOperation(kind); !ok || op != "task."+kind {
			t.Fatalf("wrong billing: %s", op)
		}
		required, _ := p.RequiredArtifactsForTaskType(kind)
		if len(required) < 2 {
			t.Fatalf("incomplete artifact contract: %s", kind)
		}
	}
}
