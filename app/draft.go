package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/royalrick/wechatwriter/app/draft"
	"github.com/spf13/cobra"
)

// sanitizeJSON 清理 JSON 中的中文引号和其他非标准字符
func sanitizeJSON(data []byte) []byte {
	// 替换中文双引号为转义的英文双引号
	result := bytes.ReplaceAll(data, []byte{0xe2, 0x80, 0x9c}, []byte("\\\""))  // " (U+201C)
	result = bytes.ReplaceAll(result, []byte{0xe2, 0x80, 0x9d}, []byte("\\\"")) // " (U+201D)
	// 替换中文单引号为英文单引号
	result = bytes.ReplaceAll(result, []byte{0xe2, 0x80, 0x98}, []byte("'")) // ' (U+2018)
	result = bytes.ReplaceAll(result, []byte{0xe2, 0x80, 0x99}, []byte("'")) // ' (U+2019)
	return result
}

// draftCmd 草稿管理命令组
func draftCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "draft",
		Short: "草稿管理（创建、测试、发布）",
		Long: `草稿管理命令组

支持的操作：
  article     - 从 JSON 文件创建图文文章草稿
  test        - 测试草稿 HTML
  publish     - 创建并发布草稿
  post        - 创建小绿书帖子（图片消息）`,
	}

	cmd.AddCommand(draftArticleCmd())
	cmd.AddCommand(draftTestCmd())
	cmd.AddCommand(draftPublishCmd())
	cmd.AddCommand(draftListCmd())
	cmd.AddCommand(draftPostCmd())

	return cmd
}

// draftArticleCmd 从 JSON 创建图文文章草稿
func draftArticleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "article <json_file>",
		Short: "从 JSON 文件创建微信图文文章草稿",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			jsonFile := args[0]
			svc := draft.NewService(cfg, log)

			// 读取 JSON 文件
			data, err := os.ReadFile(jsonFile)
			if err != nil {
				responseError(fmt.Errorf("read file: %w", err))
				return
			}

			// 清理 JSON 中的中文引号
			data = sanitizeJSON(data)

			// 解析请求
			var req struct {
				Articles []draft.Article `json:"articles"`
			}
			if err := json.Unmarshal(data, &req); err != nil {
				responseError(fmt.Errorf("parse json: %w", err))
				return
			}

			// 验证
			if len(req.Articles) == 0 {
				responseError(&AppError{Message: "no articles in request", HintText: "JSON 文件需包含 articles 数组，格式: {\"articles\": [...]}"})
				return
			}

			// 创建草稿
			result, err := svc.CreateDraft(req.Articles)
			if err != nil {
				responseError(err)
				return
			}
			responseSuccess(result)
		},
	}

	return cmd
}

