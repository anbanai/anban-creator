package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/royalrick/wechatwriter/app/config"
	"github.com/royalrick/wechatwriter/app/draft"
	"github.com/royalrick/wechatwriter/app/image"
	"github.com/royalrick/wechatwriter/app/writer"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// accountCmd 账号管理命令组
func accountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "账号管理",
		Long: `管理微信公众号账号信息和配置

使用 'wechatwriter account info' 查看账号画像信息。
使用 'wechatwriter account init' 创建配置文件。`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := showAccountInfo(); err != nil {
				responseError(err)
			}
		},
	}

	cmd.AddCommand(accountInfoCmd())
	cmd.AddCommand(accountInitCmd())
	cmd.AddCommand(accountHistoryCmd())

	return cmd
}

// accountInfoCmd 显示账号画像信息
func accountInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "显示账号画像信息（AI 创作上下文）",
		Long: `显示微信公众号账号画像和写作风格信息，供 AI 创作使用。

注意：不输出敏感信息（AppID、Secret、API Key）`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := showAccountInfo(); err != nil {
				responseError(err)
			}
		},
	}
}

func showAccountInfo() error {
	if err := initConfigMinimal(); err != nil {
		return err
	}

	// 加载风格管理器
	sm := writer.NewStyleManager()
	if err := sm.LoadStyles(); err != nil {
		return err
	}

	// 获取当前风格
	activeStyleName := cfg.Article.Style
	if activeStyleName == "" {
		activeStyleName = config.DefaultArticleStyle
	}

	// 获取关键词显示
	keywords := strings.Join(cfg.Wechat.Keywords, ", ")
	if keywords == "" {
		keywords = "(未配置)"
	}

	// 获取账号定位显示
	positioning := cfg.Wechat.Positioning
	if positioning == "" {
		positioning = "(未配置)"
	}

	// 输出 Markdown 格式
	fmt.Printf("# 账号信息\n\n")
	fmt.Printf("- 公众号: %s\n", cfg.Wechat.Name)
	fmt.Printf("- 作者: %s\n", cfg.Wechat.Author)
	fmt.Printf("- 关键词: %s\n", keywords)
	fmt.Printf("- 账号定位: %s\n", positioning)
	fmt.Printf("\n# 写作风格\n\n")
	fmt.Printf("- 当前风格: %s\n", activeStyleName)
	fmt.Printf("- 可用风格: %s\n", strings.Join(sm.ListStyleNames(), ", "))
	fmt.Printf("\n# AI 文字生成\n\n")
	fmt.Printf("- 生成模式: Claude 代理模式\n")
	fmt.Printf("- 说明: 由 Claude Code 直接生成，无需外部 API\n")
	fmt.Printf("\n# 小绿书配置\n\n")
	fmt.Printf("- 图片数量: %d\n", cfg.PostImageCount())
	fmt.Printf("- 图片尺寸: %s\n", cfg.PostImageSize())

	return nil
}

// accountInitCmd 初始化配置文件
func accountInitCmd() *cobra.Command {
	var appid, secret, name, author, style, theme, provider, aiKey, positioning string

	cmd := &cobra.Command{
		Use:   "init [output_file]",
		Short: "创建示例配置文件",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var outputFile string
			if len(args) > 0 {
				outputFile = args[0]
			} else {
				outputFile = config.DefaultConfigPath()
			}

			hasFlags := cmd.Flags().Changed("appid") || cmd.Flags().Changed("secret") ||
				cmd.Flags().Changed("name") || cmd.Flags().Changed("author") ||
				cmd.Flags().Changed("style") || cmd.Flags().Changed("theme") ||
				cmd.Flags().Changed("provider") || cmd.Flags().Changed("ai-key") ||
				cmd.Flags().Changed("positioning")

			var err error
			var action string
			if hasFlags {
				err = updateConfigFile(outputFile, appid, secret, name, author, style, theme, provider, aiKey, positioning)
				action = "已更新"
			} else {
				err = initConfigFile(outputFile)
				action = "已创建"
			}

			if err != nil {
				responseError(err)
				return
			}

			fmt.Fprintf(os.Stderr, "\n✅ 配置文件%s: %s\n", action, outputFile)
			responseSuccess(map[string]any{
				"file":    outputFile,
				"message": "Config file " + action,
			})
		},
	}

	cmd.Flags().StringVar(&appid, "appid", "", "微信 AppID")
	cmd.Flags().StringVar(&secret, "secret", "", "微信 Secret")
	cmd.Flags().StringVar(&name, "name", "", "公众号名称")
	cmd.Flags().StringVar(&author, "author", "", "作者名称")
	cmd.Flags().StringVar(&style, "style", "", "写作风格 (dan-koe/cultural-depth/casual-science)")
	cmd.Flags().StringVar(&theme, "theme", "", "文章主题 (default/apple/autumn-warm/...)")
	cmd.Flags().StringVar(&provider, "provider", "", "图片生成服务 (gemini/openai/openrouter/volcengine)")
	cmd.Flags().StringVar(&aiKey, "ai-key", "", "图片 API Key")
	cmd.Flags().StringVar(&positioning, "positioning", "", "账号定位描述")

	return cmd
}

