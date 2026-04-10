package platform

import "github.com/royalrick/anbanwriter/server/model"

// NewProvider creates a PlatformDataProvider for the given platform.
// For WeChat platforms (article/xls), appID and secret are required.
func NewProvider(platform, appID, secret string) PlatformDataProvider {
	switch platform {
	case model.PlatformArticle:
		return NewWechatProvider(appID, secret)
	case model.PlatformXLS:
		return NewMiniProgramProvider(appID, secret)
	case model.PlatformRednote:
		return NewRednoteProvider()
	default:
		return nil
	}
}
