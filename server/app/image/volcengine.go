package image

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/anbanai/anban-creator/server/app/config"
	"github.com/rs/zerolog"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// VolcengineProvider 火山方舟 Seedream 图片生成服务提供者
// 使用官方 arkruntime SDK，支持类型安全的请求体和高级选项
type VolcengineProvider struct {
	client      *arkruntime.Client
	model       string
	log         *zerolog.Logger
	sizePixel   string // 像素格式 "WIDTHxHEIGHT"，如 "1728x2304"
	volcConfig  *config.VolcengineConfig
	name        string
	endpoint    string
	diagnostics *imageProviderDiagnostics
}

const volcengineImageRequestPath = "/images/generations"

type requestObservation struct {
	Method              string
	Endpoint            string
	HTTPStatusCode      int
	ResponseContentType string
	TransportPhase      string
}

type imageProviderDiagnostics struct {
	observations sync.Map
}

type requestTransportError struct {
	observation requestObservation
	cause       error
}

func (e *requestTransportError) Error() string { return "image provider transport request failed" }
func (e *requestTransportError) Unwrap() error { return e.cause }

type diagnosticRoundTripper struct {
	base        http.RoundTripper
	diagnostics *imageProviderDiagnostics
}

func (t *diagnosticRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	requestID := strings.TrimSpace(req.Header.Get(model.ClientRequestHeader))
	observation := requestObservation{
		Method:   req.Method,
		Endpoint: sanitizeImageProviderEndpoint(req.URL.String()),
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		observation.TransportPhase = classifyImageTransportPhase(err)
		if requestID != "" && t.diagnostics != nil {
			t.diagnostics.observations.Store(requestID, observation)
		}
		return nil, &requestTransportError{observation: observation, cause: err}
	}
	observation.HTTPStatusCode = resp.StatusCode
	observation.ResponseContentType = strings.TrimSpace(resp.Header.Get("Content-Type"))
	if resp.StatusCode >= http.StatusMultipleChoices {
		observation.TransportPhase = "http_response"
	} else if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices && !isJSONContentType(observation.ResponseContentType) {
		observation.TransportPhase = "protocol"
	}
	if requestID != "" && t.diagnostics != nil && observation.TransportPhase != "" {
		t.diagnostics.observations.Store(requestID, observation)
	}
	return resp, nil
}

func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	return contentType == "application/json" || strings.HasSuffix(contentType, "+json")
}

func sanitizeImageProviderEndpoint(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "<invalid-endpoint>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func classifyImageTransportPhase(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) || strings.Contains(strings.ToLower(err.Error()), "tls") || strings.Contains(strings.ToLower(err.Error()), "x509") {
		return "tls"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		switch strings.ToLower(opErr.Op) {
		case "dial":
			return "connect"
		case "read":
			return "read"
		case "write":
			return "write"
		}
	}
	return "unknown"
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
	return newSeedreamProvider(apiCfg, log, "Volcengine")
}

// NewTokenplayerProvider creates a TokenPlayer provider using the same
// Seedream-compatible Ark request protocol as Volcengine.
func NewTokenplayerProvider(apiCfg *config.ImageAPI, log *zerolog.Logger) (*VolcengineProvider, error) {
	return newSeedreamProvider(apiCfg, log, "TokenPlayer")
}

func newSeedreamProvider(apiCfg *config.ImageAPI, log *zerolog.Logger, name string) (*VolcengineProvider, error) {
	mdl := apiCfg.Model
	if mdl == "" {
		mdl = DefaultVolcengineModel
	}

	timeout := 300 * time.Second
	if apiCfg.TimeoutSec > 0 {
		timeout = time.Duration(apiCfg.TimeoutSec) * time.Second
	}
	baseURL := strings.TrimRight(apiCfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultVolcengineBaseURL
	}
	diagnostics := &imageProviderDiagnostics{}
	opts := []arkruntime.ConfigOption{
		arkruntime.WithTimeout(timeout),
		arkruntime.WithHTTPClient(&http.Client{
			Timeout:   timeout,
			Transport: &diagnosticRoundTripper{base: http.DefaultTransport, diagnostics: diagnostics},
		}),
	}
	if apiCfg.BaseURL != "" {
		opts = append(opts, arkruntime.WithBaseUrl(baseURL))
	}
	client := arkruntime.NewClientWithApiKey(apiCfg.Key, opts...)

	aspectRatio, sizeTier := parseVolcengineSize(apiCfg.Size)
	sizePixel := volcenginePixelSize(aspectRatio, sizeTier)

	return &VolcengineProvider{
		client:      client,
		log:         log,
		model:       mdl,
		sizePixel:   sizePixel,
		volcConfig:  apiCfg.Volcengine,
		name:        name,
		endpoint:    sanitizeImageProviderEndpoint(baseURL + volcengineImageRequestPath),
		diagnostics: diagnostics,
	}, nil
}

