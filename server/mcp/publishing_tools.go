package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	appwechat "github.com/anbanai/anban-creator/app/wechat"
	"github.com/anbanai/anban-creator/server/service"
)

// registerPublishingTools registers WeChat draft publishing and listing tools.
func registerPublishingTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "create_draft",
		Description: "Create a WeChat news article draft. Accepts exactly one article with HTML content, title, author, digest, and optional cover image (thumb_media_id). The server handles WeChat API authentication and publishing.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":    map[string]any{"type": "string", "description": "Article task ID"},
				"project_id": map[string]any{"type": "string", "description": "Project ID (determines WeChat credentials)"},
				"articles": map[string]any{"type": "array", "minItems": 1, "maxItems": 1, "items": map[string]any{"type": "object", "properties": map[string]any{
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
		return createDraftErrorResult(createDraftFailure{Code: "create_draft_provider_failure", Message: "WeChat publication service not available", Hint: "Retry after the publication service is available", Retryable: true}), nil
	}
	userID := strings.TrimSpace(getUserID(ctx))
	if userID == "" {
		return createDraftErrorResult(createDraftFailure{Code: "create_draft_auth_required", Message: "authenticated user is required", Hint: "Provide a valid authenticated MCP session", Retryable: false}), nil
	}
	args := parseArgs(req.Params.Arguments)

	taskID, _ := args["task_id"].(string)
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return createDraftErrorResult(createDraftFailure{Code: "create_draft_invalid_payload", Message: "task_id is required", Hint: "Provide the Article task_id", Retryable: false}), nil
	}
	projectID, _ := args["project_id"].(string)
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return createDraftErrorResult(createDraftFailure{Code: "create_draft_invalid_payload", Message: "project_id is required", Hint: "Provide the Article project_id", Retryable: false}), nil
	}

	// Parse articles from JSON.
	articlesRaw, _ := args["articles"]
	articlesJSON, err := json.Marshal(articlesRaw)
	if err != nil {
		return createDraftErrorResult(createDraftFailure{Code: "create_draft_invalid_payload", Message: "articles must be a JSON array", Hint: "Provide exactly one article object", Retryable: false}), nil
	}

	var articles []appwechat.DraftArticle
	if err := json.Unmarshal(articlesJSON, &articles); err != nil {
		return createDraftErrorResult(createDraftFailure{Code: "create_draft_invalid_payload", Message: "articles must be a JSON array", Hint: "Provide exactly one article object", Retryable: false}), nil
	}
	if len(articles) != 1 {
		return createDraftErrorResult(createDraftFailure{Code: "create_draft_invalid_payload", Message: "exactly one article is required", Hint: "Provide one article in articles", Retryable: false}), nil
	}

	publication, err := svcs.WechatPublicationSvc.CreateDraft(ctx, userID, taskID, projectID, appwechat.DraftAddRequest{Articles: articles})
	if err != nil {
		return createDraftErrorResult(classifyCreateDraftFailure(err)), nil
	}

	return textResult(createDraftResult{DraftMediaID: publication.DraftMediaID, Status: publication.Status})
}

type createDraftFailure struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Hint      string `json:"hint"`
	Retryable bool   `json:"retryable"`
}

func createDraftErrorResult(failure createDraftFailure) *mcp.CallToolResult {
	payload, err := json.Marshal(failure)
	if err != nil {
		return errorResult(`{"code":"create_draft_provider_failure","message":"create draft failed","hint":"Retry after checking server logs","retryable":true}`)
	}
	return errorResult(string(payload))
}

func classifyCreateDraftFailure(err error) createDraftFailure {
	switch {
	case errors.Is(err, service.ErrWechatPublicationInvalidPayload):
		return createDraftFailure{Code: "create_draft_invalid_payload", Message: "draft payload is invalid", Hint: "Provide one article with a title, content, and distinct body images", Retryable: false}
	case errors.Is(err, service.ErrWechatPublicationNotFound):
		return createDraftFailure{Code: "create_draft_not_found", Message: "task or project was not found", Hint: "Use an existing Article task and project owned by the authenticated user", Retryable: false}
	case errors.Is(err, service.ErrWechatPublicationProjectMismatch):
		return createDraftFailure{Code: "create_draft_project_mismatch", Message: "task and project do not match", Hint: "Use the project_id associated with the task", Retryable: false}
	case errors.Is(err, service.ErrWechatPublicationForbidden):
		return createDraftFailure{Code: "create_draft_forbidden", Message: "task or project is not owned by the authenticated user", Hint: "Use resources owned by the authenticated user", Retryable: false}
	case errors.Is(err, service.ErrWechatPublicationModeConflict):
		return createDraftFailure{Code: "create_draft_disabled", Message: "draft creation is disabled for this project", Hint: "Enable the project's WeChat draft mode before retrying", Retryable: false}
	case errors.Is(err, service.ErrWechatPublicationDraftUnsupported):
		return createDraftFailure{Code: "create_draft_unsupported", Message: "the configured WeChat account does not support draft creation", Hint: "Enable the WeChat draft API capability, then rerun the current task", Retryable: false}
	case errors.Is(err, service.ErrWechatPublicationDraftRejected):
		return classifyDefinitiveDraftRejection(err)
	case errors.Is(err, service.ErrWechatPublicationDraftFailed):
		return createDraftFailure{Code: "create_draft_reconciliation_failed", Message: "the previous WeChat draft outcome could not be confirmed", Hint: "Automatic reconciliation has stopped; create a new task or finish the draft manually in WeChat", Retryable: false}
	case errors.Is(err, service.ErrWechatPublicationConflict):
		return createDraftFailure{Code: "create_draft_conflict", Message: "a different draft request already exists for this task", Hint: "Replay the original request or use a new task", Retryable: false}
	case errors.Is(err, service.ErrWechatPublicationPending):
		return createDraftFailure{Code: "create_draft_pending_reconciliation", Message: "the previous WeChat draft outcome is still being reconciled", Hint: "Retry after reconciliation completes; do not submit another draft", Retryable: true}
	default:
		if strings.Contains(err.Error(), "draft title and content are required") || strings.Contains(err.Error(), "exactly one draft article is required") || strings.Contains(err.Error(), "内容配图重复") {
			return createDraftFailure{Code: "create_draft_invalid_payload", Message: "draft article payload is invalid", Hint: "Provide one article with a title, content, and distinct content image URLs", Retryable: false}
		}
		return createDraftFailure{Code: "create_draft_provider_failure", Message: "WeChat draft creation failed", Hint: "Check the WeChat account configuration and retry when the provider is available", Retryable: true}
	}
}

func classifyDefinitiveDraftRejection(err error) createDraftFailure {
	var apiErr *appwechat.WechatAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrCode {
		case 40001, 40125:
			return createDraftFailure{Code: "create_draft_rejected", Message: "WeChat rejected draft creation because the account credentials are invalid", Hint: "Correct the AppID and AppSecret, then rerun the current task", Retryable: false}
		case 40164:
			return createDraftFailure{Code: "create_draft_rejected", Message: "WeChat rejected draft creation because the server IP is not allowed", Hint: "Add the server egress IP to the WeChat API allowlist, then rerun the current task", Retryable: false}
		}
	}
	return createDraftFailure{Code: "create_draft_rejected", Message: "WeChat definitively rejected draft creation", Hint: "Correct the WeChat account configuration indicated by the error, then rerun the current task", Retryable: false}
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
