package image

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/app/config"
	"github.com/anbanai/anban-creator/app/wechat"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/rs/zerolog"
)

// OpenAIProvider OpenAI 图片生成服务提供者
type OpenAIProvider struct {
	client         openai.Client
	model          string
	size           string // Image size for API calls
	sizeRatio      string // ratio string for GenerateResult.Size
	responseFormat string // "b64_json" | "url" | "" (auto → b64_json)
	log            *zerolog.Logger
}

// NewOpenAIProvider 创建 OpenAI Provider
func NewOpenAIProvider(apiCfg *config.ImageAPI, log *zerolog.Logger) (*OpenAIProvider, error) {
	model := apiCfg.Model
	if model == "" {
		model = DefaultOpenAIModel
	}

	size := mapToImageSize(apiCfg.Size, model)

	var sizeRatio string
	switch {
	case isAutoSize(apiCfg.Size):
		sizeRatio = "auto"
	case IsPixelSize(apiCfg.Size):
		sizeRatio = apiCfg.Size
	default:
		sizeRatio, _ = ParseSize(apiCfg.Size)
	}

	// 创建 OpenAI client，使用官方 SDK
	timeout := 300 * time.Second
	if apiCfg.TimeoutSec > 0 {
		timeout = time.Duration(apiCfg.TimeoutSec) * time.Second
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiCfg.Key),
		option.WithRequestTimeout(timeout),
	}

	// 如果配置了自定义 BaseURL，使用它
	if apiCfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(apiCfg.BaseURL))
	}

	client := openai.NewClient(opts...)

	return &OpenAIProvider{
		client:         client,
		model:          model,
		size:           size,
		sizeRatio:      sizeRatio,
		responseFormat: apiCfg.ResponseFormat,
		log:            log,
	}, nil
}

// mapToImageSize 将用户配置的 size（"auto"、比例或像素）映射到 OpenAI Images API 的 size 参数。
// GPT Image 模型支持 "auto"、任意满足约束的像素尺寸，以及比例（→ 1024/1536 系列）。
func mapToImageSize(size, model string) string {
	if size == "" {
		return "1024x1024"
	}

	// GPT Image 支持 size: "auto"，让 OpenAI 根据提示词自选宽高比。
	// 也接受 "auto:2K"/"auto:4K"——tier 后缀在 isAutoSize 内部被识别并剥离。
	if isGPTImageModel(model) && isAutoSize(size) {
		return "auto"
	}

	// GPT Image 接受任意满足约束的像素尺寸，直接透传。
	if isGPTImageModel(model) && IsPixelSize(size) {
		return size
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

	return "1024x1024"
}

func isGPTImageModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-image-") || model == "chatgpt-image-latest"
}

func requiresImageUsageForBilling(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "gpt-image-2" || strings.HasPrefix(model, "gpt-image-2-")
}

// isAutoSize 识别 "auto" 或 "auto:<tier>" 形式的 size。
// 大小写不敏感、忽略前后空白；tier 后缀由 mapToImageSize 在返回时丢弃。
func isAutoSize(size string) bool {
	s := strings.ToLower(strings.TrimSpace(size))
	return s == "auto" || strings.HasPrefix(s, "auto:")
}

// Name 返回提供者名称
func (p *OpenAIProvider) Name() string {
	return "OpenAI"
}

