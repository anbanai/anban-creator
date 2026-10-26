package config

import (
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

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
	for _, taskType := range []string{model.TaskTypeContentResearch, model.TaskTypeContentValidation, model.TaskTypeContentScript} {
		got := cfg.RuntimeImages.ForTask(taskType)
		if got.Profile != "content-planning" || got.Image != "creator-agent-content-planning:test" {
			t.Fatalf("runtime %s: %+v", taskType, got)
		}
	}
}
