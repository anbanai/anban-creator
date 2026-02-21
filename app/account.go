package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/royalrick/wechatwriter/app/config"
	"github.com/royalrick/wechatwriter/app/writer"
	"github.com/spf13/cobra"
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
	activeStyleName := cfg.Wechat.Style
	if activeStyleName == "" {
		activeStyleName = writer.DefaultStyleName
	}

	// 获取关键词显示
	keywords := strings.Join(cfg.Wechat.KeyWords, ", ")
	if keywords == "" {
		keywords = "(未配置)"
	}

	// 输出 Markdown 格式
	fmt.Printf("# 账号信息\n\n")
	fmt.Printf("- 公众号: %s\n", cfg.Wechat.Name)
	fmt.Printf("- 作者: %s\n", cfg.Wechat.Author)
	fmt.Printf("- 关键词: %s\n", keywords)
	fmt.Printf("\n# 写作风格\n\n")
	fmt.Printf("- 当前风格: %s\n", activeStyleName)
	fmt.Printf("- 可用风格: %s\n", strings.Join(sm.ListStyleNames(), ", "))

	return nil
}

// accountInitCmd 初始化配置文件
func accountInitCmd() *cobra.Command {
	var appid, secret, name, author, style, theme, provider, aiKey, aiBaseURL string

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
				cmd.Flags().Changed("ai-base-url")

			var err error
			var action string
			if hasFlags {
				err = updateConfigFile(outputFile, appid, secret, name, author, style, theme, provider, aiKey, aiBaseURL)
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
	cmd.Flags().StringVar(&aiKey, "ai-key", "", "AI API Key")
	cmd.Flags().StringVar(&aiBaseURL, "ai-base-url", "", "AI Base URL")

	return cmd
}

// updateConfigFile 增量写入配置字段
func updateConfigFile(outputFile, appid, secret, name, author, style, theme, provider, aiKey, aiBaseURL string) error {
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
		c.Wechat.Style = style
	}
	if theme != "" {
		c.Article.Theme = theme
	}
	if provider != "" {
		c.Article.Image.Provider = provider
		c.Post.Image.Provider = provider
	}
	if aiKey != "" {
		c.AI.Key = aiKey
		c.Article.Image.Key = aiKey
		c.Post.Image.Key = aiKey
	}
	if aiBaseURL != "" {
		c.AI.BaseURL = aiBaseURL
	}

	return config.SaveConfig(outputFile, c)
}

func initConfigFile(outputFile string) error {
	if _, err := os.Stat(outputFile); err == nil {
		return fmt.Errorf("config file already exists: %s", outputFile)
	}

	// 创建单账号配置模板
	c := &config.Config{}
	c.Wechat.Name = "your_account_name"
	c.Wechat.Author = "your_author_name"
	c.Wechat.AppID = "your_wechat_appid"
	c.Wechat.Secret = "your_wechat_secret"
	c.Wechat.KeyWords = []string{"keyword1", "keyword2"}
	c.Wechat.Style = "dan-koe"
	c.Article.Theme = "default"
	c.Article.Image.Key = "your_image_api_key"
	c.Article.Image.BaseURL = "https://api.openai.com/v1"
	c.Article.Image.Provider = "gemini"
	c.Article.Image.Model = "gemini-3-pro-image-preview"
	c.Article.Image.Size = "16:9"
	c.Post.Image.Key = "your_image_api_key"
	c.Post.Image.Provider = "gemini"
	c.Post.Image.Model = "gemini-3-pro-image-preview"
	c.Post.Image.Size = "3:4"
	c.Image.Compress = true
	c.Image.MaxWidth = 1920
	c.Image.MaxSizeMB = 5
	c.Image.HTTPTimeout = 30

	return config.SaveConfig(outputFile, c)
}
