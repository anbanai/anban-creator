package image

import (
	"context"
	"fmt"
	"strings"

	"github.com/royalrick/wechatwriter/app/config"
)

// 各图片生成服务商的默认模型和 API 地址
const (
	DefaultGeminiModel       = "gemini-3-pro-image-preview"
	DefaultOpenAIModel       = "dall-e-3"
	DefaultOpenRouterModel   = "google/gemini-3-pro-image-preview"
	DefaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
	DefaultVolcengineModel   = "doubao-seedream-4-5-251128"
	DefaultVolcengineBaseURL = "https://ark.cn-beijing.volces.com/api/v3"
)

// Provider 图片生成服务提供者接口
type Provider interface {
	// Name 返回提供者名称
	Name() string

	// Generate 生成图片，返回图片 URL
	// ctx: 上下文，用于超时控制
	// prompt: 图片生成提示词
	Generate(ctx context.Context, prompt string) (*GenerateResult, error)
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
func NewProvider(apiCfg *config.ImageAPI) (Provider, error) {
	switch apiCfg.Provider {
	case "openai", "":
		if err := validateOpenAIConfig(apiCfg); err != nil {
			return nil, err
		}
		return NewOpenAIProvider(apiCfg)
	case "gemini", "google":
		return NewGeminiProvider(apiCfg)
	case "openrouter", "or":
		return NewOpenRouterProvider(apiCfg)
	case "volcengine", "volc", "seedream":
		return NewVolcengineProvider(apiCfg)
	default:
		return nil, &config.ConfigError{
			Field:   "ImageProvider",
			Message: fmt.Sprintf("未知的图片服务提供者: %s", apiCfg.Provider),
			HintMsg: "支持的提供者: openai, gemini (google), openrouter (or), volcengine (volc, seedream)",
		}
	}
}

// validateOpenAIConfig 验证 OpenAI 配置
func validateOpenAIConfig(apiCfg *config.ImageAPI) error {
	if apiCfg.Key == "" {
		return &config.ConfigError{
			Field:   "ImageAPIKey",
			Message: "使用 OpenAI 图片服务需要配置 API Key",
			HintMsg: "在配置文件中设置 article.image.key 或 post.image.key",
		}
	}
	if apiCfg.BaseURL == "" {
		return &config.ConfigError{
			Field:   "ImageAPIBase",
			Message: "需要配置 API Base URL",
			HintMsg: "在配置文件中设置 article.image.base_url",
		}
	}
	return nil
}
