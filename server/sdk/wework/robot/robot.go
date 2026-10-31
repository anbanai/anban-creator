package robot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

const (
	weworkWebhookURL   = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send"
	weworkUploadURL    = "https://qyapi.weixin.qq.com/cgi-bin/webhook/upload_media"
	defaultHTTPTimeout = 10 * time.Second
)

var webhookCredentialQuery = regexp.MustCompile(`(?i)([?&]key=)[^&\s"'<>]+`)

type sanitizedRequestError struct {
	cause   error
	message string
}

func (e sanitizedRequestError) Error() string { return e.message }
func (e sanitizedRequestError) Unwrap() error { return e.cause }

func sanitizeRequestError(err error) error {
	if err == nil {
		return nil
	}
	redacted := webhookCredentialQuery.ReplaceAllString(err.Error(), `${1}[REDACTED]`)
	if redacted == err.Error() {
		return err
	}
	return sanitizedRequestError{cause: err, message: redacted}
}

type Robot struct {
	key        string
	httpClient *http.Client
}

type RobotOption func(*Robot)

// WithHTTPClient 注入底层 HTTP 客户端，便于复用传输设置和测试。
func WithHTTPClient(httpClient *http.Client) RobotOption {
	return func(robot *Robot) {
		if httpClient == nil {
			return
		}
		clientCopy := *httpClient
		if clientCopy.Timeout <= 0 {
			clientCopy.Timeout = defaultHTTPTimeout
		}
		robot.httpClient = &clientCopy
	}
}

func NewRobot(key string, options ...RobotOption) *Robot {
	robot := &Robot{
		key:        key,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}
	for _, option := range options {
		option(robot)
	}
	return robot
}

type UploadMediaResponse struct {
	MediaID string `json:"media_id"`
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type providerAck struct {
	ErrCode *int   `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

func (robot *Robot) UploadMedia(filename string, write func(io.Writer) error) (*UploadMediaResponse, error) {
	return robot.UploadMediaContext(context.Background(), filename, write)
}

func (robot *Robot) UploadMediaContext(ctx context.Context, filename string, write func(io.Writer) error) (*UploadMediaResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if write == nil {
		return nil, errors.New("write callback is nil")
	}

	buf := new(bytes.Buffer)
	multipartWriter := multipart.NewWriter(buf)
	fileWriter, err := multipartWriter.CreateFormFile("media", filename)
	if err != nil {
		return nil, fmt.Errorf("create writer from file error: %w", err)
	}
	if err := write(fileWriter); err != nil {
		return nil, fmt.Errorf("write error: %w", err)
	}
	if err := multipartWriter.Close(); err != nil {
		return nil, fmt.Errorf("close error: %w", err)
	}

	query := url.Values{}
	query.Set("key", robot.key)
	query.Set("type", "file")
	requestURL := weworkUploadURL + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, buf)
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())

	resp, err := robot.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request error: %w", sanitizeRequestError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upload media failed, status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response data error: %w", err)
	}
	var response UploadMediaResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode response data to json error: %w", err)
	}
	var ack providerAck
	if err := json.Unmarshal(body, &ack); err != nil {
		return nil, fmt.Errorf("decode provider acknowledgement error: %w", err)
	}
	if ack.ErrCode == nil {
		return nil, errors.New("upload media acknowledgement is missing errcode")
	}
	response.ErrCode = *ack.ErrCode
	response.ErrMsg = ack.ErrMsg
	if *ack.ErrCode != 0 {
		return &response, fmt.Errorf("upload media failed: %s", providerErrorMessage(ack.ErrMsg, *ack.ErrCode))
	}
	if response.MediaID == "" {
		return &response, errors.New("upload media acknowledgement is missing media_id")
	}
	return &response, nil
}

func (robot *Robot) SendMessage(msg fiber.Map, keys ...string) (fiber.Map, error) {
	return robot.SendMessageContext(context.Background(), msg, keys...)
}

func (robot *Robot) SendMessageContext(ctx context.Context, msg fiber.Map, keys ...string) (fiber.Map, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message error: %w", err)
	}

	key := robot.key
	if len(keys) > 0 && keys[0] != "" {
		key = keys[0]
	}
	query := url.Values{}
	query.Set("key", key)
	requestURL := weworkWebhookURL + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := robot.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send message: %w", sanitizeRequestError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("failed to send message, status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response data error: %w", err)
	}
	response := make(fiber.Map)
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode response data to json error: %w", err)
	}
	var ack providerAck
	if err := json.Unmarshal(body, &ack); err != nil {
		return response, fmt.Errorf("decode provider acknowledgement error: %w", err)
	}
	if ack.ErrCode == nil {
		return response, errors.New("send message acknowledgement is missing errcode")
	}
	if *ack.ErrCode != 0 {
		return response, fmt.Errorf("failed to send message: %s", providerErrorMessage(ack.ErrMsg, *ack.ErrCode))
	}
	return response, nil
}

func providerErrorMessage(message string, code int) string {
	if strings.TrimSpace(message) != "" {
		return message
	}
	return "provider returned errcode " + strconv.Itoa(code)
}
