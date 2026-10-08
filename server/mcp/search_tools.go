package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerSearchTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "search_web",
		Description: "Search the public web through the Server-configured search provider. Requires the current managed task execution and returns cited, normalized results. Agents must not select a provider or call search sites directly.",
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"query":      map[string]any{"type": "string", "minLength": 1},
				"limit":      map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
				"time_range": map[string]any{"type": "string"},
				"domains":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 10},
			},
			"required": []any{"query"},
		},
	}, searchWebHandler)
}

func searchWebHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult("search_web request parameters are required"), nil
	}
	if failure := requireMCPExecutionIdentity(ctx, "search_web", "", "", ""); failure != nil {
		return failure, nil
	}
	identity, _ := getMCPExecutionIdentity(ctx)
	if strings.TrimSpace(identity.UserID) == "" {
		return errorResult(`{"code":"execution_identity_required","message":"search_web requires an execution-scoped credential"}`), nil
	}
	args := parseArgs(req.Params.Arguments)
	query := strings.TrimSpace(stringArg(args, "query"))
	if query == "" {
		return errorResult(`{"code":"invalid_request","message":"query is required"}`), nil
	}
	if _, providerOverride := args["provider"]; providerOverride {
		return errorResult(`{"code":"provider_selection_forbidden","message":"search provider is controlled by Server configuration"}`), nil
	}
	limit := 10
	if rawLimit, present := args["limit"]; present {
		value, ok := rawLimit.(float64)
		if !ok || value != float64(int(value)) || value < 1 || value > 20 {
			return errorResult(`{"code":"invalid_request","message":"limit must be an integer between 1 and 20"}`), nil
		}
		limit = int(value)
	}
	if svcs == nil || svcs.SearchSvc == nil {
		return errorResult(`{"code":"provider_unavailable","message":"search provider is not configured"}`), nil
	}
	result, err := svcs.SearchSvc.Search(ctx, service.SearchOperationRequest{UserID: identity.UserID, ProjectID: identity.ProjectID, TaskID: identity.TaskID, ExecutionID: identity.ExecutionID, Request: service.SearchRequest{Query: query, Limit: limit, TimeRange: stringArg(args, "time_range"), Domains: parseStringArray(args, "domains")}})
	if err != nil {
		code := "search_failed"
		if errors.Is(err, service.ErrSearchProviderUnavailable) {
			code = "provider_unavailable"
		}
		return errorResult(searchErrorJSON(code, err.Error())), nil
	}
	return textResult(result)
}

func searchErrorJSON(code, message string) string {
	payload, _ := json.Marshal(map[string]string{"code": code, "message": message})
	return string(payload)
}
