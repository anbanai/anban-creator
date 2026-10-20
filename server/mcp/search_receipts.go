package mcp

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerSearchReceiptTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{Name: "list_search_receipts", Description: "Read successful web-search/page-fetch provenance for the authenticated current task execution. Does not call providers or mutate state.", InputSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}}, listSearchReceiptsHandler)
}
func listSearchReceiptsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if failure := requireMCPExecutionIdentity(ctx, "list_search_receipts", "", "", ""); failure != nil {
		return failure, nil
	}
	identity, _ := getMCPExecutionIdentity(ctx)
	if svcs.TaskSvc == nil {
		return errorResult("task service unavailable"), nil
	}
	result, err := svcs.TaskSvc.ListSearchReceipts(ctx, identity.UserID, identity.ProjectID, identity.TaskID, identity.ExecutionID)
	if err != nil {
		return errorResult("execution search receipts unavailable"), nil
	}
	return textResult(result)
}
