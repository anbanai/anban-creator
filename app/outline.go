package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// ContentRequest 内容生成请求
type ContentRequest struct {
	Topic    string   `json:"topic"`
	Template string   `json:"template"`
	Domain   string   `json:"domain"`
	Style    string   `json:"style"`
	Keywords []string `json:"keywords"`
}

// GeneratedContent 生成的内容框架
type GeneratedContent struct {
	Title         string           `json:"title"`
	Subtitle      string           `json:"subtitle"`
	Hook          string           `json:"hook"`
	Sections      []ContentSection `json:"sections"`
	KeyPoints     []string         `json:"key_points"`
	CallToAction  string           `json:"call_to_action"`
	ViralElements []string         `json:"viral_elements"`
	SEOKeywords   []string         `json:"seo_keywords"`
}

// ContentSection 内容段落
type ContentSection struct {
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	KeyPoints  []string `json:"key_points"`
	Engagement string   `json:"engagement"`
}

func outlineCmd() *cobra.Command {
	var (
		topic        string
		template     string
		domain       string
		style        string
		keywords     []string
		aiResultFile string
	)

	cmd := &cobra.Command{
		Use:   "outline",
		Short: "生成爆款内容框架",
		Long: `基于话题和模板生成文章框架

使用 Claude 代理生成更有针对性的框架内容（两步工作流）：
  Step 1: 返回大纲生成提示词，由 Claude 代理执行
  Step 2: 使用 --ai-result 验证并返回结构化大纲 JSON

支持的模板：
- authoritative: 权威揭秘型
- comparison: 对比评测型
- cultural: 文化故事型
- practical: 实用干货型

支持的风格：
- dan-koe: Dan Koe 风格（简洁有力）
- cultural-depth: 深度文化风格
- casual-science: 轻松科普风格

示例:
  # Step 1: 生成提示词
  wechatwriter outline -t "茶叶养生" -k "绿茶,健康,抗氧化"

  # Step 2: 验证代理返回的大纲 JSON
  wechatwriter outline -t "茶叶养生" --ai-result outline.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 回退到配置文件默认值
			if !cmd.Flags().Changed("style") {
				if err := initConfigMinimal(); err == nil && cfg.Wechat.Article.Style != "" {
					style = cfg.Wechat.Article.Style
				}
			}

			// 如果仍然为空，使用硬编码默认值
			if style == "" {
				style = "dan-koe"
			}

			// Step 2: 验证代理返回的大纲 JSON
			if aiResultFile != "" {
				return runOutlineStep2(aiResultFile)
			}

			// Step 1: 构建提示词，返回给 Claude 代理处理
			prompt := buildOutlinePrompt(topic, template, style, keywords)
			responseSuccess(map[string]any{
				"type":     "outline_prompt",
				"prompt":   prompt,
				"topic":    topic,
				"template": template,
				"style":    style,
			})
			return nil
		},
	}

	cmd.Flags().StringVarP(&topic, "topic", "t", "", "话题（必填）")
	cmd.Flags().StringVar(&template, "template", "authoritative", "模板类型")
	cmd.Flags().StringVarP(&domain, "domain", "d", "tea", "领域配置")
	cmd.Flags().StringVarP(&style, "style", "s", "", "写作风格 (default: from config or 'dan-koe')")
	cmd.Flags().StringSliceVarP(&keywords, "keywords", "k", []string{}, "关键词列表")
	cmd.Flags().StringVar(&aiResultFile, "ai-result", "", "Step 2: Claude 代理生成的大纲 JSON 文件")

	cmd.MarkFlagRequired("topic")

	_ = domain // domain is kept as a flag for future use

	return cmd
}

// buildOutlinePrompt 构建大纲提示词（供 Claude 代理使用）
func buildOutlinePrompt(topic, templateType, style string, keywords []string) string {
	kwStr := strings.Join(keywords, "、")
	if kwStr == "" {
		kwStr = "（无）"
	}

	return fmt.Sprintf(`请为以下微信公众号文章生成一个详细的内容框架，以 JSON 格式输出。

话题: %s
模板类型: %s
写作风格: %s
关键词: %s

请输出严格的 JSON 格式，结构如下（不要包含任何额外文字）:
{
  "title": "吸引眼球的文章标题",
  "subtitle": "副标题或引导语",
  "hook": "开头钩子句（吸引读者继续阅读）",
  "sections": [
    {
      "title": "1. 节标题",
      "content": "该节的核心内容描述（2-3句话）",
      "key_points": ["要点一", "要点二", "要点三"],
      "engagement": "该节的互动引导语"
    }
  ],
  "key_points": ["全文核心要点一", "全文核心要点二", "全文核心要点三"],
  "call_to_action": "结尾行动号召",
  "viral_elements": ["传播元素一", "传播元素二"],
  "seo_keywords": ["SEO关键词一", "SEO关键词二", "SEO关键词三"]
}

要求：
- 标题要有冲击力和好奇心驱动
- 钩子要能在3秒内抓住读者注意力
- 各节内容要具体，紧扣关键词
- 结构要符合%s模板类型的逻辑`, topic, templateType, style, kwStr, templateType)
}

// runOutlineStep2 解析并验证代理生成的大纲 JSON
func runOutlineStep2(aiResultFile string) error {
	data, err := os.ReadFile(aiResultFile)
	if err != nil {
		return fmt.Errorf("读取大纲文件失败: %w", err)
	}

	// 尝试提取 JSON（处理代理可能包含 markdown 代码块的情况）
	jsonData := extractJSON(string(data))

	var outline GeneratedContent
	if err := json.Unmarshal([]byte(jsonData), &outline); err != nil {
		return fmt.Errorf("解析大纲 JSON 失败: %w\n💡 提示: 确保文件包含有效的 JSON 格式大纲", err)
	}

	// 验证必填字段
	if outline.Title == "" {
		return fmt.Errorf("大纲缺少必填字段: title")
	}
	if outline.Hook == "" {
		return fmt.Errorf("大纲缺少必填字段: hook")
	}
	if len(outline.Sections) == 0 {
		return fmt.Errorf("大纲缺少 sections 内容")
	}

	responseSuccess(outline)
	return nil
}

// extractJSON 从可能包含 markdown 代码块的文本中提取 JSON
func extractJSON(text string) string {
	// 查找 ```json 或 ``` 代码块
	start := strings.Index(text, "```json")
	if start != -1 {
		text = text[start+7:]
		end := strings.Index(text, "```")
		if end != -1 {
			return strings.TrimSpace(text[:end])
		}
	}
	start = strings.Index(text, "```")
	if start != -1 {
		text = text[start+3:]
		end := strings.Index(text, "```")
		if end != -1 {
			return strings.TrimSpace(text[:end])
		}
	}

	// 尝试直接查找 JSON 对象或数组
	objStart := strings.Index(text, "{")
	arrStart := strings.Index(text, "[")
	if objStart != -1 && (arrStart == -1 || objStart < arrStart) {
		return strings.TrimSpace(text[objStart:])
	}
	if arrStart != -1 {
		return strings.TrimSpace(text[arrStart:])
	}

	return strings.TrimSpace(text)
}
