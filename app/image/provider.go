package image

import (
	"context"
	"fmt"
	"mime"
	"path/filepath"
	"strings"

	"github.com/anbanai/anban-creator/app/config"
	"github.com/rs/zerolog"
)

// 各图片生成服务商的默认模型和 API 地址
const (
	DefaultGeminiModel       = "gemini-3-pro-image-preview"
	DefaultOpenAIModel       = "gpt-image-2"
	DefaultVolcengineModel   = "doubao-seedream-5-0-pro-260628"
	DefaultVolcengineBaseURL = "https://ark.cn-beijing.volces.com/api/v3"
)

// GenerateOptions 图片生成选项
type GenerateOptions struct {
	RefImagePath        string         // 本地参考图文件路径（单张，可选）
	RefImagePaths       []string       // 多张参考图路径（组图模式，可选）
	MaskPath            string         // inpainting mask 文件路径（PNG with alpha，可选）
	Quality             string         // 图片质量: "low", "medium", "high", "auto"（可选）
	OutputFormat        string         // 输出格式: "png", "jpeg", "webp"（可选）
	OutputCompression   int            // 压缩率 0-100（仅 JPEG/WebP，0 表示使用 API 默认值）
	Background          string         // 背景: "auto", "opaque", "transparent"（可选）
	N                   int            // 批量生成数量，1-10（默认 1）
	Size                string         // 自定义尺寸或比例（覆盖默认尺寸）
	SemanticAspectRatio bool           // 任务语义比例模式：由 prompt 控制，Provider 使用自动/省略尺寸协议
	Watermark           *bool          // 是否启用水印（仅 Volcengine 支持此选项）
	StreamCB            StreamCallback // 流式回调（nil 表示不启用流式）
}

// StreamCallback 流式图片生成回调函数
type StreamCallback func(partial *PartialImage)

// PartialImage 流式生成过程中的部分图片
type PartialImage struct {
	Index    int    // 图片序号（批量生成时从 0 开始）
	B64Data  string // Base64 编码的部分图片数据
	Progress int    // 生成进度 0-100
	Final    bool   // 是否为最终图片
}

// Provider 图片生成服务提供者接口
type Provider interface {
	// Name 返回提供者名称
	Name() string

	// Generate 生成图片，返回图片 URL 或本地路径
	// ctx: 上下文，用于超时控制
	// prompt: 图片生成提示词
	// opts: 可选参数（如参考图、质量、批量等），传 nil 表示无附加选项
	Generate(ctx context.Context, prompt string, opts *GenerateOptions) (*GenerateResult, error)

	// Capabilities 返回提供者支持的能力
	Capabilities() *ProviderCapabilities
}

// GenerateResult 图片生成结果
type GenerateResult struct {
	ProviderRequestID string
	OutputWidth       int
	OutputHeight      int
	URL               string           // 生成的图片 URL（单张时使用）
	RevisedPrompt     string           // 优化后的提示词（某些提供者会返回）
	Model             string           // 实际使用的模型
	Size              string           // 实际尺寸
	ResponseType      string           // 返回类型：b64_json / url / file / empty
	ResponsePreview   string           // 原始返回预览：URL 原样输出，base64 截断输出
	Images            []GeneratedImage // 批量生成的多张图片
	Usage             *ImageGenerationUsage
}

type ImageGenerationUsage struct {
	TextInputTokens        int64
	TextCachedInputTokens  int64
	ImageInputTokens       int64
	ImageCachedInputTokens int64
	ImageOutputTokens      int64
	TotalTokens            int64
}

// GeneratedImage 单张生成的图片
type GeneratedImage struct {
	URL   string // 图片 URL 或本地路径
	B64   string // Base64 编码的图片数据（可选）
	Index int    // 图片序号
}

// ProviderCapabilities 图片生成提供者的能力描述
type ProviderCapabilities struct {
	MaxRefImages   int      // 最大参考图数量
	Batch          bool     // 是否支持批量生成
	MaxBatch       int      // 最大批量数量
	Streaming      bool     // 是否支持流式生成
	Inpainting     bool     // 是否支持 mask inpainting
	QualityLevels  []string // 支持的质量级别
	OutputFormats  []string // 支持的输出格式
	FlexibleSize   bool     // 是否支持自定义尺寸
	HasCompression bool     // 是否支持压缩率设置
	HasBackground  bool     // 是否支持背景设置
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

func (e *GenerateError) Retryable() bool {
	switch e.Code {
	case "server_error", "rate_limit", "network_error":
		return true
	default:
		return false
	}
}

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

// truncateRunes returns s truncated to maxRunes runes, with "..." appended if truncated.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// NewProvider 根据 ImageAPI 配置创建对应的 Provider
func NewProvider(apiCfg *config.ImageAPI, log *zerolog.Logger) (Provider, error) {
	switch apiCfg.Provider {
	case "openai", "wangcai_openai", "":
		if err := validateOpenAIConfig(apiCfg); err != nil {
			return nil, err
		}
		return NewOpenAIProvider(apiCfg, log)
	case "gemini", "google":
		return NewGeminiProvider(apiCfg, log)
	case "volcengine", "volcengine_ark", "volc", "seedream":
		return NewVolcengineProvider(apiCfg, log)
	default:
		return nil, &config.ConfigError{
			Field:   "ImageProvider",
			Message: fmt.Sprintf("未知的图片服务提供者: %s", apiCfg.Provider),
			HintMsg: "支持的提供者: openai (wangcai_openai), gemini (google), volcengine (volcengine_ark, volc, seedream)",
		}
	}
}

// validateOpenAIConfig 验证 OpenAI 配置
func validateOpenAIConfig(apiCfg *config.ImageAPI) error {
	if apiCfg.Key == "" {
		return &config.ConfigError{
			Field:   "ImageAPIKey",
			Message: "使用 OpenAI 图片服务需要配置 API Key",
			HintMsg: "在配置文件中设置 image.key",
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
	data, err := readGeneratedImageFile(path)
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
