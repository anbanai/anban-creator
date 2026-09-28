package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerTrendTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "list_trends",
		Description: "List current public hot trends. Results are refreshed by the Server when the configured TTL has expired.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"platforms": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		}},
	}, listTrendsHandler)
}

func listTrendsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.TrendSvc == nil {
		return errorResult("trend service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	platforms := parseStringArray(args, "platforms")
	limit := 12
	switch value := args["limit"].(type) {
	case float64:
		if int(value) > 0 {
			limit = int(value)
		}
	case int:
		if value > 0 {
			limit = value
		}
	case int64:
		if value > 0 {
			limit = int(value)
		}
	}
	result, err := svcs.TrendSvc.List(ctx, platforms, limit, false)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}
