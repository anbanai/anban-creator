package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/royalrick/wechatwriter/app/config"
	"github.com/royalrick/wechatwriter/app/storage"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	cfg     *config.Config
	log     *zap.Logger
	store   *storage.Store
	version = "2.0.0"
)

// initLogger 初始化日志，输出到文件而不是 stderr
func initLogger() (*zap.Logger, error) {
	logPath := filepath.Join(config.ConfigDir, "app.log")

	// 确保目录存在
	if err := os.MkdirAll(config.ConfigDir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	zapConfig := zap.NewProductionConfig()
	zapConfig.OutputPaths = []string{logPath}
	zapConfig.ErrorOutputPaths = []string{logPath}
	zapConfig.Encoding = "json"
	zapConfig.Sampling = nil // 禁用采样，确保所有日志都被写入
	zapConfig.EncoderConfig.TimeKey = "timestamp"
	zapConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	return zapConfig.Build()
}

// initStorage 初始化数据库（静默，失败不影响命令运行）
func initStorage() {
	if store != nil {
		return
	}
	dbPath := filepath.Join(config.ConfigDir, "data.db")
	s, err := storage.Open(dbPath)
	if err != nil {
		// DB 初始化失败不阻断命令，仅记录日志
		if log != nil {
			log.Warn("storage init failed, tracking disabled", zap.Error(err))
		}
		return
	}
	store = s
}

// initConfig 初始化配置（延迟加载，允许 help 命令无需配置）
func initConfig() error {
	if cfg != nil && log != nil {
		return nil
	}

	var err error
	cfg, err = config.Load()
	if err != nil {
		return err
	}

	log, err = initLogger()
	if err != nil {
		// 日志初始化失败不阻断命令，仅输出到 stderr（仅一次）
		fmt.Fprintf(os.Stderr, "⚠️  日志初始化失败: %v\n", err)
		// 创建 no-op logger
		log = zap.NewNop()
	}

	initStorage()
	return nil
}

// initConfigMinimal 初始化配置（不验证微信账号）
// 用于不需要微信 API 的命令（如 AI 提示词生成）
func initConfigMinimal() error {
	if cfg != nil && log != nil {
		return nil
	}

	var err error
	cfg, err = config.LoadMinimal()
	if err != nil {
		return err
	}

	log, err = initLogger()
	if err != nil {
		// 日志初始化失败不阻断命令，仅输出到 stderr（仅一次）
		fmt.Fprintf(os.Stderr, "⚠️  日志初始化失败: %v\n", err)
		// 创建 no-op logger
		log = zap.NewNop()
	}

	initStorage()
	return nil
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "writer",
		Short: "微信公众号写作工具",
		Long: `Writer - 微信公众号写作助手

提供图片处理、格式转换、草稿管理、风格化写作、AI去痕、热点评分等全流程功能。
支持多领域配置和多种写作风格。

Configuration:
  Config file: .wechatwriter/settings.json (use 'wechatwriter account init' to create)`,
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	// 添加所有子命令
	rootCmd.AddCommand(imageCmd())
	rootCmd.AddCommand(convertCmd)
	rootCmd.AddCommand(draftCmd())
	rootCmd.AddCommand(xiaohongshuCmd())
	rootCmd.AddCommand(writeCmd)
	rootCmd.AddCommand(humanizeCmd())
	rootCmd.AddCommand(scoreCmd())
	rootCmd.AddCommand(outlineCmd())
	rootCmd.AddCommand(topicsCmd())
	rootCmd.AddCommand(seoCmd())
	rootCmd.AddCommand(accountCmd())
	rootCmd.AddCommand(doctorCmd())
	rootCmd.AddCommand(contentCmd())

	if err := rootCmd.Execute(); err != nil {
		responseError(err)
		os.Exit(1)
	}

	// 确保日志写入文件
	if log != nil {
		_ = log.Sync()
	}
}

// 响应辅助函数
func responseSuccess(data any) {
	response := map[string]any{
		"success": true,
		"data":    data,
	}
	printJSON(response)
}

func responseError(err error) {
	response := map[string]any{
		"success": false,
		"error":   err.Error(),
	}
	if hint := hintFrom(err); hint != "" {
		response["hint"] = hint
	}
	printJSON(response)
	os.Exit(1)
}

func printJSON(v any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "JSON encode error: %v\n", err)
		os.Exit(1)
	}
}

// maskMediaID 遮蔽 media_id 用于日志
func maskMediaID(id string) string {
	if len(id) < 8 {
		return "***"
	}
	return id[:4] + "***" + id[len(id)-4:]
}
