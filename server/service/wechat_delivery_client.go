package service

import (
	"context"
	"errors"
	"fmt"

	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// WechatAccountClientFactory resolves the credential source for Server-owned
// WeChat draft and material operations.
type WechatAccountClientFactory struct {
	repo         repository.Repository
	publishing   *PublishingService
	openPlatform *WechatOpenPlatformService
	logger       *zerolog.Logger
}

func NewWechatAccountClientFactory(repo repository.Repository, publishing *PublishingService, openPlatform *WechatOpenPlatformService, logger *zerolog.Logger) *WechatAccountClientFactory {
	return &WechatAccountClientFactory{repo: repo, publishing: publishing, openPlatform: openPlatform, logger: logger}
}

func (f *WechatAccountClientFactory) ForProject(project *model.Project) (*appwechat.Service, error) {
	if f == nil || f.repo == nil || project == nil || project.ID == "" {
		return nil, fmt.Errorf("WeChat account client requires a project and repository")
	}
	authorization, err := f.repo.WechatOpenPlatformAuthorizations().FindByProjectID(context.Background(), project.ID)
	switch {
	case err == nil:
		if authorization.Status != model.WechatAuthorizationStatusConnected {
			return nil, ErrWechatOpenPlatformNotConnected
		}
		if f.openPlatform == nil {
			return nil, ErrWechatOpenPlatformNotConnected
		}
		if !wechatCapabilitiesForScopes(authorization.GrantedScopes).Draft {
			return nil, ErrWechatOpenPlatformCapabilityMissing
		}
		return appwechat.NewAuthorizedService(authorization.AuthorizerAppID, func(ctx context.Context) (string, error) {
			return f.openPlatform.AccessTokenForProject(ctx, project.ID)
		}, f.logger), nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, fmt.Errorf("find project WeChat authorization: %w", err)
	}

	if f.publishing == nil {
		return nil, fmt.Errorf("legacy WeChat account client is unavailable")
	}
	appConfig, err := f.publishing.BuildAppConfigForProject(project)
	if err != nil {
		return nil, err
	}
	if appConfig == nil || appConfig.Wechat.AppID == "" || appConfig.Wechat.Secret == "" {
		return nil, ErrInvalidWechatPlatformConfig
	}
	return appwechat.NewService(appConfig, f.logger), nil
}
