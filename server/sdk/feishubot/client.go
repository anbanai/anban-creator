package feishubot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"resty.dev/v3"
)

const (
	// BaseURL 飞书机器人API基础URL
	BaseURL = "https://open.feishu.cn/open-apis"
	// BotHookPath 机器人webhook路径
	BotHookPath = "/bot/v2/hook/"

	defaultHTTPTimeout = 10 * time.Second
)

// Client 飞书机器人客户端
type Client struct {
	httpClient *resty.Client
	webhookID  string
	secretKey  string // 用于签名验证，如果设置了安全设置
}

// NewClient 创建一个新的飞书机器人客户端
func NewClient(webhookID string, options ...ClientOption) *Client {
	client := &Client{
		httpClient: resty.NewWithClient(&http.Client{Timeout: defaultHTTPTimeout}),
		webhookID:  webhookID,
	}
	configureHTTPClient(client.httpClient)

	// 应用选项
	for _, option := range options {
		option(client)
	}

	return client
}

func configureHTTPClient(httpClient *resty.Client) {
	httpClient.SetBaseURL(BaseURL)
	httpClient.SetHeader("Content-Type", "application/json")
	httpClient.SetTimeout(defaultHTTPTimeout)
}

// ClientOption 客户端选项函数类型
type ClientOption func(*Client)

// WithSecretKey 设置安全签名密钥
func WithSecretKey(secretKey string) ClientOption {
	return func(c *Client) {
		c.secretKey = secretKey
	}
}

// WithHTTPClient 注入底层 HTTP 客户端，便于复用传输设置和测试。
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient == nil {
			return
		}
		c.httpClient = resty.NewWithClient(httpClient)
		configureHTTPClient(c.httpClient)
		if httpClient.Timeout > 0 {
			c.httpClient.SetTimeout(httpClient.Timeout)
		}
	}
}

// WithTimeout 设置HTTP请求超时时间
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		if timeout <= 0 {
			timeout = defaultHTTPTimeout
		}
		c.httpClient.SetTimeout(timeout)
	}
}

// WithRetry 设置HTTP请求重试次数和重试等待时间
func WithRetry(count int, waitTime time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.SetRetryCount(count)
		c.httpClient.SetRetryWaitTime(waitTime)
	}
}

// WithHTTPPoolSettings 配置连接池参数
func WithHTTPPoolSettings(maxIdle int, timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.SetTransport(&http.Transport{
			MaxIdleConns:        maxIdle,
			IdleConnTimeout:     timeout,
			TLSHandshakeTimeout: 10 * time.Second,
		})
	}
}

// generateSignature 生成签名
// timestamp 为当前时间戳，单位为秒
func (c *Client) generateSignature(timestamp int64) string {
	if c.secretKey == "" {
		return ""
	}

	// 拼接待签名字符串
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, c.secretKey)

	// 使用HMAC-SHA256算法计算签名
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

// getWebhookURL 获取完整的webhook URL
func (c *Client) getWebhookURL() string {
	return BaseURL + BotHookPath + c.webhookID
}

// Response 飞书机器人API响应
type Response struct {
	Code          int    `json:"code"`
	Msg           string `json:"msg"`
	Data          any    `json:"data"`
	StatusCode    int    `json:"StatusCode,omitempty"`
	StatusMessage string `json:"StatusMessage,omitempty"`
}

type responseEnvelope struct {
	Code                *int   `json:"code"`
	Msg                 string `json:"msg"`
	Data                any    `json:"data"`
	StatusCode          *int   `json:"StatusCode"`
	StatusMessage       string `json:"StatusMessage"`
	LegacyStatusCode    *int   `json:"status_code"`
	LegacyStatusMessage string `json:"status_message"`
	CamelStatusCode     *int   `json:"statusCode"`
	CamelStatusMessage  string `json:"statusMessage"`
}

func decodeResponse(body string, response *Response) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("provider acknowledgement is empty")
	}
	var envelope responseEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return fmt.Errorf("decode provider acknowledgement: %w", err)
	}

	statusCode := envelope.StatusCode
	statusMessage := envelope.StatusMessage
	if statusCode == nil {
		statusCode = envelope.LegacyStatusCode
		statusMessage = envelope.LegacyStatusMessage
	}
	if statusCode == nil {
		statusCode = envelope.CamelStatusCode
		statusMessage = envelope.CamelStatusMessage
	}
	if envelope.Code == nil && statusCode == nil {
		return errors.New("provider acknowledgement is missing code")
	}
	if envelope.Code != nil {
		response.Code = *envelope.Code
	}
	response.Msg = envelope.Msg
	response.Data = envelope.Data
	if statusCode != nil {
		response.StatusCode = *statusCode
		response.StatusMessage = statusMessage
	}
	if envelope.Code != nil && *envelope.Code != 0 {
		if response.Msg == "" {
			response.Msg = "provider returned a non-zero code"
		}
		return fmt.Errorf("provider returned code %d: %s", *envelope.Code, response.Msg)
	}
	if statusCode != nil && *statusCode != 0 {
		if response.StatusMessage == "" {
			response.StatusMessage = "provider returned a non-zero status code"
		}
		return fmt.Errorf("provider returned status code %d: %s", *statusCode, response.StatusMessage)
	}
	return nil
}
