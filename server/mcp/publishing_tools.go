package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	appwechat "github.com/anbanai/anban-creator/app/wechat"
)

// registerPublishingTools registers WeChat draft publishing and listing tools.
func registerPublishingTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "create_draft",
		Description: "Create a WeChat news article draft. Accepts one or more articles with HTML content, title, author, digest, and optional cover image (thumb_media_id). The server handles WeChat API authentication and publishing.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":    map[string]any{"type": "string", "description": "Article task ID"},
				"project_id": map[string]any{"type": "string", "description": "Project ID (determines WeChat credentials)"},
				"articles": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "object", "properties": map[string]any{
					"title":              map[string]any{"type": "string", "description": "Article title"},
					"author":             map[string]any{"type": "string", "description": "Author name (optional)"},
					"digest":             map[string]any{"type": "string", "description": "Article digest/summary (optional)"},
					"content":            map[string]any{"type": "string", "description": "HTML content of the article"},
					"thumb_media_id":     map[string]any{"type": "string", "description": "Cover image media ID (optional)"},
					"content_source_url": map[string]any{"type": "string", "description": "Original article URL (optional)"},
				}, "required": []any{"title", "content"}}, "description": "Array of articles to create as a draft"},
			},
			"required": []any{"task_id", "project_id", "articles"},
		},
	}, createDraftHandler)

	server.AddTool(&mcp.Tool{
		Name:        "list_drafts",
		Description: "List WeChat drafts for a project. Returns draft media IDs, titles, and update times.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"offset":     map[string]any{"type": "integer", "description": "Pagination offset (default: 0)"},
				"count":      map[string]any{"type": "integer", "description": "Number of results (default: 20)", "default": 20},
			},
			"required": []any{"project_id"},
		},
	}, listDraftsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "list_published_articles",
		Description: "List published WeChat articles for a project. Returns article IDs, titles, URLs, and update times.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"offset":     map[string]any{"type": "integer", "description": "Pagination offset (default: 0)"},
				"count":      map[string]any{"type": "integer", "description": "Number of results (default: 20)", "default": 20},
			},
			"required": []any{"project_id"},
		},
	}, listPublishedHandler)
}

type createDraftResult struct {
	DraftMediaID string `json:"draft_media_id"`
	Status       string `json:"status"`
}

func createDraftHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WechatPublicationSvc == nil {
		return errorResult("WeChat publication service not available"), nil
	}
	userID := strings.TrimSpace(getUserID(ctx))
	if userID == "" {
		return errorResult("authenticated user is required"), nil
	}
	args := parseArgs(req.Params.Arguments)

	taskID, _ := args["task_id"].(string)
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	projectID, _ := args["project_id"].(string)
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}

	// Parse articles from JSON.
	articlesRaw, _ := args["articles"]
	articlesJSON, err := json.Marshal(articlesRaw)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal articles: %v", err)), nil
	}

	var articles []appwechat.DraftArticle
	if err := json.Unmarshal(articlesJSON, &articles); err != nil {
		return errorResult(fmt.Sprintf("parse articles: %v", err)), nil
	}
	if len(articles) == 0 {
		return errorResult("at least one article is required"), nil
	}

	publication, err := svcs.WechatPublicationSvc.CreateDraft(ctx, userID, taskID, projectID, appwechat.DraftAddRequest{Articles: articles})
	if err != nil {
		return errorResult(fmt.Sprintf("create draft: %v", err)), nil
	}

	return textResult(createDraftResult{DraftMediaID: publication.DraftMediaID, Status: publication.Status})
}

func listDraftsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.PublishingSvc == nil {
		return errorResult("publishing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}

	var offset, count int64 = 0, 20
	if v, ok := args["offset"].(float64); ok {
		offset = int64(v)
	}
	if v, ok := args["count"].(float64); ok && int64(v) > 0 {
		count = int64(v)
	}

	result, err := svcs.PublishingSvc.ListDrafts(ctx, userID, projectID, offset, count)
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

	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}

	var offset, count int64 = 0, 20
	if v, ok := args["offset"].(float64); ok {
		offset = int64(v)
	}
	if v, ok := args["count"].(float64); ok && int64(v) > 0 {
		count = int64(v)
	}

	result, err := svcs.PublishingSvc.ListPublished(ctx, userID, projectID, offset, count)
	if err != nil {
		return errorResult(fmt.Sprintf("list published: %v", err)), nil
	}

	return textResult(result)
}
