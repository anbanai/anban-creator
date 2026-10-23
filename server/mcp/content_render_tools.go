package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

// longTextHeartbeatInterval is how often the long-text tool handlers push a
// progress notification to keep the SSE stream alive. Must stay well under the
// Claude Code 60s first-byte budget. See startProgressHeartbeat.
const longTextHeartbeatInterval = 15 * time.Second

// registerContentRenderTools registers local conversion/template helpers and scoring.
// Generative writing workflows live in Skills, not MCP tools.
func registerContentRenderTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "convert_markdown",
		Description: "Convert Markdown content to WeChat-compatible HTML. When task_id is given, the theme (排版样式) comes from the task's frozen project snapshot; old rows without a snapshot fall back to legacy task/project resolution. The server renders the HTML with image placeholders.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"markdown":   map[string]any{"type": "string", "description": "Markdown content to convert"},
				"theme":      map[string]any{"type": "string", "description": "Theme name override (optional). When omitted, resolves from task_id snapshot, then the project theme."},
				"task_id":    map[string]any{"type": "string", "description": "Task ID. When provided, uses the task's frozen project snapshot as the single source of truth. Omit only for direct CLI calls, which fall back to the project theme."},
			},
			"required": []any{"project_id", "markdown"},
		},
	}, convertMarkdownHandler)

	server.AddTool(&mcp.Tool{
		Name:        "render_template",
		Description: "Render Markdown to WeChat HTML using a structured layout_plan. The renderer deterministically folds planned images and layout modules into Markdown before theme rendering. Optional layout_plan.contract_version selects an explicit module contract only when article_resources.lifecycle_mode enables that contract; omission preserves legacy behavior. When task_id is given, the theme comes from the task's frozen project snapshot; old rows without a snapshot fall back to legacy task/project resolution.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"markdown":   map[string]any{"type": "string", "description": "Article Markdown (may contain inline ![alt](url) images too)"},
				"layout_plan": map[string]any{
					"type":        "object",
					"description": "Structured layout plan: { article_type, template_name, contract_version?, slots: [...], footer?: {...} }. Set contract_version only when every selected module exposes that contract under the Server-enabled lifecycle mode. Each slot has slot_id (hero/section_opener/inline_detail/footer), section_index, image_url, image_size (full-bleed/full-width/inline), module (optional layout module name), module_vars.",
					"properties": map[string]any{
						"article_type":     map[string]any{"type": "string", "description": "long-form-essay / listicle / tutorial / story-narrative"},
						"template_name":    map[string]any{"type": "string", "description": "Template name from the templates/article/ library"},
						"contract_version": map[string]any{"type": "string", "enum": []any{"anban.layout.v1"}, "description": "Optional explicit Anban module contract; requires the Server article_resources.lifecycle_mode to enable its lifecycle"},
						"slots": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"slot_id":               map[string]any{"type": "string", "enum": []any{"hero", "section_opener", "inline_detail", "footer"}},
									"section_index":         map[string]any{"type": "integer", "description": "0-based ## section index; -1 for footer"},
									"section_title":         map[string]any{"type": "string"},
									"after_paragraph_index": map[string]any{"type": "integer", "description": "for inline_detail: 0-based paragraph within section"},
									"image_url":             map[string]any{"type": "string"},
									"image_size":            map[string]any{"type": "string", "enum": []any{"full-bleed", "full-width", "inline"}},
									"module":                map[string]any{"type": "string", "description": "layout module name (hero/quote/callout/steps/etc.)"},
									"module_vars":           map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
								},
								"required": []any{"slot_id", "section_index"},
							},
						},
						"footer": map[string]any{
							"type":        "object",
							"description": "Optional footer slot (CTA / checklist / summary module)",
							"properties": map[string]any{
								"module":      map[string]any{"type": "string"},
								"image_url":   map[string]any{"type": "string"},
								"module_vars": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
							},
						},
					},
					"required": []any{"article_type", "slots"},
				},
				"theme":   map[string]any{"type": "string", "description": "Theme name override (optional). When omitted, resolves from task_id snapshot, then the project theme."},
				"task_id": map[string]any{"type": "string", "description": "Task ID. When provided, uses the task's frozen project snapshot as the single source of truth. Omit only for direct CLI calls, which fall back to the project theme."},
			},
			"required": []any{"project_id", "markdown", "layout_plan"},
		},
	}, renderTemplateHandler)

	server.AddTool(&mcp.Tool{
		Name:        "inspect_article",
		Description: "Inspect WeChat article structure, layout contracts, image URLs, HTML safety, and readiness without writing files or changing task state. Rendered and delivery checks verify HTML against a fresh Server render using the task's frozen theme.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":     map[string]any{"type": "string"},
				"task_id":        map[string]any{"type": "string", "description": "Optional task ID used to resolve the frozen theme"},
				"markdown":       map[string]any{"type": "string"},
				"layout_plan":    map[string]any{"type": "object", "description": "Optional structured layout plan"},
				"target":         map[string]any{"type": "string", "enum": []any{"source", "rendered", "delivery"}, "description": "Check target; defaults to source"},
				"html":           map[string]any{"type": "string", "description": "Exact Server-returned output/05-article.html; required for rendered and delivery targets"},
				"title":          map[string]any{"type": "string"},
				"author":         map[string]any{"type": "string"},
				"digest":         map[string]any{"type": "string"},
				"evidence_paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []any{"project_id", "markdown"},
		},
	}, inspectArticleHandler)

	server.AddTool(&mcp.Tool{
		Name:        "score_article",
		Description: "Calculate viral potential score based on article engagement metrics (reads, likes, shares, comments, collects). Returns score, level, rates, and optimization recommendations.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"read_count":    map[string]any{"type": "integer", "description": "Number of reads"},
				"like_count":    map[string]any{"type": "integer", "description": "Number of likes"},
				"share_count":   map[string]any{"type": "integer", "description": "Number of shares (default: 0)"},
				"comment_count": map[string]any{"type": "integer", "description": "Number of comments (default: 0)"},
				"collect_count": map[string]any{"type": "integer", "description": "Number of collects/bookmarks (default: 0)"},
				"topic":         map[string]any{"type": "string", "description": "Article topic (optional, for context in recommendations)"},
			},
			"required": []any{"read_count", "like_count"},
		},
	}, scoreArticleHandler)
}

func inspectArticleHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ContentRenderSvc == nil {
		return errorResult("content render service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	projectID := stringArg(args, "project_id")
	markdown := stringArg(args, "markdown")
	if projectID == "" || markdown == "" {
		return errorResult("project_id and markdown are required"), nil
	}
	target := stringArg(args, "target")
	if target == "" {
		target = "source"
	}
	var layoutPlan *service.LayoutPlan
	if rawPlan, ok := args["layout_plan"]; ok && rawPlan != nil {
		parsed, err := service.ParseLayoutPlan(rawPlan)
		if err != nil {
			return errorResult(fmt.Sprintf("invalid layout_plan: %v", err)), nil
		}
		layoutPlan = parsed
	}
	result, err := svcs.ContentRenderSvc.InspectArticle(ctx, getUserID(ctx), service.InspectArticleRequest{
		ProjectID:     projectID,
		TaskID:        stringArg(args, "task_id"),
		Markdown:      markdown,
		LayoutPlan:    layoutPlan,
		Target:        target,
		HTML:          stringArg(args, "html"),
		Title:         stringArg(args, "title"),
		Author:        stringArg(args, "author"),
		Digest:        stringArg(args, "digest"),
		EvidencePaths: stringSliceArg(args["evidence_paths"]),
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}

func stringSliceArg(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func convertMarkdownHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ContentRenderSvc == nil {
		return errorResult("content render service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	markdown, _ := args["markdown"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if markdown == "" {
		return errorResult("markdown is required"), nil
	}

	theme, _ := args["theme"].(string)
	taskID, _ := args["task_id"].(string)

	logLongTextToolStart("convert_markdown", req)
	defer logLongTextToolEnd("convert_markdown", time.Now())
	stop := startProgressHeartbeat(ctx, req.Session, req.Params.GetProgressToken(), "convert_markdown", longTextHeartbeatInterval)
	defer stop()

	result, err := svcs.ContentRenderSvc.ConvertMarkdown(ctx, userID, projectID, markdown, theme, taskID)
	if err != nil {
		return billingError("convert markdown", err), nil
	}

	return textResult(result)
}

func renderTemplateHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ContentRenderSvc == nil {
		return errorResult("content render service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	markdown, _ := args["markdown"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if markdown == "" {
		return errorResult("markdown is required"), nil
	}

	layoutPlan, err := service.ParseLayoutPlan(args["layout_plan"])
	if err != nil {
		return errorResult(fmt.Sprintf("invalid layout_plan: %v", err)), nil
	}

	theme, _ := args["theme"].(string)
	taskID, _ := args["task_id"].(string)

	logLongTextToolStart("render_template", req)
	defer logLongTextToolEnd("render_template", time.Now())
	stop := startProgressHeartbeat(ctx, req.Session, req.Params.GetProgressToken(), "render_template", longTextHeartbeatInterval)
	defer stop()

	result, err := svcs.ContentRenderSvc.RenderTemplate(ctx, userID, projectID, markdown, layoutPlan, theme, taskID)
	if err != nil {
		return billingError("render template", err), nil
	}

	return textResult(result)
}

func scoreArticleHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ArticleScoreSvc == nil {
		return errorResult("article score service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	result, err := svcs.ArticleScoreSvc.Score(service.ArticleScoreRequest{
		ReadCount: int64Arg(args, "read_count"), LikeCount: int64Arg(args, "like_count"),
		ShareCount: int64Arg(args, "share_count"), CommentCount: int64Arg(args, "comment_count"),
		CollectCount: int64Arg(args, "collect_count"), Topic: stringArg(args, "topic"),
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}

func int64Arg(args map[string]any, key string) int64 {
	value, _ := args[key].(float64)
	return int64(value)
}
