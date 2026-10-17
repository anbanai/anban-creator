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
				"query":           map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
				"limit":           map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
				"ori_question":    map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
				"localization":    map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
				"time_range":      map[string]any{"type": "string"},
				"sites":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 20},
				"block_hosts":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 5},
				"need_content":    map[string]any{"type": "boolean"},
				"need_url":        map[string]any{"type": "boolean"},
				"auth_info_level": map[string]any{"type": "integer", "minimum": 0, "maximum": 1},
				"content_formats": map[string]any{"type": "string", "enum": []any{"text", "markdown"}},
				"industry":        map[string]any{"type": "string", "enum": []any{"finance", "game", "health", "gov"}},
				"query_rewrite":   map[string]any{"type": "boolean"},
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
	originalQuestion, err := optionalSearchStringArg(args, "ori_question", 128)
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"ori_question must be a non-empty string of at most 128 characters"}`), nil
	}
	localization, err := optionalSearchStringArg(args, "localization", 128)
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"localization must be a non-empty string of at most 128 characters"}`), nil
	}
	if _, providerOverride := args["provider"]; providerOverride {
		return errorResult(`{"code":"provider_selection_forbidden","message":"search provider is controlled by Server configuration"}`), nil
	}
	// A zero limit means "use the Server-configured provider default". Keeping
	// that decision inside SearchService preserves the MCP one-capability boundary.
	limit := 0
	if rawLimit, present := args["limit"]; present {
		value, ok := rawLimit.(float64)
		if !ok || value != float64(int(value)) || value < 1 || value > 50 {
			return errorResult(`{"code":"invalid_request","message":"limit must be an integer between 1 and 50"}`), nil
		}
		limit = int(value)
	}
	needContent, err := optionalBoolArg(args, "need_content")
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"need_content must be boolean"}`), nil
	}
	needURL, err := optionalBoolArg(args, "need_url")
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"need_url must be boolean"}`), nil
	}
	queryRewrite, err := optionalBoolArg(args, "query_rewrite")
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"query_rewrite must be boolean"}`), nil
	}
	authInfoLevel, authInfoLevelSet, err := optionalIntArg(args, "auth_info_level")
	if err != nil || (authInfoLevel != 0 && authInfoLevel != 1) {
		return errorResult(`{"code":"invalid_request","message":"auth_info_level must be 0 or 1"}`), nil
	}
	domains, err := parseSearchStringArray(args, "sites")
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"sites must be a string array of non-empty strings"}`), nil
	}
	blockHosts, err := parseSearchStringArray(args, "block_hosts")
	if err != nil {
		return errorResult(`{"code":"invalid_request","message":"block_hosts must be a string array of non-empty strings"}`), nil
	}
	if svcs == nil || svcs.SearchSvc == nil {
		return errorResult(`{"code":"provider_unavailable","message":"search provider is not configured"}`), nil
	}
	result, err := svcs.SearchSvc.Search(ctx, service.SearchOperationRequest{UserID: identity.UserID, ProjectID: identity.ProjectID, TaskID: identity.TaskID, ExecutionID: identity.ExecutionID, Request: service.SearchRequest{Query: query, Limit: limit, OriginalQuestion: originalQuestion, Localization: localization, TimeRange: stringArg(args, "time_range"), Domains: domains, BlockHosts: blockHosts, NeedContent: needContent, NeedURL: needURL, AuthInfoLevel: authInfoLevel, AuthInfoLevelSet: authInfoLevelSet, ContentFormats: stringArg(args, "content_formats"), Industry: stringArg(args, "industry"), QueryRewrite: queryRewrite}})
	if err != nil {
		code := "search_failed"
		if errors.Is(err, service.ErrSearchProviderUnavailable) {
			code = "provider_unavailable"
		}
		return errorResult(searchErrorJSON(code, err.Error())), nil
	}
	return textResult(result)
}

func optionalSearchStringArg(args map[string]any, key string, maxLength int) (string, error) {
	raw, present := args[key]
	if !present {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", errors.New("string required")
	}
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > maxLength {
		return "", errors.New("invalid string length")
	}
	return value, nil
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

func optionalIntArg(args map[string]any, key string) (int, bool, error) {
	raw, ok := args[key]
	if !ok {
		return 0, false, nil
	}
	value, ok := raw.(float64)
	if !ok || value != float64(int(value)) {
		return 0, true, errors.New("integer argument required")
	}
	return int(value), true, nil
}

func searchErrorJSON(code, message string) string {
	payload, _ := json.Marshal(map[string]string{"code": code, "message": message})
	return string(payload)
}
