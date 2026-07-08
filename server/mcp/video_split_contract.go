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
	resp := mcpModelMap(task)
	rewriteMCPVideoFields(resp, task.Type, task.VideoInput.Data(), task.VideoConfig.Data())
	return resp
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
	resp := mcpModelMap(plan)
	rewriteMCPVideoFields(resp, plan.Type, plan.VideoInput.Data(), plan.VideoConfig.Data())
	return resp
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

func rewriteMCPVideoFields(resp map[string]any, platform string, input model.VideoInput, cfg model.VideoTaskConfig) {
	delete(resp, "video_input")
	delete(resp, "video_config")
	switch {
	case model.IsVideoCreatorPlatform(platform):
		resp["video_creator_input"] = input
		resp["video_creator_config"] = cfg
	case model.IsVideoEditorPlatform(platform):
		resp["video_editor_input"] = input
		resp["video_editor_config"] = cfg
	}
}
