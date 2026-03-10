package main

import (
	"fmt"
	"os"

	"github.com/royalrick/anbanwriter/app/humanizer"
	"github.com/spf13/cobra"
)

// humanizeCmd - AI 写作去痕命令（factory function）
func humanizeCmd() *cobra.Command {
	var (
		intensityFlag   string
		showChangesFlag bool
		outputFile      string
		aiResultFile    string
	)

	cmd := &cobra.Command{
		Use:   "humanize <file>",
		Short: "AI 写作去痕 - 去除文本中的 AI 生成痕迹",
		Long: `去除文本中的 AI 生成痕迹，使文章听起来更自然、更像人类书写。

两步工作流（Claude 代理模式）:
  Step 1: 生成去痕提示词，由 Claude 代理执行处理
  Step 2: 使用 --ai-result 验证结果、提取评分、写入输出文件

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
  # Step 1: 生成提示词（由 Claude 代理执行去痕处理）
  writer humanize article.md

  # Step 1: 指定强度和输出路径
  writer humanize article.md --intensity gentle -o output.md

  # Step 2: 验证代理结果、提取评分、写入输出文件
  writer humanize article.md --ai-result result.md -o output.md

  # 显示修改对比和质量评分
  writer humanize article.md --show-changes

  # 与写作风格组合使用
  writer write --style dan-koe --humanize
  writer write --style dan-koe --humanize=aggressive`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			filePath := args[0]

			// Step 2: 有 --ai-result 时处理代理返回的结果
			if aiResultFile != "" {
				if err := runHumanizeStep2(filePath, aiResultFile, outputFile, intensityFlag); err != nil {
					responseError(err)
				}
				return
			}

			// Step 1: 构建提示词
			if err := runHumanize(filePath, intensityFlag, showChangesFlag, outputFile); err != nil {
				responseError(err)
			}
		},
	}

	cmd.Flags().StringVarP(&intensityFlag, "intensity", "i", "medium", "处理强度: gentle/medium/aggressive")
	cmd.Flags().BoolVarP(&showChangesFlag, "show-changes", "c", false, "显示修改对比和质量评分")
	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "输出文件路径（Step 2 时写入人性化后的正文）")
	cmd.Flags().StringVar(&aiResultFile, "ai-result", "", "Step 2: Claude 代理处理后的结果文件")

	return cmd
}

func runHumanize(filePath, intensity string, showChanges bool, outputFile string) error {
	// 读取文件
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	// 构建请求
	req := &humanizer.HumanizeRequest{
		Content:       string(content),
		Intensity:     humanizer.ParseIntensity(intensity),
		ShowChanges:   showChanges,
		IncludeScore:  true,
		PreserveStyle: false,
	}

	// 构建提示词，返回给 Claude 代理处理
	h := humanizer.NewHumanizer()
	prompt := h.BuildAIRequestForAI(req)

	result := map[string]any{
		"type":      "humanize_prompt",
		"prompt":    prompt,
		"intensity": intensity,
		"file":      filePath,
	}
	if outputFile != "" {
		result["output_file"] = outputFile
	}

	responseSuccess(result)
	return nil
}

func runHumanizeStep2(filePath, aiResultFile, outputFile, intensity string) error {
	// 读取原始文件（用于构建请求上下文）
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("读取原始文件失败: %w", err)
	}

	// 读取 AI 代理处理后的结果
	aiResult, err := os.ReadFile(aiResultFile)
	if err != nil {
		return fmt.Errorf("读取 AI 结果文件失败: %w", err)
	}

	// 构建原始请求（用于 ParseAIResponse 回退时返回原文）
	req := &humanizer.HumanizeRequest{
		Content:   string(content),
		Intensity: humanizer.ParseIntensity(intensity),
	}

	// 解析 AI 结果
	h := humanizer.NewHumanizer()
	result := h.ParseAIResponse(string(aiResult), req)

	// 如果指定了输出文件，写入人性化后的正文
	if outputFile != "" && result.Content != "" {
		if err := os.WriteFile(outputFile, []byte(result.Content), 0644); err != nil {
			return fmt.Errorf("写入输出文件失败: %w", err)
		}
	}

	// 构建响应
	resp := map[string]any{
		"success":     result.Success,
		"content":     result.Content,
		"output_file": outputFile,
	}
	if result.Error != "" {
		resp["error"] = result.Error
	}
	if result.Report != "" {
		resp["report"] = result.Report
	}
	if result.Changes != nil {
		resp["changes"] = result.Changes
	}
	if result.Score != nil {
		resp["score"] = result.Score
	}

	responseSuccess(resp)
	return nil
}
