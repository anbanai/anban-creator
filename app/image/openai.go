package image

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// mapToDALLESize 将用户配置的 size（比例格式）映射到 OpenAI Images API 支持的尺寸
// DALL-E 3 支持: 1024x1024, 1792x1024, 1024x1792
// DALL-E 2 支持: 256x256, 512x512, 1024x1024
// GPT image 支持: 1024x1024, 1536x1024, 1024x1536
func mapToDALLESize(size, model string) string {
	if size == "" {
		return "1024x1024"
	}

	if isDallE2Model(model) {
		return "1024x1024"
	}

	ratio, _ := ParseSize(size)

	if isGPTImageModel(model) {
		w, h := parseRatioNumbers(ratio)
		switch {
		case w > h:
			return "1536x1024"
		case h > w:
			return "1024x1536"
		default:
			return "1024x1024"
		}
	}

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

func isDallE2Model(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), "dall-e-2")
}

func isGPTImageModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-image-") || model == "chatgpt-image-latest"
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
	params := openai.ImageGenerateParams{
		Prompt: prompt,
		Model:  openai.ImageModel(p.model),
		N:      param.NewOpt(int64(1)),
		Size:   openai.ImageGenerateParamsSize(p.size),
	}
	if !isGPTImageModel(p.model) {
		params.ResponseFormat = openai.ImageGenerateParamsResponseFormatB64JSON
	}
	resp, err := p.client.Images.Generate(ctx, params)

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

	return p.imageDataToResult(resp.Data[0])
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

	params := openai.ImageEditParams{
		Image:  openai.ImageEditParamsImageUnion{OfFile: f},
		Prompt: prompt,
		Model:  openai.ImageModel(p.model),
		Size:   openai.ImageEditParamsSize(p.size),
		N:      param.NewOpt(int64(1)),
	}
	if !isGPTImageModel(p.model) {
		params.ResponseFormat = openai.ImageEditParamsResponseFormatB64JSON
	}
	resp, err := p.client.Images.Edit(ctx, params)
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

	return p.imageDataToResult(resp.Data[0])
}

func (p *OpenAIProvider) imageDataToResult(img openai.Image) (*GenerateResult, error) {
	result := &GenerateResult{
		Model:         p.model,
		Size:          p.sizeRatio,
		RevisedPrompt: img.RevisedPrompt,
	}

	if img.B64JSON != "" {
		filePath, saveErr := p.saveBase64Image(img.B64JSON)
		if saveErr != nil {
			return nil, saveErr
		}
		result.URL = filePath
		return result, nil
	}

	if img.URL != "" {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "url_response_unsupported",
			Message:  "OpenAI 图片接口返回了 URL，但当前配置要求 base64 图片数据",
			HintMsg:  "请确认 Endpoint 支持 response_format=b64_json；国内环境不使用 OpenAI 临时图片 URL",
		}
	}

	return nil, &GenerateError{
		Provider: p.Name(),
		Code:     "no_image",
		Message:  "响应中没有图片 URL 或 base64 数据",
		HintMsg:  "请确认模型支持 OpenAI Images API 图片输出",
	}
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
	lowerMsg := strings.ToLower(errMsg)

	if strings.Contains(lowerMsg, "text/html") || strings.Contains(lowerMsg, "not 'application/json'") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "endpoint_protocol",
			Message:  "API 返回了 HTML 而不是 OpenAI Images API JSON 响应",
			HintMsg:  "请确认图片模型 Endpoint 支持 OpenAI Images API（/images/generations 或 /images/edits），不是聊天补全接口或需要网页登录的网关",
			Original: err,
		}
	}

	// 尝试识别常见错误类型
	if strings.Contains(errMsg, "401") || strings.Contains(lowerMsg, "unauthorized") || strings.Contains(lowerMsg, "authentication") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "unauthorized",
			Message:  "API Key 无效或已过期",
			HintMsg:  "请检查配置文件中的 article.image.key 或 xls.image.key 是否正确",
			Original: err,
		}
	}

	if strings.Contains(errMsg, "429") || strings.Contains(lowerMsg, "rate limit") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "rate_limit",
			Message:  "请求过于频繁，请稍后重试",
			HintMsg:  "OpenAI API 有速率限制，请等待一段时间后再试",
			Original: err,
		}
	}

	if strings.Contains(errMsg, "400") || strings.Contains(lowerMsg, "bad request") {
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

	if strings.Contains(errMsg, "402") || strings.Contains(errMsg, "403") || strings.Contains(lowerMsg, "insufficient") || strings.Contains(lowerMsg, "quota") {
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
