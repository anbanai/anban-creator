package dingtalkbot

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"resty.dev/v3"
)

const (
	// BaseURL 钉钉机器人API基础URL
	BaseURL = "https://oapi.dingtalk.com"
	// BotHookPath 机器人webhook路径
	BotHookPath = "/robot/send"

	defaultHTTPTimeout = 10 * time.Second
)

// Client 钉钉机器人客户端
type Client struct {
	httpClient  *resty.Client
	accessToken string
	secretKey   string // 用于签名验证，如果设置了安全设置
}

// NewClient 创建一个新的钉钉机器人客户端
func NewClient(accessToken string, options ...ClientOption) *Client {
	client := &Client{
		httpClient:  resty.NewWithClient(&http.Client{Timeout: defaultHTTPTimeout}),
		accessToken: accessToken,
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
// timestamp 为当前时间戳，单位为毫秒
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
func (c *Client) getWebhookURL(timestamp int64) string {
	query := url.Values{}
	query.Set("access_token", c.accessToken)
	if c.secretKey != "" {
		query.Set("timestamp", strconv.FormatInt(timestamp, 10))
		query.Set("sign", c.generateSignature(timestamp))
	}
	return BaseURL + BotHookPath + "?" + query.Encode()
}

// Response 钉钉机器人API响应
type Response struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// SendMessage 发送消息
func (c *Client) SendMessage(msg Message) (*Response, error) {
	return c.SendMessageContext(context.Background(), msg)
}

// SendMessageContext 使用指定上下文发送消息。
func (c *Client) SendMessageContext(ctx context.Context, msg Message) (*Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	timestamp := time.Now().UnixNano() / 1e6 // 转换为毫秒
	webhookURL := c.getWebhookURL(timestamp)

	resp, err := c.httpClient.R().
		SetContext(ctx).
		SetBody(msg).
		Post(webhookURL)
	if err != nil {
		return nil, fmt.Errorf("发送消息失败: %w", sanitizeRequestError(err))
	}

	if !resp.IsStatusSuccess() {
		return nil, fmt.Errorf("发送消息失败, status code: %d", resp.StatusCode())
	}

	response := &Response{}
	if err := decodeResponse(resp.String(), response); err != nil {
		return nil, fmt.Errorf("发送消息失败: %w", err)
	}
	if response.ErrCode != 0 {
		message := response.ErrMsg
		if message == "" {
			message = GetErrorMessage(response.ErrCode)
		}
		return response, fmt.Errorf("发送消息失败: %s", message)
	}

	return response, nil
}

func decodeResponse(body string, response *Response) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("provider acknowledgement is empty")
	}
	var envelope struct {
		ErrCode *int   `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return fmt.Errorf("decode provider acknowledgement: %w", err)
	}
	if envelope.ErrCode == nil {
		return errors.New("provider acknowledgement is missing errcode")
	}
	response.ErrCode = *envelope.ErrCode
	response.ErrMsg = envelope.ErrMsg
	return nil
}
