package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func seoCmd() *cobra.Command {
	var (
		title        string
		keywords     []string
		aiResultFile string
	)

	cmd := &cobra.Command{
		Use:   "seo <file>",
		Short: "微信搜一搜 SEO 优化分析",
		Long: `分析文章并提供微信搜一搜 SEO 优化建议

两步工作流:
  Step 1: 生成 SEO 分析提示词，由 Claude 代理执行
  Step 2: 使用 --ai-result 解析并格式化优化建议

分析维度：
  • 标题优化（搜一搜搜索匹配）
  • 关键词密度与分布
  • 内容结构（标题层级、段落长度）
  • 摘要/digest 优化建议
  • 可读性评分

示例:
  # Step 1: 生成 SEO 分析提示词
  wechatwriter seo article.md -k "茶文化" -k "养生"

  # 指定文章标题
  wechatwriter seo article.md --title "一杯茶的养生秘密" -k "茶"

  # Step 2: 解析代理返回的分析结果
  wechatwriter seo article.md --ai-result seo-result.md`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			filePath := args[0]

			// Step 2: 解析代理返回的 SEO 分析结果
			if aiResultFile != "" {
				if err := runSEOStep2(aiResultFile); err != nil {
					responseError(err)
				}
				return
			}

			// Step 1: 构建 SEO 分析提示词
			if err := runSEO(filePath, title, keywords); err != nil {
				responseError(err)
			}
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", "文章标题（可选，辅助 SEO 分析）")
	cmd.Flags().StringSliceVarP(&keywords, "keywords", "k", []string{}, "目标关键词")
	cmd.Flags().StringVar(&aiResultFile, "ai-result", "", "Step 2: Claude 代理返回的 SEO 分析结果文件")

	return cmd
}

func runSEO(filePath, title string, keywords []string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	prompt := buildSEOPrompt(string(content), title, keywords)

	result := map[string]any{
		"type":     "seo_prompt",
		"prompt":   prompt,
		"file":     filePath,
		"keywords": keywords,
	}
	if title != "" {
		result["title"] = title
	}

	responseSuccess(result)
	return nil
}

func buildSEOPrompt(content, title string, keywords []string) string {
	kwStr := strings.Join(keywords, "、")
	if kwStr == "" {
		kwStr = "（未指定，请从文章中提取核心关键词）"
	}

	titleLine := ""
	if title != "" {
		titleLine = fmt.Sprintf("文章标题: %s\n", title)
	}

	return fmt.Sprintf(`请对以下微信公众号文章进行搜一搜 SEO 优化分析，以 JSON 格式输出结构化建议。

%s目标关键词: %s

文章内容:
---
%s
---

请输出严格的 JSON 格式（不要包含任何额外文字）:
{
  "overall_score": 75,
  "title_analysis": {
    "score": 8,
    "current_title": "当前标题",
    "suggestions": ["优化建议1", "优化建议2"],
    "optimized_titles": ["优化标题1", "优化标题2"]
  },
  "keyword_analysis": {
    "score": 7,
    "target_keywords": ["关键词1", "关键词2"],
    "density_issues": ["关键词密度问题描述"],
    "suggestions": ["关键词分布建议"]
  },
  "structure_analysis": {
    "score": 8,
    "heading_issues": ["标题层级问题"],
    "paragraph_issues": ["段落问题"],
    "suggestions": ["结构优化建议"]
  },
  "digest_suggestion": "推荐的文章摘要（120字以内，包含核心关键词）",
  "readability_score": 8,
  "priority_actions": ["最高优先级优化动作1", "最高优先级优化动作2", "最高优先级优化动作3"]
}

评分说明：overall_score 满分100，各维度满分10分
分析重点：微信搜一搜搜索匹配、用户阅读体验、关键词自然融入`, titleLine, kwStr, content)
}

func runSEOStep2(aiResultFile string) error {
	data, err := os.ReadFile(aiResultFile)
	if err != nil {
		return fmt.Errorf("读取 SEO 分析文件失败: %w", err)
	}

	// 尝试提取 JSON
	jsonData := extractJSON(string(data))

	// 验证是有效 JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		return fmt.Errorf("解析 SEO 分析 JSON 失败: %w\n💡 提示: 确保文件包含有效的 JSON 格式", err)
	}

	responseSuccess(result)
	return nil
}
