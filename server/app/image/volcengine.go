package image

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/app/config"
	"github.com/rs/zerolog"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// VolcengineProvider 火山方舟 Seedream 图片生成服务提供者
// 使用官方 arkruntime SDK，支持类型安全的请求体和高级选项
type VolcengineProvider struct {
	client     *arkruntime.Client
	model      string
	log        *zerolog.Logger
	sizePixel  string // 像素格式 "WIDTHxHEIGHT"，如 "1728x2304"
	volcConfig *config.VolcengineConfig
}

// volcenginePixelMap 将 aspect_ratio:sizeTier 映射到像素格式
var volcenginePixelMap = map[string]string{
	"1:1:2K": "2048x2048",
	"4:3:2K": "2304x1728", "3:4:2K": "1728x2304",
	"16:9:2K": "2560x1440", "9:16:2K": "1440x2560",
	"3:2:2K": "2496x1664", "2:3:2K": "1664x2496",
	"21:9:2K": "3024x1296",
	"1:1:4K":  "4096x4096",
	"4:3:4K":  "4704x3520", "3:4:4K": "3520x4704",
	"16:9:4K": "5504x3040", "9:16:4K": "3040x5504",
	"3:2:4K": "4992x3328", "2:3:4K": "3328x4992",
	"21:9:4K": "6240x2656",
}

// volcenginePixelSize 将 aspect_ratio + sizeTier 转换为像素格式
func volcenginePixelSize(aspectRatio, sizeTier string) string {
	key := aspectRatio + ":" + sizeTier
	if px, ok := volcenginePixelMap[key]; ok {
		return px
	}
	return "2048x2048" // fallback
}

// NewVolcengineProvider 创建火山方舟 Seedream Provider
func NewVolcengineProvider(apiCfg *config.ImageAPI, log *zerolog.Logger) (*VolcengineProvider, error) {
	mdl := apiCfg.Model
	if mdl == "" {
		mdl = DefaultVolcengineModel
	}

	timeout := 300 * time.Second
	if apiCfg.TimeoutSec > 0 {
		timeout = time.Duration(apiCfg.TimeoutSec) * time.Second
	}
	opts := []arkruntime.ConfigOption{
		arkruntime.WithTimeout(timeout),
	}
	if apiCfg.BaseURL != "" {
		opts = append(opts, arkruntime.WithBaseUrl(apiCfg.BaseURL))
	}
	client := arkruntime.NewClientWithApiKey(apiCfg.Key, opts...)

	aspectRatio, sizeTier := parseVolcengineSize(apiCfg.Size)
	sizePixel := volcenginePixelSize(aspectRatio, sizeTier)

	return &VolcengineProvider{
		client:     client,
		log:        log,
		model:      mdl,
		sizePixel:  sizePixel,
		volcConfig: apiCfg.Volcengine,
	}, nil
}

// Name 返回提供者名称
func (p *VolcengineProvider) Name() string {
	return "Volcengine"
}

// Capabilities returns Volcengine provider capabilities
func (p *VolcengineProvider) Capabilities() *ProviderCapabilities {
	return &ProviderCapabilities{
		MaxRefImages:  10,
		Batch:         false,
		MaxBatch:      1,
		Streaming:     false,
		Inpainting:    false,
		QualityLevels: []string{},
		OutputFormats: []string{"png", "jpeg"},
		FlexibleSize:  false,
	}
}

// Generate 生成图片
func (p *VolcengineProvider) Generate(ctx context.Context, prompt string, opts *GenerateOptions) (*GenerateResult, error) {
	start := time.Now()

	wm := false
	if opts != nil && opts.Watermark != nil {
		wm = *opts.Watermark
	}

	refCount := 0
	if opts != nil {
		refCount = len(opts.RefImagePaths)
		if opts.RefImagePath != "" {
			refCount++
		}
	}

	outputFmt := ""
	if opts != nil {
		outputFmt = opts.OutputFormat
	}
	vcOutputFmt := ""
	if p.volcConfig != nil && p.volcConfig.OutputFormat != "" {
		vcOutputFmt = p.volcConfig.OutputFormat
	}

	p.log.Debug().
		Str("prompt_preview", truncateRunes(prompt, 80)).
		Str("model", p.model).
		Str("size_pixel", p.sizePixel).
		Str("output_format", outputFmt).
		Str("volcengine_output_format", vcOutputFmt).
		Bool("watermark", wm).
		Int("ref_count", refCount).
		Msg("volcengine: generating image")

	respFmt := model.GenerateImagesResponseFormatURL
	requestSize := volcengineRequestSize(p.sizePixel, opts != nil && opts.SemanticAspectRatio)
	req := model.GenerateImagesRequest{
		Watermark:      &wm,
		Model:          p.model,
		Prompt:         prompt,
		Size:           requestSize,
		ResponseFormat: &respFmt,
	}

	// 应用高级选项（从 config.VolcengineConfig）
	if vc := p.volcConfig; vc != nil {
		if vc.Seed != nil {
			req.Seed = vc.Seed
		}
		if vc.GuidanceScale != nil {
			req.GuidanceScale = vc.GuidanceScale
		}
		if vc.OptimizePrompt != nil {
			req.OptimizePrompt = vc.OptimizePrompt
		}
		if vc.OutputFormat != "" {
			format := model.OutputFormat(vc.OutputFormat)
			req.OutputFormat = &format
		}
	}

	if opts != nil {
		imageInput, err := p.buildReferenceImageInput(opts)
		if err != nil {
			return nil, err
		}
		req.Image = imageInput
	}

	resp, err := p.client.GenerateImages(ctx, req)
	if err != nil {
		p.log.Error().Err(err).Str("model", p.model).Dur("elapsed", time.Since(start)).Msg("volcengine: image generation failed")
		return nil, p.convertSDKError(err)
	}

	if len(resp.Data) == 0 || resp.Data[0].Url == nil || *resp.Data[0].Url == "" {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "no_image",
			Message:  "未生成图片",
			HintMsg:  "提示词可能不符合内容政策，请尝试修改提示词",
		}
	}

	// Return ratio (not pixel size) for consistent GenerateResult.Size format
	ratio, _ := ParseSize(p.sizePixel)

	p.log.Info().
		Str("model", p.model).
		Dur("elapsed", time.Since(start)).
		Msg("volcengine: image generation completed")

	return &GenerateResult{
		URL:             *resp.Data[0].Url,
		Model:           p.model,
		Size:            ratio,
		ResponseType:    "url",
		ResponsePreview: *resp.Data[0].Url,
	}, nil
}

