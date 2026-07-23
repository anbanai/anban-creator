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
		Description: "Returns the canonical task-relative output directory, a relative path rooted at the agent task workspace/current working directory. Managed server tasks must provide task_id.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content_type": map[string]any{"type": "string", "description": "Content type (articles, seednote, moments, ecommerce, montage)"},
				"task_id":      map[string]any{"type": "string", "description": "Managed server task ID"},
			},
			"required": []any{"content_type", "task_id"},
		},
	}, prepareWorkspaceHandler)
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
