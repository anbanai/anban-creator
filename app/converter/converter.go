// Package converter 提供 Markdown 到微信公众号 HTML 的转换功能
// 通过 Claude AI 生成微信公众号格式的 HTML
package converter

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/rs/zerolog"
)

// ImageType 图片类型
type ImageType string

const (
	ImageTypeLocal  ImageType = "local"  // 本地图片
	ImageTypeOnline ImageType = "online" // 在线图片
	ImageTypeAI     ImageType = "ai"     // AI 生成图片
)

// ConvertRequest 转换请求
type ConvertRequest struct {
	// 基础输入
	Markdown string // Markdown 内容
	Theme    string // 主题名称 / AI 提示词名称

	// AI 模式专用
	CustomPrompt string // 自定义提示词
}

// ImageRef 图片引用
type ImageRef struct {
	Index       int       // 位置索引
	Original    string    // 原始路径或提示词
	Placeholder string    // HTML 中的占位符 <!-- IMG:0 -->
	WechatURL   string    // 上传后的 URL (处理完成后)
	Type        ImageType // 图片类型
	AIPrompt    string    // AI 图片的生成提示词
}

// ConvertResult 转换结果
type ConvertResult struct {
	HTML      string     // 生成的 HTML（含占位符）
	Theme     string     // 使用的主题
	Images    []ImageRef // 图片引用列表
	Success   bool       // 是否成功
	Error     string     // 错误信息
	AIRequest string     // explicit AI conversion prompt (replaces "AI_MODE_REQUEST:" prefix in Error)
}

// Converter 转换器接口
type Converter interface {
	// Convert 执行转换
	Convert(req *ConvertRequest) *ConvertResult

	// ExtractImages 从 Markdown 中提取图片引用
	ExtractImages(markdown string) []ImageRef
}

// converter 转换器实现
type converter struct {
	log           zerolog.Logger
	theme         *ThemeManager
	promptBuilder *PromptBuilder
}

// NewConverter 创建转换器
func NewConverter(log zerolog.Logger) Converter {
	return &converter{
		log:           log,
		theme:         NewThemeManager(),
		promptBuilder: NewPromptBuilder(),
	}
}

// Convert 执行转换
func (c *converter) Convert(req *ConvertRequest) *ConvertResult {
	result := &ConvertResult{
		Theme: req.Theme,
	}

	// 验证请求
	if err := c.validateRequest(req); err != nil {
		result.Success = false
		result.Error = err.Error()
		return result
	}

	// 使用 AI 模式转换
	return c.convertViaAI(req)
}

// validateRequest 验证请求参数
func (c *converter) validateRequest(req *ConvertRequest) error {
	if req.Markdown == "" {
		return ErrEmptyMarkdown
	}

	if req.Theme == "" {
		req.Theme = "default"
	}

	return nil
}

// ExtractImages 从 Markdown 中提取图片引用
func (c *converter) ExtractImages(markdown string) []ImageRef {
	var images []ImageRef

	// 匹配本地图片: ![alt](./path/to/image.png)
	localPattern := regexp.MustCompile(`!\[([^\]]*)\]\((\.\/[^)]+)\)`)
	for i, match := range localPattern.FindAllStringSubmatch(markdown, -1) {
		if len(match) >= 3 {
			images = append(images, ImageRef{
				Index:       i,
				Original:    match[2],
				Placeholder: "",
				Type:        ImageTypeLocal,
			})
		}
	}

	// 匹配在线图片: ![alt](https://...)
	onlinePattern := regexp.MustCompile(`!\[([^\]]*)\]\((https?://[^)]+)\)`)
	offset := len(images)
	for i, match := range onlinePattern.FindAllStringSubmatch(markdown, -1) {
		if len(match) >= 3 {
			images = append(images, ImageRef{
				Index:       offset + i,
				Original:    match[2],
				Placeholder: "",
				Type:        ImageTypeOnline,
			})
		}
	}

	// 匹配 AI 生成图片: ![alt](__generate:prompt__)
	aiPattern := regexp.MustCompile(`!\[([^\]]*)\]\(__generate:([^)]+)__\)`)
	offset = len(images)
	for i, match := range aiPattern.FindAllStringSubmatch(markdown, -1) {
		if len(match) >= 3 {
			images = append(images, ImageRef{
				Index:       offset + i,
				Original:    match[2],
				Placeholder: "",
				Type:        ImageTypeAI,
				AIPrompt:    match[2],
			})
		}
	}

	return images
}

