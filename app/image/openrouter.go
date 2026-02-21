package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/royalrick/wechatwriter/app/config"
)

// OpenRouterProvider OpenRouter 图片生成服务提供者
// OpenRouter 提供统一的 API 接口，支持多种图片生成模型（如 Gemini、Flux 等）
type OpenRouterProvider struct {
	apiKey      string
	baseURL     string
	model       string
	aspectRatio string
	imageSize   string
	client      *http.Client
}

// NewOpenRouterProvider 创建 OpenRouter Provider
func NewOpenRouterProvider(apiCfg *config.ImageAPI) (*OpenRouterProvider, error) {
	model := apiCfg.Model
	if model == "" {
		model = "google/gemini-3-pro-image-preview" // 默认模型
	}

	// 将 WIDTHxHEIGHT 格式映射到 OpenRouter 的 aspect_ratio 和 image_size
	aspectRatio, imageSize := mapSizeToOpenRouter(apiCfg.Size)

	baseURL := apiCfg.BaseURL
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}

	return &OpenRouterProvider{
		apiKey:      apiCfg.Key,
		baseURL:     baseURL,
		model:       model,
		aspectRatio: aspectRatio,
		imageSize:   imageSize,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}, nil
}

// Name 返回提供者名称
func (p *OpenRouterProvider) Name() string {
	return "OpenRouter"
}

// Generate 生成图片
// OpenRouter 返回 base64 编码的图片，此方法将其保存为临时文件并返回文件路径
func (p *OpenRouterProvider) Generate(ctx context.Context, prompt string) (*GenerateResult, error) {
	reqBody := p.buildRequest(prompt)

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "marshal_error",
			Message:  "请求构造失败",
			Original: err,
		}
	}

	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "request_error",
			Message:  "创建请求失败",
			Original: err,
		}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/royalrick/wechatwriter")
	req.Header.Set("X-Title", "wechatwriter")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "network_error",
			Message:  "网络请求失败，请检查网络连接",
			HintMsg:  "确认网络连接正常，API 地址正确",
			Original: err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.handleErrorResponse(resp)
	}

	filePath, err := p.parseResponseAndSave(resp.Body)
	if err != nil {
		return nil, err
	}

	return &GenerateResult{
		URL:   filePath,
		Model: p.model,
		Size:  p.aspectRatio,
	}, nil
}

// buildRequest 构建 OpenRouter 请求体（Chat Completions 格式）
func (p *OpenRouterProvider) buildRequest(prompt string) map[string]any {
	req := map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"modalities": []string{"image"},
	}

	imageConfig := map[string]string{}
	if p.aspectRatio != "" {
		imageConfig["aspect_ratio"] = p.aspectRatio
	}
	if p.imageSize != "" {
		imageConfig["image_size"] = p.imageSize
	}
	if len(imageConfig) > 0 {
		req["image_config"] = imageConfig
	}

	return req
}

// openRouterResponse OpenRouter API 响应结构
type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content,omitempty"`
			Images  []struct {
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"images,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// parseResponseAndSave 解析响应并将 base64 图片保存到临时文件
func (p *OpenRouterProvider) parseResponseAndSave(body io.Reader) (string, error) {
	var result openRouterResponse
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return "", &GenerateError{
			Provider: p.Name(),
			Code:     "decode_error",
			Message:  "响应解析失败",
			Original: err,
		}
	}

	if len(result.Choices) == 0 || len(result.Choices[0].Message.Images) == 0 {
		return "", &GenerateError{
			Provider: p.Name(),
			Code:     "no_image",
			Message:  "未生成图片",
			HintMsg:  "提示词可能不符合内容政策，请尝试修改提示词",
		}
	}

	dataURL := result.Choices[0].Message.Images[0].ImageURL.URL

	imageData, ext, err := parseDataURL(dataURL)
	if err != nil {
		return "", &GenerateError{
			Provider: p.Name(),
			Code:     "parse_error",
			Message:  "图片数据解析失败",
			Original: err,
		}
	}

	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("wechatwriter_openrouter_%d%s", time.Now().UnixNano(), ext))
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

