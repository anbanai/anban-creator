package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/draft"
	"github.com/royalrick/anbanwriter/app/storage"
	"github.com/royalrick/anbanwriter/app/writer"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// accountCmd 账号管理命令组
func accountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "账号管理",
		Long: `管理微信公众号账号信息和配置

使用 'anbanwriter account info' 查看账号画像信息。
使用 'anbanwriter account init' 创建配置文件。`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := showAccountInfo(""); err != nil {
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
	var scope string

	cmd := &cobra.Command{
		Use:   "info",
		Short: "显示账号画像信息（AI 创作上下文）",
		Long: `显示微信公众号账号画像和写作风格信息，供 AI 创作使用。

注意：不输出敏感信息（AppID、Secret、API Key）

--scope 可选值：
  article  仅输出账号信息 + 写作风格
  xls      仅输出账号信息 + 小绿书配置
  rednote  仅输出账号信息 + 小红书配置
  flower   仅输出账号信息 + 花卉图片配置
  (不传)   输出全部章节`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := showAccountInfo(scope); err != nil {
				responseError(err)
			}
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "", "按场景过滤输出 (article/xls/rednote/flower)")

	return cmd
}

func showAccountInfo(scope string) error {
	if err := initConfigMinimal(); err != nil {
		return err
	}

	// 获取关键词显示
	keywords := strings.Join(cfg.Keywords, ", ")
	if keywords == "" {
		keywords = "(未配置)"
	}

	// 获取账号定位显示
	positioning := cfg.Positioning
	if positioning == "" {
		positioning = "(未配置)"
	}

	// 按 scope 输出各章节
	switch scope {
	case "article":
		sm := writer.NewStyleManager()
		if err := sm.LoadStyles(); err != nil {
			return err
		}
		activeStyleName := cfg.Wechat.Article.Style
		if activeStyleName == "" {
			activeStyleName = config.DefaultArticleStyle
		}
		fmt.Printf("\n# 写作风格\n\n")
		fmt.Printf("- 当前风格: %s\n", activeStyleName)
		fmt.Printf("- 可用风格: %s\n", strings.Join(sm.ListStyleNames(), ", "))

	case "xls":
		fmt.Printf("\n# 图文发布配置（微信小绿书 + 小红书）\n\n")
		fmt.Printf("- 图片数量: %d\n", cfg.XlsImageCount())
		fmt.Printf("- 图片尺寸: %s\n", cfg.XlsImageSize())

		// 图片生成就绪状态
		resolved := cfg.ResolvedXlsContentImage()
		if resolved.Key != "" {
			provider := resolved.Provider
			if provider == "" {
				provider = config.DefaultImageProvider
			}
			fmt.Printf("- 图片生成: 就绪（%s）\n", provider)
			// 检查 refer 配置（优先用于组图模式）
			contentRefer := cfg.Wechat.Xls.Content.Image.Refer
			if cfg.Rednote != nil && cfg.Rednote.Content.Image.Refer != "" {
				contentRefer = cfg.Rednote.Content.Image.Refer
			}
			if contentRefer != "" {
				fmt.Printf("- 组图参考图: 已配置（%s）\n", contentRefer)
			}
		} else {
			fmt.Printf("- 图片生成: 未配置（需设置 rednote.content.image.key 或 wechat.xls.content.image.key）\n")
		}

		// 参考图配置
		fmt.Printf("\n## 参考图配置\n\n")
		if cfg.Rednote != nil {
			if cfg.Rednote.Cover.Image.Refer != "" {
				fmt.Printf("- 小红书封面参考图: %s\n", cfg.Rednote.Cover.Image.Refer)
			}
			if cfg.Rednote.Content.Image.Refer != "" {
				fmt.Printf("- 小红书内容参考图: %s\n", cfg.Rednote.Content.Image.Refer)
				fmt.Printf("  💡 组图生成时优先使用此参考图\n")
			}
		}
		if cfg.Wechat.Xls.Cover.Image.Refer != "" {
			fmt.Printf("- 小绿书封面参考图: %s\n", cfg.Wechat.Xls.Cover.Image.Refer)
		}
		if cfg.Wechat.Xls.Content.Image.Refer != "" {
			fmt.Printf("- 小绿书内容参考图: %s\n", cfg.Wechat.Xls.Content.Image.Refer)
			fmt.Printf("  💡 组图生成时优先使用此参考图\n")
		}
		if (cfg.Rednote == nil || (cfg.Rednote.Cover.Image.Refer == "" && cfg.Rednote.Content.Image.Refer == "")) &&
			cfg.Wechat.Xls.Cover.Image.Refer == "" && cfg.Wechat.Xls.Content.Image.Refer == "" {
			fmt.Printf("- (未配置)\n")
			fmt.Printf("  💡 建议配置 rednote.content.image.refer 或 wechat.xls.content.image.refer\n")
			fmt.Printf("     用于组图模式保持风格一致性\n")
		}

		fmt.Printf("\n## 视觉风格\n\n")
		fmt.Printf("- 风格描述: %s\n", cfg.Wechat.Xls.Style)

	case "rednote":
		fmt.Printf("\n# 小红书配置\n\n")
		fmt.Printf("- 图片数量: %d\n", cfg.RednoteImageCount())
		fmt.Printf("- 图片尺寸: %s\n", cfg.RednoteImageSize())

		// 图片生成就绪状态
		resolvedRednote := cfg.ResolvedRednoteContentImage()
		if resolvedRednote.Key != "" {
			provider := resolvedRednote.Provider
			if provider == "" {
				provider = config.DefaultImageProvider
			}
			fmt.Printf("- 图片生成: 就绪（%s）\n", provider)
		} else {
			fmt.Printf("- 图片生成: 未配置（需设置 rednote.content.image.key 或 wechat.xls.content.image.key）\n")
		}

		// 参考图配置
		fmt.Printf("\n## 参考图配置\n\n")
		if cfg.Rednote != nil {
			if cfg.Rednote.Cover.Image.Refer != "" {
				fmt.Printf("- 封面参考图: %s\n", cfg.Rednote.Cover.Image.Refer)
			}
			if cfg.Rednote.Content.Image.Refer != "" {
				fmt.Printf("- 内容参考图: %s\n", cfg.Rednote.Content.Image.Refer)
				fmt.Printf("  💡 组图生成时优先使用此参考图\n")
			}
		}

		// 视觉风格
		fmt.Printf("\n## 视觉风格\n\n")
		rednoteStyle := ""
		if cfg.Rednote != nil {
			rednoteStyle = cfg.Rednote.Style
		}
		if rednoteStyle != "" {
			fmt.Printf("- 风格描述: %s\n", rednoteStyle)
		} else {
			fmt.Printf("- 风格描述: (未配置，AI 将根据内容动态设计)\n")
		}

	case "flower":
		fmt.Printf("\n# 花卉图片配置\n\n")
		fmt.Printf("- 花卉种数: %d\n", cfg.FlowerImageCount())
		fmt.Printf("- 图片尺寸: %s\n", cfg.FlowerImageSize())

		// 图片生成就绪状态
		resolvedFlower := cfg.ResolvedFlowerImage()
		if resolvedFlower.Key != "" {
			provider := resolvedFlower.Provider
			if provider == "" {
				provider = config.DefaultImageProvider
			}
		} else {
			fmt.Printf("- 图片生成: 未配置（需设置 flower.content.image.key）\n")
		}

		// 参考图配置
		fmt.Printf("\n## 参考图配置\n\n")
		if cfg.Flower != nil && cfg.Flower.Content.Image.Refer != "" {
			fmt.Printf("- 参考图: %s\n", cfg.Flower.Content.Image.Refer)
			fmt.Printf("  💡 所有花卉图片将基于此参考图保持风格一致\n")
		} else {
			fmt.Printf("- (未配置)\n")
			fmt.Printf("  💡 建议配置 flower.content.image.refer 用于保持多张花卉图片风格一致\n")
			fmt.Printf("     未配置时，第一张图片自动作为后续图片的参考图\n")
		}

	default:
		// 全量输出（向后兼容）
		// 输出账号信息
		fmt.Printf("# 账号信息\n\n")
		fmt.Printf("- 公众号: %s\n", cfg.Name)
		fmt.Printf("- 作者: %s\n", cfg.Wechat.Article.Author)
		fmt.Printf("- 关键词: %s\n", keywords)
		fmt.Printf("- 账号定位: %s\n", positioning)

		sm := writer.NewStyleManager()
		if err := sm.LoadStyles(); err != nil {
			return err
		}
		activeStyleName := cfg.Wechat.Article.Style
		if activeStyleName == "" {
			activeStyleName = config.DefaultArticleStyle
		}
		fmt.Printf("\n# 写作风格\n\n")
		fmt.Printf("- 当前风格: %s\n", activeStyleName)
		fmt.Printf("- 可用风格: %s\n", strings.Join(sm.ListStyleNames(), ", "))
		fmt.Printf("\n# AI 文字生成\n\n")
		fmt.Printf("- 生成模式: Claude 代理模式\n")
		fmt.Printf("- 说明: 由 Claude Code 直接生成，无需外部 API\n")
		fmt.Printf("\n# 小绿书配置\n\n")
		fmt.Printf("- 图片数量: %d\n", cfg.XlsImageCount())
		fmt.Printf("- 图片尺寸: %s\n", cfg.XlsImageSize())

		// 图片生成就绪状态
		resolvedXls := cfg.ResolvedXlsContentImage()
		if resolvedXls.Key != "" {
			provider := resolvedXls.Provider
			if provider == "" {
				provider = config.DefaultImageProvider
			}
			fmt.Printf("- 图片生成: 就绪（%s）\n", provider)
			// 检查 refer 配置（优先用于组图模式）
			contentRefer := cfg.Wechat.Xls.Content.Image.Refer
			if cfg.Rednote != nil && cfg.Rednote.Content.Image.Refer != "" {
				contentRefer = cfg.Rednote.Content.Image.Refer
			}
			if contentRefer != "" {
				fmt.Printf("- 组图参考图: 已配置（%s）\n", contentRefer)
			}
		} else {
			fmt.Printf("- 图片生成: 未配置（需设置 rednote.content.image.key 或 wechat.xls.content.image.key）\n")
		}

		// 发布能力状态
		if cfg.Wechat.AppID != "" && cfg.Wechat.Secret != "" {
			fmt.Printf("- 微信发布: 就绪\n")
		} else {
			fmt.Printf("- 微信发布: 未配置（仅微信小绿书需要；小红书发布通过 MCP 工具，无需此配置）\n")
		}

		fmt.Printf("\n## 视觉风格\n\n")
		fmt.Printf("- 风格描述: %s\n", cfg.Wechat.Xls.Style)
	}

	return nil
}

