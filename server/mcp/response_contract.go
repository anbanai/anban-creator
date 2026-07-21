package mcp

import (
	"encoding/json"

	"github.com/anbanai/anban-creator/server/model"
)

func mcpTaskResponses(tasks []*model.Task) []map[string]any {
	items := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, mcpTaskResponse(task))
	}
	return items
}

func mcpTaskResponse(task *model.Task) map[string]any {
	if task == nil {
		return nil
	}
	return mcpModelMap(task)
}

func mcpPlanResponses(plans []*model.Plan) []map[string]any {
	items := make([]map[string]any, 0, len(plans))
	for _, plan := range plans {
		items = append(items, mcpPlanResponse(plan))
	}
	return items
}

func mcpPlanResponse(plan *model.Plan) map[string]any {
	if plan == nil {
		return nil
	}
	return mcpModelMap(plan)
}

func mcpModelMap(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		return map[string]any{}
	}
	return resp
}