func volcengineRequestSize(defaultSize string, semanticAspectRatio bool) *string {
	if semanticAspectRatio {
		return nil
	}
	return new(defaultSize)
}

func (p *VolcengineProvider) buildReferenceImageInput(opts *GenerateOptions) (any, error) {
	paths := make([]string, 0, len(opts.RefImagePaths)+1)
	if opts.RefImagePath != "" {
		paths = append(paths, opts.RefImagePath)
	}
	paths = append(paths, opts.RefImagePaths...)
	if len(paths) == 0 {
		return nil, nil
	}

	dataURIs := make([]string, 0, len(paths))
	for _, path := range paths {
		data, mimeType, err := ReadRefImage(path)
		if err != nil {
			return nil, &GenerateError{
				Provider: p.Name(),
				Code:     "refer_error",
				Message:  "读取参考图失败",
				Original: err,
			}
		}
		dataURIs = append(dataURIs, "data:"+mimeType+";base64,"+base64.StdEncoding.EncodeToString(data))
	}
	if len(dataURIs) == 1 {
		return dataURIs[0], nil
	}
	return dataURIs, nil
}

// convertSDKError 将 SDK 错误转换为 GenerateError
func (p *VolcengineProvider) convertSDKError(err error) error {
	var apiErr *model.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatusCode {
		case 401:
			return &GenerateError{
				Provider: p.Name(),
				Code:     "unauthorized",
				Message:  "API Key 无效或已过期",
				HintMsg:  "请检查配置中的 image.key 是否正确，或前往火山引擎控制台获取新的 API Key",
				Original: err,
			}
		case 429:
			return &GenerateError{
				Provider: p.Name(),
				Code:     "rate_limit",
				Message:  "请求过于频繁，请稍后重试",
				Original: err,
			}
		case 400:
			if isContentSafetyError(apiErr.Message) {
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
				Message:  fmt.Sprintf("请求参数错误: %s", apiErr.Message),
				HintMsg:  "请检查 size 等参数是否正确",
				Original: err,
			}
		case 402, 403:
			return &GenerateError{
				Provider: p.Name(),
				Code:     "payment_required",
				Message:  "账户余额不足或访问受限",
				HintMsg:  "请前往火山引擎控制台检查账户余额",
				Original: err,
			}
		default:
			if apiErr.HTTPStatusCode >= 500 {
				return &GenerateError{
					Provider: p.Name(),
					Code:     "server_error",
					Message:  fmt.Sprintf("上游服务暂时不可用 (HTTP %d): %s", apiErr.HTTPStatusCode, apiErr.Message),
					HintMsg:  "服务端错误，请稍后重试",
					Original: err,
				}
			}
			return &GenerateError{
				Provider: p.Name(),
				Code:     "unknown",
				Message:  fmt.Sprintf("API 返回错误 (HTTP %d): %s", apiErr.HTTPStatusCode, apiErr.Message),
				Original: err,
			}
		}
	}

	var reqErr *model.RequestError
	if errors.As(err, &reqErr) {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "network_error",
			Message:  "网络请求失败，请检查网络连接",
			HintMsg:  "确认网络连接正常，API 地址正确",
			Original: err,
		}
	}

	return &GenerateError{
		Provider: p.Name(),
		Code:     "unknown",
		Message:  err.Error(),
		Original: err,
	}
}

// parseVolcengineSize 解析 size 配置字段，返回 aspectRatio 和 sizeTier
// 支持格式：
//   - "3:4"      → aspect_ratio=3:4, sizeTier=2K
//   - "3:4:1K"   → aspect_ratio=3:4, sizeTier=1K
//   - "3:4:4K"   → aspect_ratio=3:4, sizeTier=4K
func parseVolcengineSize(size string) (aspectRatio, sizeTier string) {
	return ParseSize(size)
}
