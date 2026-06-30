package mcp

import (
	"context"
	"fmt"
	"math"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/service"
)

// longTextHeartbeatInterval is how often the long-text tool handlers push a
// progress notification to keep the SSE stream alive. Must stay well under the
// Claude Code 60s first-byte budget. See startProgressHeartbeat.
const longTextHeartbeatInterval = 15 * time.Second

// streamingProgressInterval is the minimum gap between two stream-progress
// notifications during a streaming write_article. A token stream fires many
// small deltas per second; we coalesce into at most one NotifyProgress per
// interval so the client gets a real, character-count-based progress signal
// without flooding the SSE stream. The 15s heartbeat still runs underneath as a
// floor for non-streaming tools.
const streamingProgressInterval = time.Second

// newStreamingProgressRelayer returns an onDelta callback that relays article
// generation progress to the MCP client, throttled to at most one
// NotifyProgress per streamingProgressInterval. Each notification carries how
// many characters have been generated so far — a real progress signal, unlike
// the heartbeat's static "生成中…". No-op when there is no progress routing
// target (token nil / sess nil). The returned callback is invoked only from the
// single-threaded stream loop inside CompleteStream, so its throttling state
// needs no lock.
func newStreamingProgressRelayer(ctx context.Context, sess progressNotifier, token any) func(string) {
	if token == nil || sess == nil {
		return func(string) {}
	}
	var (
		chars      int
		lastNotify time.Time
	)
	return func(delta string) {
		chars += utf8.RuneCountInString(delta)
		if time.Since(lastNotify) < streamingProgressInterval {
			return
		}
		lastNotify = time.Now()
		n := chars
		_ = sess.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token,
			Message:       fmt.Sprintf("write_article 生成中… 已生成 %d 字", n),
		})
	}
}

// registerWritingTools registers article writing, conversion, topic research, SEO, outline generation, and scoring tools.
func registerWritingTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "write_article",
		Description: "Generate an article using the resolved writing style and an LLM. When task_id is given, the writing style comes from the task's frozen project snapshot; old rows without a snapshot fall back to legacy task/project resolution. The server assembles the writing prompt from the resolved writer resource, calls the LLM, and returns the article text in Markdown format.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":   map[string]any{"type": "string", "description": "Project ID"},
				"topic":        map[string]any{"type": "string", "description": "Article topic or idea to write about"},
				"input_type":   map[string]any{"type": "string", "enum": []any{"idea", "fragment", "outline", "title"}, "description": "Type of input content (default: idea)"},
				"article_type": map[string]any{"type": "string", "enum": []any{"essay", "commentary", "story", "tutorial", "review"}, "description": "Article type (default: essay)"},
				"length":       map[string]any{"type": "string", "enum": []any{"short", "medium", "long"}, "description": "Desired article length (default: medium)"},
				"task_id":      map[string]any{"type": "string", "description": "Task ID. When provided, uses the task's frozen project snapshot as the single source of truth. Omit only for direct CLI calls, which fall back to the project's writer."},
			},
			"required": []any{"project_id", "topic"},
		},
	}, writeArticleHandler)

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
		Description: "Render Markdown to WeChat HTML using a structured layout_plan (template-based). Unlike convert_markdown (which lets the renderer freely decide image placement and layout), render_template annotates the markdown with explicit [SLOT: ...] markers so each image is placed at the planned position and each section is wrapped in the specified layout module. When task_id is given, the theme (排版样式) comes from the task's frozen project snapshot; old rows without a snapshot fall back to legacy task/project resolution. Use this when you have a visual-rhythm-plan that dictates where each image goes (hero / section_opener / inline_detail / footer).",
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
		Name:        "research_topics",
		Description: "Generate topic suggestions based on a project's instructions positioning and keywords. Returns an array of topics with viral scores, angles, and keywords.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID (uses its instructions positioning and keywords)"},
				"keywords":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Override keywords (optional, uses project keywords by default)"},
				"domain":     map[string]any{"type": "string", "description": "Domain or niche focus (optional)"},
				"count":      map[string]any{"type": "integer", "description": "Number of topics to generate (1-20, default 5)", "minimum": 1, "maximum": 20},
			},
			"required": []any{"project_id"},
		},
	}, researchTopicsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "optimize_seo",
		Description: "Optimize a title and keywords for search engine ranking. Returns the optimized title, keywords, and a summary.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"content":    map[string]any{"type": "string", "description": "Article content to optimize"},
				"title":      map[string]any{"type": "string", "description": "Original title to optimize"},
				"keywords":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Current keywords (optional)"},
			},
			"required": []any{"project_id", "content", "title"},
		},
	}, optimizeSEOHandler)

	server.AddTool(&mcp.Tool{
		Name:        "generate_outline",
		Description: "Generate a structured article outline/framework based on a topic and template. When task_id is given, the writing style comes from the task's frozen project snapshot; old rows without a snapshot fall back to legacy task/project resolution. Returns title, hook, sections, key points, CTA, and viral elements.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID (uses its keywords)"},
				"topic":      map[string]any{"type": "string", "description": "Article topic or idea"},
				"template":   map[string]any{"type": "string", "enum": []any{"authoritative", "comparison", "cultural", "practical"}, "description": "Outline template type (default: authoritative)"},
				"style":      map[string]any{"type": "string", "description": "Writing style override (optional). When omitted, resolves from task_id snapshot, then the project's writer."},
				"task_id":    map[string]any{"type": "string", "description": "Task ID. When provided, uses the task's frozen project snapshot as the single source of truth. Omit only for direct CLI calls."},
			},
			"required": []any{"project_id", "topic"},
		},
	}, generateOutlineHandler)

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

func writeArticleHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WritingSvc == nil {
		return errorResult("writing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	topic, _ := args["topic"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if topic == "" {
		return errorResult("topic is required"), nil
	}

	inputType, _ := args["input_type"].(string)
	articleType, _ := args["article_type"].(string)
	length, _ := args["length"].(string)
	taskID, _ := args["task_id"].(string)

	logLongTextToolStart("write_article", req)
	defer logLongTextToolEnd("write_article", time.Now())
	stop := startProgressHeartbeat(ctx, req.Session, req.Params.GetProgressToken(), "write_article", longTextHeartbeatInterval)
	defer stop()

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeArticleWrite, provider, mdl, 1); err != nil {
		return billingError("write article", err), nil
	}

	// Stream the generation so a long article writes progress to the client
	// chunk-by-chunk (no single blocking deadline). Falls back to blocking
	// Complete when the resolved client can't stream.
	onDelta := newStreamingProgressRelayer(ctx, req.Session, req.Params.GetProgressToken())
	result, err := svcs.WritingSvc.WriteArticleStream(ctx, userID, projectID, topic, inputType, articleType, length, taskID, onDelta)
	if err != nil {
		return billingError("write article", err), nil
	}

	return textResult(result)
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

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeConvert, provider, mdl, 1); err != nil {
		return billingError("convert markdown", err), nil
	}

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

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeConvert, provider, mdl, 1); err != nil {
		return billingError("render template", err), nil
	}

	result, err := svcs.WritingSvc.RenderTemplate(ctx, userID, projectID, markdown, layoutPlan, theme, taskID)
	if err != nil {
		return billingError("render template", err), nil
	}

	return textResult(result)
}

func researchTopicsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WritingSvc == nil {
		return errorResult("writing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}

	var keywords []string
	if kwArr, ok := args["keywords"].([]any); ok {
		for _, kw := range kwArr {
			if s, ok := kw.(string); ok {
				keywords = append(keywords, s)
			}
		}
	}

	domain, _ := args["domain"].(string)
	count := 5
	if v, ok := args["count"].(float64); ok && int(v) > 0 {
		count = int(v)
	}

	logLongTextToolStart("research_topics", req)
	defer logLongTextToolEnd("research_topics", time.Now())
	stop := startProgressHeartbeat(ctx, req.Session, req.Params.GetProgressToken(), "research_topics", longTextHeartbeatInterval)
	defer stop()

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeTopicResearch, provider, mdl, 1); err != nil {
		return billingError("research topics", err), nil
	}

	result, err := svcs.WritingSvc.ResearchTopics(ctx, userID, projectID, keywords, domain, count)
	if err != nil {
		return billingError("research topics", err), nil
	}

	return textResult(result)
}

func optimizeSEOHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WritingSvc == nil {
		return errorResult("writing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	content, _ := args["content"].(string)
	title, _ := args["title"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if content == "" {
		return errorResult("content is required"), nil
	}
	if title == "" {
		return errorResult("title is required"), nil
	}

	var keywords []string
	if kwArr, ok := args["keywords"].([]any); ok {
		for _, kw := range kwArr {
			if s, ok := kw.(string); ok {
				keywords = append(keywords, s)
			}
		}
	}

	logLongTextToolStart("optimize_seo", req)
	defer logLongTextToolEnd("optimize_seo", time.Now())
	stop := startProgressHeartbeat(ctx, req.Session, req.Params.GetProgressToken(), "optimize_seo", longTextHeartbeatInterval)
	defer stop()

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeSEO, provider, mdl, 1); err != nil {
		return billingError("optimize seo", err), nil
	}

	result, err := svcs.WritingSvc.OptimizeSEO(ctx, userID, projectID, content, title, keywords)
	if err != nil {
		return billingError("optimize seo", err), nil
	}

	return textResult(result)
}

func generateOutlineHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WritingSvc == nil {
		return errorResult("writing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	topic, _ := args["topic"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if topic == "" {
		return errorResult("topic is required"), nil
	}

	template, _ := args["template"].(string)
	style, _ := args["style"].(string)
	taskID, _ := args["task_id"].(string)

	logLongTextToolStart("generate_outline", req)
	defer logLongTextToolEnd("generate_outline", time.Now())
	stop := startProgressHeartbeat(ctx, req.Session, req.Params.GetProgressToken(), "generate_outline", longTextHeartbeatInterval)
	defer stop()

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeOutline, provider, mdl, 1); err != nil {
		return billingError("generate outline", err), nil
	}

	result, err := svcs.WritingSvc.GenerateOutline(ctx, userID, projectID, topic, template, style, taskID)
	if err != nil {
		return billingError("generate outline", err), nil
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
