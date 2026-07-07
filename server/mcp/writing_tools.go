package mcp

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

// longTextHeartbeatInterval is how often the long-text tool handlers push a
// progress notification to keep the SSE stream alive. Must stay well under the
// Claude Code 60s first-byte budget. See startProgressHeartbeat.
const longTextHeartbeatInterval = 15 * time.Second

// registerWritingTools registers local conversion/template helpers and scoring.
// Generative writing workflows live in Skills, not MCP tools.
func registerWritingTools(server *mcp.Server) {
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
		Description: "Render Markdown to WeChat HTML using a structured layout_plan (template-based). Unlike convert_markdown (which lets the renderer freely decide image placement and layout), render_template deterministically folds planned images and layout modules into the Markdown before theme rendering. When task_id is given, the theme (排版样式) comes from the task's frozen project snapshot; old rows without a snapshot fall back to legacy task/project resolution. Use this when visual-rhythm-plan.md dictates where each image/module goes (hero / section_opener / inline_detail / footer).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"markdown":   map[string]any{"type": "string", "description": "Article Markdown (may contain inline ![alt](url) images too)"},
				"layout_plan": map[string]any{
					"type":        "object",
					"description": "Structured layout plan: { article_type, template_name, slots: [...], footer?: {...} }. Each slot has slot_id (hero/section_opener/inline_detail/footer), section_index, image_url, image_size (full-bleed/full-width/inline), module (optional layout module name), module_vars.",
					"properties": map[string]any{
						"article_type":  map[string]any{"type": "string", "description": "long-form-essay / listicle / tutorial / story-narrative"},
						"template_name": map[string]any{"type": "string", "description": "Template name from the templates/article/ library"},
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

func convertMarkdownHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WritingSvc == nil {
		return errorResult("writing service not available"), nil
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

	result, err := svcs.WritingSvc.ConvertMarkdown(ctx, userID, projectID, markdown, theme, taskID)
	if err != nil {
		return billingError("convert markdown", err), nil
	}

	return textResult(result)
}

func renderTemplateHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WritingSvc == nil {
		return errorResult("writing service not available"), nil
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

	result, err := svcs.WritingSvc.RenderTemplate(ctx, userID, projectID, markdown, layoutPlan, theme, taskID)
	if err != nil {
		return billingError("render template", err), nil
	}

	return textResult(result)
}

func scoreArticleHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)

	readCount := int64(0)
	likeCount := int64(0)
	shareCount := int64(0)
	commentCount := int64(0)
	collectCount := int64(0)

	if v, ok := args["read_count"].(float64); ok {
		readCount = int64(v)
	}
	if v, ok := args["like_count"].(float64); ok {
		likeCount = int64(v)
	}
	if v, ok := args["share_count"].(float64); ok {
		shareCount = int64(v)
	}
	if v, ok := args["comment_count"].(float64); ok {
		commentCount = int64(v)
	}
	if v, ok := args["collect_count"].(float64); ok {
		collectCount = int64(v)
	}

	if readCount <= 0 {
		return errorResult("read_count must be positive"), nil
	}
	if likeCount < 0 {
		return errorResult("like_count must be non-negative"), nil
	}

	topic, _ := args["topic"].(string)

	// Compute engagement rates.
	engagementRate := float64(likeCount+shareCount+commentCount) / float64(readCount)
	shareRate := float64(shareCount) / float64(readCount)
	likeRate := float64(likeCount) / float64(readCount)
	commentRate := float64(commentCount) / float64(readCount)

	// Calculate viral score.
	score := calculateViralScore(readCount, engagementRate, shareRate, likeRate, commentRate)
	level := getViralLevel(score)
	recommendations := generateScoreRecommendations(engagementRate, shareRate, likeRate, commentRate)

	result := map[string]any{
		"score":           score,
		"level":           level,
		"topic":           topic,
		"read_count":      readCount,
		"like_count":      likeCount,
		"share_count":     shareCount,
		"comment_count":   commentCount,
		"collect_count":   collectCount,
		"engagement_rate": engagementRate,
		"share_rate":      shareRate,
		"like_rate":       likeRate,
		"comment_rate":    commentRate,
		"recommendations": recommendations,
	}

	return textResult(result)
}

// ---------------------------------------------------------------------------
// Scoring helpers (pure computation, no external dependencies)
// ---------------------------------------------------------------------------

func calculateViralScore(readCount int64, engagementRate, shareRate, likeRate, commentRate float64) float64 {
	score := 0.0

	// Read count score (30%).
	readScore := math.Min(math.Log10(float64(readCount))*10, 100)
	score += readScore * 0.3

	// Engagement rate score (30%).
	score += math.Min(engagementRate*1000, 30)

	// Share rate score (25%).
	score += math.Min(shareRate*2500, 25)

	// Comment rate score (15%).
	score += math.Min(commentRate*3750, 15)

	return math.Min(score, 100)
}

func getViralLevel(score float64) string {
	switch {
	case score >= 90:
		return "超级爆款"
	case score >= 80:
		return "热门爆款"
	case score >= 70:
		return "优质内容"
	case score >= 60:
		return "潜力内容"
	case score >= 50:
		return "普通内容"
	default:
		return "待优化"
	}
}

func generateScoreRecommendations(engagementRate, shareRate, likeRate, commentRate float64) []string {
	var recs []string

	if engagementRate < 0.02 {
		recs = append(recs, "互动率偏低，建议增加互动引导或话题讨论点")
	}

	if shareRate < 0.01 {
		recs = append(recs, "分享率偏低，建议增加实用价值或情感共鸣点")
	}

	if commentRate < likeRate*0.05 {
		recs = append(recs, "评论率偏低，建议增加争议性或思考性内容")
	}

	if len(recs) == 0 {
		recs = append(recs, "各项指标表现良好，继续保持！")
	}

	return recs
}
