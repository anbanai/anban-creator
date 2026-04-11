// Package main provides the writer CLI tool
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/royalrick/anbanwriter/app/writer"
	"github.com/spf13/cobra"
)

// writeCmd 写作命令
var writeCmd = &cobra.Command{
	Use:   "write [input]",
	Short: "Writer Style Assistant - Write with creator styles",
	Long: `Assisted writing with customizable creator styles.

Default style: Dan Koe (profound, sharp, grounded)

Examples:
  # Interactive mode
  writer write

  # Write from idea
  writer write --style dan-koe

  # Refine existing content
  writer write --style dan-koe --input-type fragment article.md

  # Generate with cover
  writer write --style dan-koe --cover

  # Write with AI trace removal
  writer write --style dan-koe --humanize
  writer write --style dan-koe --humanize --humanize-intensity aggressive`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := runWrite(cmd, args); err != nil {
			responseError(err)
		}
	},
}

// write 命令参数
var (
	writeStyle             string
	writeInputType         string
	writeArticleType       string
	writeLength            string
	writeTitle             string
	writeCover             bool
	writeCoverOnly         bool
	writeListStyles        bool
	writeStyleDetail       bool
	writeHumanize          bool
	writeHumanizeIntensity string
)

func init() {
	// 添加 flags
	writeCmd.Flags().StringVar(&writeStyle, "style", "", "Writer style (default: from config or 'dan-koe')")
	writeCmd.Flags().StringVar(&writeInputType, "input-type", "idea", "Input type: idea/fragment/outline/title")
	writeCmd.Flags().StringVar(&writeArticleType, "article-type", "essay", "Article type: essay/commentary/story/tutorial/review")
	writeCmd.Flags().StringVar(&writeLength, "length", "medium", "Article length: short/medium/long")
	writeCmd.Flags().StringVar(&writeTitle, "title", "", "Article title")
	writeCmd.Flags().BoolVar(&writeCover, "cover", false, "Generate matching cover")
	writeCmd.Flags().BoolVar(&writeCoverOnly, "cover-only", false, "Generate cover only")
	writeCmd.Flags().BoolVar(&writeListStyles, "list", false, "List all available styles")
	writeCmd.Flags().BoolVar(&writeStyleDetail, "detail", false, "Show detailed style info")

	// Humanizer flags
	writeCmd.Flags().BoolVar(&writeHumanize, "humanize", false, "Enable AI trace removal")
	writeCmd.Flags().StringVar(&writeHumanizeIntensity, "humanize-intensity", "medium", "Humanize intensity: gentle/medium/aggressive")
}

// runWrite 执行写作命令
func runWrite(cmd *cobra.Command, args []string) error {
	// 处理列出风格
	if writeListStyles {
		return runListStyles()
	}

	// 回退到配置文件默认值
	if !cmd.Flags().Changed("style") {
		if err := initConfigMinimal(); err == nil && cfg.Wechat.Article.Style != "" {
			writeStyle = cfg.Wechat.Article.Style
		}
	}

	// 如果仍然为空，使用硬编码默认值
	if writeStyle == "" {
		writeStyle = "dan-koe"
	}

	// 获取输入内容
	input := ""
	if len(args) > 0 {
		// 从文件读取
		content, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("读取文件: %w", err)
		}
		input = string(content)

		// 如果没有明确指定输入类型，默认为 fragment
		if writeInputType == "idea" {
			writeInputType = "fragment"
		}
	} else {
		// 检查 stdin 是否有输入
		stdinContent, err := readStdin()
		if err == nil && stdinContent != "" {
			input = stdinContent
		}
	}

	// 如果没有输入，进入交互模式
	if input == "" {
		return runInteractiveWrite()
	}

	// 执行写作
	return executeWrite(input)
}

// runListStyles 列出所有风格
func runListStyles() error {
	asst := writer.NewAssistant()
	result := asst.ListStyles()

	if !result.Success {
		return fmt.Errorf("%s", result.Error)
	}

	responseSuccess(map[string]any{"styles": result.Styles})
	return nil
}