// draftTestCmd 测试草稿
func draftTestCmd() *cobra.Command {
	var (
		title  string
		digest string
	)

	cmd := &cobra.Command{
		Use:   "test <html_file> <cover_image>",
		Short: "测试从 HTML 文件创建微信草稿",
		Args:  cobra.ExactArgs(2),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			htmlFile := args[0]
			coverImage := args[1]

			// 读取 HTML
			html, err := os.ReadFile(htmlFile)
			if err != nil {
				responseError(fmt.Errorf("read HTML file: %w", err))
				return
			}

			// 上传封面图片
			coverMediaID, err := uploadCoverImage(coverImage)
			if err != nil {
				responseError(fmt.Errorf("upload cover: %w", err))
				return
			}

			// 创建草稿
			svc := draft.NewService(cfg, log)
			articles := []draft.Article{
				{
					Title:        title,
					Content:      string(html),
					Digest:       digest,
					ThumbMediaID: coverMediaID,
					ShowCoverPic: 1,
				},
			}

			result, err := svc.CreateDraft(articles)
			if err != nil {
				responseError(fmt.Errorf("create draft: %w", err))
				return
			}

			responseSuccess(map[string]any{
				"success":   true,
				"media_id":  result.MediaID,
				"draft_url": result.DraftURL,
				"message":   "Draft created successfully!",
			})
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", time.Now().Format("2006-01-02 15:04:05"), "文章标题")
	cmd.Flags().StringVar(&digest, "digest", "的微信公众号文章草稿", "文章摘要")

	return cmd
}

// draftPublishCmd 创建并发布草稿
func draftPublishCmd() *cobra.Command {
	var (
		title     string
		content   string
		author    string
		digest    string
		coverID   string
		outputDir string
		images    string
	)

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "创建草稿并上传到微信公众号",
		Long: `将文章内容上传到微信公众号草稿箱

需要在配置文件中设置微信公众号账号信息。
使用 'wechatwriter account init' 创建配置文件。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPublish(title, content, author, digest, coverID, outputDir, images)
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", "文章标题（必填）")
	cmd.Flags().StringVarP(&content, "content", "c", "", "文章内容（HTML格式）")
	cmd.Flags().StringVarP(&author, "author", "a", "", "作者名称")
	cmd.Flags().StringVar(&digest, "digest", "", "文章摘要")
	cmd.Flags().StringVar(&coverID, "cover", "", "封面图片Media ID")
	cmd.Flags().StringVarP(&outputDir, "output", "o", ".", "输出目录（保存草稿JSON）")
	cmd.Flags().StringVar(&images, "images", "", "图片URL列表（逗号分隔）")

	cmd.MarkFlagRequired("title")
	cmd.MarkFlagRequired("content")

	return cmd
}

// draftListCmd 列出草稿
func draftListCmd() *cobra.Command {
	var (
		offset int64
		count  int64
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出微信公众号草稿箱",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			svc := draft.NewService(cfg, log)
			result, err := svc.ListDrafts(offset, count)
			if err != nil {
				responseError(err)
				return
			}
			responseSuccess(result)
		},
	}

	cmd.Flags().Int64Var(&offset, "offset", 0, "偏移量（从第几条开始）")
	cmd.Flags().Int64Var(&count, "count", 20, "返回条数（最多20）")

	return cmd
}

func runPublish(title, content, author, digest, coverID, outputDir, images string) error {
	// 检查 content 是否是文件路径
	if _, err := os.Stat(content); err == nil {
		// content 是一个存在的文件，读取其内容
		fileContent, err := os.ReadFile(content)
		if err != nil {
			return fmt.Errorf("无法读取内容文件: %w", err)
		}
		content = string(fileContent)
	}

	// 如果提供了图片URL列表，插入到HTML中
	if images != "" {
		imageURLs := strings.Split(images, ",")
		for i := range imageURLs {
			imageURLs[i] = strings.TrimSpace(imageURLs[i])
		}
		content = insertImagesIntoHTML(content, imageURLs)
	}

	// 创建草稿JSON
	draftData := map[string]interface{}{
		"articles": []map[string]interface{}{
			{
				"title":   title,
				"content": content,
				"author":  author,
				"digest":  digest,
			},
		},
	}

	if coverID != "" {
		draftData["articles"].([]map[string]interface{})[0]["thumb_media_id"] = coverID
		draftData["articles"].([]map[string]interface{})[0]["show_cover_pic"] = 1
	}

	// 保存到文件
	draftFile := fmt.Sprintf("%s/draft.json", outputDir)
	data, _ := json.MarshalIndent(draftData, "", "  ")
	if err := os.WriteFile(draftFile, data, 0644); err != nil {
		return fmt.Errorf("无法保存草稿文件: %w", err)
	}

	fmt.Printf("✅ 草稿JSON已保存到: %s\n", draftFile)
	fmt.Printf("\n使用以下命令上传草稿:\n")
	fmt.Printf("  writer draft article %s\n", draftFile)

	return nil
}

// insertImagesIntoHTML 在 HTML 中插入图片标签
func insertImagesIntoHTML(html string, imageURLs []string) string {
	if len(imageURLs) == 0 {
		return html
	}

	// 查找所有 </p> 标签位置作为插入点
	insertionPoints := findParagraphEnds(html)
	if len(insertionPoints) == 0 {
		return html
	}

	// 计算图片分布间隔
	interval := len(insertionPoints) / (len(imageURLs) + 1)
	if interval < 1 {
		interval = 1
	}

	// 从后向前插入，避免索引偏移
	result := html
	imageIndex := len(imageURLs) - 1
	for i := len(insertionPoints) - 1; i >= 0 && imageIndex >= 0; i-- {
		if (len(insertionPoints)-i-1)%interval == 0 {
			imgTag := fmt.Sprintf(`<p><img src="%s" style="max-width:100%%;height:auto;display:block;margin:20px auto;" /></p>`, imageURLs[imageIndex])
			insertPos := insertionPoints[i]
			result = result[:insertPos] + imgTag + result[insertPos:]
			imageIndex--
		}
	}

	return result
}

// findParagraphEnds 查找 HTML 中所有段落结束位置
func findParagraphEnds(html string) []int {
	var points []int
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
	return points
}
