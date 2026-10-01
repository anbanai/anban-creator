package mcp

import (
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestMCPTaskResponseExposesAgentInput(t *testing.T) {
	task := &model.Task{ID: "task-1", Type: model.TaskTypeWechatArticle}
	task.SetAgentInput(map[string]any{"format": "brief"})

	response := mcpTaskResponse(task)
	input, ok := response["agent_input"].(map[string]any)
	if !ok || input["format"] != "brief" {
		t.Fatalf("agent_input = %#v, want task extension payload", response["agent_input"])
	}
}
