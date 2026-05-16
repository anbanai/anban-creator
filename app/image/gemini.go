package image

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/royalrick/anbanwriter/app/config"
	"google.golang.org/genai"
)

// GeminiProvider Google Gemini 图片生成服务提供者
// 直接调用 Google Gemini API，使用官方 Go SDK
type GeminiProvider struct {
	apiKey      string
	model       string
	aspectRatio string
	client      *genai.Client
}

// NewGeminiProvider 创建 Gemini Provider
func NewGeminiProvider(apiCfg *config.ImageAPI) (*GeminiProvider, error) {
	model := apiCfg.Model
	if model == "" {
		model = DefaultGeminiModel
	}

	// 处理宽高比配置
	aspectRatio := mapSizeToGeminiAspectRatio(apiCfg.Size)

	timeout := 300 * time.Second
	if apiCfg.TimeoutSec > 0 {
		timeout = time.Duration(apiCfg.TimeoutSec) * time.Second
	}

	// 创建 Gemini 客户端
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiCfg.Key,
		Backend: genai.BackendGeminiAPI,
		HTTPClient: &http.Client{Timeout: timeout},
	})
	if err != nil {
		return nil, &GenerateError{
			Provider: "Gemini",
			Code:     "client_error",
			Message:  "创建 Gemini 客户端失败",
			HintMsg:  "请检查 API Key 是否正确",
			Original: err,
		}
	}

	return &GeminiProvider{
		apiKey:      apiCfg.Key,
		model:       model,
		aspectRatio: aspectRatio,
		client:      client,
	}, nil
}

// Name 返回提供者名称
func (p *GeminiProvider) Name() string {
	return "Gemini"
}

// Generate 生成图片
func (p *GeminiProvider) Generate(ctx context.Context, prompt string, opts *GenerateOptions) (*GenerateResult, error) {
	// 构建请求内容
	parts := []*genai.Part{
		genai.NewPartFromText(prompt),
	}

	// 如果有参考图，追加内联数据
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
		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{
				MIMEType: mimeType,
				Data:     data,
			},
		})
	}

	contents := []*genai.Content{
		{
			Parts: parts,
			Role:  "user",
		},
	}

	// 配置生成参数
	genCfg := &genai.GenerateContentConfig{
		ResponseModalities: []string{"TEXT", "IMAGE"},
		ImageConfig: &genai.ImageConfig{
			AspectRatio: p.aspectRatio,
		},
	}

	// 调用 Gemini API
	resp, err := p.client.Models.GenerateContent(ctx, p.model, contents, genCfg)
	if err != nil {
		return nil, p.handleError(err)
	}

	// 解析响应，提取图片
	filePath, err := p.extractAndSaveImage(resp)
	if err != nil {
		return nil, err
	}

	return &GenerateResult{
		URL:   filePath, // 返回本地文件路径
		Model: p.model,
		Size:  p.aspectRatio,
	}, nil
}

// extractAndSaveImage 从响应中提取图片并保存到临时文件
func (p *GeminiProvider) extractAndSaveImage(resp *genai.GenerateContentResponse) (string, error) {
	if resp == nil || len(resp.Candidates) == 0 {
		return "", &GenerateError{
			Provider: p.Name(),
			Code:     "no_response",
			Message:  "未收到响应",
			HintMsg:  "请稍后重试",
		}
	}

	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return "", &GenerateError{
			Provider: p.Name(),
			Code:     "no_content",
			Message:  "响应中没有内容",
			HintMsg:  "提示词可能不符合内容政策，请尝试修改提示词",
		}
	}

	// 遍历响应部分，查找图片
	for _, part := range candidate.Content.Parts {
		// 检查是否是内联数据（图片）
		if part.InlineData != nil {
			return p.saveInlineData(part.InlineData)
		}
	}

	return "", &GenerateError{
		Provider: p.Name(),
		Code:     "no_image",
		Message:  "响应中没有图片",
		HintMsg:  "模型可能只返回了文本，请确保使用支持图片生成的模型",
	}
}

// saveInlineData 保存内联数据到临时文件
func (p *GeminiProvider) saveInlineData(data *genai.Blob) (string, error) {
	if data == nil || len(data.Data) == 0 {
		return "", &GenerateError{
			Provider: p.Name(),
			Code:     "empty_data",
			Message:  "图片数据为空",
		}
	}

	// 确定文件扩展名
	ext := ".png" // 默认 PNG
	mimeType := data.MIMEType
	if strings.Contains(mimeType, "jpeg") || strings.Contains(mimeType, "jpg") {
		ext = ".jpg"
	} else if strings.Contains(mimeType, "gif") {
		ext = ".gif"
	} else if strings.Contains(mimeType, "webp") {
		ext = ".webp"
	}

	// 保存到临时文件
	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("anbanwriter_gemini_%d%s", time.Now().UnixNano(), ext))

	if err := os.WriteFile(tmpPath, data.Data, 0644); err != nil {
		return "", &GenerateError{
			Provider: p.Name(),
			Code:     "write_error",
			Message:  "图片保存失败",
			Original: err,
		}
	}

	return tmpPath, nil
}

// handleError 处理 Gemini API 错误
func (p *GeminiProvider) handleError(err error) error {
	errStr := err.Error()

	if strings.Contains(errStr, "PERMISSION_DENIED") || strings.Contains(errStr, "401") || strings.Contains(errStr, "403") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "unauthorized",
			Message:  "Google API Key 无效或权限不足",
			HintMsg:  "请检查配置文件中的 image.key 是否正确，前往 https://aistudio.google.com/apikey 获取",
			Original: err,
		}
	}

	if strings.Contains(errStr, "RESOURCE_EXHAUSTED") || strings.Contains(errStr, "429") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "rate_limit",
			Message:  "请求过于频繁或配额已用尽",
			HintMsg:  "请等待一段时间后再试，或检查 Google AI Studio 中的配额使用情况",
			Original: err,
		}
	}

	if strings.Contains(errStr, "INVALID_ARGUMENT") || strings.Contains(errStr, "400") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "bad_request",
			Message:  "请求参数错误",
			HintMsg:  "请检查模型名称是否正确。支持的模型: gemini-3-pro-image-preview, gemini-2.5-flash-preview-image",
			Original: err,
		}
	}

	if strings.Contains(errStr, "NOT_FOUND") || strings.Contains(errStr, "404") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "not_found",
			Message:  "模型不存在",
			HintMsg:  "请检查模型名称是否正确: gemini-3-pro-image-preview",
			Original: err,
		}
	}

	if strings.Contains(errStr, "SAFETY") || strings.Contains(errStr, "blocked") {
		return &GenerateError{
			Provider: p.Name(),
			Code:     "safety_blocked",
			Message:  "内容被安全过滤器阻止",
			HintMsg:  "提示词可能包含敏感内容，请修改提示词后重试",
			Original: err,
		}
	}

	return &GenerateError{
		Provider: p.Name(),
		Code:     "unknown",
		Message:  fmt.Sprintf("Gemini API 错误: %s", errStr),
		HintMsg:  "请稍后重试，或查看 https://ai.google.dev/gemini-api/docs 了解更多信息",
		Original: err,
	}
}

// mapSizeToGeminiAspectRatio 将尺寸配置映射到 Gemini 支持的宽高比
func mapSizeToGeminiAspectRatio(size string) string {
	ratio, _ := ParseSize(size)
	return ratio
}
