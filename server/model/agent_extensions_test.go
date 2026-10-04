package model

import "testing"

func TestAgentPackExtensionJSONUsesTypedModelHelpers(t *testing.T) {
	task := &Task{}
	task.SetAgentInput(map[string]any{"format": "brief"})
	if task.AgentInput.Data()["format"] != "brief" {
		t.Fatalf("task AgentInput = %#v", task.AgentInput.Data())
	}
}