// sanitizeImageURL validates and escapes a URL for safe use in an HTML src attribute.
// Returns empty string if the URL is unsafe (non-http scheme, parse error).
func sanitizeImageURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	// HTML-encode the entire URL to prevent breaking out of the src attribute
	return html.EscapeString(rawURL)
}

// ReplaceImagePlaceholders 在 HTML 中替换图片占位符
func ReplaceImagePlaceholders(html string, images []ImageRef) string {
	result := html
	for _, img := range images {
		if img.WechatURL != "" {
			safeURL := sanitizeImageURL(img.WechatURL)
			if safeURL == "" {
				continue // 拒绝不安全的 URL，跳过此图片
			}
			imgTag := fmt.Sprintf(`<img src="%s" style="max-width:100%%;height:auto;display:block;margin:20px auto;" />`, safeURL)
			result = strings.ReplaceAll(result, img.Placeholder, imgTag)
		}
	}
	return result
}

// InsertImagePlaceholders 在 HTML 中插入图片占位符
func InsertImagePlaceholders(html string, images []ImageRef) string {
	if len(images) == 0 {
		return html
	}

	// 查找所有段落结束标签作为插入点
	paragraphEnds := findInsertionPoints(html)
	if len(paragraphEnds) == 0 {
		return html
	}

	// 计算图片分布间隔
	interval := max(len(paragraphEnds)/(len(images)+1), 1)

	// 从后向前插入，避免索引偏移
	result := html
	imageIndex := len(images) - 1
	for i := len(paragraphEnds) - 1; i >= 0 && imageIndex >= 0; i-- {
		if (len(paragraphEnds)-i-1)%interval == 0 {
			placeholder := fmt.Sprintf("<!-- IMG:%d -->", imageIndex)
			insertPos := paragraphEnds[i]
			result = result[:insertPos] + placeholder + result[insertPos:]
			imageIndex--
		}
	}

	return result
}

// findInsertionPoints 查找 HTML 中适合插入图片的位置
func findInsertionPoints(html string) []int {
	var points []int

	// 查找 </p> 标签位置
	pEnd := "</p>"
	pos := 0
	for {
		idx := strings.Index(html[pos:], pEnd)
		if idx == -1 {
			break
		}
		actualPos := pos + idx + len(pEnd)
		points = append(points, actualPos)
		pos = actualPos
	}

	// 如果没有 </p> 标签，查找 </section> 标签
	if len(points) == 0 {
		sectionEnd := "</section>"
		pos = 0
		for {
			idx := strings.Index(html[pos:], sectionEnd)
			if idx == -1 {
				break
			}
			actualPos := pos + idx + len(sectionEnd)
			points = append(points, actualPos)
			pos = actualPos
		}
	}

	return points
}

// 错误定义
var (
	ErrEmptyMarkdown = &ConvertError{Code: "EMPTY_MARKDOWN", Message: "markdown content cannot be empty", HintMsg: "请提供非空的 Markdown 内容"}
	ErrInvalidTheme  = &ConvertError{Code: "INVALID_THEME", Message: "invalid theme name", HintMsg: "使用 'abwriter convert --help' 查看支持的主题"}
	ErrAIFailure     = &ConvertError{Code: "AI_FAILURE", Message: "AI generation failed", HintMsg: "检查 AI API 配置是否正确"}
)

// ConvertError 转换错误
type ConvertError struct {
	Code    string
	Message string
	HintMsg string
	Err     error
}

func (e *ConvertError) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *ConvertError) Unwrap() error {
	return e.Err
}

func (e *ConvertError) Hint() string { return e.HintMsg }

// GetPromptBuilder 获取 Prompt 构建器（用于外部访问）
func GetPromptBuilder() *PromptBuilder {
	return NewPromptBuilder()
}

// ValidateAIRequest 验证 AI 转换请求
func ValidateAIRequest(prompt string) *ValidationResult {
	return ValidatePromptContent(prompt)
}

// GetMarkdownTitle 提取 Markdown 标题
func GetMarkdownTitle(markdown string) string {
	return ParseMarkdownTitle(markdown)
}

// EstimateTokens 估算文本 token 数量
func EstimateTokens(text string) int {
	return EstimateTokenCount(text)
}