// handleErrorResponse 处理错误响应
func (p *OpenRouterProvider) handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var errResp openRouterResponse
	_ = json.Unmarshal(body, &errResp)

	errMsg := ""
	if errResp.Error != nil {
		errMsg = errResp.Error.Message
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "unauthorized",
			Message:  "OpenRouter API Key 无效或已过期",
			HintMsg:  "请检查配置中的 image.api.key 是否正确，或前往 openrouter.ai 获取新的 API Key",
			Original: fmt.Errorf("status 401: %s", string(body)),
		}
	case http.StatusTooManyRequests:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "rate_limit",
			Message:  "请求过于频繁，请稍后重试",
			HintMsg:  "OpenRouter API 有速率限制，请等待一段时间后再试",
			Original: fmt.Errorf("status 429: %s", string(body)),
		}
	case http.StatusBadRequest:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "bad_request",
			Message:  fmt.Sprintf("请求参数错误: %s", errMsg),
			HintMsg:  "请检查模型名称、aspect_ratio 等参数是否正确",
			Original: fmt.Errorf("status 400: %s", string(body)),
		}
	case http.StatusPaymentRequired, http.StatusForbidden:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "payment_required",
			Message:  "OpenRouter 账户余额不足或访问受限",
			HintMsg:  "请前往 openrouter.ai 检查账户余额和 API 使用权限",
			Original: fmt.Errorf("status %d: %s", resp.StatusCode, string(body)),
		}
	default:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "unknown",
			Message:  fmt.Sprintf("OpenRouter API 返回错误 (HTTP %d)", resp.StatusCode),
			HintMsg:  "请稍后重试，或访问 openrouter.ai 查看服务状态",
			Original: fmt.Errorf("status %d: %s", resp.StatusCode, string(body)),
		}
	}
}

// mapSizeToOpenRouter 将 WIDTHxHEIGHT 格式映射到 OpenRouter 的 aspect_ratio 和 image_size
func mapSizeToOpenRouter(size string) (aspectRatio, imageSize string) {
	if size == "" {
		return "1:1", "2K"
	}

	sizeMap := map[string]struct{ ratio, size string }{
		"1024x1024": {"1:1", "1K"}, "2048x2048": {"1:1", "2K"}, "4096x4096": {"1:1", "4K"},
		"1344x768": {"16:9", "1K"}, "1920x1080": {"16:9", "2K"}, "3840x2160": {"16:9", "4K"},
		"768x1344": {"9:16", "1K"}, "1080x1920": {"9:16", "2K"}, "2160x3840": {"9:16", "4K"},
		"1184x864": {"4:3", "1K"}, "1600x1200": {"4:3", "2K"}, "2048x1536": {"4:3", "2K"},
		"864x1184": {"3:4", "1K"}, "1200x1600": {"3:4", "2K"}, "1536x2048": {"3:4", "2K"},
		"1248x832": {"3:2", "1K"}, "1800x1200": {"3:2", "2K"}, "3072x2048": {"3:2", "4K"},
		"832x1248": {"2:3", "1K"}, "1200x1800": {"2:3", "2K"}, "2048x3072": {"2:3", "4K"},
		"1152x896": {"5:4", "1K"},
		"896x1152": {"4:5", "1K"},
		"1536x672": {"21:9", "1K"},
		// 用户分辨率表：2K 档位
		"2304x1728": {"4:3", "2K"}, "1728x2304": {"3:4", "2K"},
		"2560x1440": {"16:9", "2K"}, "1440x2560": {"9:16", "2K"},
		"2496x1664": {"3:2", "2K"}, "1664x2496": {"2:3", "2K"},
		"3024x1296": {"21:9", "2K"},
		// 用户分辨率表：4K 档位
		"4704x3520": {"4:3", "4K"}, "3520x4704": {"3:4", "4K"},
		"5504x3040": {"16:9", "4K"}, "3040x5504": {"9:16", "4K"},
		"4992x3328": {"3:2", "4K"}, "3328x4992": {"2:3", "4K"},
		"6240x2656": {"21:9", "4K"},
	}

	if mapped, ok := sizeMap[size]; ok {
		return mapped.ratio, mapped.size
	}

	validRatios := map[string]bool{
		"1:1": true, "2:3": true, "3:2": true, "3:4": true, "4:3": true,
		"4:5": true, "5:4": true, "9:16": true, "16:9": true, "21:9": true,
	}
	if validRatios[size] {
		return size, "2K"
	}

	return "1:1", "2K"
}
