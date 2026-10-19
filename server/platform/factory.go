package platform

import (
	"github.com/anbanai/anban-creator/server/model"
)

// NewProvider creates a PlatformDataProvider for the given platform.
// For the WeChat platform, appID and secret are required.
func NewProvider(platform, appID, secret string) PlatformDataProvider {
	switch platform {
	case model.PlatformWechat:
		return NewWechatProvider(appID, secret)
	default:
		return nil
	}
}
