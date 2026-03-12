package image

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	openrouter "github.com/revrost/go-openrouter"
	"github.com/royalrick/anbanwriter/app/config"
)

// OpenRouterProvider OpenRouter 图片生成服务提供者
// OpenRouter 提供统一的 API 接口，支持多种图片生成模型（如 Gemini、Flux 等）
type OpenRouterProvider struct {
	client      *openrouter.Client
	model       string
	aspectRatio openrouter.ChatCompletionAspectRatio
	imageSize   openrouter.ChatCompletionImageSize
}

// NewOpenRouterProvider 创建 OpenRouter Provider
func NewOpenRouterProvider(apiCfg *config.ImageAPI) (*OpenRouterProvider, error) {
	model := apiCfg.Model
	if model == "" {
		model = DefaultOpenRouterModel
	}

	// 将 WIDTHxHEIGHT 格式映射到 OpenRouter 的 aspect_ratio 和 image_size
	aspectRatio, imageSize := mapSizeToOpenRouter(apiCfg.Size)

	cfg := openrouter.DefaultConfig(apiCfg.Key)
	cfg.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	cfg.XTitle = "anbanwriter"
	cfg.HttpReferer = "https://github.com/royalrick/anbanwriter"
	if apiCfg.BaseURL != "" {
		cfg.BaseURL = apiCfg.BaseURL
	}

	return &OpenRouterProvider{
		client:      openrouter.NewClientWithConfig(*cfg),
		model:       model,
		aspectRatio: aspectRatio,
		imageSize:   imageSize,
	}, nil
}

// Name 返回提供者名称
func (p *OpenRouterProvider) Name() string {
	return "OpenRouter"
}

// Generate 生成图片
// OpenRouter 返回 base64 编码的图片，此方法将其保存为临时文件并返回文件路径
func (p *OpenRouterProvider) Generate(ctx context.Context, prompt string, opts *GenerateOptions) (*GenerateResult, error) {
	req := p.buildRequest(prompt, opts)

	resp, err := p.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, p.wrapSDKError(err)
	}

	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Images) == 0 {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "no_image",
			Message:  "未生成图片",
			HintMsg:  "提示词可能不符合内容政策，请尝试修改提示词",
		}
	}

	dataURL := resp.Choices[0].Message.Images[0].ImageURL.URL

	imageData, ext, err := parseDataURL(dataURL)
	if err != nil {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "parse_error",
			Message:  "图片数据解析失败",
			Original: err,
		}
	}

	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("anbanwriter_openrouter_%d%s", time.Now().UnixNano(), ext))
	if err := os.WriteFile(tmpPath, imageData, 0644); err != nil {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "write_error",
			Message:  "图片保存失败",
			Original: err,
		}
	}

	return &GenerateResult{
		URL:   tmpPath,
		Model: p.model,
		Size:  string(p.aspectRatio),
	}, nil
}

// buildRequest 构建 OpenRouter 请求体（Chat Completions 格式）
func (p *OpenRouterProvider) buildRequest(prompt string, opts *GenerateOptions) openrouter.ChatCompletionRequest {
	var messages []openrouter.ChatCompletionMessage

	if opts != nil && opts.RefImagePath != "" {
		// 有参考图时，使用多模态消息格式
		data, mimeType, err := ReadRefImage(opts.RefImagePath)
		if err == nil {
			dataURI := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
			messages = []openrouter.ChatCompletionMessage{openrouter.UserMessageWithImage(prompt, dataURI)}
		} else {
			// 读取参考图失败，降级为纯文本
			messages = []openrouter.ChatCompletionMessage{openrouter.UserMessage(prompt)}
		}
	} else {
		messages = []openrouter.ChatCompletionMessage{openrouter.UserMessage(prompt)}
	}

	req := openrouter.ChatCompletionRequest{
		Model:      p.model,
		Messages:   messages,
		Modalities: []openrouter.ChatCompletionModality{openrouter.ModalityImage},
	}

	if p.aspectRatio != "" || p.imageSize != "" {
		req.ImageConfig = &openrouter.ChatCompletionImageConfig{
			AspectRatio: p.aspectRatio,
			ImageSize:   p.imageSize,
		}
	}

	return req
}

