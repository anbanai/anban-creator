package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerWritingTools registers article writing, conversion, humanization, topic research, and SEO tools.
func registerWritingTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "write_article",
		Description: "Generate an article using the channel's configured writing style and an LLM. The server assembles the writing prompt from the channel's style settings, calls the LLM, and returns the article text in Markdown format with optional image generation placeholders (__generate:prompt__).",
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
				"intensity":  map[string]any{"type": "string", "enum": []any{"gentle", "medium", "aggressive"}, "description": "Humanization intensity (default: medium)"},
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

	result, err := svcs.WritingSvc.WriteArticle(ctx, userID, channelID, topic, inputType, articleType, length)
	if err != nil {
		return errorResult(fmt.Sprintf("write article: %v", err)), nil
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

	result, err := svcs.WritingSvc.ConvertMarkdown(ctx, userID, channelID, markdown, theme)
	if err != nil {
		return errorResult(fmt.Sprintf("convert markdown: %v", err)), nil
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

	result, err := svcs.WritingSvc.HumanizeArticle(ctx, userID, channelID, content, intensity)
	if err != nil {
		return errorResult(fmt.Sprintf("humanize article: %v", err)), nil
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

	result, err := svcs.WritingSvc.ResearchTopics(ctx, userID, channelID, keywords, domain, count)
	if err != nil {
		return errorResult(fmt.Sprintf("research topics: %v", err)), nil
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

	result, err := svcs.WritingSvc.OptimizeSEO(ctx, userID, channelID, content, title, keywords)
	if err != nil {
		return errorResult(fmt.Sprintf("optimize seo: %v", err)), nil
	}

	return textResult(result)
}
