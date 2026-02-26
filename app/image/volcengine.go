package image

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/royalrick/wechatwriter/app/config"
)

// VolcengineProvider 火山方舟 Seedream 图片生成服务提供者
// 使用 OpenAI 兼容接口，但参数格式不同：
//   - size: 分辨率档位 "1K" | "2K" | "4K"（默认 2K）
//   - aspect_ratio: 宽高比 "1:1" | "16:9" | "9:16" | "3:4" | "4:3" | "2:3" | "3:2" | "21:9"
type VolcengineProvider struct {
	apiKey      string
	baseURL     string
	model       string
	aspectRatio string // 来自 ImageAPI.Size（如 "3:4", "16:9"）
	sizeTier    string // 分辨率档位（"1K" / "2K" / "4K"），默认 "2K"
	client      *http.Client
}

// volcengineSupportedRatios 火山方舟 Seedream 支持的宽高比
var volcengineSupportedRatios = map[string]bool{
	"1:1":  true,
	"16:9": true,
	"9:16": true,
	"3:4":  true,
	"4:3":  true,
	"2:3":  true,
	"3:2":  true,
	"21:9": true,
}

// NewVolcengineProvider 创建火山方舟 Seedream Provider
func NewVolcengineProvider(apiCfg *config.ImageAPI) (*VolcengineProvider, error) {
	model := apiCfg.Model
	if model == "" {
		model = DefaultVolcengineModel
	}

	baseURL := apiCfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultVolcengineBaseURL
	}

	// 解析 size 字段为 aspect_ratio + sizeTier
	// 格式：
	//   "3:4"      → aspect_ratio=3:4, sizeTier=2K（默认）
	//   "3:4:1K"   → aspect_ratio=3:4, sizeTier=1K
	//   "3:4:4K"   → aspect_ratio=3:4, sizeTier=4K
	aspectRatio, sizeTier := parseVolcengineSize(apiCfg.Size)

	return &VolcengineProvider{
		apiKey:      apiCfg.Key,
		baseURL:     baseURL,
		model:       model,
		aspectRatio: aspectRatio,
		sizeTier:    sizeTier,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}, nil
}

// Name 返回提供者名称
func (p *VolcengineProvider) Name() string {
	return "Volcengine"
}

// Generate 生成图片
func (p *VolcengineProvider) Generate(ctx context.Context, prompt string) (*GenerateResult, error) {
	reqBody := map[string]any{
		"model":        p.model,
		"prompt":       prompt,
		"size":         p.sizeTier,
		"aspect_ratio": p.aspectRatio,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "marshal_error",
			Message:  "请求构造失败",
			Original: err,
		}
	}

	url := strings.TrimRight(p.baseURL, "/") + "/images/generations"
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

	return p.parseResponse(resp.Body)
}