// wrapSDKError 将 SDK 错误转换为 GenerateError
func (p *OpenRouterProvider) wrapSDKError(err error) error {
	var apiErr *openrouter.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatusCode {
		case http.StatusUnauthorized:
			return &GenerateError{
				Provider: p.Name(),
				Code:     "unauthorized",
				Message:  "OpenRouter API Key 无效或已过期",
				HintMsg:  "请检查配置中的 article.image.key 或 xls.image.key 是否正确，或前往 openrouter.ai 获取新的 API Key",
				Original: err,
			}
		case http.StatusTooManyRequests:
			return &GenerateError{
				Provider: p.Name(),
				Code:     "rate_limit",
				Message:  "请求过于频繁，请稍后重试",
				HintMsg:  "OpenRouter API 有速率限制，请等待一段时间后再试",
				Original: err,
			}
		case http.StatusBadRequest:
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
				HintMsg:  "请检查模型名称、aspect_ratio 等参数是否正确",
				Original: err,
			}
		case http.StatusPaymentRequired, http.StatusForbidden:
			return &GenerateError{
				Provider: p.Name(),
				Code:     "payment_required",
				Message:  "OpenRouter 账户余额不足或访问受限",
				HintMsg:  "请前往 openrouter.ai 检查账户余额和 API 使用权限",
				Original: err,
			}
		default:
			return &GenerateError{
				Provider: p.Name(),
				Code:     "unknown",
				Message:  fmt.Sprintf("OpenRouter API 返回错误 (HTTP %d)", apiErr.HTTPStatusCode),
				HintMsg:  "请稍后重试，或访问 openrouter.ai 查看服务状态",
				Original: err,
			}
		}
	}

	var reqErr *openrouter.RequestError
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
		Message:  "未知错误",
		Original: err,
	}
}

// parseDataURL 解析 data URL 并返回解码后的字节和文件扩展名
// 格式: data:image/png;base64,iVBORw0KGgo...
func parseDataURL(dataURL string) ([]byte, string, error) {
	if !strings.HasPrefix(dataURL, "data:") {
		return nil, "", fmt.Errorf("invalid data URL format: missing 'data:' prefix")
	}

	commaIdx := strings.Index(dataURL, ",")
	if commaIdx == -1 {
		return nil, "", fmt.Errorf("invalid data URL: no comma separator found")
	}

	metadata := dataURL[5:commaIdx]
	base64Data := dataURL[commaIdx+1:]

	ext := ".png"
	if strings.Contains(metadata, "image/jpeg") || strings.Contains(metadata, "image/jpg") {
		ext = ".jpg"
	} else if strings.Contains(metadata, "image/gif") {
		ext = ".gif"
	} else if strings.Contains(metadata, "image/webp") {
		ext = ".webp"
	}

	imageData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil, "", fmt.Errorf("base64 decode failed: %w", err)
	}

	return imageData, ext, nil
}

// mapSizeToOpenRouter 将尺寸配置映射到 OpenRouter 的 aspect_ratio 和 image_size
func mapSizeToOpenRouter(size string) (openrouter.ChatCompletionAspectRatio, openrouter.ChatCompletionImageSize) {
	ratio, tier := ParseSize(size)

	ratioMap := map[string]openrouter.ChatCompletionAspectRatio{
		"1:1":  openrouter.AspectRatio1x1,
		"16:9": openrouter.AspectRatio16x9,
		"9:16": openrouter.AspectRatio9x16,
		"4:3":  openrouter.AspectRatio4x3,
		"3:4":  openrouter.AspectRatio3x4,
		"3:2":  openrouter.AspectRatio3x2,
		"2:3":  openrouter.AspectRatio2x3,
		"4:5":  openrouter.AspectRatio4x5,
		"5:4":  openrouter.AspectRatio5x4,
		"21:9": openrouter.AspectRatio21x9,
	}
	tierMap := map[string]openrouter.ChatCompletionImageSize{
		"1K": openrouter.ImageSize1K,
		"2K": openrouter.ImageSize2K,
		"4K": openrouter.ImageSize4K,
	}

	ar := ratioMap[ratio]
	is := tierMap[tier]
	return ar, is
}
