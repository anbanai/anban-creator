// Package converter 提供 Markdown 到微信公众号 HTML 的转换功能
// 通过 Claude AI 生成微信公众号格式的 HTML
package converter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rs/zerolog"
)

// ImageType 图片类型
type ImageType string

const (
	ImageTypeLocal  ImageType = "local"  // 本地图片
	ImageTypeOnline ImageType = "online" // 在线图片
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
}

// ConvertResult 转换结果
type ConvertResult struct {
	HTML    string     // 生成的 HTML（含占位符）
	Theme   string     // 使用的主题
	Images  []ImageRef // 图片引用列表
	Success bool       // 是否成功
	Error   string     // 错误信息
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
	log   *zerolog.Logger
	theme *ThemeManager
}

// NewConverter 创建转换器
func NewConverter(log *zerolog.Logger) Converter {
	return &converter{
		log:   log,
		theme: NewThemeManager(),
	}
}

// NewConverterWithThemes creates a Converter with pre-loaded theme data from raw YAML bytes.
func NewConverterWithThemes(log *zerolog.Logger, themeData map[string][]byte) Converter {
	tm := NewThemeManager()
	for name, data := range themeData {
		if err := tm.LoadFromBytes(data); err != nil {
			(*log).Warn().Err(err).Str("theme", name).Msg("failed to load theme from bytes")
		}
	}
	return &converter{
		log:   log,
		theme: tm,
	}
}

// Convert 执行转换 — 确定性渲染（goldmark + 结构化 theme），不再经过 LLM。
// markdown 为空或缺主题时直接返回 error，不做静默兜底。
func (c *converter) Convert(req *ConvertRequest) *ConvertResult {
	return c.renderDeterministic(req)
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

	return images
}

// ReplaceImagePlaceholders 在 HTML 中替换图片占位符
func ReplaceImagePlaceholders(html string, images []ImageRef) string {
	result := html
	for _, img := range images {
		if img.WechatURL != "" {
			// 替换占位符为实际图片标签
			imgTag := `<img src="` + img.WechatURL + `" style="max-width:100%;height:auto;display:block;margin:16px auto;" />`
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
	ErrInvalidTheme  = &ConvertError{Code: "INVALID_THEME", Message: "invalid theme name", HintMsg: "请在 Studio 或 themes 资源目录中选择支持的主题"}
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
