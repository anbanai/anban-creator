package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerProgressTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "update_task_progress",
		Description: "Update task progress at a pipeline stage. Agents call this at each step with stage name, title, and optional description.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":          map[string]any{"type": "string", "description": "Task ID"},
				"stage":            map[string]any{"type": "string", "description": "Pipeline stage name (e.g. research, writing, image_generation)"},
				"title":            map[string]any{"type": "string", "description": "Human-readable stage title (e.g. '选题研究', 'AI写作')"},
				"description":      map[string]any{"type": "string", "description": "Detailed progress description (optional)"},
				"progress_percent": map[string]any{"type": "integer", "description": "Progress percentage 0-100 (optional)"},
			},
			"required": []any{"task_id", "stage", "title"},
		},
	}, progressUpdateHandler)
}

func progressUpdateHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	stage, _ := args["stage"].(string)
	title, _ := args["title"].(string)
	description, _ := args["description"].(string)

	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	if stage == "" {
		return errorResult("stage is required"), nil
	}
	if title == "" {
		return errorResult("title is required"), nil
	}

	var percent int
	if v, ok := args["progress_percent"].(float64); ok {
		percent = int(v)
	}
	if percent < 0 || percent > 100 {
		return errorResult("progress_percent must be between 0 and 100"), nil
	}

	if svcs.TaskSvc == nil {
		return errorResult("task service not available"), nil
	}
	if err := svcs.TaskSvc.UpdateProgress(ctx, taskID, stage, title, description, percent); err != nil {
		return errorResult(fmt.Sprintf("update progress: %v", err)), nil
	}
	return textResult(map[string]any{"task_id": taskID, "stage": stage, "title": title, "updated": true})
}