// volcengineResponse OpenAI 兼容响应结构
type volcengineResponse struct {
	Data []struct {
		URL           string `json:"url"`
		B64JSON       string `json:"b64_json"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

func (p *VolcengineProvider) parseResponse(body io.Reader) (*GenerateResult, error) {
	var result volcengineResponse
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "decode_error",
			Message:  "响应解析失败",
			Original: err,
		}
	}

	if result.Error != nil {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "api_error",
			Message:  fmt.Sprintf("API 返回错误: %s", result.Error.Message),
		}
	}

	if len(result.Data) == 0 || result.Data[0].URL == "" {
		return nil, &GenerateError{
			Provider: p.Name(),
			Code:     "no_image",
			Message:  "未生成图片",
			HintMsg:  "提示词可能不符合内容政策，请尝试修改提示词",
		}
	}

	return &GenerateResult{
		URL:           result.Data[0].URL,
		RevisedPrompt: result.Data[0].RevisedPrompt,
		Model:         p.model,
		Size:          p.aspectRatio,
	}, nil
}

func (p *VolcengineProvider) handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var errResp volcengineResponse
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
			Message:  "API Key 无效或已过期",
			HintMsg:  "请检查配置中的 article.image.key 或 post.image.key 是否正确，或前往火山引擎控制台获取新的 API Key",
			Original: fmt.Errorf("status 401: %s", string(body)),
		}
	case http.StatusTooManyRequests:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "rate_limit",
			Message:  "请求过于频繁，请稍后重试",
			Original: fmt.Errorf("status 429: %s", string(body)),
		}
	case http.StatusBadRequest:
		if isContentSafetyError(errMsg) || isContentSafetyError(string(body)) {
			return &GenerateError{
				Provider: p.Name(),
				Code:     "safety_blocked",
				Message:  "提示词被内容安全策略拦截",
				HintMsg:  "提示词可能包含敏感内容，请修改提示词后重试",
				Original: fmt.Errorf("status 400: %s", string(body)),
			}
		}
		return &GenerateError{
			Provider: p.Name(),
			Code:     "bad_request",
			Message:  fmt.Sprintf("请求参数错误: %s", errMsg),
			HintMsg:  "请检查 aspect_ratio、size 等参数是否正确",
			Original: fmt.Errorf("status 400: %s", string(body)),
		}
	case http.StatusPaymentRequired, http.StatusForbidden:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "payment_required",
			Message:  "账户余额不足或访问受限",
			HintMsg:  "请前往火山引擎控制台检查账户余额",
			Original: fmt.Errorf("status %d: %s", resp.StatusCode, string(body)),
		}
	default:
		return &GenerateError{
			Provider: p.Name(),
			Code:     "unknown",
			Message:  fmt.Sprintf("API 返回错误 (HTTP %d): %s", resp.StatusCode, errMsg),
			Original: fmt.Errorf("status %d: %s", resp.StatusCode, string(body)),
		}
	}
}

// parseVolcengineSize 解析 size 配置字段
// 支持格式：
//   - "3:4"          → aspect_ratio=3:4, sizeTier=2K
//   - "3:4:1K"       → aspect_ratio=3:4, sizeTier=1K
//   - "3:4:4K"       → aspect_ratio=3:4, sizeTier=4K
//   - "1728x2304"    → aspect_ratio=3:4, sizeTier=2K（像素格式）
func parseVolcengineSize(size string) (aspectRatio, sizeTier string) {
	if size == "" {
		return "1:1", "2K"
	}

	// 像素格式映射表（WIDTHxHEIGHT → ratio, tier）
	pixelMap := map[string]struct{ ratio, tier string }{
		// 2K 档位
		"2048x2048": {"1:1", "2K"},
		"2304x1728": {"4:3", "2K"}, "1728x2304": {"3:4", "2K"},
		"2560x1440": {"16:9", "2K"}, "1440x2560": {"9:16", "2K"},
		"2496x1664": {"3:2", "2K"}, "1664x2496": {"2:3", "2K"},
		"3024x1296": {"21:9", "2K"},
		// 4K 档位
		"4096x4096": {"1:1", "4K"},
		"4704x3520": {"4:3", "4K"}, "3520x4704": {"3:4", "4K"},
		"5504x3040": {"16:9", "4K"}, "3040x5504": {"9:16", "4K"},
		"4992x3328": {"3:2", "4K"}, "3328x4992": {"2:3", "4K"},
		"6240x2656": {"21:9", "4K"},
	}
	if mapped, ok := pixelMap[size]; ok {
		return mapped.ratio, mapped.tier
	}

	// 检查是否包含分辨率档位后缀（如 "3:4:2K"）
	upper := strings.ToUpper(size)
	for _, tier := range []string{"4K", "2K", "1K"} {
		suffix := ":" + tier
		if strings.HasSuffix(upper, suffix) {
			ratio := size[:len(size)-len(suffix)]
			if volcengineSupportedRatios[ratio] {
				return ratio, tier
			}
		}
	}

	// 纯宽高比格式（如 "3:4"）
	if volcengineSupportedRatios[size] {
		return size, "2K"
	}

	// 默认
	return "1:1", "2K"
}
