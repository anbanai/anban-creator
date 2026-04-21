// Package writer provides assisted writing functionality with customizable creator styles
package writer

import (
	"fmt"
	"os"
	"strings"
)

// Assistant 写作助手 - 核心协调器
type Assistant struct {
	styleManager *StyleManager
	generator    Generator
}

// NewAssistant 创建写作助手
func NewAssistant() *Assistant {
	return &Assistant{
		styleManager: NewStyleManager(),
		generator:    NewGenerator(),
	}
}

// WriteResult 写作结果（对外）
type WriteResult struct {
	Article     string   // 生成的文章
	Title       string   // 生成的标题
	Titles      []string // 备选标题
	Quotes      []string // 提取的金句
	Style       *WriterStyle
	Prompt      string // 用于 AI 的提示词
	IsAIRequest bool   // 是否需要 AI 处理
	Success     bool
	Error       string
}

// Write 写作 - 主入口
func (a *Assistant) Write(req *WriteRequest) *WriteResult {
	// 验证输入
	if err := ValidateInput(req.Input); err != nil {
		return &WriteResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	// 获取风格
	style, err := a.styleManager.GetStyle(req.StyleName)
	if err != nil {
		return &WriteResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	// 设置默认值
	if req.ArticleType == "" {
		req.ArticleType = ArticleTypeEssay
	}
	if req.Length == "" {
		req.Length = LengthMedium
	}

	// 构建生成请求
	genReq := &GenerateRequest{
		Style:       style,
		UserInput:   req.Input,
		InputType:   req.InputType,
		Title:       req.Title,
		Length:      req.Length,
		ArticleType: req.ArticleType,
	}

	// 调用生成器
	genResult := a.generator.Generate(genReq)

	result := &WriteResult{
		Style:   style,
		Success: genResult.Success,
		Error:   genResult.Error,
		Prompt:  genResult.Prompt,
	}

	// 检查是否需要 AI 处理
	if IsAIRequest(genResult) {
		result.IsAIRequest = true
		return result
	}

	// 处理生成结果
	result.Article = genResult.Article
	result.Quotes = genResult.Quotes

	// 生成标题
	result.Titles = a.generator.GenerateTitles(style, req.Input, 5)
	if len(result.Titles) > 0 {
		result.Title = result.Titles[0]
	}

	return result
}

// WriteFromFile 从文件读取并写作
func (a *Assistant) WriteFromFile(filePath string, styleName string) *WriteResult {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return &WriteResult{
			Success: false,
			Error:   fmt.Sprintf("读取文件: %v", err),
		}
	}

	req := &WriteRequest{
		Input:     string(content),
		InputType: InputTypeFragment,
		StyleName: styleName,
	}

	return a.Write(req)
}

// Refine 润色文章
func (a *Assistant) Refine(req *RefineRequest) *RefineResult {
	// 获取风格
	style, err := a.styleManager.GetStyle(req.StyleName)
	if err != nil {
		return &RefineResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	// 构建润色提示词
	prompt := a.buildRefinePrompt(style, req.Content, req.Feedback)

	return &RefineResult{
		Success: true,
		// 实际润色由 AI 完成
		Error: "AI_REFINE_REQUEST:" + prompt,
	}
}

// buildRefinePrompt 构建润色提示词
func (a *Assistant) buildRefinePrompt(style *WriterStyle, content, feedback string) string {
	var prompt strings.Builder

	prompt.WriteString(style.WritingPrompt)
	prompt.WriteString("\n\n")

	prompt.WriteString("## 润色任务\n")
	prompt.WriteString("请将以下内容用该风格重新润色：\n\n")

	prompt.WriteString("### 原文\n")
	prompt.WriteString(content)

	if feedback != "" {
		prompt.WriteString("\n\n### 用户反馈\n")
		prompt.WriteString(feedback)
	}

	prompt.WriteString("\n\n---\n\n")
	prompt.WriteString("请输出润色后的内容，保持原意，用该风格重新表达。")

	return prompt.String()
}

// IsRefineRequest 检查结果是否是润色请求
func IsRefineRequest(result *RefineResult) bool {
	return result != nil && result.Error != "" &&
		strings.HasPrefix(result.Error, "AI_REFINE_REQUEST:")
}

// ExtractRefineRequest 提取润色请求
func ExtractRefineRequest(result *RefineResult) string {
	if IsRefineRequest(result) {
		return strings.TrimPrefix(result.Error, "AI_REFINE_REQUEST:")
	}
	return ""
}

// ListStyles 列出所有可用风格
func (a *Assistant) ListStyles() *StyleListResult {
	styles := a.styleManager.ListStyles()

	return &StyleListResult{
		Styles:  styles,
		Success: true,
	}
}

// GetStyleInfo 获取风格详情
func (a *Assistant) GetStyleInfo(name string) (*WriterStyle, error) {
	return a.styleManager.GetStyle(name)
}

// GetDefaultStyle 获取默认风格
func (a *Assistant) GetDefaultStyle() (*WriterStyle, error) {
	return a.styleManager.GetDefaultStyle()
}

// GetStyleManager 获取风格管理器（用于外部访问）
func (a *Assistant) GetStyleManager() *StyleManager {
	return a.styleManager
}

// GeneratePrompt 生成提示词（用于 AI 调用）
func (a *Assistant) GeneratePrompt(req *WriteRequest) string {
	result := a.Write(req)
	if result.IsAIRequest {
		return result.Prompt
	}
	return ""
}

// SaveArticle 保存文章到文件
func (a *Assistant) SaveArticle(article, filePath string) error {
	return os.WriteFile(filePath, []byte(article), 0644)
}

// ValidateWriteRequest 验证写作请求
func (a *Assistant) ValidateWriteRequest(req *WriteRequest) error {
	if req.Input == "" {
		return NewInvalidInputError("请提供输入内容")
	}

	// 验证输入类型
	validInputType := false
	for _, t := range InputTypes {
		if req.InputType == t {
			validInputType = true
			break
		}
	}
	if !validInputType {
		req.InputType = InputTypeIdea // 默认为观点类型
	}

	// 验证文章类型
	validArticleType := false
	for _, t := range ArticleTypes {
		if req.ArticleType == t {
			validArticleType = true
			break
		}
	}
	if !validArticleType {
		req.ArticleType = ArticleTypeEssay // 默认为散文
	}

	// 验证长度
	validLength := false
	for _, l := range Lengths {
		if req.Length == l {
			validLength = true
			break
		}
	}
	if !validLength {
		req.Length = LengthMedium // 默认中等长度
	}

	return nil
}

// BuildInteractivePrompt 构建交互式提示词（用于引导用户）
func (a *Assistant) BuildInteractivePrompt() string {
	return `我可以帮你写文章。请告诉我：

1. 你想写什么主题或观点？
2. 用什么风格？（默认：Dan Koe 风格）

例如：
- "用 Dan Koe 风格写关于自律的文章"
- "我觉得年轻人都不爱读书了，用犀利点的风格写写"`
}

// GetStyleByName 根据名称获取风格（支持模糊匹配）
func (a *Assistant) GetStyleByName(name string) (*WriterStyle, error) {
	return a.styleManager.GetStyle(name)
}

// GetAvailableStyles 获取所有可用风格名称
func (a *Assistant) GetAvailableStyles() []string {
	return a.styleManager.ListStyleNames()
}

// GetStylesByCategory 获取指定分类的风格
func (a *Assistant) GetStylesByCategory(category string) []StyleSummary {
	return a.styleManager.GetStyleByCategory(category)
}

// GetAllCategories 获取所有分类
func (a *Assistant) GetAllCategories() []string {
	return a.styleManager.ListCategories()
}

// CreateStyleDirectory 创建风格目录
func (a *Assistant) CreateStyleDirectory() error {
	return a.styleManager.CreateStyleDirectory()
}

// ExportStyle 导出风格配置
func (a *Assistant) ExportStyle(style *WriterStyle, destPath string) error {
	return a.styleManager.ExportStyle(style, destPath)
}

// ReloadStyles 重新加载风格
func (a *Assistant) ReloadStyles() error {
	return a.styleManager.ReloadStyles()
}

// GetStyleCount 获取风格数量
func (a *Assistant) GetStyleCount() int {
	return a.styleManager.GetStyleCount()
}

// FormatStyleSummary 格式化风格摘要用于显示
func FormatStyleSummary(style StyleSummary) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📝 %s (%s)\n", style.Name, style.EnglishName))
	sb.WriteString(fmt.Sprintf("   分类: %s\n", style.Category))
	sb.WriteString(fmt.Sprintf("   描述: %s", style.Description))
	if style.CoverStyle != "" {
		sb.WriteString(fmt.Sprintf("\n   封面: %s", style.CoverStyle))
	}
	return sb.String()
}

// FormatStyleList 格式化风格列表用于显示
func FormatStyleList(styles []StyleSummary) string {
	if len(styles) == 0 {
		return "暂无可用风格。请在 writers/ 目录添加风格配置文件。"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("可用风格 (%d 个):\n\n", len(styles)))

	for _, style := range styles {
		sb.WriteString(FormatStyleSummary(style))
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// ParseStyleInput 解析风格输入（支持各种格式）
func ParseStyleInput(input string) string {
	input = strings.TrimSpace(input)

	// 如果为空，使用默认
	if input == "" {
		return DefaultStyleName
	}

	// 移除可能的前缀
	input = strings.TrimPrefix(input, "--style=")
	input = strings.TrimPrefix(input, "style:")
	input = strings.TrimPrefix(input, "风格:")

	return input
}
