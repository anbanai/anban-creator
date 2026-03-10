package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// rednoteCmd 小红书发布相关命令组
func rednoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rednote",
		Short: "小红书内容导出工具（用于 MCP 工具发布前的内容准备）",
		Long: `小红书内容导出工具 —— 将 Markdown 内容格式化为小红书发布格式

平台说明：
  - 小红书发布通过 MCP 工具 publish_content() 进行
  - 本命令用于将内容导出为可直接复制到小红书 App 的格式

支持的操作：
  export    - 导出 Markdown 内容为小红书发布格式`,
	}

	cmd.AddCommand(rednoteExportCmd())

	return cmd
}

// rednoteExportCmd 导出小红书发布内容
func rednoteExportCmd() *cobra.Command {
	var (
		format  string
		output  string
		title   string
		content string
		images  string
		tags    string
	)

	cmd := &cobra.Command{
		Use:   "export [markdown_file]",
		Short: "导出 Markdown 为小红书发布格式（剪贴板/JSON/Markdown）",
		Long: `将 Markdown 文件或直接传参导出为小红书发布格式

两种使用模式：

模式1：从 Markdown 文件解析（传统方式）
  anbanwriter rednote export ./content.md

模式2：直接传参（新增）
  anbanwriter rednote export --title "标题" --content "正文内容" --images "a.png,b.png" --tags "tag1,tag2"

导出格式：
  clipboard    - 复制到剪贴板（默认），可直接粘贴到小红书 App
  json         - 输出 JSON 格式（包含 title/content/tags/images 字段）
  markdown     - 输出格式化 Markdown（适合人工审核）

内容处理（文件模式）：
  - 自动提取标题（第一个 # 标题）
  - 正文自动去除所有 # 话题标签
  - 自动收集所有 # 标签作为 tags 字段
  - 自动移除 Markdown 格式（保留段落和列表结构）

直接传参模式：
  - --title: 标题（必填）
  - --content: 正文内容（必填）
  - --images: 图片路径，逗号分隔（可选）
  - --tags: 标签，逗号分隔（可选）

示例:
  # 从文件导出到剪贴板（默认）
  anbanwriter rednote export ./content.md

  # 从文件导出为 JSON
  anbanwriter rednote export ./content.md --format json -o output.json

  # 直接传参导出
  anbanwriter rednote export --title "春季茶园攻略" \
    --content "春天是采茶的好季节..." \
    --images "cover.png,img2.png,img3.png" \
    --tags "茶园,春季,采茶" \
    --format json`,
		Args: cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfigMinimal()
		},
		Run: func(cmd *cobra.Command, args []string) {
			var result *XiaohongshuContent

			// 判断使用哪种模式
			if len(args) > 0 {
				// 模式1：从文件解析
				mdFile := args[0]

				// 读取 Markdown 文件
				data, err := os.ReadFile(mdFile)
				if err != nil {
					responseError(fmt.Errorf("读取文件失败: %w", err))
					return
				}

				// 解析内容
				content := string(data)
				result = parseXiaohongshuContent(content)
			} else {
				// 模式2：直接传参
				if title == "" || content == "" {
					responseError(fmt.Errorf("直接传参模式需要同时提供 --title 和 --content"))
					return
				}

				result = &XiaohongshuContent{
					Title:   title,
					Content: cleanContent(content),
					Tags:    []string{},
				}

				// 解析图片路径
				if images != "" {
					imageList := strings.Split(images, ",")
					for _, img := range imageList {
						img = strings.TrimSpace(img)
						if img != "" {
							result.Images = append(result.Images, img)
						}
					}
				}

				// 解析标签
				if tags != "" {
					tagList := strings.Split(tags, ",")
					for _, tag := range tagList {
						tag = strings.TrimSpace(tag)
						if tag != "" {
							result.Tags = append(result.Tags, tag)
						}
					}
				}
			}

			// 根据格式输出
			switch format {
			case "json":
				outputJSON(result, output)
			case "markdown":
				outputMarkdown(result, output)
			default: // clipboard
				outputClipboard(result)
			}
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "clipboard", "导出格式: clipboard/json/markdown")
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出文件路径（json/markdown 格式时有效）")
	cmd.Flags().StringVar(&title, "title", "", "标题（直接传参模式）")
	cmd.Flags().StringVar(&content, "content", "", "正文内容（直接传参模式）")
	cmd.Flags().StringVar(&images, "images", "", "图片路径，逗号分隔（直接传参模式，可选）")
	cmd.Flags().StringVar(&tags, "tags", "", "标签，逗号分隔（直接传参模式，可选）")

	return cmd
}

