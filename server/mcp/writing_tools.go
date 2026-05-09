package mcp

import (
	"context"
	"math"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/model"
)

// registerWritingTools registers article writing, conversion, humanization, topic research, SEO, outline generation, and scoring tools.
func registerWritingTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "write_article",
		Description: "Generate an article using the channel's configured writing style and an LLM. The server assembles the writing prompt from the channel's style settings, calls the LLM, and returns the article text in Markdown format.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id":   map[string]any{"type": "string", "description": "Channel ID (determines writing style)"},
				"topic":        map[string]any{"type": "string", "description": "Article topic or idea to write about"},
				"input_type":   map[string]any{"type": "string", "enum": []any{"idea", "fragment", "outline", "title"}, "description": "Type of input content (default: idea)"},
				"article_type": map[string]any{"type": "string", "enum": []any{"essay", "commentary", "story", "tutorial", "review"}, "description": "Article type (default: essay)"},
				"length":       map[string]any{"type": "string", "enum": []any{"short", "medium", "long"}, "description": "Desired article length (default: medium)"},
			},
			"required": []any{"channel_id", "topic"},
		},
	}, writeArticleHandler)

	server.AddTool(&mcp.Tool{
		Name:        "convert_markdown",
		Description: "Convert Markdown content to WeChat-compatible HTML. The server uses the channel's theme settings, assembles the conversion prompt, calls an LLM, and returns the HTML with image placeholders.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID (determines theme)"},
				"markdown":   map[string]any{"type": "string", "description": "Markdown content to convert"},
				"theme":      map[string]any{"type": "string", "description": "Theme name override (optional, uses channel theme by default)"},
			},
			"required": []any{"channel_id", "markdown"},
		},
	}, convertMarkdownHandler)

	server.AddTool(&mcp.Tool{
		Name:        "humanize_article",
		Description: "Remove AI-generated writing traces from content. The server builds a humanization prompt based on the specified intensity, calls an LLM, and returns the naturalized content.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
				"content":    map[string]any{"type": "string", "description": "Article content to humanize"},
				"intensity":  map[string]any{"type": "string", "enum": []any{"gentle", "medium", "aggressive", "authentic"}, "description": "Humanization intensity (default: medium). 'authentic' uses 6-dimension rules to rewrite like real human writing."},
			},
			"required": []any{"channel_id", "content"},
		},
	}, humanizeArticleHandler)

	server.AddTool(&mcp.Tool{
		Name:        "research_topics",
		Description: "Generate topic suggestions based on a channel's positioning and keywords. Returns an array of topics with viral scores, angles, and keywords.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID (uses its positioning and keywords)"},
				"keywords":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Override keywords (optional, uses channel keywords by default)"},
				"domain":     map[string]any{"type": "string", "description": "Domain or niche focus (optional)"},
				"count":      map[string]any{"type": "integer", "description": "Number of topics to generate (1-20, default 5)", "minimum": 1, "maximum": 20},
			},
			"required": []any{"channel_id"},
		},
	}, researchTopicsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "optimize_seo",
		Description: "Optimize a title and keywords for search engine ranking. Returns the optimized title, keywords, and a summary.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
				"content":    map[string]any{"type": "string", "description": "Article content to optimize"},
				"title":      map[string]any{"type": "string", "description": "Original title to optimize"},
				"keywords":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Current keywords (optional)"},
			},
			"required": []any{"channel_id", "content", "title"},
		},
	}, optimizeSEOHandler)

	server.AddTool(&mcp.Tool{
		Name:        "generate_outline",
		Description: "Generate a structured article outline/framework based on a topic and template. Returns title, hook, sections, key points, CTA, and viral elements.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID (determines style and keywords)"},
				"topic":      map[string]any{"type": "string", "description": "Article topic or idea"},
				"template":   map[string]any{"type": "string", "enum": []any{"authoritative", "comparison", "cultural", "practical"}, "description": "Outline template type (default: authoritative)"},
				"style":      map[string]any{"type": "string", "description": "Writing style override (optional, uses channel style by default)"},
			},
			"required": []any{"channel_id", "topic"},
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

	channelID, _ := args["channel_id"].(string)
	topic, _ := args["topic"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if topic == "" {
		return errorResult("topic is required"), nil
	}

	inputType, _ := args["input_type"].(string)
	articleType, _ := args["article_type"].(string)
	length, _ := args["length"].(string)

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeArticleWrite, provider, mdl, 1); err != nil {
		return billingError("write article", err), nil
	}

	result, err := svcs.WritingSvc.WriteArticle(ctx, userID, channelID, topic, inputType, articleType, length)
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

	channelID, _ := args["channel_id"].(string)
	markdown, _ := args["markdown"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if markdown == "" {
		return errorResult("markdown is required"), nil
	}

	theme, _ := args["theme"].(string)

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeConvert, provider, mdl, 1); err != nil {
		return billingError("convert markdown", err), nil
	}

	result, err := svcs.WritingSvc.ConvertMarkdown(ctx, userID, channelID, markdown, theme)
	if err != nil {
		return billingError("convert markdown", err), nil
	}

	return textResult(result)
}

func humanizeArticleHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WritingSvc == nil {
		return errorResult("writing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	content, _ := args["content"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if content == "" {
		return errorResult("content is required"), nil
	}

	intensity, _ := args["intensity"].(string)

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeHumanize, provider, mdl, 1); err != nil {
		return billingError("humanize article", err), nil
	}

	result, err := svcs.WritingSvc.HumanizeArticle(ctx, userID, channelID, content, intensity)
	if err != nil {
		return billingError("humanize article", err), nil
	}

	return textResult(result)
}

func researchTopicsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WritingSvc == nil {
		return errorResult("writing service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
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

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeTopicResearch, provider, mdl, 1); err != nil {
		return billingError("research topics", err), nil
	}

	result, err := svcs.WritingSvc.ResearchTopics(ctx, userID, channelID, keywords, domain, count)
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

	channelID, _ := args["channel_id"].(string)
	content, _ := args["content"].(string)
	title, _ := args["title"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
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

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeSEO, provider, mdl, 1); err != nil {
		return billingError("optimize seo", err), nil
	}

	result, err := svcs.WritingSvc.OptimizeSEO(ctx, userID, channelID, content, title, keywords)
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

	channelID, _ := args["channel_id"].(string)
	topic, _ := args["topic"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if topic == "" {
		return errorResult("topic is required"), nil
	}

	template, _ := args["template"].(string)
	style, _ := args["style"].(string)

	provider, mdl := resolveTextModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeOutline, provider, mdl, 1); err != nil {
		return billingError("generate outline", err), nil
	}

	result, err := svcs.WritingSvc.GenerateOutline(ctx, userID, channelID, topic, template, style)
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
