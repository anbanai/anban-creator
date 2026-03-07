package image

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/royalrick/wechatwriter/app/config"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// VolcengineProvider 火山方舟 Seedream 图片生成服务提供者
// 使用官方 arkruntime SDK，支持类型安全的请求体和高级选项
type VolcengineProvider struct {
	client     *arkruntime.Client
	model      string
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
func NewVolcengineProvider(apiCfg *config.ImageAPI) (*VolcengineProvider, error) {
	mdl := apiCfg.Model
	if mdl == "" {
		mdl = DefaultVolcengineModel
	}

	opts := []arkruntime.ConfigOption{
		arkruntime.WithTimeout(120 * time.Second),
	}
	if apiCfg.BaseURL != "" {
		opts = append(opts, arkruntime.WithBaseUrl(apiCfg.BaseURL))
	}
	client := arkruntime.NewClientWithApiKey(apiCfg.Key, opts...)

	aspectRatio, sizeTier := parseVolcengineSize(apiCfg.Size)
	sizePixel := volcenginePixelSize(aspectRatio, sizeTier)

	return &VolcengineProvider{
		client:     client,
		model:      mdl,
		sizePixel:  sizePixel,
		volcConfig: apiCfg.Volcengine,
	}, nil
}

// Name 返回提供者名称
func (p *VolcengineProvider) Name() string {
	return "Volcengine"
}

// Generate 生成图片
func (p *VolcengineProvider) Generate(ctx context.Context, prompt string, opts *GenerateOptions) (*GenerateResult, error) {
	respFmt := model.GenerateImagesResponseFormatURL
	req := model.GenerateImagesRequest{
		Watermark:      new(false),
		Model:          p.model,
		Prompt:         prompt,
		Size:           new(p.sizePixel),
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

	// 有参考图时，添加 image 字段（data URI）
	if opts != nil && opts.RefImagePath != "" {
		data, mimeType, err := ReadRefImage(opts.RefImagePath)
		if err != nil {
			return nil, &GenerateError{
				Provider: p.Name(),
				Code:     "refer_error",
				Message:  "读取参考图失败",
				Original: err,
			}
		}
		dataURI := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
		req.Image = dataURI
	}

	resp, err := p.client.GenerateImages(ctx, req)
	if err != nil {
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
	return &GenerateResult{
		URL:   *resp.Data[0].Url,
		Model: p.model,
		Size:  ratio,
	}, nil
}

// GenerateBatch 一次 API 调用生成多张风格一致的图片（实现 BatchProvider 接口）
func (p *VolcengineProvider) GenerateBatch(ctx context.Context, prompt string, opts *GenerateOptions) (*GenerateBatchResult, error) {
	respFmt := model.GenerateImagesResponseFormatURL
	seqGen := model.SequentialImageGeneration(model.SequentialImageGenerationAuto)

	req := model.GenerateImagesRequest{
		Watermark:                 new(false),
		Model:                     p.model,
		Prompt:                    prompt,
		Size:                      new(p.sizePixel),
		ResponseFormat:            &respFmt,
		SequentialImageGeneration: &seqGen,
	}

	// 设置组图数量
	if opts != nil && opts.MaxImages > 0 {
		maxImages := opts.MaxImages
		req.SequentialImageGenerationOptions = &model.SequentialImageGenerationOptions{
			MaxImages: &maxImages,
		}
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

	// 参考图：多张时使用数组，单张时使用字符串
	if opts != nil {
		if len(opts.RefImagePaths) > 0 {
			var dataURIs []string
			for _, path := range opts.RefImagePaths {
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
			req.Image = dataURIs
		} else if opts.RefImagePath != "" {
			data, mimeType, err := ReadRefImage(opts.RefImagePath)
			if err != nil {
				return nil, &GenerateError{
					Provider: p.Name(),
					Code:     "refer_error",
					Message:  "读取参考图失败",
					Original: err,
				}
			}
			req.Image = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
		}
	}

	resp, err := p.client.GenerateImages(ctx, req)
	if err != nil {
		return nil, p.convertSDKError(err)
	}

	if len(resp.Data) == 0 {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "no_image",
			Message:  "未生成图片",
			HintMsg:  "提示词可能不符合内容政策，请尝试修改提示词",
		}
	}

	ratio, _ := ParseSize(p.sizePixel)
	var results []*GenerateResult
	for _, img := range resp.Data {
		if img.Url == nil || *img.Url == "" {
			continue
		}
		results = append(results, &GenerateResult{
			URL:   *img.Url,
			Model: p.model,
			Size:  ratio,
		})
	}

	if len(results) == 0 {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "no_image",
			Message:  "未生成图片",
			HintMsg:  "提示词可能不符合内容政策，请尝试修改提示词",
		}
	}

	return &GenerateBatchResult{Images: results}, nil
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
				HintMsg:  "请检查配置中的 article.image.key 或 post.image.key 是否正确，或前往火山引擎控制台获取新的 API Key",
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
			return &GenerateError{
				Provider: p.Name(),
				Code:     "api_error",
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
