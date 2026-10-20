package config

import "testing"

func TestContentPlanningRuntimeIsOptionalAndMapped(t *testing.T) {
	cfg := validClaudeConfigForTest()
	delete(cfg.RuntimeImages, "content-planning")
	if err := cfg.Validate(); err != nil {
		t.Fatalf("manual-only config: %v", err)
	}
	cfg.RuntimeImages["content-planning"] = "creator-agent-content-planning:test"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("planning config: %v", err)
	}
	for _, kind := range []string{"content_research", "content_validation", "content_script"} {
		got := cfg.RuntimeImages.ForTask(kind)
		if got.Profile != "content-planning" || got.Image != "creator-agent-content-planning:test" {
			t.Fatalf("runtime %s: %+v", kind, got)
		}
	}
}
