package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerWorkspaceTools registers workspace management tools.
func registerWorkspaceTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "prepare_workspace",
		Description: "Returns the canonical working directory path for the given content type and task. Does NOT create directories — the agent must run mkdir -p locally. When task_id is provided, returns 'output' (relative to the task workspace root). Otherwise, returns the base output directory for the content type (e.g. 'output/seednote').",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content_type": map[string]any{"type": "string", "description": "Content type (articles, xls, seednote, flower)"},
				"task_id":      map[string]any{"type": "string", "description": "Task ID — when provided, returns 'output' relative to task workspace"},
			},
			"required": []any{"content_type"},
		},
	}, prepareWorkspaceHandler)

	server.AddTool(&mcp.Tool{
		Name:        "archive_workspace",
		Description: "Returns the computed archive directory path for the given content type. Does NOT move files — the agent must run mkdir -p and mv locally. If name is provided, the archive directory is named after the sanitized title.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content_type": map[string]any{"type": "string", "description": "Content type"},
				"name":         map[string]any{"type": "string", "description": "Archive directory name (title-based)"},
			},
			"required": []any{"content_type"},
		},
	}, archiveWorkspaceHandler)
}

func prepareWorkspaceHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WorkspaceSvc == nil {
		return errorResult("workspace service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)

	contentType, _ := args["content_type"].(string)
	taskID, _ := args["task_id"].(string)
	if contentType == "" {
		return errorResult("content_type is required"), nil
	}

	result, err := svcs.WorkspaceSvc.Prepare(contentType, taskID)
	if err != nil {
		return errorResult(fmt.Sprintf("prepare workspace: %v", err)), nil
	}

	return textResult(result)
}

func archiveWorkspaceHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WorkspaceSvc == nil {
		return errorResult("workspace service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)

	contentType, _ := args["content_type"].(string)
	name, _ := args["name"].(string)
	if contentType == "" {
		return errorResult("content_type is required"), nil
	}

	result, err := svcs.WorkspaceSvc.Archive(contentType, name)
	if err != nil {
		return errorResult(fmt.Sprintf("archive workspace: %v", err)), nil
	}

	return textResult(result)
}
