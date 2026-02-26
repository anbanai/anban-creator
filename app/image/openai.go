package image

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/royalrick/wechatwriter/app/config"
)

// OpenAIProvider OpenAI 图片生成服务提供者
type OpenAIProvider struct {
	client openai.Client
	model  string
	size   string
}

// NewOpenAIProvider 创建 OpenAI Provider
func NewOpenAIProvider(apiCfg *config.ImageAPI) (*OpenAIProvider, error) {
	model := apiCfg.Model
	if model == "" {
		model = DefaultOpenAIModel
	}

	size := mapToDALLESize(apiCfg.Size, model)

	// 创建 OpenAI client，使用官方 SDK
	opts := []option.RequestOption{
		option.WithAPIKey(apiCfg.Key),
	}

	// 如果配置了自定义 BaseURL，使用它
	if apiCfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(apiCfg.BaseURL))
	}

	client := openai.NewClient(opts...)

	return &OpenAIProvider{
		client: client,
		model:  model,
		size:   size,
	}, nil
}

// mapToDALLESize 将用户配置的 size（宽高比或像素格式）映射到 DALL-E 支持的尺寸
// DALL-E 3 支持: 1024x1024, 1792x1024, 1024x1792
// DALL-E 2 支持: 256x256, 512x512, 1024x1024
func mapToDALLESize(size, model string) string {
	if size == "" {
		return "1024x1024"
	}

	isDallE2 := model == "dall-e-2"

	// 精确尺寸直接通过（DALL-E 3 格式）
	dalle3Sizes := map[string]bool{
		"1024x1024": true, "1792x1024": true, "1024x1792": true,
	}
	if !isDallE2 && dalle3Sizes[size] {
		return size
	}

	// DALL-E 2 支持的尺寸
	dalle2Sizes := map[string]bool{
		"256x256": true, "512x512": true, "1024x1024": true,
	}
	if isDallE2 && dalle2Sizes[size] {
		return size
	}

	if isDallE2 {
		return "1024x1024"
	}

	// 宽高比映射到 DALL-E 3 尺寸
	ratioMap := map[string]string{
		"1:1":   "1024x1024",
		"16:9":  "1792x1024",
		"9:16":  "1024x1792",
		"4:3":   "1792x1024", // 近似横向
		"3:4":   "1024x1792", // 近似纵向
		"3:2":   "1792x1024",
		"2:3":   "1024x1792",
		"21:9":  "1792x1024",
		"wide":  "1792x1024",
		"tall":  "1024x1792",
		"square": "1024x1024",
	}
	if mapped, ok := ratioMap[size]; ok {
		return mapped
	}

	// 像素格式：分析宽高比确定方向
	// WIDTHxHEIGHT 格式
	var w, h int
	if n, err := fmt.Sscanf(size, "%dx%d", &w, &h); n == 2 && err == nil {
		if w > h {
			return "1792x1024" // 横向
		} else if h > w {
			return "1024x1792" // 纵向
		}
		return "1024x1024" // 正方形
	}

	return "1024x1024" // 默认
}

// Name 返回提供者名称
func (p *OpenAIProvider) Name() string {
	return "OpenAI"
}

// Generate 生成图片
func (p *OpenAIProvider) Generate(ctx context.Context, prompt string) (*GenerateResult, error) {
	// 调用 SDK 生成图片
	resp, err := p.client.Images.Generate(ctx, openai.ImageGenerateParams{
		Prompt: prompt,
		Model:  openai.ImageModel(p.model),
		N:      param.NewOpt(int64(1)),
		Size:   openai.ImageGenerateParamsSize(p.size),
	})

	if err != nil {
		// 包装 SDK 错误为 GenerateError
		return nil, p.wrapSDKError(err)
	}

	// 检查是否有生成的图片
	if len(resp.Data) == 0 {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "no_image",
			Message:  "未生成图片",
			HintMsg:  "提示词可能不符合内容政策，请尝试修改提示词",
		}
	}

	result := &GenerateResult{
		Model: p.model,
		Size:  p.size,
	}

	// 提取 URL
	if resp.Data[0].URL != "" {
		result.URL = resp.Data[0].URL
	}

	// 提取修订后的提示词（如果有）
	if resp.Data[0].RevisedPrompt != "" {
		result.RevisedPrompt = resp.Data[0].RevisedPrompt
	}

	return result, nil
}

// wrapSDKError 将 SDK 错误包装为 GenerateError
func (p *OpenAIProvider) wrapSDKError(err error) error {
	// SDK 错误已经包含详细信息，我们只需要添加友好的提示
	errMsg := err.Error()

	// 尝试识别常见错误类型
	if contains(errMsg, "401") || contains(errMsg, "unauthorized") || contains(errMsg, "authentication") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "unauthorized",
			Message:  "API Key 无效或已过期",
			HintMsg:  "请检查配置文件中的 article.image.key 或 post.image.key 是否正确",
			Original: err,
		}
	}

	if contains(errMsg, "429") || contains(errMsg, "rate limit") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "rate_limit",
			Message:  "请求过于频繁，请稍后重试",
			HintMsg:  "OpenAI API 有速率限制，请等待一段时间后再试",
			Original: err,
		}
	}

	if contains(errMsg, "400") || contains(errMsg, "bad request") {
		if isContentSafetyError(errMsg) {
			return &GenerateError{
				Provider: p.Name(),
				Code:     "safety_blocked",
				Message:  "提示词被内容安全策略拦截",
				HintMsg:  "提示词可能包含敏感内容，请修改提示词后重试",
				Original: err,
			}
		}
		return &GenerateError{
			Provider: p.Name(),
			Code:     "bad_request",
			Message:  "请求参数错误",
			HintMsg:  "请检查图片尺寸、模型名称等参数是否正确",
			Original: err,
		}
	}

	if contains(errMsg, "402") || contains(errMsg, "403") || contains(errMsg, "insufficient") || contains(errMsg, "quota") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "payment_required",
			Message:  "账户余额不足或访问受限",
			HintMsg:  "请检查 OpenAI 账户余额和 API 使用权限",
			Original: err,
		}
	}

	// 其他错误
	return &GenerateError{
		Provider: p.Name(),
		Code:     "unknown",
		Message:  fmt.Sprintf("API 请求失败: %v", err),
		HintMsg:  "请稍后重试，或检查 OpenAI 服务状态",
		Original: err,
	}
}

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

// indexOf 返回子串在字符串中的位置
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
