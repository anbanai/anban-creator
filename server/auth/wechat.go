package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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
}

// NewWeChatService creates a new WeChatService.
func NewWeChatService(appID, appSecret string, logger *zerolog.Logger) *WeChatService {
	return &WeChatService{
		appID:     appID,
		appSecret: appSecret,
		baseURL:   defaultWeChatAPIBase,
		logger:    logger,
		client:    &http.Client{},
	}
}

// SetBaseURL overrides the WeChat API base URL (for testing).
func (s *WeChatService) SetBaseURL(url string) {
	s.baseURL = url
}

// Code2Session exchanges a WeChat mini-program login code for an openID and
// session key by calling the WeChat jscode2session API.
func (s *WeChatService) Code2Session(code string) (*WxSession, error) {
	url := fmt.Sprintf(
		"%s/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		s.baseURL, s.appID, s.appSecret, code,
	)

	resp, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("wechat api request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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
