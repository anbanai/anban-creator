package mcp

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerContentReuseTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "get_content_reuse_context",
		Description: "Read Server-owned content asset evidence and frozen reuse policy for the current task. Does not change production or publication state.",
		InputSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"project_id": map[string]any{"type": "string"}, "task_id": map[string]any{"type": "string"},
		}, "required": []string{"project_id", "task_id"}},
	}, contentReuseContextHandler)
}

func contentReuseContextHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult("project_id and task_id are required"), nil
	}
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	taskID, _ := args["task_id"].(string)
	projectID, taskID = strings.TrimSpace(projectID), strings.TrimSpace(taskID)
	if projectID == "" || taskID == "" {
		return errorResult("project_id and task_id are required"), nil
	}
	if failure := requireMCPExecutionIdentity(ctx, "get_content_reuse_context", projectID, taskID, ""); failure != nil {
		return failure, nil
	}
	if svcs.ContentAssetSvc == nil {
		return errorResult("content reuse service unavailable"), nil
	}
	result, err := svcs.ContentAssetSvc.GetReuseContext(ctx, getUserID(ctx), projectID, taskID)
	if err != nil {
		return errorResult("content reuse context unavailable for this task"), nil
	}
	return textResult(result)
}
