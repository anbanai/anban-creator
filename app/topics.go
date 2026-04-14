package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// TopicSuggestion AI 生成的话题建议
type TopicSuggestion struct {
	Topic      string   `json:"topic"`
	Angle      string   `json:"angle"`
	ViralScore int      `json:"viral_score"`
	Reason     string   `json:"reason"`
	Keywords   []string `json:"keywords"`
	Template   string   `json:"template"`
}

// topicsCmd 话题发现命令
func topicsCmd() *cobra.Command {
	var (
		domain       string
		count        int
		keywords     []string
		aiResultFile string
	)

	cmd := &cobra.Command{
		Use:     "topics",
		Aliases: []string{"topic"},
		Short:   "智能话题发现",
		Long: `基于公众号关键词和领域，生成高传播潜力的话题提示词，由 Claude 代理处理

两步工作流:
  Step 1: 生成话题建议提示词，由 Claude 代理执行
  Step 2: 使用 --ai-result 验证并返回结构化话题列表

Claude 代理会根据你的账号画像（关键词、领域）生成定制化的话题建议，
每个话题包含写作角度、病毒传播得分、推荐模板等信息。

示例:
  # 基于账号关键词生成话题（Step 1）
  abwriter topics

  # 指定领域和数量
  abwriter topics --domain "传统文化" --count 5

  # 指定额外关键词
  abwriter topics -k "茶文化" -k "养生"

  # Step 2: 验证代理返回的话题列表
  abwriter topics --ai-result topics.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initConfigMinimal(); err != nil {
				return fmt.Errorf("初始化配置失败: %w", err)
			}

			// Step 2: 验证代理返回的话题 JSON
			if aiResultFile != "" {
				return runTopicsStep2(aiResultFile)
			}

			// 从配置获取账号关键词，命令行关键词优先追加
			accountKeywords := cfg.Keywords
			for _, kw := range keywords {
				if !slices.Contains(accountKeywords, kw) {
					accountKeywords = append(accountKeywords, kw)
				}
			}
			if domain != "" && !slices.Contains(accountKeywords, domain) {
				accountKeywords = append([]string{domain}, accountKeywords...)
			}

			prompt := buildTopicsPrompt(accountKeywords, domain, count, cfg.Positioning)
			responseSuccess(map[string]any{
				"type":   "topics_prompt",
				"prompt": prompt,
				"domain": domain,
				"count":  count,
			})

			return nil
		},
	}

	cmd.Flags().StringVarP(&domain, "domain", "d", "", "内容领域（如：茶文化、健康养生）")
	cmd.Flags().IntVarP(&count, "count", "n", 5, "生成话题数量（1-10）")
	cmd.Flags().StringSliceVarP(&keywords, "keywords", "k", []string{}, "额外关键词（优先追加到账号关键词）")
	cmd.Flags().StringVar(&aiResultFile, "ai-result", "", "Step 2: Claude 代理生成的话题 JSON 文件")

	return cmd
}

// buildTopicsPrompt 构建话题发现提示词（供 Claude 代理使用）
func buildTopicsPrompt(keywords []string, domain string, count int, positioning string) string {
	if count < 1 {
		count = 1
	}
	if count > 10 {
		count = 10
	}

	kwStr := strings.Join(keywords, "、")
	if kwStr == "" {
		kwStr = "通用内容"
	}

	domainHint := domain
	if domainHint == "" {
		if len(keywords) > 0 {
			domainHint = keywords[0]
		} else {
			domainHint = "通用"
		}
	}

	return fmt.Sprintf(`你是一位微信公众号内容策略专家，专注于%s领域。
请根据以下账号关键词，推荐%d个高传播潜力的文章话题，以 JSON 数组格式输出。

账号关键词: %s
账号定位: %s

请输出严格的 JSON 数组格式（不要包含任何额外文字）:
[
  {
    "topic": "具体的文章话题标题方向",
    "angle": "独特的写作角度（一句话）",
    "viral_score": 85,
    "reason": "为什么这个话题有传播潜力（1-2句话）",
    "keywords": ["关键词1", "关键词2", "关键词3"],
    "template": "authoritative"
  }
]

template 必须是以下之一: authoritative（权威揭秘）, comparison（对比评测）, cultural（文化故事）, practical（实用干货）
viral_score 范围 1-100，越高说明传播潜力越大

要求：
- 话题要接地气，贴近用户实际需求
- 角度要独特，避免老生常谈
- 关键词要与账号定位强相关
- 重点关注近期热点和常青话题的结合`, domainHint, count, kwStr, positioning)
}

// runTopicsStep2 解析并验证代理生成的话题 JSON
func runTopicsStep2(aiResultFile string) error {
	data, err := os.ReadFile(aiResultFile)
	if err != nil {
		return fmt.Errorf("读取话题文件失败: %w", err)
	}

	// 尝试提取 JSON
	jsonData := extractJSON(string(data))

	var topics []TopicSuggestion
	if err := json.Unmarshal([]byte(jsonData), &topics); err != nil {
		return fmt.Errorf("解析话题 JSON 失败: %w\n💡 提示: 确保文件包含有效的 JSON 数组格式", err)
	}

	if len(topics) == 0 {
		return fmt.Errorf("话题列表为空")
	}

	// 验证每个话题的必填字段
	for i, t := range topics {
		if t.Topic == "" {
			return fmt.Errorf("话题 #%d 缺少必填字段: topic", i+1)
		}
	}

	responseSuccess(map[string]any{
		"topics": topics,
		"count":  len(topics),
	})
	return nil
}