// Generate 生成图片
func (p *OpenAIProvider) Generate(ctx context.Context, prompt string, opts *GenerateOptions) (*GenerateResult, error) {
	if opts == nil {
		opts = &GenerateOptions{}
	}

	start := time.Now()

	refCount := len(opts.RefImagePaths)
	if opts.RefImagePath != "" {
		refCount++
	}

	p.log.Debug().
		Str("prompt_preview", truncateRunes(prompt, 80)).
		Str("model", p.model).
		Str("size", p.size).
		Str("quality", opts.Quality).
		Str("output_format", opts.OutputFormat).
		Str("response_format", p.responseFormat).
		Int("n", opts.N).
		Int("ref_count", refCount).
		Bool("has_mask", opts.MaskPath != "").
		Bool("streaming", opts.StreamCB != nil && isGPTImageModel(p.model)).
		Msg("openai: generating image")

	// Determine effective size
	size := p.size
	if opts.Size != "" {
		size = mapToImageSize(opts.Size, p.model)
	}

	hasRefImages := opts.RefImagePath != "" || len(opts.RefImagePaths) > 0
	hasMask := opts.MaskPath != ""
	useStreaming := opts.StreamCB != nil && isGPTImageModel(p.model)

	var result *GenerateResult
	var err error

	if hasRefImages || hasMask {
		if useStreaming {
			result, err = p.generateEditStreaming(ctx, prompt, opts, size)
		} else {
			result, err = p.generateEdit(ctx, prompt, opts, size)
		}
	} else if useStreaming {
		result, err = p.generateStreaming(ctx, prompt, opts, size)
	} else {
		result, err = p.generateStandard(ctx, prompt, opts, size)
	}

	if err != nil {
		p.log.Error().Err(err).
			Str("model", p.model).
			Dur("elapsed", time.Since(start)).
			Msg("openai: image generation failed")
		return nil, err
	}
	if requiresImageUsageForBilling(p.model) && (result.Usage == nil || result.Usage.TotalTokens <= 0 || result.Usage.ImageOutputTokens <= 0) {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "missing_usage",
			Message:  fmt.Sprintf("%s usage is required for billing", p.model),
			HintMsg:  "请确认 OpenAI-compatible 网关会透传 image usage；否则不能启用 GPT Image 2 生产通道。",
		}
	}

	imgCount := 1
	if len(result.Images) > 0 {
		imgCount = len(result.Images)
	}
	p.log.Info().
		Str("model", p.model).
		Int("image_count", imgCount).
		Dur("elapsed", time.Since(start)).
		Msg("openai: image generation completed")

	return result, nil
}

// Capabilities returns OpenAI provider capabilities
func (p *OpenAIProvider) Capabilities() *ProviderCapabilities {
	caps := &ProviderCapabilities{
		MaxRefImages:  1,
		Batch:         false,
		MaxBatch:      1,
		Streaming:     false,
		Inpainting:    false,
		OutputFormats: []string{"png"},
		FlexibleSize:  false,
	}

	if isGPTImageModel(p.model) {
		caps.MaxRefImages = 16
		caps.Batch = true
		caps.MaxBatch = 10
		caps.Streaming = true
		caps.Inpainting = true
		caps.QualityLevels = []string{"auto", "low", "medium", "high"}
		caps.OutputFormats = []string{"png", "jpeg", "webp"}
		caps.FlexibleSize = true
		caps.HasCompression = true
		caps.HasBackground = true
	}

	return caps
}

// generateStandard generates images without streaming
func (p *OpenAIProvider) generateStandard(ctx context.Context, prompt string, opts *GenerateOptions, size string) (*GenerateResult, error) {
	n := int64(1)
	if opts.N > 1 {
		n = int64(opts.N)
	}

	params := openai.ImageGenerateParams{
		Prompt: prompt,
		Model:  openai.ImageModel(p.model),
		N:      param.NewOpt(n),
		Size:   openai.ImageGenerateParamsSize(size),
	}

	switch p.responseFormat {
	case "url":
		params.ResponseFormat = openai.ImageGenerateParamsResponseFormatURL
	default:
		params.ResponseFormat = openai.ImageGenerateParamsResponseFormatB64JSON
	}

	if opts.Quality != "" {
		params.Quality = openai.ImageGenerateParamsQuality(opts.Quality)
	}
	if opts.OutputFormat != "" {
		params.OutputFormat = openai.ImageGenerateParamsOutputFormat(opts.OutputFormat)
	}
	if opts.OutputCompression > 0 && opts.OutputCompression < 100 {
		params.OutputCompression = param.NewOpt(int64(opts.OutputCompression))
	}
	if opts.Background != "" {
		params.Background = openai.ImageGenerateParamsBackground(opts.Background)
	}

	resp, err := p.client.Images.Generate(ctx, params)
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

	return p.imageResponseToResult(resp)
}

