package model

import "testing"

func TestAgentPackExtensionJSONUsesTypedModelHelpers(t *testing.T) {
	project := &Project{}
	project.SetAgentConfig(map[string]any{"audience": "developers"})
	if project.AgentConfig.Data()["audience"] != "developers" || !project.AgentConfigSet {
		t.Fatalf("project AgentConfig = %#v set=%v", project.AgentConfig.Data(), project.AgentConfigSet)
	}
	task := &Task{}
	task.SetAgentInput(map[string]any{"format": "brief"})
	if task.AgentInput.Data()["format"] != "brief" {
		t.Fatalf("task AgentInput = %#v", task.AgentInput.Data())
	}
	plan := &Plan{}
	plan.SetAgentInput(map[string]any{"format": "scheduled"})
	if plan.AgentInput.Data()["format"] != "scheduled" {
		t.Fatalf("plan AgentInput = %#v", plan.AgentInput.Data())
	}
}
