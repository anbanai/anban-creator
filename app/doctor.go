package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/royalrick/wechatwriter/app/config"
	"github.com/royalrick/wechatwriter/app/storage"
	"github.com/royalrick/wechatwriter/app/wechat"
	"github.com/spf13/cobra"
)

// CheckResult 单项检查结果
type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "pass", "fail", "warn"
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func doctorCmd() *cobra.Command {
	var skipNetwork bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "诊断配置和连接问题",
		Long:  `检查配置文件、API 密钥、网络连接和运行环境，输出诊断报告。`,
		Run: func(cmd *cobra.Command, args []string) {
			var checks []CheckResult

			// 加载配置（不验证微信账号）
			cfg, cfgErr := config.LoadMinimal()

			// 配置检查
			checks = append(checks, checkConfig(cfg, cfgErr)...)

			// 环境检查
			checks = append(checks, checkEnvironment()...)

			// 数据库检查
			checks = append(checks, checkDatabase()...)

			// 网络检查
			if !skipNetwork {
				checks = append(checks, checkNetwork(cfg)...)
			}

			// 统计结果
			passed, failed, warned := 0, 0, 0
			for _, c := range checks {
				switch c.Status {
				case "pass":
					passed++
				case "fail":
					failed++
				case "warn":
					warned++
				}
			}

			responseSuccess(map[string]any{
				"checks": checks,
				"summary": map[string]int{
					"pass": passed,
					"fail": failed,
					"warn": warned,
				},
			})
		},
	}

	cmd.Flags().BoolVar(&skipNetwork, "skip-network", false, "跳过网络连接检查")
	return cmd
}

func checkConfig(cfg *config.Config, cfgErr error) []CheckResult {
	var checks []CheckResult

	// 配置文件
	configPath := config.DefaultConfigPath()
	if _, err := os.Stat(configPath); err == nil {
		checks = append(checks, CheckResult{Name: "config_file", Status: "pass", Message: fmt.Sprintf("配置文件存在: %s", configPath)})
	} else {
		checks = append(checks, CheckResult{Name: "config_file", Status: "warn", Message: "配置文件不存在", Hint: "运行 'wechatwriter account init' 创建配置文件"})
	}

	if cfgErr != nil || cfg == nil {
		checks = append(checks, CheckResult{Name: "config_load", Status: "fail", Message: "配置加载失败", Hint: "检查配置文件格式是否正确"})
		return checks
	}

	// AppID / Secret
	if cfg.Wechat.AppID != "" {
		checks = append(checks, CheckResult{Name: "wechat_appid", Status: "pass", Message: "AppID 已配置"})
	} else {
		checks = append(checks, CheckResult{Name: "wechat_appid", Status: "fail", Message: "AppID 未配置", Hint: "在配置文件中设置 wechat.appid"})
	}
	if cfg.Wechat.Secret != "" {
		checks = append(checks, CheckResult{Name: "wechat_secret", Status: "pass", Message: "AppSecret 已配置"})
	} else {
		checks = append(checks, CheckResult{Name: "wechat_secret", Status: "fail", Message: "AppSecret 未配置", Hint: "登录微信公众平台 > 设置与开发 > 基本配置 > 获取 Secret"})
	}

	// AI 文字生成模式
	checks = append(checks, CheckResult{Name: "ai_text_mode", Status: "pass", Message: "AI 文字生成: Claude 代理模式（由 Claude Code 直接生成）"})

	// 图片 API
	articleKey := cfg.Article.Image.Key
	if articleKey != "" {
		checks = append(checks, CheckResult{Name: "article_image_key", Status: "pass", Message: "文章图片 API Key 已配置"})
	} else {
		checks = append(checks, CheckResult{Name: "article_image_key", Status: "warn", Message: "文章图片 API Key 未配置", Hint: "在配置文件中设置 article.image.key（仅 AI 图片生成需要）"})
	}

	// Provider 有效性
	validProviders := map[string]bool{"": true, "openai": true, "gemini": true, "google": true, "openrouter": true, "or": true, "volcengine": true, "volc": true, "seedream": true}
	if p := cfg.Article.Image.Provider; !validProviders[p] {
		checks = append(checks, CheckResult{Name: "article_image_provider", Status: "fail", Message: fmt.Sprintf("无效的图片提供者: %s", p), Hint: "支持的提供者: openai, gemini, openrouter, volcengine"})
	} else if p != "" {
		checks = append(checks, CheckResult{Name: "article_image_provider", Status: "pass", Message: fmt.Sprintf("图片提供者: %s", p)})
	}

	return checks
}