// accountInitCmd 初始化配置文件
func accountInitCmd() *cobra.Command {
	var appid, secret, name, author, style, theme, provider, aiKey, positioning, visualStyle string

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
				cmd.Flags().Changed("positioning") || cmd.Flags().Changed("visual-style")

			var err error
			var action string
			if hasFlags {
				err = updateConfigFile(outputFile, appid, secret, name, author, style, theme, provider, aiKey, positioning, visualStyle)
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
	cmd.Flags().StringVar(&visualStyle, "visual-style", "", "小绿书视觉风格预设名称 (morandi-flat/minimal-white/warm-paper/nature-watercolor/tech-dark)")

	return cmd
}

// updateConfigFile 增量写入配置字段
func updateConfigFile(outputFile, appid, secret, name, author, style, theme, provider, aiKey, positioning, visualStyle string) error {
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
		c.Name = name
	}
	if author != "" {
		c.Wechat.Article.Author = author
	}
	if style != "" {
		c.Wechat.Article.Style = style
	}
	if theme != "" {
		c.Wechat.Article.Theme = theme
	}
	if provider != "" {
		c.Wechat.Article.Content.Image.Provider = provider
		c.Wechat.Xls.Content.Image.Provider = provider
	}
	if aiKey != "" {
		c.Wechat.Article.Content.Image.Key = aiKey
		c.Wechat.Xls.Content.Image.Key = aiKey
	}
	if positioning != "" {
		c.Positioning = positioning
	}
	if visualStyle != "" {
		c.Wechat.Xls.Style = visualStyle
	}

	return config.SaveConfig(outputFile, c)
}