// generateStreaming generates images with progressive partial delivery
func (p *OpenAIProvider) generateStreaming(ctx context.Context, prompt string, opts *GenerateOptions, size string) (*GenerateResult, error) {
	n := int64(1)
	if opts.N > 1 {
		n = int64(opts.N)
	}

	params := openai.ImageGenerateParams{
		Prompt:        prompt,
		Model:         openai.ImageModel(p.model),
		N:             param.NewOpt(n),
		Size:          openai.ImageGenerateParamsSize(size),
		PartialImages: param.NewOpt(int64(3)),
	}

	if opts.Quality != "" {
		params.Quality = openai.ImageGenerateParamsQuality(opts.Quality)
	}
	if opts.OutputFormat != "" {
		params.OutputFormat = openai.ImageGenerateParamsOutputFormat(opts.OutputFormat)
	}
	if opts.OutputCompression > 0 && opts.OutputCompression < 100 {
		params.OutputCompression = param.NewOpt(int64(opts.OutputCompression))
	}
	if opts.Background != "" {
		params.Background = openai.ImageGenerateParamsBackground(opts.Background)
	}

	stream := p.client.Images.GenerateStreaming(ctx, params)
	var finalB64 string
	var resultSize string
	var usage *ImageGenerationUsage

	for stream.Next() {
		event := stream.Current()
		switch evt := event.AsAny().(type) {
		case openai.ImageGenPartialImageEvent:
			if opts.StreamCB != nil {
				progress := int((evt.PartialImageIndex + 1) * 25)
				if progress > 100 {
					progress = 100
				}
				opts.StreamCB(&PartialImage{
					Index:    int(evt.PartialImageIndex),
					B64Data:  evt.B64JSON,
					Progress: progress,
					Final:    false,
				})
			}
		case openai.ImageGenCompletedEvent:
			finalB64 = evt.B64JSON
			resultSize = string(evt.Size)
			usage = usageFromImageGenCompletedEvent(evt.Usage)
		}
	}

	if err := stream.Err(); err != nil {
		return nil, p.wrapSDKError(err)
	}

	if finalB64 == "" {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "no_image",
			Message:  "流式生成完成但未收到最终图片",
			HintMsg:  "请稍后重试",
		}
	}

	// Send final via callback
	if opts.StreamCB != nil {
		opts.StreamCB(&PartialImage{
			Index:    0,
			B64Data:  finalB64,
			Progress: 100,
			Final:    true,
		})
	}

	filePath, err := p.saveBase64Image(finalB64)
	if err != nil {
		return nil, err
	}

	return &GenerateResult{
		URL:             filePath,
		Model:           p.model,
		Size:            resultSize,
		ResponseType:    "b64_json",
		ResponsePreview: previewBase64(finalB64),
		Usage:           usage,
	}, nil
}

// generateEdit generates images using the edit API (with reference images / mask)
func (p *OpenAIProvider) generateEdit(ctx context.Context, prompt string, opts *GenerateOptions, size string) (*GenerateResult, error) {
	params, cleanup, err := p.buildEditParams(prompt, opts, size)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	resp, err := p.client.Images.Edit(ctx, *params)
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

	return p.imageResponseToResult(resp)
}

// generateEditStreaming generates edited images with streaming
func (p *OpenAIProvider) generateEditStreaming(ctx context.Context, prompt string, opts *GenerateOptions, size string) (*GenerateResult, error) {
	params, cleanup, err := p.buildEditParams(prompt, opts, size)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	stream := p.client.Images.EditStreaming(ctx, *params)
	var finalB64 string
	var resultSize string
	var usage *ImageGenerationUsage

	for stream.Next() {
		event := stream.Current()
		switch evt := event.AsAny().(type) {
		case openai.ImageEditPartialImageEvent:
			if opts.StreamCB != nil {
				progress := int((evt.PartialImageIndex + 1) * 25)
				if progress > 100 {
					progress = 100
				}
				opts.StreamCB(&PartialImage{
					Index:    int(evt.PartialImageIndex),
					B64Data:  evt.B64JSON,
					Progress: progress,
					Final:    false,
				})
			}
		case openai.ImageEditCompletedEvent:
			finalB64 = evt.B64JSON
			resultSize = string(evt.Size)
			usage = usageFromImageEditCompletedEvent(evt.Usage)
		}
	}

	if err := stream.Err(); err != nil {
		return nil, p.wrapSDKError(err)
	}

	if finalB64 == "" {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "no_image",
			Message:  "流式编辑完成但未收到最终图片",
			HintMsg:  "请稍后重试",
		}
	}

	if opts.StreamCB != nil {
		opts.StreamCB(&PartialImage{
			Index:    0,
			B64Data:  finalB64,
			Progress: 100,
			Final:    true,
		})
	}

	filePath, err := p.saveBase64Image(finalB64)
	if err != nil {
		return nil, err
	}

	return &GenerateResult{
		URL:             filePath,
		Model:           p.model,
		Size:            resultSize,
		ResponseType:    "b64_json",
		ResponsePreview: previewBase64(finalB64),
		Usage:           usage,
	}, nil
}

