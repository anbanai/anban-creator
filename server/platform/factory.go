package platform

import (
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/xhs"
)

// NewProvider creates a PlatformDataProvider for the given platform.
// For WeChat platforms (article/xls), appID and secret are required.
// For Rednote, xhsClient is required.
func NewProvider(platform, appID, secret string, xhsClient *xhs.Client) PlatformDataProvider {
	switch platform {
	case model.PlatformArticle:
		return NewWechatProvider(appID, secret)
	case model.PlatformXLS:
		return NewMiniProgramProvider(appID, secret)
	case model.PlatformRednote:
		return NewRednoteProvider(xhsClient)
	default:
		return nil
	}
}