// XiaohongshuContent 小红书内容结构
type XiaohongshuContent struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Images  []string `json:"images,omitempty"`
}

// parseXiaohongshuContent 解析 Markdown 内容为小红书格式
func parseXiaohongshuContent(content string) *XiaohongshuContent {
	result := &XiaohongshuContent{
		Tags: []string{},
	}

	lines := strings.Split(content, "\n")
	var contentLines []string
	inCodeBlock := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 代码块边界
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}

		// 跳过代码块内容
		if inCodeBlock {
			continue
		}

		// 提取标题（第一个 # 标题）
		if result.Title == "" && strings.HasPrefix(trimmed, "# ") {
			result.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			// 限制标题长度（小红书标题通常不超过 20 字）
			if len([]rune(result.Title)) > 20 {
				runes := []rune(result.Title)
				result.Title = string(runes[:20])
			}
			continue
		}

		// 跳过其他 Markdown 标题标记
		if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "#标签") {
			// 将标题转换为加粗文本
			titleText := strings.TrimLeft(trimmed, "# ")
			contentLines = append(contentLines, "**"+titleText+"**")
			continue
		}

		// 提取 # 标签（包括中文标签）
		// 匹配 #中文标签 或 #english-tag 格式
		if !inCodeBlock {
			tags := extractTags(trimmed)
			result.Tags = append(result.Tags, tags...)
			// 移除标签后的行
			trimmed = removeTags(trimmed)
		}

		// 转换 Markdown 格式为纯文本
		trimmed = markdownToPlain(trimmed)

		// 跳过空行（但保留段落间隔）
		if trimmed == "" {
			if len(contentLines) > 0 && contentLines[len(contentLines)-1] != "" {
				contentLines = append(contentLines, "")
			}
			continue
		}

		contentLines = append(contentLines, trimmed)

		// 限制内容行数（小红书建议 300-500 字）
		if i > 100 && len(contentLines) > 50 {
			break
		}
	}

	// 清理并合并内容
	result.Content = cleanContent(strings.Join(contentLines, "\n"))

	// 去重标签
	result.Tags = uniqueStrings(result.Tags)

	return result
}

