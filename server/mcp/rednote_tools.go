package mcp

import (
	"context"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerRednoteTools registers rednote content formatting tools.
func registerRednoteTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "export_rednote",
		Description: "Format content for Xiaohongshu (Little Red Book) publishing. Parses Markdown or accepts direct parameters, extracts tags, cleans formatting, and returns structured content ready for publishing.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"markdown": map[string]any{"type": "string", "description": "Markdown content to parse"},
				"title":    map[string]any{"type": "string", "description": "Title (used when not parsing markdown)"},
				"content":  map[string]any{"type": "string", "description": "Body text (used when not parsing markdown)"},
				"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags (used when not parsing markdown)"},
				"format":   map[string]any{"type": "string", "enum": []any{"json", "markdown"}, "description": "Output format (default: json)"},
			},
		},
	}, exportRednoteHandler)
}

func exportRednoteHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)

	markdown, _ := args["markdown"].(string)
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	format, _ := args["format"].(string)
	if format == "" {
		format = "json"
	}

	var result *RednoteExportResult

	if markdown != "" {
		// Parse from Markdown.
		result = parseRednoteContent(markdown)
	} else if title != "" && content != "" {
		// Direct parameters.
		result = &RednoteExportResult{
			Title:   title,
			Content: cleanRednoteContent(content),
			Tags:    []string{},
		}
		// Extract tags from arguments.
		if tagArr, ok := args["tags"].([]any); ok {
			for _, t := range tagArr {
				if s, ok := t.(string); ok {
					result.Tags = append(result.Tags, strings.TrimSpace(s))
				}
			}
		}
	} else {
		return errorResult("provide either markdown content or both title and content"), nil
	}

	switch format {
	case "markdown":
		return textResult(formatRednoteMarkdown(result))
	default:
		return textResult(result)
	}
}

// ---------------------------------------------------------------------------
// Rednote content types
// ---------------------------------------------------------------------------

// RednoteExportResult contains formatted rednote publishing content.
type RednoteExportResult struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

// ---------------------------------------------------------------------------
// Rednote parsing helpers (adapted from app/rednote.go)
// ---------------------------------------------------------------------------

// Pre-compiled regexps for rednote content processing.
var (
	reRednoteTag          = regexp.MustCompile(`#([^#\s][^\s#]*)`)
	reRednoteImage        = regexp.MustCompile(`!\[([^\]]*)\]\([^\)]+\)`)
	reRednoteLink         = regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`)
	reRednoteBold         = regexp.MustCompile(`\*\*([^\*]+)\*\*|__([^_]+)__`)
	reRednoteItalic       = regexp.MustCompile(`\*([^\*]+)\*|_([^_]+)_`)
	reRednoteCode         = regexp.MustCompile("`([^`]+)`")
	reRednoteStrike       = regexp.MustCompile(`~~([^~]+)~~`)
	reRednoteList         = regexp.MustCompile(`^[\s]*[-\*\d]+[\.\)]?\s*`)
	reRednoteMultiSpace   = regexp.MustCompile(`\s+`)
	reRednoteMultiNewline = regexp.MustCompile(`\n{3,}`)
)

// parseRednoteContent parses Markdown content into structured rednote publishing data.
func parseRednoteContent(markdown string) *RednoteExportResult {
	result := &RednoteExportResult{
		Tags: []string{},
	}

	lines := strings.Split(markdown, "\n")
	var contentLines []string
	inCodeBlock := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Code block boundaries.
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		// Extract title (first # heading).
		if result.Title == "" && strings.HasPrefix(trimmed, "# ") {
			result.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			// Limit title length (rednote titles are typically <= 20 chars).
			if len([]rune(result.Title)) > 20 {
				runes := []rune(result.Title)
				result.Title = string(runes[:20])
			}
			continue
		}

		// Skip other Markdown heading markers but convert to bold.
		if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "#标签") {
			titleText := strings.TrimLeft(trimmed, "# ")
			contentLines = append(contentLines, "**"+titleText+"**")
			continue
		}

		// Extract # tags.
		if !inCodeBlock {
			tags := extractRednoteTags(trimmed)
			result.Tags = append(result.Tags, tags...)
			trimmed = removeRednoteTags(trimmed)
		}

		// Convert Markdown to plain text.
		trimmed = rednoteMarkdownToPlain(trimmed)

		// Skip empty lines but preserve paragraph spacing.
		if trimmed == "" {
			if len(contentLines) > 0 && contentLines[len(contentLines)-1] != "" {
				contentLines = append(contentLines, "")
			}
			continue
		}

		contentLines = append(contentLines, trimmed)

		// Limit content length.
		if i > 100 && len(contentLines) > 50 {
			break
		}
	}

	result.Content = cleanRednoteContent(strings.Join(contentLines, "\n"))
	result.Tags = uniqueStrings(result.Tags)

	return result
}

// extractRednoteTags extracts # tags from text.
func extractRednoteTags(text string) []string {
	var tags []string
	matches := reRednoteTag.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) > 1 {
			tag := strings.TrimSpace(match[1])
			if len(tag) >= 2 && len(tag) <= 20 {
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

// removeRednoteTags removes # tags from text.
func removeRednoteTags(text string) string {
	return strings.TrimSpace(reRednoteTag.ReplaceAllString(text, ""))
}

// rednoteMarkdownToPlain converts Markdown formatting to plain text.
func rednoteMarkdownToPlain(text string) string {
	text = reRednoteImage.ReplaceAllString(text, "")
	text = reRednoteLink.ReplaceAllString(text, "$1")
	text = reRednoteBold.ReplaceAllString(text, "$1$2")
	text = reRednoteItalic.ReplaceAllString(text, "$1$2")
	text = reRednoteCode.ReplaceAllString(text, "$1")
	text = reRednoteStrike.ReplaceAllString(text, "$1")
	text = reRednoteList.ReplaceAllString(text, "")
	text = reRednoteMultiSpace.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

// cleanRednoteContent cleans content formatting (collapse whitespace, trim length).
func cleanRednoteContent(content string) string {
	content = reRednoteMultiNewline.ReplaceAllString(content, "\n\n")

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	content = strings.Join(lines, "\n")

	// Limit content length (rednote recommends 300-800 chars).
	runes := []rune(content)
	if len(runes) > 1000 {
		content = string(runes[:1000]) + "..."
	}

	return strings.TrimSpace(content)
}

// uniqueStrings deduplicates a string slice.
func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range slice {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// formatRednoteMarkdown formats a RednoteExportResult as a Markdown string.
func formatRednoteMarkdown(r *RednoteExportResult) map[string]any {
	var sb strings.Builder

	sb.WriteString("# Xiaohongshu Publishing Content\n\n")
	sb.WriteString("## Title\n\n")
	sb.WriteString(r.Title)
	sb.WriteString("\n\n## Body\n\n")
	sb.WriteString(r.Content)
	sb.WriteString("\n\n## Tags\n\n")
	if len(r.Tags) > 0 {
		sb.WriteString(strings.Join(r.Tags, " "))
	} else {
		sb.WriteString("(none)")
	}
	sb.WriteString("\n")

	return map[string]any{
		"format":  "markdown",
		"title":   r.Title,
		"content": r.Content,
		"tags":    r.Tags,
		"output":  sb.String(),
	}
}

