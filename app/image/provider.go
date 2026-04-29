package image

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/royalrick/anbanwriter/app/config"
	"github.com/rs/zerolog"
)

// 各图片生成服务商的默认模型和 API 地址
const (
	DefaultGeminiModel       = "gemini-3-pro-image-preview"
	DefaultOpenAIModel       = "dall-e-3"
	DefaultVolcengineModel   = "doubao-seedream-5-0-250128"
	DefaultVolcengineBaseURL = "https://ark.cn-beijing.volces.com/api/v3"
)

// GenerateOptions 图片生成选项
type GenerateOptions struct {
	RefImagePath  string   // 本地参考图文件路径（单张，可选）
	RefImagePaths []string // 多张参考图路径（组图模式，可选）
	MaxImages     int      // 组图模式：期望生成图片数量（0 = 单图模式）
}

// GenerateBatchResult 组图生成结果
type GenerateBatchResult struct {
	Images []*GenerateResult
}

// BatchProvider 支持原生组图的提供者（可选接口）
// 不支持原生组图的 provider 无需实现此接口，processor 会自动降级为循环单图生成
type BatchProvider interface {
	Provider
	GenerateBatch(ctx context.Context, prompt string, opts *GenerateOptions) (*GenerateBatchResult, error)
}

// Provider 图片生成服务提供者接口
type Provider interface {
	// Name 返回提供者名称
	Name() string

	// Generate 生成图片，返回图片 URL 或本地路径
	// ctx: 上下文，用于超时控制
	// prompt: 图片生成提示词
	// opts: 可选参数（如参考图），传 nil 表示无附加选项
	Generate(ctx context.Context, prompt string, opts *GenerateOptions) (*GenerateResult, error)
}

// GenerateResult 图片生成结果
type GenerateResult struct {
	URL           string // 生成的图片 URL
	RevisedPrompt string // 优化后的提示词（某些提供者会返回）
	Model         string // 实际使用的模型
	Size          string // 实际尺寸
}

// GenerateError 图片生成错误
type GenerateError struct {
	Provider string // 提供者名称
	Code     string // 错误码
	Message  string // 用户友好的错误信息
	HintMsg  string // 解决提示
	Original error  // 原始错误
}

func (e *GenerateError) Error() string {
	msg := fmt.Sprintf("[%s] %s", e.Provider, e.Message)
	if e.HintMsg != "" {
		msg += fmt.Sprintf("\n提示: %s", e.HintMsg)
	}
	return msg
}

func (e *GenerateError) Unwrap() error {
	return e.Original
}

func (e *GenerateError) Hint() string { return e.HintMsg }

// isContentSafetyError 检测错误信息是否为内容安全/审核拦截
func isContentSafetyError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	safetyKeywords := []string{
		"sensitive", "safety", "content_filter", "blocked", "moderat",
		"违规", "敏感", "违反", "审核", "屏蔽", "过滤", "不合规",
		"content policy", "content filter", "inappropriate",
	}
	for _, kw := range safetyKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// NewProvider 根据 ImageAPI 配置创建对应的 Provider
func NewProvider(apiCfg *config.ImageAPI, log *zerolog.Logger) (Provider, error) {
	switch apiCfg.Provider {
	case "openai", "":
		if err := validateOpenAIConfig(apiCfg); err != nil {
			return nil, err
		}
		return NewOpenAIProvider(apiCfg)
	case "gemini", "google":
		return NewGeminiProvider(apiCfg)
	case "volcengine", "volc", "seedream":
		return NewVolcengineProvider(apiCfg, log)
	default:
		return nil, &config.ConfigError{
			Field:   "ImageProvider",
			Message: fmt.Sprintf("未知的图片服务提供者: %s", apiCfg.Provider),
			HintMsg: "支持的提供者: openai, gemini (google), volcengine (volc, seedream)",
		}
	}
}

// validateOpenAIConfig 验证 OpenAI 配置
func validateOpenAIConfig(apiCfg *config.ImageAPI) error {
	if apiCfg.Key == "" {
		return &config.ConfigError{
			Field:   "ImageAPIKey",
			Message: "使用 OpenAI 图片服务需要配置 API Key",
			HintMsg: "在配置文件中设置 article.image.key 或 xls.image.key",
		}
	}
	if apiCfg.BaseURL == "" {
		return &config.ConfigError{
			Field:   "ImageAPIBase",
			Message: "使用 OpenAI 图片服务需要配置 API Base URL",
			HintMsg: "在配置文件中设置 article.image.base_url，或切换到其他提供者: gemini, volcengine",
		}
	}
	return nil
}

// ReadRefImage 读取参考图文件，返回文件内容、MIME 类型和错误
func ReadRefImage(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("读取参考图失败: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		// 根据扩展名手动设置常见图片类型
		switch ext {
		case ".jpg", ".jpeg":
			mimeType = "image/jpeg"
		case ".png":
			mimeType = "image/png"
		case ".gif":
			mimeType = "image/gif"
		case ".webp":
			mimeType = "image/webp"
		default:
			mimeType = "image/jpeg"
		}
	}

	return data, mimeType, nil
}