// extractTags 从文本中提取 # 标签
func extractTags(text string) []string {
	var tags []string
	// 匹配 #中文 或 #english-tag 格式，但不匹配 ## Markdown 标题
	re := regexp.MustCompile(`#([^#\s][^\s#]*)`)
	matches := re.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) > 1 {
			tag := strings.TrimSpace(match[1])
			// 过滤掉纯数字标签和过短标签
			if len(tag) >= 2 && len(tag) <= 20 {
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

// removeTags 从文本中移除 # 标签
func removeTags(text string) string {
	// 移除 #标签 但保留其他内容
	re := regexp.MustCompile(`#([^#\s][^\s#]*)`)
	return strings.TrimSpace(re.ReplaceAllString(text, ""))
}

// markdownToPlain 将 Markdown 转换为纯文本
func markdownToPlain(text string) string {
	// 移除图片 ![alt](url)
	imgRe := regexp.MustCompile(`!\[([^\]]*)\]\([^\)]+\)`)
	text = imgRe.ReplaceAllString(text, "")

	// 转换链接 [text](url) → text
	linkRe := regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`)
	text = linkRe.ReplaceAllString(text, "$1")

	// 转换加粗 **text** 或 __text__ → text
	boldRe := regexp.MustCompile(`\*\*([^\*]+)\*\*|__([^_]+)__`)
	text = boldRe.ReplaceAllString(text, "$1$2")

	// 转换斜体 *text* 或 _text_ → text
	italicRe := regexp.MustCompile(`\*([^\*]+)\*|_([^_]+)_`)
	text = italicRe.ReplaceAllString(text, "$1$2")

	// 转换行内代码 `code` → code
	codeRe := regexp.MustCompile("`([^`]+)`")
	text = codeRe.ReplaceAllString(text, "$1")

	// 转换删除线 ~~text~~ → text
	strikeRe := regexp.MustCompile(`~~([^~]+)~~`)
	text = strikeRe.ReplaceAllString(text, "$1")

	// 转换列表标记 - 或 * 或 1.
	listRe := regexp.MustCompile(`^[\s]*[-\*\d]+[\.\)]?\s*`)
	text = listRe.ReplaceAllString(text, "")

	// 清理多余的空格
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

// cleanContent 清理内容格式
func cleanContent(content string) string {
	// 移除多余的空行
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")

	// 移除行首行尾空白
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	content = strings.Join(lines, "\n")

	// 限制内容长度（小红书正文建议 300-800 字）
	runes := []rune(content)
	if len(runes) > 1000 {
		content = string(runes[:1000]) + "..."
	}

	return strings.TrimSpace(content)
}

// uniqueStrings 字符串去重
func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range slice {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// outputJSON 输出 JSON 格式
func outputJSON(content *XiaohongshuContent, outputPath string) {
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		responseError(fmt.Errorf("JSON 序列化失败: %w", err))
		return
	}

	if outputPath != "" {
		if err := os.WriteFile(outputPath, data, 0644); err != nil {
			responseError(fmt.Errorf("写入文件失败: %w", err))
			return
		}
		responseSuccess(map[string]interface{}{
			"file":    outputPath,
			"format":  "json",
			"message": "已导出到文件",
		})
	} else {
		fmt.Println(string(data))
	}
}

// outputMarkdown 输出 Markdown 格式
func outputMarkdown(content *XiaohongshuContent, outputPath string) {
	var sb strings.Builder

	sb.WriteString("# 小红书发布内容\n\n")

	sb.WriteString("## 标题\n\n")
	sb.WriteString(content.Title)
	sb.WriteString("\n\n")

	sb.WriteString("## 正文\n\n")
	sb.WriteString(content.Content)
	sb.WriteString("\n\n")

	// 图片信息
	if len(content.Images) > 0 {
		sb.WriteString("## 图片\n\n")
		for i, img := range content.Images {
			sb.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, img))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## 话题标签\n\n")
	if len(content.Tags) > 0 {
		sb.WriteString(strings.Join(content.Tags, " "))
	} else {
		sb.WriteString("（未检测到标签）")
	}
	sb.WriteString("\n")

	result := sb.String()

	if outputPath != "" {
		if err := os.WriteFile(outputPath, []byte(result), 0644); err != nil {
			responseError(fmt.Errorf("写入文件失败: %w", err))
			return
		}
		responseSuccess(map[string]interface{}{
			"file":    outputPath,
			"format":  "markdown",
			"message": "已导出到文件",
		})
	} else {
		fmt.Println(result)
	}
}

// outputClipboard 输出到剪贴板（通过 stdout 显示并提示用户复制）
func outputClipboard(content *XiaohongshuContent) {
	var sb strings.Builder

	// 标题（如果有）
	if content.Title != "" {
		sb.WriteString(content.Title)
		sb.WriteString("\n\n")
	}

	// 正文
	sb.WriteString(content.Content)

	// 标签（放在最后）
	if len(content.Tags) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString("#")
		sb.WriteString(strings.Join(content.Tags, " #"))
	}

	text := sb.String()

	// 输出到 stdout 并提示用户复制
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║  📋 小红书发布内容（请全选并复制以下内容）                  ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println(text)
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║  💡 提示：在小红书 App 中长按输入框粘贴即可                 ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")

	responseSuccess(map[string]interface{}{
		"format":  "clipboard",
		"title":   content.Title,
		"tags":    content.Tags,
		"message": "内容已输出，请全选复制后粘贴到小红书 App",
	})
}
