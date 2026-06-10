package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerSeednoteFormatTools registers seednote content formatting tools.
func registerSeednoteFormatTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "export_seednote",
		Description: "Format content for Seednote publishing. Parses Markdown or accepts direct parameters, extracts tags, cleans formatting, and returns structured content ready for publishing.",
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
	}, exportSeednoteHandler)
}

func exportSeednoteHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)

	markdown, _ := args["markdown"].(string)
	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	format, _ := args["format"].(string)
	if format == "" {
		format = "json"
	}

	var result *SeednoteExportResult

	if markdown != "" {
		// Parse from Markdown.
		result = parseSeednoteContent(markdown)
	} else if title != "" && content != "" {
		// Direct parameters.
		result = &SeednoteExportResult{
			Title:   title,
			Content: cleanSeednoteContent(content),
			Tags:    []string{},
			Images:  []SeednoteImage{},
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
		return textResult(formatSeednoteMarkdown(result))
	default:
		return textResult(result)
	}
}

// ---------------------------------------------------------------------------
// Seednote content types
// ---------------------------------------------------------------------------

// SeednoteImage represents an extracted image from seednote content.
type SeednoteImage struct {
	Alt      string `json:"alt"`
	URL      string `json:"url"`
	Position int    `json:"position"`
}

// SeednoteExportResult contains formatted seednote publishing content.
type SeednoteExportResult struct {
	Title   string          `json:"title"`
	Content string          `json:"content"`
	Tags    []string        `json:"tags"`
	Images  []SeednoteImage `json:"images"`
}

// ---------------------------------------------------------------------------
// Seednote parsing helpers (adapted from app/seednote.go)
// ---------------------------------------------------------------------------

// Pre-compiled regexps for seednote content processing.
var (
	reSeednoteTag          = regexp.MustCompile(`#([^#\s][^\s#]*)`)
	reSeednoteLink         = regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`)
	reSeednoteBold         = regexp.MustCompile(`\*\*([^\*]+)\*\*|__([^_]+)__`)
	reSeednoteItalic       = regexp.MustCompile(`\*([^\*]+)\*|_([^_]+)_`)
	reSeednoteCode         = regexp.MustCompile("`([^`]+)`")
	reSeednoteStrike       = regexp.MustCompile(`~~([^~]+)~~`)
	reSeednoteList         = regexp.MustCompile(`^[\s]*[-\*\d]+[\.\)]?\s*`)
	reSeednoteMultiSpace   = regexp.MustCompile(`\s+`)
	reSeednoteMultiNewline = regexp.MustCompile(`\n{3,}`)
	// reSeednoteImageFull matches ![alt](url) with two capture groups for alt and url.
	reSeednoteImageFull = regexp.MustCompile(`!\[([^\]]*)\]\(([^\)]+)\)`)
)

// parseSeednoteContent parses Markdown content into structured seednote publishing data.
func parseSeednoteContent(markdown string) *SeednoteExportResult {
	result := &SeednoteExportResult{
		Tags:   []string{},
		Images: []SeednoteImage{},
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
			// Limit title length (seednote titles are typically <= 20 chars).
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
			tags := extractSeednoteTags(trimmed)
			result.Tags = append(result.Tags, tags...)
			trimmed = removeSeednoteTags(trimmed)
		}

		// Convert Markdown to plain text.
		trimmed = seednoteMarkdownToPlain(trimmed, &result.Images)

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

	result.Content = cleanSeednoteContent(strings.Join(contentLines, "\n"))
	result.Tags = uniqueStrings(result.Tags)
	result.Images = filterImagesByContent(result.Content, result.Images)

	return result
}

// extractSeednoteTags extracts # tags from text.
func extractSeednoteTags(text string) []string {
	var tags []string
	matches := reSeednoteTag.FindAllStringSubmatch(text, -1)
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

// removeSeednoteTags removes # tags from text.
func removeSeednoteTags(text string) string {
	return strings.TrimSpace(reSeednoteTag.ReplaceAllString(text, ""))
}

// parseImageMarkdown extracts alt text and URL from a markdown image syntax.
func parseImageMarkdown(match string) (alt, url string) {
	sub := reSeednoteImageFull.FindStringSubmatch(match)
	if len(sub) > 2 {
		alt = sub[1]
		url = sub[2]
	}
	return
}

// seednoteMarkdownToPlain converts Markdown formatting to plain text.
// Images are extracted into the images slice and replaced with [图片:N] placeholders.
func seednoteMarkdownToPlain(text string, images *[]SeednoteImage) string {
	pos := len(*images)
	text = reSeednoteImageFull.ReplaceAllStringFunc(text, func(match string) string {
		pos++
		alt, url := parseImageMarkdown(match)
		*images = append(*images, SeednoteImage{Alt: alt, URL: url, Position: pos})
		return fmt.Sprintf("[图片:%d]", pos)
	})
	text = reSeednoteLink.ReplaceAllString(text, "$1")
	text = reSeednoteBold.ReplaceAllString(text, "$1$2")
	text = reSeednoteItalic.ReplaceAllString(text, "$1$2")
	text = reSeednoteCode.ReplaceAllString(text, "$1")
	text = reSeednoteStrike.ReplaceAllString(text, "$1")
	text = reSeednoteList.ReplaceAllString(text, "")
	text = reSeednoteMultiSpace.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

// cleanSeednoteContent cleans content formatting (collapse whitespace, trim length).
func cleanSeednoteContent(content string) string {
	content = reSeednoteMultiNewline.ReplaceAllString(content, "\n\n")

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	content = strings.Join(lines, "\n")

	// Limit content length (seednote recommends 300-800 chars).
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

// filterImagesByContent keeps only images whose [图片:N] placeholder still exists in content.
func filterImagesByContent(content string, images []SeednoteImage) []SeednoteImage {
	var filtered []SeednoteImage
	for _, img := range images {
		if strings.Contains(content, fmt.Sprintf("[图片:%d]", img.Position)) {
			filtered = append(filtered, img)
		}
	}
	if len(filtered) == 0 {
		return []SeednoteImage{}
	}
	return filtered
}

// formatSeednoteMarkdown formats a SeednoteExportResult as a Markdown string.
func formatSeednoteMarkdown(r *SeednoteExportResult) map[string]any {
	var sb strings.Builder

	sb.WriteString("# Seednote Publishing Content\n\n")
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

	if len(r.Images) > 0 {
		sb.WriteString("\n## Images\n\n")
		for _, img := range r.Images {
			fmt.Fprintf(&sb, "%d. ![%s](%s)\n", img.Position, img.Alt, img.URL)
		}
	}

	return map[string]any{
		"format":  "markdown",
		"title":   r.Title,
		"content": r.Content,
		"tags":    r.Tags,
		"output":  sb.String(),
	}
}
