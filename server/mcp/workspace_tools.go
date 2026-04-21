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
		Description: "Prepare a clean working directory for content creation. Archives any existing files and creates a fresh directory. When task_id is provided, the working directory is the task workspace root. Otherwise, uses the base output directory for the content type. Returns the working directory path.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content_type": map[string]any{"type": "string", "description": "Content type (articles, xls, rednote, flower)"},
				"task_id":      map[string]any{"type": "string", "description": "Task ID — when provided, working directory is the task workspace root"},
			},
			"required": []any{"content_type"},
		},
	}, prepareWorkspaceHandler)

	server.AddTool(&mcp.Tool{
		Name:        "archive_workspace",
		Description: "Archive files from the content type directory to a dated or named archive subdirectory. Leaves existing archive subdirectories in place.",
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
