package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/royalrick/anbanwriter/app/draft"
	"github.com/royalrick/anbanwriter/app/storage"
	"github.com/spf13/cobra"
)

// draftXlsCmd 创建小绿书帖子（图文笔记）命令
func draftXlsCmd() *cobra.Command {
	var (
		title       string
		content     string
		images      string
		mediaIDs    string
		fromMD      string
		openComment bool
		fansOnly    bool
		dryRun      bool
		output      string
		dir         string
	)

	cmd := &cobra.Command{
		Use:   "xls",
		Short: "【微信】创建小绿书帖子（图文笔记/newspic），支持最多 20 张图片",
		Long: `创建微信公众号小绿书帖子（图文笔记/newspic），支持最多 20 张图片。

╔════════════════════════════════════════════════════════════════╗
║  ⚠️  平台提示：此命令仅用于微信小绿书发布                      ║
║                                                                ║
║  小红书发布请使用 MCP 工具：                                   ║
║    publish_content(title="...", content="...", images=[...])   ║
║                                                                ║
║  或先使用 abwriter rednote export 导出内容             ║
╚════════════════════════════════════════════════════════════════╝

示例：
  # 从逗号分隔的本地图片文件路径创建（自动上传到微信素材库）
  abwriter draft xls -t "周末出游" --images photo1.jpg,photo2.jpg,photo3.jpg

  # 从 Markdown 文件提取图片
  abwriter draft xls -t "旅行日记" -m article.md

  # 带描述文字和评论设置
  abwriter draft xls -t "美食分享" -c "今天的午餐" --images food.jpg --open-comment

  # 使用微信素材 ID 创建（先用 image upload 获取 media_id，再跳过重复上传）
  abwriter draft xls -t "AI 图集" --media-ids "MEDIA_ID_1,MEDIA_ID_2"

  # 混合使用：微信素材 ID + 本地文件路径
  abwriter draft xls -t "混合图集" --media-ids "MEDIA_ID_1" --images "local.jpg"

  # 从 stdin 读取描述
  echo "每日打卡" | abwriter draft xls -t "每日" --images pic.jpg

  # 预览模式（不实际创建）
  abwriter draft xls -t "测试" --images a.jpg,b.jpg --dry-run`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			// 构造请求
			req := &draft.ImageXlsRequest{
				Title:       title,
				Content:     content,
				OpenComment: openComment,
				FansOnly:    fansOnly,
			}

			// 处理图片列表
			if images != "" {
				for _, img := range strings.Split(images, ",") {
					img = strings.TrimSpace(img)
					if img != "" {
						req.Images = append(req.Images, img)
					}
				}
			}

			// 处理已上传的 media_id
			if mediaIDs != "" {
				for _, id := range strings.Split(mediaIDs, ",") {
					id = strings.TrimSpace(id)
					if id != "" {
						req.MediaIDs = append(req.MediaIDs, id)
					}
				}
			}

			// 从 Markdown 提取图片
			if fromMD != "" {
				req.FromMarkdown = fromMD
			}

			// 从 stdin 读取描述内容
			if content == "" && !isTerminal() {
				scanner := bufio.NewScanner(os.Stdin)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if len(lines) > 0 {
					req.Content = strings.Join(lines, "\n")
				}
			}

			// 验证
			if req.Title == "" {
				responseError(&AppError{Message: "--title is required", HintText: "使用 -t 参数指定帖子标题"})
				return
			}

			if len(req.Images) == 0 && req.FromMarkdown == "" && len(req.MediaIDs) == 0 {
				responseError(&AppError{Message: "--images, --media-ids, or --from-markdown is required", HintText: "使用 --images 指定本地图片文件路径，--media-ids 指定微信素材 ID（media_id），或 -m 从 Markdown 提取图片"})
				return
			}

			svc := draft.NewService(cfg, log)

			// Dry-run 模式
			if dryRun {
				preview, err := svc.GetImageXlsPreview(req)
				if err != nil {
					responseError(err)
					return
				}

				responseSuccess(map[string]any{
					"mode":    "dry-run",
					"preview": preview,
				})
				return
			}

			// 创建小绿书
			result, err := svc.CreateImageXls(req)
			if err != nil {
				responseError(err)
				return
			}

			// 静默记录到 DB
			if store != nil {
				_ = store.CreateDraft(&storage.Draft{
					MediaID:   result.MediaID,
					DraftURL:  result.DraftURL,
					Title:     req.Title,
					Type:      "xls",
					CreatedAt: time.Now(),
				})

				// 自动更新 Content 状态为 published
				if dir != "" {
					absDir, _ := filepath.Abs(dir)
					_ = store.UpdateContentFields(absDir, map[string]any{
						"status":   "published",
						"media_id": result.MediaID,
						"title":    req.Title,
					})
				}
			}

			// 若指定了 --output，将结果写入文件
			if output != "" {
				data, marshalErr := json.MarshalIndent(result, "", "  ")
				if marshalErr != nil {
					responseError(fmt.Errorf("marshal result: %w", marshalErr))
					return
				}
				if writeErr := os.WriteFile(output, data, 0644); writeErr != nil {
					responseError(fmt.Errorf("write output %s: %w", output, writeErr))
					return
				}
			}

			responseSuccess(result)
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", "帖子标题（必填）")
	cmd.Flags().StringVarP(&content, "content", "c", "", "描述文字")
	cmd.Flags().StringVar(&images, "images", "", "本地图片文件路径，逗号分隔（将自动上传到微信素材库）")
	cmd.Flags().StringVar(&mediaIDs, "media-ids", "", "微信素材 ID（media_id），逗号分隔（已上传到微信素材库的图片，跳过重复上传）")
	cmd.Flags().StringVarP(&fromMD, "from-markdown", "m", "", "从 Markdown 文件提取图片")
	cmd.Flags().BoolVar(&openComment, "open-comment", false, "开启评论")
	cmd.Flags().BoolVar(&fansOnly, "fans-only", false, "仅粉丝可评论")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "预览模式（不实际创建草稿）")
	cmd.Flags().StringVarP(&output, "output", "o", "", "将结果写入 JSON 文件")
	cmd.Flags().StringVar(&content, "desc", "", "描述文字（--content 的别名）")
	_ = cmd.Flags().MarkHidden("desc")
	cmd.Flags().StringVar(&dir, "dir", "", "内容目录路径（用于自动更新内容状态）")

	return cmd
}

// isTerminal 检查 stdin 是否是终端
func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return true
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
