package platform

import (
	"context"
	"fmt"

	"github.com/silenceper/wechat/v2"
	"github.com/silenceper/wechat/v2/cache"
	miniConfig "github.com/silenceper/wechat/v2/miniprogram/config"
	offConfig "github.com/silenceper/wechat/v2/officialaccount/config"
)

// WechatProvider fetches profile data from WeChat using the SDK.
type WechatProvider struct {
	appID  string
	secret string
}

// NewWechatProvider creates a new WeChat platform provider.
func NewWechatProvider(appID, secret string) *WechatProvider {
	return &WechatProvider{appID: appID, secret: secret}
}

// FetchProfile fetches WeChat account info using the access token derived from AppID/Secret.
func (p *WechatProvider) FetchProfile(ctx context.Context, profileURL string) (*PlatformProfile, error) {
	if p.appID == "" || p.secret == "" {
		return nil, fmt.Errorf("wechat app_id and secret are required")
	}

	// Use the wechat SDK to get account info.
	wc := wechat.NewWechat()
	memCache := cache.NewMemory()
	cfg := &offConfig.Config{
		AppID:     p.appID,
		AppSecret: p.secret,
		Cache:     memCache,
	}
	_ = wc.GetOfficialAccount(cfg)

	// The wechat SDK doesn't have a direct "get account info" API.
	// For now, return basic info derived from the URL.
	// TODO: Implement actual WeChat account info fetching when API is available.
	return &PlatformProfile{
		Name:        "",
		AvatarURL:   "",
		Positioning: "",
	}, nil
}

// MiniProgramProvider fetches profile data from WeChat Mini Program.
type MiniProgramProvider struct {
	appID  string
	secret string
}

// NewMiniProgramProvider creates a new Mini Program platform provider.
func NewMiniProgramProvider(appID, secret string) *MiniProgramProvider {
	return &MiniProgramProvider{appID: appID, secret: secret}
}

// FetchProfile fetches mini program account info.
func (p *MiniProgramProvider) FetchProfile(ctx context.Context, profileURL string) (*PlatformProfile, error) {
	if p.appID == "" || p.secret == "" {
		return nil, fmt.Errorf("wechat app_id and secret are required")
	}

	wc := wechat.NewWechat()
	memCache := cache.NewMemory()
	cfg := &miniConfig.Config{
		AppID:     p.appID,
		AppSecret: p.secret,
		Cache:     memCache,
	}
	_ = wc.GetMiniProgram(cfg)

	// TODO: Implement actual profile fetching.
	return &PlatformProfile{
		Name:        "",
		AvatarURL:   "",
		Positioning: "",
	}, nil
}
