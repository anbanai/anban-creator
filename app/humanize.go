package main

import (
	"context"
	"fmt"
	"os"

	"github.com/royalrick/wechatwriter/app/ai"
	"github.com/royalrick/wechatwriter/app/humanizer"
	"github.com/spf13/cobra"
)

var (
	intensityFlag   string
	showChangesFlag bool
	outputFlag      string
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

	// 加载配置（不验证微信账号）
	if err := initConfigMinimal(); err != nil {
		return fmt.Errorf("初始化配置失败: %w", err)
	}

	// 创建 AI 客户端
	aiClient, err := ai.NewClient(&cfg.AI)
	if err != nil {
		return fmt.Errorf("创建 AI 客户端失败: %w", err)
	}

	// 构建请求
	req := &humanizer.HumanizeRequest{
		Content:       string(content),
		Intensity:     humanizer.ParseIntensity(intensityFlag),
		ShowChanges:   showChangesFlag,
		IncludeScore:  true,
		PreserveStyle: false,
	}

	// 构建提示词并调用 AI
	h := humanizer.NewHumanizer()
	prompt := h.BuildAIRequestForAI(req)

	aiResponse, err := aiClient.ChatCompletion(context.Background(), prompt)
	if err != nil {
		return fmt.Errorf("AI 处理失败: %w", err)
	}

	// 解析 AI 响应
	result := h.ParseAIResponse(aiResponse, req)

	if !result.Success {
		return fmt.Errorf("解析 AI 结果失败: %s", result.Error)
	}

	// 输出结果
	if outputFlag != "" {
		if err := os.WriteFile(outputFlag, []byte(result.Content), 0644); err != nil {
			return fmt.Errorf("写入输出文件失败: %w", err)
		}
	} else {
		fmt.Println(result.Content)
	}

	return nil
}

func init() {
	humanizeCmd.Flags().StringVarP(&intensityFlag, "intensity", "i", "medium", "处理强度: gentle/medium/aggressive")
	humanizeCmd.Flags().BoolVarP(&showChangesFlag, "show-changes", "c", false, "显示修改对比和质量评分")
	humanizeCmd.Flags().StringVarP(&outputFlag, "output", "o", "", "输出文件路径")
}