func initConfigFile(outputFile string) error {
	if _, err := os.Stat(outputFile); err == nil {
		return fmt.Errorf("config file already exists: %s", outputFile)
	}

	c := config.NewDefaultConfig()
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
		sync    bool
	)

	cmd := &cobra.Command{
		Use:   "history",
		Short: "查看草稿箱和已发布文章的统一历史",
		Long: `以表格形式展示草稿箱和已发布文章，按更新时间降序排列

默认读取本地缓存（毫秒级响应），首次使用自动从微信 API 同步。
使用 --sync 手动触发 API 同步。

示例:
  anbanwriter account history
  anbanwriter account history --count 10
  anbanwriter account history --json
  anbanwriter account history --sync`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if sync {
				// --sync 需要微信 API，加载完整配置
				return initConfig()
			}
			// 默认只读本地 DB，无需微信凭证
			return initConfigMinimal()
		},
		Run: func(cmd *cobra.Command, args []string) {
			// 判断是否需要从 API 同步
			needSync := sync

			// DB 为空时自动触发同步
			if !needSync && store != nil {
				if has, err := store.HasHistories(); err == nil && !has {
					needSync = true
				}
			}

			// store 不可用时降级为直接 API
			if store == nil {
				needSync = true
			}

			var items []historyItem
			var warnings []string

			if needSync {
				// 自动同步但缺少微信凭证时，输出提示而非报错
				if !sync && (cfg.Wechat.AppID == "" || cfg.Wechat.Secret == "") {
					fmt.Fprintf(os.Stderr, "⚠️  本地历史记录为空，自动同步需要配置微信凭证（wechat.appid / wechat.secret）\n")
					fmt.Fprintf(os.Stderr, "   运行 'anbanwriter account init' 设置，或使用 --sync 手动触发同步。\n")
				} else {
					items, warnings = fetchHistoryFromAPI(count)
					// 同步结果持久化到 DB
					if store != nil && len(items) > 0 {
						syncHistoriesToDB(items)
					}
				}
			} else {
				// 读本地 DB
				dbItems, err := store.ListHistories(int(count * 2))
				if err != nil {
					log.Warn("read history from db failed, falling back to API", zap.Error(err))
					items, warnings = fetchHistoryFromAPI(count)
				} else {
					for _, h := range dbItems {
						status := "发布"
						if h.Source == "draft" {
							status = "草稿"
						}
						items = append(items, historyItem{
							Status:     status,
							Title:      h.Title,
							Digest:     h.Digest,
							UpdateTime: h.UpdateTime,
							ID:         h.ItemID,
						})
					}
				}
			}

			// 限制条数
			if int64(len(items)) > count*2 {
				items = items[:count*2]
			}

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
	cmd.Flags().BoolVar(&sync, "sync", false, "手动触发微信 API 同步")

	return cmd
}

// fetchHistoryFromAPI 从微信 API 获取历史记录
func fetchHistoryFromAPI(count int64) ([]historyItem, []string) {
	svc := draft.NewService(cfg, log)
	var warnings []string
	var items []historyItem

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
		msg := err.Error()
		if h, ok := err.(interface{ Hint() string }); ok && h.Hint() != "" {
			msg = h.Hint()
		}
		warnings = append(warnings, msg)
		publishedResult = &draft.ListPublishedResult{}
	}

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

	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdateTime > items[j].UpdateTime
	})

	return items, warnings
}

// syncHistoriesToDB 将历史记录 upsert 到本地 DB
func syncHistoriesToDB(items []historyItem) {
	now := time.Now()
	var records []storage.History
	for _, item := range items {
		source := "published"
		if item.Status == "草稿" {
			source = "draft"
		}
		records = append(records, storage.History{
			Source:     source,
			ItemID:     item.ID,
			Title:      item.Title,
			Digest:     item.Digest,
			UpdateTime: item.UpdateTime,
			SyncedAt:   now,
		})
	}
	if err := store.UpsertHistories(records); err != nil {
		log.Warn("sync histories to db failed", zap.Error(err))
	}
}
