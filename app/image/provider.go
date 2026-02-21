package image

import (
	"context"
	"fmt"

	"github.com/royalrick/wechatwriter/app/config"
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