// buildEditParams constructs ImageEditParams from GenerateOptions.
// Returns the params, a cleanup function, and any error.
func (p *OpenAIProvider) buildEditParams(prompt string, opts *GenerateOptions, size string) (*openai.ImageEditParams, func(), error) {
	var openFiles []*os.File
	var cleanupFiles []string
	cleanup := func() {
		for _, f := range openFiles {
			f.Close()
		}
		for _, f := range cleanupFiles {
			os.Remove(f)
		}
	}

	// Collect all reference image paths
	paths := make([]string, 0, len(opts.RefImagePaths)+1)
	if opts.RefImagePath != "" {
		paths = append(paths, opts.RefImagePath)
	}
	paths = append(paths, opts.RefImagePaths...)

	params := &openai.ImageEditParams{
		Prompt: prompt,
		Model:  openai.ImageModel(p.model),
		Size:   openai.ImageEditParamsSize(size),
	}

	if opts.Quality != "" {
		params.Quality = openai.ImageEditParamsQuality(opts.Quality)
	}
	if opts.OutputFormat != "" {
		params.OutputFormat = openai.ImageEditParamsOutputFormat(opts.OutputFormat)
	}
	if opts.OutputCompression > 0 && opts.OutputCompression < 100 {
		params.OutputCompression = param.NewOpt(int64(opts.OutputCompression))
	}
	if opts.Background != "" {
		params.Background = openai.ImageEditParamsBackground(opts.Background)
	}
	switch p.responseFormat {
	case "url":
		params.ResponseFormat = openai.ImageEditParamsResponseFormatURL
	default:
		params.ResponseFormat = openai.ImageEditParamsResponseFormatB64JSON
	}
	if opts.N > 1 {
		params.N = param.NewOpt(int64(opts.N))
	}

	// Handle reference images
	if len(paths) == 1 {
		f, err := os.Open(paths[0])
		if err != nil {
			cleanup()
			return nil, nil, &GenerateError{
				Provider: p.Name(),
				Code:     "refer_error",
				Message:  "打开参考图失败",
				Original: err,
			}
		}
		openFiles = append(openFiles, f)
		params.Image = openai.ImageEditParamsImageUnion{OfFile: f}
	} else if len(paths) > 1 {
		readers := make([]io.Reader, 0, len(paths))
		for _, p := range paths {
			f, err := os.Open(p)
			if err != nil {
				cleanup()
				return nil, nil, &GenerateError{
					Provider: "OpenAI",
					Code:     "refer_error",
					Message:  fmt.Sprintf("打开参考图失败: %s", p),
					Original: err,
				}
			}
			openFiles = append(openFiles, f)
			readers = append(readers, f)
		}
		params.Image = openai.ImageEditParamsImageUnion{OfFileArray: readers}
	}

	// Handle mask
	if opts.MaskPath != "" {
		maskFile, err := os.Open(opts.MaskPath)
		if err != nil {
			cleanup()
			return nil, nil, &GenerateError{
				Provider: p.Name(),
				Code:     "mask_error",
				Message:  "打开 mask 文件失败",
				Original: err,
			}
		}
		openFiles = append(openFiles, maskFile)
		params.Mask = maskFile
	}

	return params, cleanup, nil
}

// imageResponseToResult converts an ImagesResponse to a GenerateResult
func (p *OpenAIProvider) imageResponseToResult(resp *openai.ImagesResponse) (*GenerateResult, error) {
	size := p.sizeRatio
	if resp.Size != "" {
		size = string(resp.Size)
	}
	usage := usageFromImagesResponse(resp.Usage)
	result := &GenerateResult{
		Model: p.model,
		Size:  size,
		Usage: usage,
	}

	if len(resp.Data) == 1 {
		single, err := p.imageDataToResult(resp.Data[0])
		if err != nil {
			return nil, err
		}
		single.Size = size
		single.Usage = usage
		return single, nil
	}

	// Multiple images (batch)
	images := make([]GeneratedImage, 0, len(resp.Data))
	for i, img := range resp.Data {
		filePath, err := p.saveImageData(img)
		if err != nil {
			return nil, err
		}
		images = append(images, GeneratedImage{
			URL:   filePath,
			Index: i,
		})
	}
	result.Images = images
	if len(images) > 0 {
		result.URL = images[0].URL
	}
	result.ResponseType = "b64_json"
	return result, nil
}