// runInteractiveWrite 交互式写作模式
func runInteractiveWrite() error {
	fmt.Println("📝 Writer Style Assistant")
	fmt.Println()

	// 显示可用风格
	asst := writer.NewAssistant()
	styles := asst.GetAvailableStyles()

	fmt.Printf("可用风格 (%d 个):\n", len(styles))
	for _, styleName := range styles {
		style, _ := asst.GetStyleInfo(styleName)
		fmt.Printf("  - %s (%s)\n", style.Name, style.EnglishName)
	}
	fmt.Println()

	// 获取输入
	fmt.Print("请选择风格 [默认: dan-koe]: ")
	styleInput := readLine()
	if styleInput == "" {
		styleInput = "dan-koe"
	}

	fmt.Print("请输入你的观点或内容 (Ctrl+D 结束):\n")
	input := readMultiline()
	if strings.TrimSpace(input) == "" {
		return fmt.Errorf("输入不能为空")
	}

	// 构建请求
	req := &writer.WriteRequest{
		Input:     input,
		InputType: writer.GetInputTypeFromString(writeInputType),
		StyleName: styleInput,
		Length:    writer.GetLengthFromString(writeLength),
	}

	// 执行写作：返回提示词给 Claude 代理处理
	result := asst.Write(req)

	if result.IsAIRequest {
		data := map[string]any{
			"type":               "write_prompt",
			"prompt":             result.Prompt,
			"style":              styleInput,
			"humanize":           writeHumanize,
			"humanize_intensity": writeHumanizeIntensity,
		}
		responseSuccess(data)
		return nil
	}

	if !result.Success && result.Article == "" {
		return fmt.Errorf("%s", result.Error)
	}

	// 输出结果
	data := map[string]any{
		"article": result.Article,
		"quotes":  result.Quotes,
		"style":   styleInput,
	}

	if writeCover {
		coverGen := writer.NewCoverGenerator(asst.GetStyleManager())
		coverResult, _ := coverGen.GeneratePrompt(&writer.GenerateCoverRequest{
			StyleName:      styleInput,
			ArticleContent: input,
		})
		if coverResult.Success {
			data["cover_prompt"] = coverResult.Prompt
		}
	}

	responseSuccess(data)
	return nil
}

// executeWrite 执行写作
func executeWrite(input string) error {
	asst := writer.NewAssistant()

	req := &writer.WriteRequest{
		Input:     input,
		InputType: writer.GetInputTypeFromString(writeInputType),
		StyleName: writer.ParseStyleInput(writeStyle),
		Title:     writeTitle,
		Length:    writer.GetLengthFromString(writeLength),
	}

	result := asst.Write(req)

	if result.IsAIRequest {
		// 返回提示词给 Claude 代理处理
		data := map[string]any{
			"type":               "write_prompt",
			"prompt":             result.Prompt,
			"style":              req.StyleName,
			"humanize":           writeHumanize,
			"humanize_intensity": writeHumanizeIntensity,
		}
		responseSuccess(data)
		return nil
	}

	if !result.Success && result.Article == "" {
		return fmt.Errorf("%s", result.Error)
	}

	// 只生成封面
	if writeCoverOnly {
		coverPrompt, err := generateCover(asst, req)
		if err != nil {
			return err
		}
		responseSuccess(map[string]any{"cover_prompt": coverPrompt})
		return nil
	}

	// 输出文章
	data := map[string]any{
		"article": result.Article,
		"quotes":  result.Quotes,
		"style":   req.StyleName,
	}

	if writeCover {
		coverPrompt, err := generateCover(asst, req)
		if err != nil {
			return err
		}
		data["cover_prompt"] = coverPrompt
	}

	responseSuccess(data)
	return nil
}

// generateCover 生成封面，返回封面提示词
func generateCover(asst *writer.Assistant, req *writer.WriteRequest) (string, error) {
	coverGen := writer.NewCoverGenerator(asst.GetStyleManager())

	coverReq := &writer.GenerateCoverRequest{
		StyleName:      req.StyleName,
		ArticleTitle:   req.Title,
		ArticleContent: req.Input,
	}

	result, err := coverGen.GeneratePrompt(coverReq)
	if err != nil {
		return "", fmt.Errorf("生成封面提示词: %w", err)
	}

	return result.Prompt, nil
}

// readLine 读取一行输入
func readLine() string {
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// readMultiline 读取多行输入（空行保留，Ctrl+D 结束）
func readMultiline() string {
	scanner := bufio.NewScanner(os.Stdin)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return strings.Join(lines, "\n")
}

// readStdin 读取标准输入，如果 stdin 为空或来自终端则返回空字符串
func readStdin() (string, error) {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}

	// 检查 stdin 是否来自管道或重定向（而非终端）
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return "", nil // 来自终端，无管道输入
	}

	// 读取所有 stdin 内容
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(content)), nil
}
