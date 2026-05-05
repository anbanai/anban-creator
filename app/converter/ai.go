package converter

import (
	"fmt"
	"strings"
)

// AIConvertRequest is the assembled prompt + metadata ready for an LLM call.
// WritingService extracts this via GetAIRequestInfo and sends it to the LLM.
type AIConvertRequest struct {
	Markdown     string // Markdown content
	Prompt       string // Fully assembled prompt (theme + rules + markdown)
	Theme        string // Theme name used
	CustomPrompt string // Custom prompt override (if any)
}

// AIConvertResult is the final conversion result after the LLM returns HTML.
type AIConvertResult struct {
	HTML    string
	Success bool
	Error   string
}

// convertViaAI assembles the full LLM prompt from the theme and markdown,
// extracts image references, and returns both via a sentinel value in Error.
// The actual LLM call is performed by WritingService after extracting the prompt
// with GetAIRequestInfo.
func (c *converter) convertViaAI(req *ConvertRequest) *ConvertResult {
	result := &ConvertResult{
		Theme:   req.Theme,
		Success: false,
	}

	prompt, err := c.buildAIPrompt(req)
	if err != nil {
		result.Error = fmt.Sprintf("build AI prompt failed: %s", err.Error())
		return result
	}

	images := c.ExtractImages(req.Markdown)

	// Encode the assembled prompt as a sentinel in Error so WritingService can
	// extract it via GetAIRequestInfo and send it to the LLM.
	result.Error = "AI_MODE_REQUEST:" + prompt
	result.Images = images

	c.log.Info().Str("theme", req.Theme).Int("count", len(images)).Int("prompt_length", len(prompt)).Msg("AI conversion request prepared")

	return result
}

// buildAIPrompt builds the full LLM prompt from the theme or custom prompt.
func (c *converter) buildAIPrompt(req *ConvertRequest) (string, error) {
	var prompt string

	if req.CustomPrompt != "" {
		prompt = BuildCustomAIPrompt(req.CustomPrompt)
	} else {
		theme, err := c.theme.GetTheme(req.Theme)
		if err != nil {
			// Theme not found — fall back to generic prompt.
			prompt = c.getGenericPrompt()
		} else {
			var err2 error
			prompt, err2 = c.promptBuilder.BuildPromptFromTheme(theme, req.Markdown, nil)
			if err2 != nil {
				c.log.Warn().Err(err2).Msg("build prompt from theme failed, using raw prompt")
				prompt = theme.Prompt + "\n\n```\n" + req.Markdown + "\n```"
			}
		}
	}

	// If no {{MARKDOWN}} placeholder was found, append the markdown in a code block.
	if !strings.Contains(prompt, req.Markdown) {
		prompt = prompt + "\n\n```\n" + req.Markdown + "\n```"
	}

	return prompt, nil
}

// getGenericPrompt returns a basic conversion prompt when no theme is found.
func (c *converter) getGenericPrompt() string {
	return `你是一个专业的微信公众号排版助手。请将以下 Markdown 内容转换为微信公众号兼容的 HTML。

## 样式要求
- 使用内联 CSS（style 属性）
- 整洁大方的排版
- 适当的间距和行高

## 重要规则
1. 所有 CSS 必须使用内联 style 属性
2. 不使用外部样式表或 <style> 标签
3. 只使用安全的 HTML 标签
4. 图片使用占位符格式：<!-- IMG:index -->
5. 返回完整的 HTML，不需要其他说明文字`
}

// PrepareAIRequest assembles the prompt and extracts images for an LLM call.
func (c *converter) PrepareAIRequest(req *ConvertRequest) (*AIConvertRequest, error) {
	prompt, err := c.buildAIPrompt(req)
	if err != nil {
		return nil, err
	}

	return &AIConvertRequest{
		Markdown:     req.Markdown,
		Prompt:       prompt,
		Theme:        req.Theme,
		CustomPrompt: req.CustomPrompt,
	}, nil
}

// CompleteAIConversion builds a successful ConvertResult from LLM output.
func CompleteAIConversion(html string, images []ImageRef, theme string) *ConvertResult {
	return &ConvertResult{
		HTML:    html,
		Theme:   theme,
		Images:  images,
		Success: true,
	}
}

// IsAIRequest checks if a ConvertResult contains a sentinel AI prompt.
func IsAIRequest(result *ConvertResult) bool {
	return result.Error != "" && len(result.Error) > 16 && result.Error[:16] == "AI_MODE_REQUEST:"
}

// ExtractAIRequest extracts the prompt from a sentinel result.
func ExtractAIRequest(result *ConvertResult) string {
	if IsAIRequest(result) {
		return strings.TrimPrefix(result.Error, "AI_MODE_REQUEST:")
	}
	return ""
}

// GetAIRequestInfo extracts the prompt and images from a sentinel result.
func GetAIRequestInfo(result *ConvertResult) (prompt string, images []ImageRef, ok bool) {
	if !IsAIRequest(result) {
		return "", nil, false
	}
	return ExtractAIRequest(result), result.Images, true
}
