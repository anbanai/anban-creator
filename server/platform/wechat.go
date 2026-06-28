package platform

import (
	"context"
	"fmt"
)

// WechatProvider is the platform provider for WeChat Official Accounts.
//
// Profile auto-fetch is intentionally unsupported: the Official Account
// platform exposes no public API to retrieve an account's own nickname/avatar
// via app_id+secret, so a profile cannot be populated automatically. The HTTP
// handler (ProjectHandler.FetchProfile) gates this path off — it only permits
// the Seednote platform — so this provider is effectively unreachable.
type WechatProvider struct {
	appID  string
	secret string
}

// NewWechatProvider creates a new WeChat platform provider.
func NewWechatProvider(appID, secret string) *WechatProvider {
	return &WechatProvider{appID: appID, secret: secret}
}

// FetchProfile returns an explicit "not supported" error rather than a
// misleading empty-success profile. There is no public WeChat Official Account
// API for fetching account profile info with app_id+secret.
func (p *WechatProvider) FetchProfile(ctx context.Context, profileURL string) (*PlatformProfile, error) {
	if p.appID == "" || p.secret == "" {
		return nil, fmt.Errorf("wechat app_id and secret are required")
	}
	return nil, fmt.Errorf("wechat official-account profile fetching is not supported")
}

// MiniProgramProvider is the platform provider for WeChat Mini Programs.
type MiniProgramProvider struct {
	appID  string
	secret string
}

// NewMiniProgramProvider creates a new Mini Program platform provider.
func NewMiniProgramProvider(appID, secret string) *MiniProgramProvider {
	return &MiniProgramProvider{appID: appID, secret: secret}
}

// FetchProfile returns an explicit "not supported" error: WeChat Mini Program
// profile fetching is not available via app_id+secret.
func (p *MiniProgramProvider) FetchProfile(ctx context.Context, profileURL string) (*PlatformProfile, error) {
	if p.appID == "" || p.secret == "" {
		return nil, fmt.Errorf("wechat app_id and secret are required")
	}
	return nil, fmt.Errorf("wechat mini-program profile fetching is not supported")
}
