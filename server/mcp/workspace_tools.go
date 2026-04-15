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
		Description: "Prepare a clean working directory for content creation. Archives any existing staging directory and creates a fresh one. Returns the staging path.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content_type": map[string]any{"type": "string", "description": "Content type (articles, xls, rednote, flower)"},
			},
			"required": []any{"content_type"},
		},
	}, prepareWorkspaceHandler)

	server.AddTool(&mcp.Tool{
		Name:        "archive_workspace",
		Description: "Archive the staging directory for a content type. Moves staging to a dated or named archive directory.",
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
	if contentType == "" {
		return errorResult("content_type is required"), nil
	}

	result, err := svcs.WorkspaceSvc.Prepare(contentType)
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