// Name 返回提供者名称
func (p *VolcengineProvider) Name() string {
	if p.name != "" {
		return p.name
	}
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
		converted := p.convertSDKError(err)
		var generateErr *GenerateError
		if errors.As(converted, &generateErr) {
			p.log.Error().
				Str("provider", p.Name()).
				Str("model", p.model).
				Str("error_code", generateErr.Code).
				Int("http_status", generateErr.HTTPStatusCode).
				Str("provider_request_id", generateErr.ProviderRequestID).
				Str("request_method", generateErr.RequestMethod).
				Str("request_endpoint", generateErr.RequestEndpoint).
				Str("response_content_type", generateErr.ResponseContentType).
				Str("transport_phase", generateErr.TransportPhase).
				Bool("retryable", generateErr.Retryable()).
				Dur("elapsed", time.Since(start)).
				Msg("volcengine: image generation failed")
		} else {
			p.log.Error().
				Str("provider", p.Name()).
				Str("model", p.model).
				Str("error_code", "image_generation_error").
				Dur("elapsed", time.Since(start)).
				Msg("volcengine: image generation failed")
		}
		return nil, converted
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
func (p *VolcengineProvider) convertSDKError(err error) (result error) {
	var apiErr *model.APIError
	if errors.As(err, &apiErr) {
		observation := p.takeRequestObservation(apiErr.RequestId)
		defer func() {
			var generateErr *GenerateError
			if errors.As(result, &generateErr) {
				generateErr.HTTPStatusCode = apiErr.HTTPStatusCode
				generateErr.ProviderRequestID = strings.TrimSpace(apiErr.RequestId)
				p.applyRequestObservation(generateErr, observation)
			}
		}()
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
		observation := p.takeRequestObservation(reqErr.RequestId)
		var transportErr *requestTransportError
		if errors.As(err, &transportErr) {
			observation = transportErr.observation
		}
		if observation.Method == "" {
			observation.Method = http.MethodPost
		}
		if observation.Endpoint == "" {
			observation.Endpoint = p.endpoint
		}
		if observation.HTTPStatusCode == 0 && transportErr == nil {
			observation.HTTPStatusCode = reqErr.HTTPStatusCode
		}
		if observation.TransportPhase == "" {
			if reqErr.HTTPStatusCode >= http.StatusOK && reqErr.HTTPStatusCode < http.StatusMultipleChoices {
				observation.TransportPhase = "protocol"
			} else if transportErr == nil {
				observation.TransportPhase = "http_response"
			}
		}
		if reqErr.HTTPStatusCode >= http.StatusOK && reqErr.HTTPStatusCode < http.StatusMultipleChoices {
			generateErr := &GenerateError{
				Provider:          p.Name(),
				Code:              "endpoint_protocol",
				Message:           "图片服务返回了无法解析的非 JSON 响应",
				HintMsg:           "请检查火山方舟 Base URL，应指向官方 Ark API（通常以 /api/v3 结尾），不要使用网页地址或不兼容的 OpenAI 网关",
				HTTPStatusCode:    reqErr.HTTPStatusCode,
				ProviderRequestID: strings.TrimSpace(reqErr.RequestId),
				Original:          err,
			}
			p.applyRequestObservation(generateErr, observation)
			return generateErr
		}
		statusCode := reqErr.HTTPStatusCode
		if transportErr != nil {
			// The SDK uses HTTP 500 as a sentinel for RoundTripper failures;
			// it is not an upstream response status.
			statusCode = 0
		}
		generateErr := &GenerateError{
			Provider:          p.Name(),
			Code:              "network_error",
			Message:           "网络请求失败，请检查网络连接",
			HintMsg:           "确认网络连接正常，API 地址正确",
			HTTPStatusCode:    statusCode,
			ProviderRequestID: strings.TrimSpace(reqErr.RequestId),
			Original:          err,
		}
		p.applyRequestObservation(generateErr, observation)
		return generateErr
	}

	return &GenerateError{
		Provider: p.Name(),
		Code:     "unknown",
		Message:  err.Error(),
		Original: err,
	}
}

func (p *VolcengineProvider) takeRequestObservation(requestID string) requestObservation {
	if p.diagnostics != nil && strings.TrimSpace(requestID) != "" {
		if value, ok := p.diagnostics.observations.LoadAndDelete(requestID); ok {
			if observation, ok := value.(requestObservation); ok {
				return observation
			}
		}
	}
	return requestObservation{}
}

func (p *VolcengineProvider) applyRequestObservation(generateErr *GenerateError, observation requestObservation) {
	if generateErr == nil {
		return
	}
	if generateErr.RequestMethod == "" {
		generateErr.RequestMethod = observation.Method
	}
	if generateErr.RequestEndpoint == "" {
		generateErr.RequestEndpoint = observation.Endpoint
	}
	if generateErr.ResponseContentType == "" {
		generateErr.ResponseContentType = observation.ResponseContentType
	}
	if generateErr.TransportPhase == "" {
		generateErr.TransportPhase = observation.TransportPhase
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
