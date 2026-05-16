package platform

import (
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/seednote"
)

// NewProvider creates a PlatformDataProvider for the given platform.
// For WeChat platforms (article), appID and secret are required.
// For Seednote, seednoteClient is required.
func NewProvider(platform, appID, secret string, seednoteClient *seednote.Client) PlatformDataProvider {
	switch platform {
	case model.PlatformArticle:
		return NewWechatProvider(appID, secret)
	case model.PlatformSeednote:
		return NewSeednoteProvider(seednoteClient)
	default:
		return nil
	}
}
