package main

import (
	"fmt"
	"os"

	"github.com/royalrick/wechatwriter/app/humanizer"
	"github.com/spf13/cobra"
)

var (
	intensityFlag   string
	showChangesFlag bool
)

// humanizeCmd - AI 写作去痕命令
var humanizeCmd = &cobra.Command{
	Use:   "humanize <file>",
	Short: "AI 写作去痕 - 去除文本中的 AI 生成痕迹",
	Long: `去除文本中的 AI 生成痕迹，使文章听起来更自然、更像人类书写。

基于 humanizer-zh 方法，检测并处理 24 种 AI 写作痕迹模式：
  • 内容模式：过度强调、夸大意义、宣传语言、模糊归因
  • 语言语法：AI 词汇、否定排比、三段式、同义词循环
  • 风格模式：破折号过度、粗体滥用、表情符号
  • 填充词回避：填充短语、过度限定、通用结论
  • 协作痕迹：对话式填充、知识截止免责声明

处理强度:
  gentle      - 温和处理，只修改明显的问题
  medium      - 中等强度 (默认)
  aggressive  - 激进处理，深度去除 AI 痕迹

示例:
  # 基本用法
  writer humanize article.md

  # 指定强度
  writer humanize article.md --intensity gentle

  # 显示修改对比和质量评分
  writer humanize article.md --show-changes

  # 输出到文件
  writer humanize article.md -o output.md

  # 与写作风格组合使用
  writer write --style dan-koe --humanize
  writer write --style dan-koe --humanize=aggressive`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := runHumanize(args[0]); err != nil {
			responseError(err)
		}
	},
}

func runHumanize(filePath string) error {
	// 读取文件
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	// 构建请求
	req := &humanizer.HumanizeRequest{
		Content:       string(content),
		Intensity:     humanizer.ParseIntensity(intensityFlag),
		ShowChanges:   showChangesFlag,
		IncludeScore:  true,
		PreserveStyle: false,
	}

	// 构建提示词，返回给 Claude 代理处理
	h := humanizer.NewHumanizer()
	prompt := h.BuildAIRequestForAI(req)

	responseSuccess(map[string]any{
		"type":      "humanize_prompt",
		"prompt":    prompt,
		"intensity": intensityFlag,
		"file":      filePath,
	})
	return nil
}

func init() {
	humanizeCmd.Flags().StringVarP(&intensityFlag, "intensity", "i", "medium", "处理强度: gentle/medium/aggressive")
	humanizeCmd.Flags().BoolVarP(&showChangesFlag, "show-changes", "c", false, "显示修改对比和质量评分")
}
