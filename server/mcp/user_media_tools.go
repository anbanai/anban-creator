package mcp

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerUserMediaTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "search_user_media_assets",
		Description: "Search the authenticated user's ready image and video media-library assets. Project-tag matches from the current execution or interactive session are ranked first. Each candidate has a stable asset_id; when presenting choices, use media_asset:<id> as the option label and wait for user confirmation before using it.",
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"query":      map[string]any{"type": "string", "maxLength": 200},
				"media_type": map[string]any{"type": "string", "enum": []any{"image", "video"}},
				"limit":      map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
			},
		},
	}, searchUserMediaAssetsHandler)
}

func searchUserMediaAssetsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult(`{"code":"invalid_request","message":"request parameters are required"}`), nil
	}
	if svcs == nil || svcs.UserMediaSvc == nil {
		return errorResult(`{"code":"provider_unavailable","message":"media library is not configured"}`), nil
	}
	userID, projectID := "", ""
	if identity, ok := getMCPExecutionIdentity(ctx); ok {
		userID, projectID = identity.UserID, identity.ProjectID
	} else if identity, ok := getMCPInteractiveIdentity(ctx); ok {
		userID, projectID = identity.UserID, identity.ProjectID
	} else {
		userID = getUserID(ctx)
	}
	if strings.TrimSpace(userID) == "" {
		return errorResult(`{"code":"identity_required","message":"an authenticated user identity is required"}`), nil
	}
	args := parseArgs(req.Params.Arguments)
	if _, present := args["project_id"]; present {
		return errorResult(`{"code":"invalid_request","message":"project_id is derived from the authenticated session"}`), nil
	}
	query, present, ok := stringArgument(args, "query")
	if present && (!ok || len([]rune(strings.TrimSpace(query))) > 200) {
		return errorResult(`{"code":"invalid_request","message":"query must be a string of at most 200 characters"}`), nil
	}
	mediaType, present, ok := stringArgument(args, "media_type")
	if present && (!ok || mediaType != "image" && mediaType != "video") {
		return errorResult(`{"code":"invalid_request","message":"media_type must be image or video"}`), nil
	}
	limit := 20
	if raw, present := args["limit"]; present {
		value, ok := raw.(float64)
		if !ok || value < 1 || value > 20 || value != float64(int(value)) {
			return errorResult(`{"code":"invalid_request","message":"limit must be an integer between 1 and 20"}`), nil
		}
		limit = int(value)
	}
	items, err := svcs.UserMediaSvc.SearchCandidates(ctx, userID, projectID, strings.TrimSpace(query), strings.TrimSpace(mediaType), limit)
	if err != nil {
		return errorResult(`{"code":"media_search_failed","message":"failed to search media library"}`), nil
	}
	// The library row ID is the stable ID that Studio can resolve through
	// GET /media-assets/:id. Keep the immutable storage Asset ID internal to
	// the API view so agents cannot accidentally produce an unresolvable card.
	candidates := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		candidate := map[string]any{
			"asset_id":         item.ID,
			"file_name":        item.FileName,
			"content_type":     item.ContentType,
			"media_type":       item.MediaType,
			"project_id":       item.ProjectID,
			"description":      item.Description,
			"recommended_uses": item.RecommendedUses,
			"usage_notes":      item.UsageNotes,
			"preview_url":      item.PreviewURL,
			"option_label":     item.OptionLabel,
		}
		candidates = append(candidates, candidate)
	}
	return textResult(map[string]any{"items": candidates, "selection": map[string]any{"option_label_prefix": "media_asset:", "requires_confirmation": true}})
}
