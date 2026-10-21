package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerSearchTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name: "search_web", Description: "Search the public web through the Server-configured provider. The Server owns provider selection and credentials; results include citations and freshness metadata.",
		InputSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"query": map[string]any{"type": "string", "minLength": 1, "maxLength": 128}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
			"sites": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 20}, "blocked_sites": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 20},
			"after": map[string]any{"type": "string", "pattern": "^\\d{4}-\\d{2}-\\d{2}$"}, "before": map[string]any{"type": "string", "pattern": "^\\d{4}-\\d{2}-\\d{2}$"}, "include_content": map[string]any{"type": "boolean"},
		}, "required": []any{"query"}},
	}, searchWebHandler)
	server.AddTool(&mcp.Tool{
		Name: "fetch_web_page", Description: "Fetch one public HTTP(S) page through the Server-configured page-fetch provider and return Markdown content.",
		InputSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"url": map[string]any{"type": "string", "format": "uri", "minLength": 1, "maxLength": 4096}}, "required": []any{"url"}},
	}, fetchWebPageHandler)
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
	if hasProviderOverride(args) {
		return errorResult(`{"code":"provider_selection_forbidden","message":"search provider is controlled by Server configuration"}`), nil
	}
	queryValue, queryPresent, queryOK := stringArgument(args, "query")
	query := strings.TrimSpace(queryValue)
	if !queryPresent || !queryOK || query == "" || len([]rune(query)) > 128 {
		return errorResult(`{"code":"invalid_request","message":"query must be a non-empty string of at most 128 characters"}`), nil
	}
	for _, field := range []string{"ori_question", "localization", "time_range", "block_hosts", "need_content", "need_url", "auth_info_level", "content_formats", "industry", "query_rewrite"} {
		if _, present := args[field]; present {
			return errorResult(`{"code":"invalid_request","message":"provider-specific search parameters are not supported"}`), nil
		}
	}
	limit, err := optionalLimitArg(args)
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"limit must be an integer between 1 and 50"}`), nil
	}
	sites, err := parseSearchStringArray(args, "sites")
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"sites must be a string array of non-empty strings"}`), nil
	}
	blocked, err := parseSearchStringArray(args, "blocked_sites")
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"blocked_sites must be a string array of non-empty strings"}`), nil
	}
	if len(sites) > 20 || len(blocked) > 20 {
		return errorResult(`{"code":"invalid_request","message":"sites and blocked_sites support at most 20 entries"}`), nil
	}
	for _, key := range []string{"after", "before"} {
		value, present, ok := stringArgument(args, key)
		if present && !ok {
			return errorResult(`{"code":"invalid_request","message":"after and before must be strings"}`), nil
		}
		if value = strings.TrimSpace(value); value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return errorResult(`{"code":"invalid_request","message":"after and before must use YYYY-MM-DD"}`), nil
			}
		}
	}
	afterValue, _, _ := stringArgument(args, "after")
	beforeValue, _, _ := stringArgument(args, "before")
	if after, before := strings.TrimSpace(afterValue), strings.TrimSpace(beforeValue); after != "" && before != "" && after > before {
		return errorResult(`{"code":"invalid_request","message":"after must not be later than before"}`), nil
	}
	includeContent, err := optionalBoolArg(args, "include_content")
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"include_content must be boolean"}`), nil
	}
	searchReq := service.WebSearchRequest{Query: query, Limit: limit, Sites: sites, BlockedSites: blocked, After: stringArg(args, "after"), Before: stringArg(args, "before")}
	if includeContent != nil {
		searchReq.IncludeContent = *includeContent
	}
	if svcs == nil || svcs.SearchSvc == nil {
		return errorResult(`{"code":"provider_unavailable","message":"web search provider is not configured"}`), nil
	}
	result, err := svcs.SearchSvc.Search(ctx, service.SearchOperationRequest{UserID: identity.UserID, ProjectID: identity.ProjectID, TaskID: identity.TaskID, ExecutionID: identity.ExecutionID, Request: searchReq})
	if err != nil {
		return errorResult(searchErrorJSON(searchErrorCode(err, "search_failed"), safeCapabilityError(err))), nil
	}
	return textResult(result)
}

func fetchWebPageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult("fetch_web_page request parameters are required"), nil
	}
	if failure := requireMCPExecutionIdentity(ctx, "fetch_web_page", "", "", ""); failure != nil {
		return failure, nil
	}
	identity, _ := getMCPExecutionIdentity(ctx)
	if strings.TrimSpace(identity.UserID) == "" {
		return errorResult(`{"code":"execution_identity_required","message":"fetch_web_page requires an execution-scoped credential"}`), nil
	}
	args := parseArgs(req.Params.Arguments)
	if hasProviderOverride(args) {
		return errorResult(`{"code":"provider_selection_forbidden","message":"page fetch provider is controlled by Server configuration"}`), nil
	}
	urlRaw, urlPresent, urlOK := stringArgument(args, "url")
	urlValue := strings.TrimSpace(urlRaw)
	if !urlPresent || !urlOK || urlValue == "" || len([]rune(urlValue)) > 4096 {
		return errorResult(`{"code":"invalid_request","message":"url is required and must be at most 4096 characters"}`), nil
	}
	if svcs == nil || svcs.FetchSvc == nil {
		return errorResult(`{"code":"provider_unavailable","message":"page fetch provider is not configured"}`), nil
	}
	result, err := svcs.FetchSvc.Fetch(ctx, service.FetchOperationRequest{UserID: identity.UserID, ProjectID: identity.ProjectID, TaskID: identity.TaskID, ExecutionID: identity.ExecutionID, Request: service.PageFetchRequest{URL: urlValue}})
	if err != nil {
		return errorResult(searchErrorJSON(searchErrorCode(err, "fetch_failed"), safeCapabilityError(err))), nil
	}
	return textResult(result)
}

func searchErrorCode(err error, fallback string) string {
	if errors.Is(err, service.ErrSearchProviderUnavailable) || errors.Is(err, service.ErrPageFetchUnavailable) {
		return "provider_unavailable"
	}
	return fallback
}

func hasProviderOverride(args map[string]any) bool {
	for _, key := range []string{"provider", "model", "endpoint", "api_key", "apiKey", "base_url"} {
		if _, present := args[key]; present {
			return true
		}
	}
	return false
}

func stringArgument(args map[string]any, key string) (string, bool, bool) {
	raw, present := args[key]
	if !present {
		return "", false, true
	}
	value, ok := raw.(string)
	return value, true, ok
}

func safeCapabilityError(err error) string {
	if err == nil {
		return "capability failed"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "capability failed"
	}
	return message
}

func optionalLimitArg(args map[string]any) (int, error) {
	if raw, present := args["limit"]; present {
		value, ok := raw.(float64)
		if !ok || value != float64(int(value)) || value < 1 || value > 50 {
			return 0, errors.New("invalid limit")
		}
		return int(value), nil
	}
	return 0, nil
}

func parseSearchStringArray(args map[string]any, key string) ([]string, error) {
	raw, present := args[key]
	if !present {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		if values, ok := raw.([]string); ok {
			items = make([]any, len(values))
			for i, item := range values {
				items[i] = item
			}
		} else {
			return nil, errors.New("string array required")
		}
	}
	result := make([]string, len(items))
	for i, item := range items {
		value, ok := item.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, errors.New("non-empty string array required")
		}
		result[i] = strings.TrimSpace(value)
	}
	return result, nil
}

func optionalBoolArg(args map[string]any, key string) (*bool, error) {
	raw, ok := args[key]
	if !ok {
		return nil, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return nil, errors.New("boolean argument required")
	}
	return &value, nil
}

func searchErrorJSON(code, message string) string {
	payload, _ := json.Marshal(map[string]string{"code": code, "message": message})
	return string(payload)
}