// saveImageData saves a single Image from API response to a temp file
func (p *OpenAIProvider) saveImageData(img openai.Image) (string, error) {
	if img.B64JSON != "" {
		return p.saveBase64Image(img.B64JSON)
	}
	if img.URL != "" {
		filePath, err := wechat.DownloadFile(img.URL)
		if err != nil {
			return "", &GenerateError{
				Provider: p.Name(),
				Code:     "url_download_error",
				Message:  fmt.Sprintf("下载图片失败: %s", img.URL),
				Original: err,
			}
		}
		return filePath, nil
	}
	return "", &GenerateError{
		Provider: p.Name(),
		Code:     "no_image",
		Message:  "响应中没有图片数据",
	}
}

func (p *OpenAIProvider) imageDataToResult(img openai.Image) (*GenerateResult, error) {
	result := &GenerateResult{
		Model:         p.model,
		Size:          p.sizeRatio,
		RevisedPrompt: img.RevisedPrompt,
	}

	if img.B64JSON != "" {
		result.ResponseType = "b64_json"
		result.ResponsePreview = previewBase64(img.B64JSON)
		filePath, saveErr := p.saveBase64Image(img.B64JSON)
		if saveErr != nil {
			return nil, saveErr
		}
		result.URL = filePath
		return result, nil
	}

	if img.URL != "" {
		result.ResponseType = "url"
		result.ResponsePreview = img.URL
		filePath, err := wechat.DownloadFile(img.URL)
		if err != nil {
			return nil, &GenerateError{
				Provider: p.Name(),
				Code:     "url_download_error",
				Message:  fmt.Sprintf("OpenAI 图片接口返回了 URL，但下载失败: %s", img.URL),
				HintMsg:  fmt.Sprintf("RevisedPrompt=%q", img.RevisedPrompt),
				Original: err,
			}
		}
		result.URL = filePath
		return result, nil
	}

	result.ResponseType = "empty"
	return nil, &GenerateError{
		Provider: p.Name(),
		Code:     "no_image",
		Message:  "响应中没有图片 URL 或 base64 数据",
		HintMsg:  "请确认模型支持 OpenAI Images API 图片输出",
	}
}

func usageFromImagesResponse(usage openai.ImagesResponseUsage) *ImageGenerationUsage {
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
		return nil
	}
	outputTokens := usage.OutputTokensDetails.ImageTokens
	if outputTokens <= 0 {
		outputTokens = usage.OutputTokens
	}
	return &ImageGenerationUsage{
		TextInputTokens:   usage.InputTokensDetails.TextTokens,
		ImageInputTokens:  usage.InputTokensDetails.ImageTokens,
		ImageOutputTokens: outputTokens,
		TotalTokens:       usage.TotalTokens,
	}
}

func usageFromImageGenCompletedEvent(usage openai.ImageGenCompletedEventUsage) *ImageGenerationUsage {
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
		return nil
	}
	return &ImageGenerationUsage{
		TextInputTokens:   usage.InputTokensDetails.TextTokens,
		ImageInputTokens:  usage.InputTokensDetails.ImageTokens,
		ImageOutputTokens: usage.OutputTokens,
		TotalTokens:       usage.TotalTokens,
	}
}

func usageFromImageEditCompletedEvent(usage openai.ImageEditCompletedEventUsage) *ImageGenerationUsage {
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
		return nil
	}
	return &ImageGenerationUsage{
		TextInputTokens:   usage.InputTokensDetails.TextTokens,
		ImageInputTokens:  usage.InputTokensDetails.ImageTokens,
		ImageOutputTokens: usage.OutputTokens,
		TotalTokens:       usage.TotalTokens,
	}
}

func previewBase64(value string) string {
	const maxPreviewLen = 120
	value = strings.TrimSpace(value)
	if len(value) <= maxPreviewLen {
		return value
	}
	return fmt.Sprintf("%s... (truncated, total %d chars)", value[:maxPreviewLen], len(value))
}

