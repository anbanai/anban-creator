package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const defaultWeChatAPIBase = "https://api.weixin.qq.com"

// WxSession holds the response from WeChat jscode2session API.
type WxSession struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

// WeChatService handles WeChat mini-program authentication.
type WeChatService struct {
	appID     string
	appSecret string
	baseURL   string // override for testing
	logger    *zerolog.Logger
	client    *http.Client

	// access_token cache
	tokenMu     sync.Mutex
	tokenValue  string
	tokenExpiry time.Time
}

// NewWeChatService creates a new WeChatService.
func NewWeChatService(appID, appSecret string, logger *zerolog.Logger) *WeChatService {
	return &WeChatService{
		appID:     appID,
		appSecret: appSecret,
		baseURL:   defaultWeChatAPIBase,
		logger:    logger,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// SetBaseURL overrides the WeChat API base URL (for testing).
func (s *WeChatService) SetBaseURL(url string) {
	s.baseURL = url
}

// getAccessToken returns a cached access_token, fetching a new one if expired.
func (s *WeChatService) getAccessToken() (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	if s.tokenValue != "" && time.Now().Before(s.tokenExpiry) {
		return s.tokenValue, nil
	}

	apiURL := fmt.Sprintf(
		"%s/cgi-bin/token?grant_type=client_credential&appid=%s&secret=%s",
		s.baseURL, url.QueryEscape(s.appID), url.QueryEscape(s.appSecret),
	)

	resp, err := s.client.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("wechat get access_token failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read access_token response: %w", err)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse access_token response: %w", err)
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wechat access_token error (code=%d): %s", result.ErrCode, result.ErrMsg)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("wechat returned empty access_token")
	}

	// Cache with buffer before expiry (clamped to half the TTL).
	buffer := 300
	if result.ExpiresIn < 600 {
		buffer = result.ExpiresIn / 2
	}
	s.tokenValue = result.AccessToken
	s.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn-buffer) * time.Second)
	return s.tokenValue, nil
}

// RGB represents an RGB color value.
type RGB struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}

// GenerateUnlimitedQRCode generates a WeChat mini program unlimited QR code.
// Returns raw PNG image bytes.
func (s *WeChatService) GenerateUnlimitedQRCode(scene, page, envVersion string, width int, lineColor RGB, isHyaline bool) ([]byte, error) {
	token, err := s.getAccessToken()
	if err != nil {
		return nil, err
	}

	if width <= 0 {
		width = 430
	}

	reqBody := map[string]any{
		"scene":       scene,
		"page":        page,
		"env_version": envVersion,
		"width":       width,
		"auto_color":  false,
		"line_color":  lineColor,
		"is_hyaline":  isHyaline,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal qrcode request: %w", err)
	}

	apiURL := fmt.Sprintf("%s/wxa/getwxacodeunlimit?access_token=%s", s.baseURL, url.QueryEscape(token))
	resp, err := s.client.Post(apiURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("wechat qrcode api request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read qrcode response: %w", err)
	}

	// WeChat returns JSON with errcode on error, otherwise raw PNG bytes.
	// Check if response is JSON by looking at the first byte.
	if len(body) > 0 && body[0] == '{' {
		var errResp struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.ErrCode != 0 {
			return nil, fmt.Errorf("wechat qrcode api error (code=%d): %s", errResp.ErrCode, errResp.ErrMsg)
		}
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("wechat qrcode api returned empty response")
	}

	return body, nil
}

// Code2Session exchanges a WeChat mini-program login code for an openID and
// session key by calling the WeChat jscode2session API.
func (s *WeChatService) Code2Session(code string) (*WxSession, error) {
	apiURL := fmt.Sprintf(
		"%s/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		s.baseURL, url.QueryEscape(s.appID), url.QueryEscape(s.appSecret), url.QueryEscape(code),
	)

	resp, err := s.client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("wechat api request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read wechat api response: %w", err)
	}

	var session WxSession
	if err := json.Unmarshal(body, &session); err != nil {
		return nil, fmt.Errorf("parse wechat api response: %w", err)
	}

	if session.ErrCode != 0 {
		return nil, fmt.Errorf("wechat api error (code=%d): %s", session.ErrCode, session.ErrMsg)
	}

	if session.OpenID == "" {
		return nil, fmt.Errorf("wechat api returned empty openid")
	}

	return &session, nil
}
