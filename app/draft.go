package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/royalrick/wechatwriter/app/draft"
	"github.com/royalrick/wechatwriter/app/storage"
	"github.com/spf13/cobra"
)

// sanitizeJSON 清理 JSON 中的非标准字符
// 如果 JSON 本身有效则跳过处理；否则尝试替换中文引号
func sanitizeJSON(data []byte) []byte {
	if json.Valid(data) {
		return data
	}
	// 替换中文双引号为英文双引号（未转义，让 JSON 解析器处理上下文）
	result := bytes.ReplaceAll(data, []byte{0xe2, 0x80, 0x9c}, []byte("\""))  // " (U+201C)
	result = bytes.ReplaceAll(result, []byte{0xe2, 0x80, 0x9d}, []byte("\"")) // " (U+201D)
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
	cmd.AddCommand(draftPostCmd())

	return cmd
}

// draftArticleCmd 从 JSON 创建图文文章草稿
func draftArticleCmd() *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "article <json_file>",
		Short: "从 JSON 文件创建微信图文文章草稿",
		Long: `从 JSON 文件创建微信图文文章草稿

JSON 格式示例:
  {
    "articles": [
      {
        "title": "文章标题",
        "author": "作者",
        "digest": "文章摘要（120字以内）",
        "content": "<p>HTML 正文内容</p>",
        "thumb_media_id": "封面图片 media_id",
        "show_cover_pic": 1
      }
    ]
  }

使用 content_file 替代内联 content（推荐用于长文章）:
  {
    "articles": [
      {
        "title": "文章标题",
        "content_file": "/path/to/article.html",
        "thumb_media_id": "封面图片 media_id"
      }
    ]
  }

注意：
  - 需要在配置文件中设置微信公众号账号信息
  - 运行 'wechatwriter account init' 创建配置文件
  - 内容长度限制 20000 字符，超出请使用 content_file 分离 HTML 文件`,
		Args: cobra.ExactArgs(1),
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
				// 提供更有用的错误信息
				syntaxErr, ok := err.(*json.SyntaxError)
				if ok {
					responseError(fmt.Errorf("JSON 解析失败（位置 %d）: %w\n💡 提示: 如果正文 HTML 含有特殊字符，建议使用 content_file 字段引用外部 HTML 文件", syntaxErr.Offset, err))
				} else {
					responseError(fmt.Errorf("JSON 解析失败: %w\n💡 提示: 可使用 content_file 字段替代内联 content，避免 HTML 转义问题", err))
				}
				return
			}

			// 验证
			if len(req.Articles) == 0 {
				responseError(&AppError{Message: "no articles in request", HintText: "JSON 文件需包含 articles 数组，格式: {\"articles\": [...]}"})
				return
			}

			// 解析 content_file 字段（相对路径相对于 CWD 解析）
			for i := range req.Articles {
				if req.Articles[i].ContentFile != "" && req.Articles[i].Content == "" {
					htmlPath := req.Articles[i].ContentFile
					htmlData, err := os.ReadFile(htmlPath)
					if err != nil {
						responseError(fmt.Errorf("读取 content_file 失败 (%s): %w", htmlPath, err))
						return
					}
					req.Articles[i].Content = string(htmlData)
				}
			}

			// 创建草稿
			result, err := svc.CreateDraft(req.Articles)
			if err != nil {
				responseError(err)
				return
			}

			// 静默记录到 DB
			if store != nil {
				title := ""
				digest := ""
				if len(req.Articles) > 0 {
					title = req.Articles[0].Title
					digest = req.Articles[0].Digest
				}
				_ = store.CreateDraft(&storage.Draft{
					MediaID:   result.MediaID,
					DraftURL:  result.DraftURL,
					Title:     title,
					Digest:    digest,
					Type:      "article",
					CreatedAt: time.Now(),
				})

				// 自动更新 Content 状态为 published
				if dir != "" {
					absDir, _ := filepath.Abs(dir)
					_ = store.UpdateContentFields(absDir, map[string]any{
						"status":   "published",
						"media_id": result.MediaID,
						"title":    title,
						"digest":   digest,
					})
				}
			}

			responseSuccess(result)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "内容目录路径（用于自动更新内容状态）")

	return cmd
}

// draftTestCmd
func draftTestCmd() *cobra.Command {
	var (
		title  string
		digest string
	)

	cmd := &cobra.Command{
		Use:   "test <html_file> <cover_image>",
		Short: "测试从 HTML 文件创建微信草稿",
		Long: `从 HTML 文件和封面图片创建微信草稿（测试用途）

快速验证 HTML 内容能否正确发布到微信草稿箱，适合调试转换效果。

需要配置:
  wechat.appid   - 微信公众号 AppID
  wechat.secret  - 微信公众号 Secret

示例:
  wechatwriter draft test output.html cover.jpg
  wechatwriter draft test output.html cover.jpg --title "测试文章" --digest "摘要"`,
		Args: cobra.ExactArgs(2),
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

	responseSuccess(map[string]any{
		"draft_file":   draftFile,
		"message":      "草稿JSON已保存",
		"next_command": "wechatwriter draft article " + draftFile,
	})

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
