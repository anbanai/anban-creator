package worldtree

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultBaseURL = "https://www.worldtreetech.cn"

var ErrNotConfigured = errors.New("WorldTree API key is not configured")

type Client struct {
	baseURL    string
	key        string
	httpClient *http.Client
}

type VideoInfo struct {
	ObjectID      string `json:"object_id"`
	ObjectNonceID string `json:"object_nonce_id"`
	URL           string `json:"url"`
	Title         string `json:"title"`
	Username      string `json:"username"`
	AuthorName    string `json:"author_name"`
	CreateTime    int64  `json:"create_time"`
	CreateTimeStr string `json:"create_time_str"`
	FavoriteCount int    `json:"fav_count"`
	ForwardCount  int    `json:"forward_count"`
	LikeCount     int    `json:"like_count"`
	CommentCount  int    `json:"comment_count"`
	CoverURL      string `json:"cover_url"`
}

type videoInfoResponse struct {
	Code    int       `json:"code"`
	Message string    `json:"msg"`
	Data    VideoInfo `json:"data"`
}

func NewClient(baseURL, key string, timeout time.Duration) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: baseURL,
		key:     strings.TrimSpace(key),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.key != ""
}

func (c *Client) GetVideoInfoByURL(ctx context.Context, videoURL string) (*VideoInfo, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	body, err := json.Marshal(map[string]string{
		"key": c.key,
		"url": videoURL,
	})
	if err != nil {
		return nil, fmt.Errorf("encode WorldTree request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v2/wechat/video/getVideoInfoByUrl", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create WorldTree request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request WorldTree video details: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read WorldTree response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("WorldTree returned HTTP %d", resp.StatusCode)
	}
	var decoded videoInfoResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode WorldTree response: %w", err)
	}
	if decoded.Code != http.StatusOK {
		message := strings.TrimSpace(decoded.Message)
		if message == "" {
			message = "request failed"
		}
		return nil, fmt.Errorf("WorldTree returned code %d: %s", decoded.Code, message)
	}
	if strings.TrimSpace(decoded.Data.ObjectID) == "" {
		return nil, errors.New("WorldTree response is missing video ID")
	}
	return &decoded.Data, nil
}