func checkEnvironment() []CheckResult {
	var checks []CheckResult

	tmpDir := os.TempDir()

	// 临时目录写入权限
	tmpFile, err := os.CreateTemp(tmpDir, "wechatwriter_doctor_*")
	if err != nil {
		checks = append(checks, CheckResult{Name: "tmp_writable", Status: "fail", Message: "临时目录不可写: " + tmpDir, Hint: "检查临时目录权限"})
	} else {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		checks = append(checks, CheckResult{Name: "tmp_writable", Status: "pass", Message: "临时目录可写"})
	}

	// 磁盘空间
	if available, ok := getDiskAvailable(tmpDir); ok {
		if available < 100*1024*1024 {
			checks = append(checks, CheckResult{Name: "disk_space", Status: "warn", Message: fmt.Sprintf("临时目录可用空间不足: %dMB", available/1024/1024), Hint: "清理磁盘空间以避免图片处理失败"})
		} else {
			checks = append(checks, CheckResult{Name: "disk_space", Status: "pass", Message: fmt.Sprintf("临时目录可用空间: %dMB", available/1024/1024)})
		}
	}

	return checks
}

func checkNetwork(cfg *config.Config) []CheckResult {
	var checks []CheckResult
	client := &http.Client{Timeout: 5 * time.Second}

	// 微信 API 可达性
	resp, err := client.Get("https://api.weixin.qq.com")
	if err != nil {
		checks = append(checks, CheckResult{Name: "wechat_api_reachable", Status: "fail", Message: "微信 API 不可达", Hint: "检查网络连接或代理设置"})
	} else {
		resp.Body.Close()
		checks = append(checks, CheckResult{Name: "wechat_api_reachable", Status: "pass", Message: "微信 API 可达"})
	}

	// Access Token 测试（仅当 AppID/Secret 已配置）
	if cfg != nil && cfg.Wechat.AppID != "" && cfg.Wechat.Secret != "" {
		ws := wechat.NewService(cfg, log)
		if _, err := ws.GetAccessToken(); err != nil {
			hint := ""
			if wErr := wechat.ParseWechatError(err); wErr != nil {
				hint = wErr.Hint()
			}
			checks = append(checks, CheckResult{Name: "wechat_access_token", Status: "fail", Message: "获取 Access Token 失败: " + err.Error(), Hint: hint})
		} else {
			checks = append(checks, CheckResult{Name: "wechat_access_token", Status: "pass", Message: "Access Token 获取成功"})
		}
	}

	// 图片 API 可达性
	if cfg != nil && cfg.Article.Image.BaseURL != "" {
		resp, err := client.Get(cfg.Article.Image.BaseURL)
		if err != nil {
			checks = append(checks, CheckResult{Name: "image_api_reachable", Status: "warn", Message: "图片 API 不可达: " + cfg.Article.Image.BaseURL, Hint: "检查 article.image.base_url 配置"})
		} else {
			resp.Body.Close()
			checks = append(checks, CheckResult{Name: "image_api_reachable", Status: "pass", Message: "图片 API 可达"})
		}
	}

	return checks
}

func checkDatabase() []CheckResult {
	var checks []CheckResult

	dbPath := filepath.Join(config.ConfigDir, "data.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		checks = append(checks, CheckResult{Name: "db_file", Status: "warn", Message: "数据库未初始化（首次运行时自动创建）", Hint: "运行任意命令后自动创建"})
		return checks
	}

	// 尝试打开 DB 并获取记录数
	s, err := storage.Open(dbPath)
	if err != nil {
		checks = append(checks, CheckResult{Name: "db_open", Status: "fail", Message: "数据库打开失败: " + err.Error(), Hint: "检查 .wechatwriter/data.db 文件权限"})
		return checks
	}
	defer s.Close()

	stat, _ := os.Stat(dbPath)
	sizeMB := float64(0)
	if stat != nil {
		sizeMB = float64(stat.Size()) / (1024 * 1024)
	}

	checks = append(checks, CheckResult{
		Name:    "db_file",
		Status:  "pass",
		Message: fmt.Sprintf("数据库正常: %s (%.2f MB)", dbPath, sizeMB),
	})

	return checks
}
