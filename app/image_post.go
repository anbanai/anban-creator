package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"

	"github.com/royalrick/wechatwriter/app/draft"
	"github.com/spf13/cobra"
)

// draftPostCmd 创建小绿书帖子（图片消息）命令
func draftPostCmd() *cobra.Command {
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
	)

	cmd := &cobra.Command{
		Use:   "post",
		Short: "创建微信小绿书帖子（图片消息/newspic），支持最多 20 张图片",
		Long: `创建微信公众号小绿书帖子（图片消息/newspic），支持最多 20 张图片。

示例：
  # 从逗号分隔的图片列表创建
  wechatwriter draft post -t "周末出游" --images photo1.jpg,photo2.jpg,photo3.jpg

  # 从 Markdown 文件提取图片
  wechatwriter draft post -t "旅行日记" -m article.md

  # 带描述文字和评论设置
  wechatwriter draft post -t "美食分享" -c "今天的午餐" --images food.jpg --open-comment

  # 从 stdin 读取描述
  echo "每日打卡" | wechatwriter draft post -t "每日" --images pic.jpg

  # 预览模式（不实际创建）
  wechatwriter draft post -t "测试" --images a.jpg,b.jpg --dry-run`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			// 构造请求
			req := &draft.ImagePostRequest{
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
				responseError(&AppError{Message: "--images, --media-ids, or --from-markdown is required", HintText: "使用 --images 指定图片路径，--media-ids 指定已上传的 media_id，或 -m 从 Markdown 提取图片"})
				return
			}

			svc := draft.NewService(cfg, log)

			// Dry-run 模式
			if dryRun {
				preview, err := svc.GetImagePostPreview(req)
				if err != nil {
					responseError(err)
					return
				}

				// 保存到文件
				if output != "" {
					data, _ := json.MarshalIndent(preview, "", "  ")
					if err := os.WriteFile(output, data, 0644); err != nil {
						responseError(err)
						return
					}
				}

				responseSuccess(map[string]any{
					"mode":    "dry-run",
					"preview": preview,
				})
				return
			}

			// 创建小绿书
			result, err := svc.CreateImagePost(req)
			if err != nil {
				responseError(err)
				return
			}

			// 保存结果到文件
			if output != "" {
				data, _ := json.MarshalIndent(result, "", "  ")
				if err := os.WriteFile(output, data, 0644); err != nil {
					responseError(err)
					return
				}
			}

			responseSuccess(result)
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", "帖子标题（必填）")
	cmd.Flags().StringVarP(&content, "content", "c", "", "描述文字")
	cmd.Flags().StringVar(&images, "images", "", "图片路径（逗号分隔）")
	cmd.Flags().StringVar(&mediaIDs, "media-ids", "", "已上传的 media_id（逗号分隔，跳过重复上传）")
	cmd.Flags().StringVarP(&fromMD, "from-markdown", "m", "", "从 Markdown 文件提取图片")
	cmd.Flags().BoolVar(&openComment, "open-comment", false, "开启评论")
	cmd.Flags().BoolVar(&fansOnly, "fans-only", false, "仅粉丝可评论")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "预览模式（不实际创建草稿）")
	cmd.Flags().StringVarP(&output, "output", "o", "", "保存结果到 JSON 文件")

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
