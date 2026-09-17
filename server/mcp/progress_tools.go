package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/model"
)

func registerProgressTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "set_task_progress_plan",
		Description: "Declare or replan the current task execution's ordered work stages. Call once before starting work and only from the top-level agent.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"task_id":      map[string]any{"type": "string", "description": "Current task ID"},
				"execution_id": map[string]any{"type": "string", "description": "Current execution ID"},
				"stages": map[string]any{
					"type": "array", "minItems": 2, "maxItems": 7,
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"id":    map[string]any{"type": "string", "pattern": `^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`},
							"title": map[string]any{"type": "string", "minLength": 1},
							"goal":  map[string]any{"type": "string"},
						},
						"required": []any{"id", "title"},
					},
				},
			},
			"required": []any{"task_id", "execution_id", "stages"},
		},
	}, progressPlanHandler)

	server.AddTool(&mcp.Tool{
		Name:        "update_task_progress",
		Description: "Mark one declared stage active or complete and optionally record its latest concise update.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"task_id":      map[string]any{"type": "string", "description": "Current task ID"},
				"execution_id": map[string]any{"type": "string", "description": "Current execution ID"},
				"stage":        map[string]any{"type": "string", "description": "Declared stable stage ID"},
				"state":        map[string]any{"type": "string", "enum": []any{"active", "complete"}},
				"description":  map[string]any{"type": "string", "description": "Latest concise user-facing update"},
			},
			"required": []any{"task_id", "execution_id", "stage", "state"},
		},
	}, progressUpdateHandler)
}

func progressPlanHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID := strings.TrimSpace(stringArg(args, "task_id"))
	executionID := strings.TrimSpace(stringArg(args, "execution_id"))
	if taskID == "" || executionID == "" {
		return errorResult("task_id and execution_id are required"), nil
	}
	if result := requireMCPExecutionIdentity(ctx, "set_task_progress_plan", "", taskID, executionID); result != nil {
		return result, nil
	}
	rawStages, ok := args["stages"]
	if !ok {
		return errorResult("stages is required"), nil
	}
	encoded, err := json.Marshal(rawStages)
	if err != nil {
		return errorResult("stages must be an array"), nil
	}
	var stages []model.TaskLifecyclePlanStage
	if err := json.Unmarshal(encoded, &stages); err != nil {
		return errorResult("stages must contain id, title, and optional goal"), nil
	}
	if svcs.TaskSvc == nil {
		return errorResult("task service not available"), nil
	}
	lifecycle, err := svcs.TaskSvc.SetTaskProgressPlan(ctx, taskID, executionID, stages)
	if err != nil {
		return errorResult(fmt.Sprintf("set task progress plan: %v", err)), nil
	}
	return textResult(map[string]any{"task_id": taskID, "execution_id": executionID, "lifecycle": lifecycle})
}

func progressUpdateHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID := strings.TrimSpace(stringArg(args, "task_id"))
	executionID := strings.TrimSpace(stringArg(args, "execution_id"))
	stage := strings.TrimSpace(stringArg(args, "stage"))
	state := strings.TrimSpace(stringArg(args, "state"))
	description := strings.TrimSpace(stringArg(args, "description"))
	if taskID == "" || executionID == "" {
		return errorResult("task_id and execution_id are required"), nil
	}
	if stage == "" {
		return errorResult("stage is required"), nil
	}
	if state != model.TaskLifecycleStateActive && state != model.TaskLifecycleStateComplete {
		return errorResult("state must be active or complete"), nil
	}
	if result := requireMCPExecutionIdentity(ctx, "update_task_progress", "", taskID, executionID); result != nil {
		return result, nil
	}
	if svcs.TaskSvc == nil {
		return errorResult("task service not available"), nil
	}
	lifecycle, err := svcs.TaskSvc.UpdateTaskProgress(ctx, taskID, executionID, stage, state, description)
	if err != nil {
		return errorResult(fmt.Sprintf("update task progress: %v", err)), nil
	}
	return textResult(map[string]any{"task_id": taskID, "execution_id": executionID, "lifecycle": lifecycle})
}