// saveBase64Image 将 base64 编码的图片数据保存到临时文件，返回文件路径
func (p *OpenAIProvider) saveBase64Image(b64data string) (string, error) {
	preview := previewBase64(b64data)
	imageData, err := base64.StdEncoding.DecodeString(normalizeBase64ImageData(b64data))
	if err != nil {
		return "", &GenerateError{
			Provider: p.Name(),
			Code:     "decode_error",
			Message:  "图片数据解码失败",
			HintMsg:  fmt.Sprintf("b64_json 预览: %s", preview),
			Original: err,
		}
	}

	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("anban-creator_openai_%d.png", time.Now().UnixNano()))
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

func normalizeBase64ImageData(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:") {
		if comma := strings.Index(value, ","); comma >= 0 {
			return strings.TrimSpace(value[comma+1:])
		}
	}
	return value
}

// wrapSDKError 将 SDK 错误包装为 GenerateError
func (p *OpenAIProvider) wrapSDKError(err error) error {
	e, ok := errors.AsType[*openai.Error](err)
	if ok {
		return p.wrapAPIError(e)
	}

	errMsg := err.Error()
	lowerMsg := strings.ToLower(errMsg)

	// HTML 响应说明 Endpoint 配置错误（SDK 不会为此返回 APIError）
	if strings.Contains(lowerMsg, "text/html") || strings.Contains(lowerMsg, "not 'application/json'") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "endpoint_protocol",
			Message:  "API 返回了 HTML 而不是 OpenAI Images API JSON 响应",
			HintMsg:  "请确认图片模型 Endpoint 支持 OpenAI Images API（/images/generations 或 /images/edits），不是聊天补全接口或需要网页登录的网关",
			Original: err,
		}
	}

	return &GenerateError{
		Provider: p.Name(),
		Code:     "network_error",
		Message:  fmt.Sprintf("网络请求失败: %v", err),
		HintMsg:  "请检查网络连接和 API 地址是否正确",
		Original: err,
	}
}

// wrapAPIError 将 SDK APIError 按 HTTP StatusCode 分类包装为 GenerateError
func (p *OpenAIProvider) wrapAPIError(err *openai.Error) error {
	original := error(err)

	switch err.StatusCode {
	case http.StatusUnauthorized:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "unauthorized",
			Message:  "API Key 无效或已过期",
			HintMsg:  "请检查配置文件中的 image.key 是否正确",
			Original: original,
		}
	case http.StatusTooManyRequests:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "rate_limit",
			Message:  "请求过于频繁，请稍后重试",
			HintMsg:  "OpenAI API 有速率限制，请等待一段时间后再试",
			Original: original,
		}
	case http.StatusBadRequest:
		if isContentSafetyError(err.Message) || isContentSafetyError(err.Code) || isContentSafetyError(err.Type) {
			return &GenerateError{
				Provider: p.Name(),
				Code:     "safety_blocked",
				Message:  "提示词被内容安全策略拦截",
				HintMsg:  "提示词可能包含敏感内容，请修改提示词后重试",
				Original: original,
			}
		}
		return &GenerateError{
			Provider: p.Name(),
			Code:     "bad_request",
			Message:  "请求参数错误",
			HintMsg:  "请检查图片尺寸、模型名称等参数是否正确",
			Original: original,
		}
	case http.StatusPaymentRequired, http.StatusForbidden:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "payment_required",
			Message:  "账户余额不足或访问受限",
			HintMsg:  "请检查 OpenAI 账户余额和 API 使用权限",
			Original: original,
		}
	default:
		if err.StatusCode >= 500 {
			return &GenerateError{
				Provider: p.Name(),
				Code:     "server_error",
				Message:  fmt.Sprintf("上游服务暂时不可用 (HTTP %d): %s", err.StatusCode, err.Message),
				HintMsg:  "服务端错误，请稍后重试",
				Original: original,
			}
		}
		return &GenerateError{
			Provider: p.Name(),
			Code:     "unknown",
			Message:  fmt.Sprintf("API 请求失败 (HTTP %d): %s", err.StatusCode, err.Message),
			HintMsg:  "请稍后重试，或检查 OpenAI 服务状态",
			Original: original,
		}
	}
}