// updateConfigFile 增量写入配置字段
func updateConfigFile(outputFile, appid, secret, name, author, style, theme, provider, aiKey, positioning string) error {
	c := &config.Config{}
	if _, err := os.Stat(outputFile); err == nil {
		data, err := os.ReadFile(outputFile)
		if err != nil {
			return fmt.Errorf("read config: %w", err)
		}
		if err := json.Unmarshal(data, c); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}

	if appid != "" {
		c.Wechat.AppID = appid
	}
	if secret != "" {
		c.Wechat.Secret = secret
	}
	if name != "" {
		c.Wechat.Name = name
	}
	if author != "" {
		c.Wechat.Author = author
	}
	if style != "" {
		c.Article.Style = style
	}
	if theme != "" {
		c.Article.Theme = theme
	}
	if provider != "" {
		c.Article.Image.Provider = provider
		c.Post.Image.Provider = provider
	}
	if aiKey != "" {
		c.Article.Image.Key = aiKey
		c.Post.Image.Key = aiKey
	}
	if positioning != "" {
		c.Wechat.Positioning = positioning
	}

	return config.SaveConfig(outputFile, c)
}

func initConfigFile(outputFile string) error {
	if _, err := os.Stat(outputFile); err == nil {
		return fmt.Errorf("config file already exists: %s", outputFile)
	}

	c := config.NewDefaultConfig()
	// 设置默认 provider 对应的模型名（方便用户了解当前模型）
	c.Article.Image.Model = image.DefaultGeminiModel
	c.Post.Image.Model = image.DefaultGeminiModel

	return config.SaveConfig(outputFile, c)
}

// historyItem 统一的历史记录条目
type historyItem struct {
	Status     string `json:"status"`
	Title      string `json:"title"`
	Digest     string `json:"digest,omitempty"`
	UpdateTime int64  `json:"update_time"`
	ID         string `json:"id"`
}

// accountHistoryCmd 展示草稿箱 + 已发布文章的统一历史
func accountHistoryCmd() *cobra.Command {
	var (
		count   int64
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "history",
		Short: "查看草稿箱和已发布文章的统一历史",
		Long: `以表格形式展示草稿箱和已发布文章，按更新时间降序排列

示例:
  wechatwriter account history
  wechatwriter account history --count 10
  wechatwriter account history --json`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
		Run: func(cmd *cobra.Command, args []string) {
			svc := draft.NewService(cfg, log)

			var warnings []string

			// 获取草稿列表（soft-fail）
			draftsResult, err := svc.ListDrafts(0, count)
			if err != nil {
				log.Warn("list drafts failed", zap.Error(err))
				warnings = append(warnings, fmt.Sprintf("草稿箱获取失败: %s", err.Error()))
				draftsResult = &draft.ListDraftsResult{}
			}

			// 获取已发布文章列表（soft-fail）
			publishedResult, err := svc.ListPublished(0, count)
			if err != nil {
				log.Warn("list published failed", zap.Error(err))
				warnings = append(warnings, fmt.Sprintf("已发布文章获取失败: %s", err.Error()))
				publishedResult = &draft.ListPublishedResult{}
			}

			// 两者都失败时才返回错误
			if len(warnings) == 2 {
				responseError(fmt.Errorf("无法获取历史记录：%s；%s", warnings[0], warnings[1]))
				return
			}

			// 合并
			var items []historyItem
			for _, d := range draftsResult.Items {
				items = append(items, historyItem{
					Status:     "草稿",
					Title:      d.Title,
					Digest:     d.Digest,
					UpdateTime: d.UpdateTime,
					ID:         d.MediaID,
				})
			}
			for _, p := range publishedResult.Items {
				items = append(items, historyItem{
					Status:     "发布",
					Title:      p.Title,
					Digest:     p.Digest,
					UpdateTime: p.UpdateTime,
					ID:         p.ArticleID,
				})
			}

			// 按 update_time 降序排序
			sort.Slice(items, func(i, j int) bool {
				return items[i].UpdateTime > items[j].UpdateTime
			})

			// JSON 输出
			if jsonOut {
				if len(warnings) > 0 {
					responseSuccess(map[string]any{
						"items":    items,
						"warnings": warnings,
					})
				} else {
					responseSuccess(items)
				}
				return
			}

			// 表格模式：先打印 warning 到 stderr
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "⚠️  %s\n", w)
			}

			// 表格输出
			headers := []string{"#", "状态", "标题", "摘要", "更新时间"}
			rows := make([][]string, len(items))
			for i, item := range items {
				rows[i] = []string{
					fmt.Sprintf("%d", i+1),
					item.Status,
					truncateByWidth(item.Title, 30),
					truncateByWidth(item.Digest, 20),
					relativeTime(item.UpdateTime),
				}
			}
			renderTable(headers, rows)

			// 统计
			draftCount := 0
			publishedCount := 0
			for _, item := range items {
				if item.Status == "草稿" {
					draftCount++
				} else {
					publishedCount++
				}
			}
			fmt.Printf("  共 %d 篇（草稿 %d / 已发布 %d）\n", len(items), draftCount, publishedCount)
		},
	}

	cmd.Flags().Int64Var(&count, "count", 20, "每类最多获取条数")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "输出 JSON 格式")

	return cmd
}
