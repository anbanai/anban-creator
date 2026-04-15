package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/service"
)

// registerPublishingTools registers WeChat draft publishing and listing tools.
func registerPublishingTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "publish_draft",
		Description: "Create a WeChat news article draft. Accepts one or more articles with HTML content, title, author, digest, and optional cover image (thumb_media_id). The server handles WeChat API authentication and publishing.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID (determines WeChat credentials)"},
				"articles":   map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
					"title":     map[string]any{"type": "string", "description": "Article title"},
					"author":    map[string]any{"type": "string", "description": "Author name (optional)"},
					"digest":    map[string]any{"type": "string", "description": "Article digest/summary (optional)"},
					"content":   map[string]any{"type": "string", "description": "HTML content of the article"},
					"thumb_media_id": map[string]any{"type": "string", "description": "Cover image media ID (optional)"},
					"content_source_url": map[string]any{"type": "string", "description": "Original article URL (optional)"},
				}}, "description": "Array of articles to publish as a draft"},
			},
			"required": []any{"channel_id", "articles"},
		},
	}, publishDraftHandler)

	server.AddTool(&mcp.Tool{
		Name:        "publish_xls",
		Description: "Create a WeChat Xiaolvshu (image post/newspic) draft. Uploads local images to WeChat and creates an image-based post. Supports up to 20 images.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id":    map[string]any{"type": "string", "description": "Channel ID"},
				"title":         map[string]any{"type": "string", "description": "Post title"},
				"content":       map[string]any{"type": "string", "description": "Post text content (plain text, no HTML)"},
				"images":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Local image file paths to upload"},
				"media_ids":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Pre-uploaded WeChat media IDs (optional)"},
				"from_markdown": map[string]any{"type": "string", "description": "Markdown file path to extract images from (optional)"},
				"open_comment":  map[string]any{"type": "boolean", "description": "Enable comments (default: false)"},
				"fans_only":     map[string]any{"type": "boolean", "description": "Comments from fans only (default: false)"},
			},
			"required": []any{"channel_id", "title"},
		},
	}, publishXlsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "list_drafts",
		Description: "List WeChat drafts for a channel. Returns draft media IDs, titles, and update times.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
				"offset":     map[string]any{"type": "integer", "description": "Pagination offset (default: 0)"},
				"count":      map[string]any{"type": "integer", "description": "Number of results (default: 20)", "default": 20},
			},
			"required": []any{"channel_id"},
		},
	}, listDraftsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "list_published",
		Description: "List published WeChat articles for a channel. Returns article IDs, titles, URLs, and update times.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
				"offset":     map[string]any{"type": "integer", "description": "Pagination offset (default: 0)"},
				"count":      map[string]any{"type": "integer", "description": "Number of results (default: 20)", "default": 20},
			},
			"required": []any{"channel_id"},
		},
	}, listPublishedHandler)
}

func publishDraftHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.PublishingSvc == nil {
		return errorResult("publishing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}

	// Parse articles from JSON.
	articlesRaw, _ := args["articles"]
	articlesJSON, err := json.Marshal(articlesRaw)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal articles: %v", err)), nil
	}

	var articles []service.DraftArticleInput
	if err := json.Unmarshal(articlesJSON, &articles); err != nil {
		return errorResult(fmt.Sprintf("parse articles: %v", err)), nil
	}
	if len(articles) == 0 {
		return errorResult("at least one article is required"), nil
	}

	result, err := svcs.PublishingSvc.PublishDraft(ctx, userID, channelID, articles)
	if err != nil {
		return errorResult(fmt.Sprintf("publish draft: %v", err)), nil
	}

	return textResult(result)
}

func publishXlsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.PublishingSvc == nil {
		return errorResult("publishing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	title, _ := args["title"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if title == "" {
		return errorResult("title is required"), nil
	}

	var images []string
	if imgArr, ok := args["images"].([]any); ok {
		for _, img := range imgArr {
			if s, ok := img.(string); ok {
				images = append(images, s)
			}
		}
	}

	var mediaIDs []string
	if idArr, ok := args["media_ids"].([]any); ok {
		for _, id := range idArr {
			if s, ok := id.(string); ok {
				mediaIDs = append(mediaIDs, s)
			}
		}
	}

	publishReq := service.XlsPublishRequest{
		Title:    title,
		Content:  args["content"].(string),
		Images:   images,
		MediaIDs: mediaIDs,
	}

	if fromMD, ok := args["from_markdown"].(string); ok {
		publishReq.FromMarkdown = fromMD
	}
	if openComment, ok := args["open_comment"].(bool); ok {
		publishReq.OpenComment = openComment
	}
	if fansOnly, ok := args["fans_only"].(bool); ok {
		publishReq.FansOnly = fansOnly
	}

	result, err := svcs.PublishingSvc.PublishXls(ctx, userID, channelID, publishReq)
	if err != nil {
		return errorResult(fmt.Sprintf("publish xls: %v", err)), nil
	}

	return textResult(result)
}

func listDraftsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.PublishingSvc == nil {
		return errorResult("publishing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}

	var offset, count int64 = 0, 20
	if v, ok := args["offset"].(float64); ok {
		offset = int64(v)
	}
	if v, ok := args["count"].(float64); ok && int64(v) > 0 {
		count = int64(v)
	}

	result, err := svcs.PublishingSvc.ListDrafts(ctx, userID, channelID, offset, count)
	if err != nil {
		return errorResult(fmt.Sprintf("list drafts: %v", err)), nil
	}

	return textResult(result)
}

func listPublishedHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.PublishingSvc == nil {
		return errorResult("publishing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}

	var offset, count int64 = 0, 20
	if v, ok := args["offset"].(float64); ok {
		offset = int64(v)
	}
	if v, ok := args["count"].(float64); ok && int64(v) > 0 {
		count = int64(v)
	}

	result, err := svcs.PublishingSvc.ListPublished(ctx, userID, channelID, offset, count)
	if err != nil {
		return errorResult(fmt.Sprintf("list published: %v", err)), nil
	}

	return textResult(result)
}
