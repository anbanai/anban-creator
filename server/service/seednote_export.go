package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type SeednoteExportRequest struct {
	Format   string
	Markdown string
	Title    string
	Content  string
	Tags     []string
}

type SeednoteImage struct {
	Alt      string `json:"alt"`
	URL      string `json:"url"`
	Position int    `json:"position"`
}

type SeednoteExportResult struct {
	Format  string          `json:"format,omitempty"`
	Title   string          `json:"title"`
	Content string          `json:"content"`
	Tags    []string        `json:"tags"`
	Images  []SeednoteImage `json:"images,omitempty"`
	Output  string          `json:"output,omitempty"`
}

func (r SeednoteExportResult) MarshalJSON() ([]byte, error) {
	if r.Format == "markdown" {
		return json.Marshal(struct {
			Format  string   `json:"format"`
			Title   string   `json:"title"`
			Content string   `json:"content"`
			Tags    []string `json:"tags"`
			Output  string   `json:"output"`
		}{Format: r.Format, Title: r.Title, Content: r.Content, Tags: r.Tags, Output: r.Output})
	}
	return json.Marshal(struct {
		Title   string          `json:"title"`
		Content string          `json:"content"`
		Tags    []string        `json:"tags"`
		Images  []SeednoteImage `json:"images"`
	}{Title: r.Title, Content: r.Content, Tags: r.Tags, Images: r.Images})
}

type SeednoteExportService struct{}

func NewSeednoteExportService() *SeednoteExportService {
	return &SeednoteExportService{}
}

func (s *SeednoteExportService) Export(req SeednoteExportRequest) (*SeednoteExportResult, error) {
	format := req.Format
	if format == "" {
		format = "json"
	}

	var result *SeednoteExportResult
	switch {
	case req.Markdown != "":
		result = parseSeednoteMarkdown(req.Markdown)
	case req.Title != "" && req.Content != "":
		result = &SeednoteExportResult{
			Title: req.Title, Content: cleanSeednoteExportContent(req.Content),
			Tags: trimSeednoteStrings(req.Tags), Images: []SeednoteImage{},
		}
	default:
		return nil, fmt.Errorf("provide either markdown content or both title and content")
	}

	if format == "markdown" {
		result.Format = "markdown"
		result.Output = formatSeednoteExportMarkdown(result)
	}
	return result, nil
}

func trimSeednoteStrings(values []string) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = strings.TrimSpace(value)
	}
	return result
}

var (
	reSeednoteExportTag          = regexp.MustCompile(`#([^#\s][^\s#]*)`)
	reSeednoteExportLink         = regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`)
	reSeednoteExportBold         = regexp.MustCompile(`\*\*([^\*]+)\*\*|__([^_]+)__`)
	reSeednoteExportItalic       = regexp.MustCompile(`\*([^\*]+)\*|_([^_]+)_`)
	reSeednoteExportCode         = regexp.MustCompile("`([^`]+)`")
	reSeednoteExportStrike       = regexp.MustCompile(`~~([^~]+)~~`)
	reSeednoteExportList         = regexp.MustCompile(`^[\s]*[-\*\d]+[\.\)]?\s*`)
	reSeednoteExportMultiSpace   = regexp.MustCompile(`\s+`)
	reSeednoteExportMultiNewline = regexp.MustCompile(`\n{3,}`)
	reSeednoteExportImage        = regexp.MustCompile(`!\[([^\]]*)\]\(([^\)]+)\)`)
)

func parseSeednoteMarkdown(markdown string) *SeednoteExportResult {
	result := &SeednoteExportResult{Tags: []string{}, Images: []SeednoteImage{}}
	lines := strings.Split(markdown, "\n")
	contentLines := make([]string, 0, len(lines))
	inCodeBlock := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}
		if result.Title == "" && strings.HasPrefix(trimmed, "# ") {
			result.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			if runes := []rune(result.Title); len(runes) > 20 {
				result.Title = string(runes[:20])
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "#标签") {
			contentLines = append(contentLines, "**"+strings.TrimLeft(trimmed, "# ")+"**")
			continue
		}
		result.Tags = append(result.Tags, extractSeednoteExportTags(trimmed)...)
		trimmed = strings.TrimSpace(reSeednoteExportTag.ReplaceAllString(trimmed, ""))
		trimmed = seednoteExportMarkdownToPlain(trimmed, &result.Images)
		if trimmed == "" {
			if len(contentLines) > 0 && contentLines[len(contentLines)-1] != "" {
				contentLines = append(contentLines, "")
			}
			continue
		}
		contentLines = append(contentLines, trimmed)
		if i > 100 && len(contentLines) > 50 {
			break
		}
	}

	result.Content = cleanSeednoteExportContent(strings.Join(contentLines, "\n"))
	result.Tags = uniqueSeednoteStrings(result.Tags)
	result.Images = filterSeednoteExportImages(result.Content, result.Images)
	return result
}

func extractSeednoteExportTags(text string) []string {
	var tags []string
	for _, match := range reSeednoteExportTag.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			tag := strings.TrimSpace(match[1])
			if len(tag) >= 2 && len(tag) <= 20 {
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

func seednoteExportMarkdownToPlain(text string, images *[]SeednoteImage) string {
	position := len(*images)
	text = reSeednoteExportImage.ReplaceAllStringFunc(text, func(match string) string {
		position++
		parts := reSeednoteExportImage.FindStringSubmatch(match)
		*images = append(*images, SeednoteImage{Alt: parts[1], URL: parts[2], Position: position})
		return fmt.Sprintf("[图片:%d]", position)
	})
	text = reSeednoteExportLink.ReplaceAllString(text, "$1")
	text = reSeednoteExportBold.ReplaceAllString(text, "$1$2")
	text = reSeednoteExportItalic.ReplaceAllString(text, "$1$2")
	text = reSeednoteExportCode.ReplaceAllString(text, "$1")
	text = reSeednoteExportStrike.ReplaceAllString(text, "$1")
	text = reSeednoteExportList.ReplaceAllString(text, "")
	text = reSeednoteExportMultiSpace.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func cleanSeednoteExportContent(content string) string {
	content = reSeednoteExportMultiNewline.ReplaceAllString(content, "\n\n")
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	content = strings.Join(lines, "\n")
	if runes := []rune(content); len(runes) > 1000 {
		content = string(runes[:1000]) + "..."
	}
	return strings.TrimSpace(content)
}

func uniqueSeednoteStrings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func filterSeednoteExportImages(content string, images []SeednoteImage) []SeednoteImage {
	result := make([]SeednoteImage, 0, len(images))
	for _, image := range images {
		if strings.Contains(content, fmt.Sprintf("[图片:%d]", image.Position)) {
			result = append(result, image)
		}
	}
	return result
}

func formatSeednoteExportMarkdown(result *SeednoteExportResult) string {
	var output strings.Builder
	output.WriteString("# Seednote Publishing Content\n\n## Title\n\n")
	output.WriteString(result.Title)
	output.WriteString("\n\n## Body\n\n")
	output.WriteString(result.Content)
	output.WriteString("\n\n## Tags\n\n")
	if len(result.Tags) == 0 {
		output.WriteString("(none)")
	} else {
		output.WriteString(strings.Join(result.Tags, " "))
	}
	output.WriteString("\n")
	if len(result.Images) > 0 {
		output.WriteString("\n## Images\n\n")
		for _, image := range result.Images {
			fmt.Fprintf(&output, "%d. ![%s](%s)\n", image.Position, image.Alt, image.URL)
		}
	}
	return output.String()
}
