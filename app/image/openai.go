package image

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/royalrick/anbanwriter/app/config"
)

// OpenAIProvider OpenAI 图片生成服务提供者
type OpenAIProvider struct {
	client    openai.Client
	model     string
	size      string // DALL-E pixel size for API calls
	sizeRatio string // ratio string for GenerateResult.Size
}

// NewOpenAIProvider 创建 OpenAI Provider
func NewOpenAIProvider(apiCfg *config.ImageAPI) (*OpenAIProvider, error) {
	model := apiCfg.Model
	if model == "" {
		model = DefaultOpenAIModel
	}

	size := mapToDALLESize(apiCfg.Size, model)
	ratio, _ := ParseSize(apiCfg.Size)

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
		client:    client,
		model:     model,
		size:      size,
		sizeRatio: ratio,
	}, nil
}

// mapToDALLESize 将用户配置的 size（比例格式）映射到 DALL-E 支持的尺寸
// DALL-E 3 支持: 1024x1024, 1792x1024, 1024x1792
// DALL-E 2 支持: 256x256, 512x512, 1024x1024
func mapToDALLESize(size, model string) string {
	if size == "" {
		return "1024x1024"
	}

	isDallE2 := model == "dall-e-2"
	if isDallE2 {
		return "1024x1024"
	}

	ratio, _ := ParseSize(size)

	// 宽高比映射到 DALL-E 3 尺寸
	ratioMap := map[string]string{
		"1:1":    "1024x1024",
		"16:9":   "1792x1024",
		"9:16":   "1024x1792",
		"4:3":    "1792x1024", // 近似横向
		"3:4":    "1024x1792", // 近似纵向
		"3:2":    "1792x1024",
		"2:3":    "1024x1792",
		"4:5":    "1024x1792",
		"5:4":    "1792x1024",
		"21:9":   "1792x1024",
		"wide":   "1792x1024",
		"tall":   "1024x1792",
		"square": "1024x1024",
	}
	if mapped, ok := ratioMap[ratio]; ok {
		return mapped
	}

	return "1024x1024" // 默认
}

// Name 返回提供者名称
func (p *OpenAIProvider) Name() string {
	return "OpenAI"
}

// Generate 生成图片
func (p *OpenAIProvider) Generate(ctx context.Context, prompt string, opts *GenerateOptions) (*GenerateResult, error) {
	// 有参考图时，使用 Images.Edit() API
	if opts != nil && opts.RefImagePath != "" {
		return p.generateWithRef(ctx, prompt, opts.RefImagePath)
	}

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
		Size:  p.sizeRatio,
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

// generateWithRef 使用参考图生成图片（调用 Images.Edit() API）
func (p *OpenAIProvider) generateWithRef(ctx context.Context, prompt, refImagePath string) (*GenerateResult, error) {
	f, err := os.Open(refImagePath)
	if err != nil {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "refer_error",
			Message:  "打开参考图失败",
			Original: err,
		}
	}
	defer f.Close()

	resp, err := p.client.Images.Edit(ctx, openai.ImageEditParams{
		Image:  openai.ImageEditParamsImageUnion{OfFile: f},
		Prompt: prompt,
		Model:  openai.ImageModel(p.model),
		Size:   openai.ImageEditParamsSize(p.size),
		N:      param.NewOpt(int64(1)),
	})
	if err != nil {
		return nil, p.wrapSDKError(err)
	}

	if len(resp.Data) == 0 {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "no_image",
			Message:  "未生成图片",
			HintMsg:  "提示词可能不符合内容政策，请尝试修改提示词",
		}
	}

	img := resp.Data[0]

	// GPT image 模型返回 B64JSON，dall-e-2 返回 URL
	if img.B64JSON != "" {
		filePath, saveErr := p.saveBase64Image(img.B64JSON)
		if saveErr != nil {
			return nil, saveErr
		}
		result := &GenerateResult{
			URL:   filePath,
			Model: p.model,
			Size:  p.sizeRatio,
		}
		if img.RevisedPrompt != "" {
			result.RevisedPrompt = img.RevisedPrompt
		}
		return result, nil
	}

	return &GenerateResult{
		URL:           img.URL,
		RevisedPrompt: img.RevisedPrompt,
		Model:         p.model,
		Size:          p.sizeRatio,
	}, nil
}

// saveBase64Image 将 base64 编码的图片数据保存到临时文件，返回文件路径
func (p *OpenAIProvider) saveBase64Image(b64data string) (string, error) {
	imageData, err := base64.StdEncoding.DecodeString(b64data)
	if err != nil {
		return "", &GenerateError{
			Provider: p.Name(),
			Code:     "decode_error",
			Message:  "图片数据解码失败",
			Original: err,
		}
	}

	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("anbanwriter_openai_%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(tmpPath, imageData, 0644); err != nil {
		return "", &GenerateError{
			Provider: p.Name(),
			Code:     "write_error",
			Message:  "图片保存失败",
			Original: err,
		}
	}
	return tmpPath, nil
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
