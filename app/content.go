package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/royalrick/anbanwriter/app/storage"
	"github.com/spf13/cobra"
)

// contentCmd 内容生命周期追踪命令组
func contentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "content",
		Short: "内容生命周期追踪",
		Long: `内容生命周期追踪命令组

支持的操作：
  track  - 记录/更新内容状态
  list   - 列出本地内容`,
	}

	cmd.AddCommand(contentTrackCmd())
	cmd.AddCommand(contentListCmd())

	return cmd
}

// contentTrackCmd 记录/更新内容状态
func contentTrackCmd() *cobra.Command {
	var (
		dir     string
		ctype   string
		status  string
		title   string
		digest  string
		topic   string
		style   string
		mediaID string
	)

	cmd := &cobra.Command{
		Use:   "track",
		Short: "记录或更新内容状态",
		Long: `记录或更新内容生命周期状态

Article 状态流转: created → outlined → drafted → polished → converted → published
Post 状态流转:    created → planned → images_ready → published

示例：
  anbanwriter content track --dir $DIR --type article --status created --topic "茶文化"
  anbanwriter content track --dir $DIR --status outlined --title "标题"
  anbanwriter content track --dir $DIR --status drafted
  anbanwriter content track --dir $DIR --status published --media-id "xxx"`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfigMinimal()
		},
		Run: func(cmd *cobra.Command, args []string) {
			if dir == "" {
				responseError(&AppError{Message: "--dir is required", HintText: "使用 --dir 参数指定内容目录路径"})
				return
			}
			if status == "" {
				responseError(&AppError{Message: "--status is required", HintText: "使用 --status 参数指定内容状态"})
				return
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				responseError(fmt.Errorf("resolve dir: %w", err))
				return
			}

			if store == nil {
				responseError(&AppError{Message: "storage not available", HintText: "数据库初始化失败，请检查 .anbanwriter/data.db 文件权限"})
				return
			}

			c := &storage.Content{
				Type:      ctype,
				Dir:       absDir,
				Status:    status,
				Title:     title,
				Digest:    digest,
				Topic:     topic,
				Style:     style,
				MediaID:   mediaID,
				UpdatedAt: time.Now(),
			}

			if err := store.UpsertContent(c); err != nil {
				responseError(fmt.Errorf("track content: %w", err))
				return
			}

			found, err := store.FindContentByDir(absDir)
			if err != nil {
				responseError(fmt.Errorf("fetch content: %w", err))
				return
			}

			responseSuccess(found)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "项目目录路径（必填）")
	cmd.Flags().StringVar(&ctype, "type", "article", "内容类型 (article/xls)")
	cmd.Flags().StringVar(&status, "status", "", "内容状态（必填）")
	cmd.Flags().StringVar(&title, "title", "", "标题")
	cmd.Flags().StringVar(&digest, "digest", "", "摘要")
	cmd.Flags().StringVar(&topic, "topic", "", "话题")
	cmd.Flags().StringVar(&style, "style", "", "写作风格")
	cmd.Flags().StringVar(&mediaID, "media-id", "", "微信 media_id")

	return cmd
}

// contentListCmd 列出本地内容
func contentListCmd() *cobra.Command {
	var (
		count   int
		jsonOut bool
		all     bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出本地内容",
		Long: `列出本地追踪的内容，默认仅显示未发布内容

示例：
  anbanwriter content list
  anbanwriter content list --all
  anbanwriter content list --json`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfigMinimal()
		},
		Run: func(cmd *cobra.Command, args []string) {
			if store == nil {
				responseError(&AppError{Message: "storage not available", HintText: "数据库初始化失败，请检查 .anbanwriter/data.db 文件权限"})
				return
			}

			var items []storage.Content
			var err error
			if all {
				items, err = store.ListContents(count)
			} else {
				items, err = store.ListUnpublishedContents(count)
			}
			if err != nil {
				responseError(fmt.Errorf("list contents: %w", err))
				return
			}

			if jsonOut {
				responseSuccess(items)
				return
			}

			if len(items) == 0 {
				fmt.Println("  暂无内容记录")
				return
			}

			headers := []string{"#", "类型", "状态", "标题", "更新时间"}
			rows := make([][]string, len(items))
			for i, item := range items {
				typeLabel := "文章"
				if item.Type == "xls" {
					typeLabel = "小绿书"
				}
				titleDisplay := item.Title
				if titleDisplay == "" {
					titleDisplay = item.Topic
				}
				if titleDisplay == "" {
					titleDisplay = "(未命名)"
				}
				rows[i] = []string{
					fmt.Sprintf("%d", i+1),
					typeLabel,
					contentStatusLabel(item.Status),
					truncateByWidth(titleDisplay, 25),
					relativeTime(item.UpdatedAt.Unix()),
				}
			}
			renderTable(headers, rows)
			fmt.Printf("  共 %d 条\n", len(items))
		},
	}

	cmd.Flags().IntVar(&count, "count", 20, "最多显示条数")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "输出 JSON 格式")
	cmd.Flags().BoolVar(&all, "all", false, "显示全部内容（含已发布，默认仅显示未发布）")

	return cmd
}

// contentStatusLabel 将状态转为中文显示
func contentStatusLabel(status string) string {
	labels := map[string]string{
		"created":      "创建",
		"outlined":     "大纲",
		"drafted":      "初稿",
		"polished":     "润色",
		"converted":    "转换",
		"planned":      "规划",
		"images_ready": "图片就绪",
		"published":    "已发布",
	}
	if l, ok := labels[status]; ok {
		return l
	}
	return status
}
