package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appconfig "github.com/anbanai/anban-creator/server/app/config"
	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

var ErrWechatCredentialsMissing = errors.New("project missing WeChat credentials")
var ErrWechatAuthorizerInactive = errors.New("project WeChat authorization is not connected")
var ErrWechatAnalyticsCapabilityMissing = errors.New("WeChat authorization lacks article analytics capability")
var ErrWechatAnalyticsClientUnavailable = errors.New("authorized WeChat analytics client unavailable")

const wechatArticleAnalyticsScope = 7

type WechatArticleDetailAPI interface {
	GetArticleTotalDetail(context.Context, appwechat.ArticleTotalDetailRequest) (*appwechat.ArticleTotalDetailResponse, error)
}

type WechatOfficialAnalyticsProvider struct {
	logger                 *zerolog.Logger
	apiFactory             func(context.Context, *model.Project) (WechatArticleDetailAPI, error)
	legacyAPIFactory       func(context.Context, *model.Project) (WechatArticleDetailAPI, error)
	authorizationLookup    func(context.Context, string) (*model.WechatOpenPlatformAuthorization, error)
	openPlatformAPIFactory func(context.Context, *model.Project, *model.WechatOpenPlatformAuthorization) (WechatArticleDetailAPI, error)
	repo                   repository.Repository
}

func NewWechatOfficialAnalyticsProvider(logger *zerolog.Logger, repos ...repository.Repository) *WechatOfficialAnalyticsProvider {
	provider := &WechatOfficialAnalyticsProvider{logger: logger}
	if len(repos) > 0 && repos[0] != nil {
		provider.repo = repos[0]
		provider.authorizationLookup = repos[0].WechatOpenPlatformAuthorizations().FindByProjectID
	}
	provider.apiFactory = provider.officialAPI
	return provider
}

func (p *WechatOfficialAnalyticsProvider) SetOpenPlatformAPIFactory(factory func(context.Context, *model.Project, *model.WechatOpenPlatformAuthorization) (WechatArticleDetailAPI, error)) {
	p.openPlatformAPIFactory = factory
}

func (p *WechatOfficialAnalyticsProvider) officialAPI(ctx context.Context, project *model.Project) (WechatArticleDetailAPI, error) {
	if project == nil {
		return nil, ErrWechatCredentialsMissing
	}
	if p.authorizationLookup != nil {
		authorization, err := p.authorizationLookup(ctx, project.ID)
		switch {
		case err == nil:
			if authorization.Status != model.WechatAuthorizationStatusConnected {
				return nil, ErrWechatAuthorizerInactive
			}
			if !wechatScopeGranted(authorization.GrantedScopes, wechatArticleAnalyticsScope) {
				return nil, ErrWechatAnalyticsCapabilityMissing
			}
			if p.openPlatformAPIFactory == nil {
				return nil, ErrWechatAnalyticsClientUnavailable
			}
			return p.openPlatformAPIFactory(ctx, project, authorization)
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return nil, fmt.Errorf("find WeChat Open Platform authorization: %w", err)
		}
	}
	if p.repo != nil {
		if config, err := p.repo.ProjectChannelConfigs().Get(ctx, project.ID, model.ChannelArticle); err == nil && config != nil {
			project.RuntimeChannelConfig = config.Config.Data()
		}
	}
	appID, _ := project.RuntimeChannelConfig["wechat_app_id"].(string)
	secret, _ := project.RuntimeChannelConfig["wechat_secret"].(string)
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(secret) == "" {
		return nil, ErrWechatCredentialsMissing
	}
	cfg := &appconfig.Config{}
	cfg.Wechat.AppID = strings.TrimSpace(appID)
	cfg.Wechat.Secret = strings.TrimSpace(secret)
	if p.legacyAPIFactory != nil {
		return p.legacyAPIFactory(ctx, project)
	}
	return appwechat.NewService(cfg, p.logger).OfficialAPI(), nil
}

func (p *WechatOfficialAnalyticsProvider) FetchArticleTotalDetail(ctx context.Context, project *model.Project, publicationDate string) (*appwechat.ArticleTotalDetailResponse, error) {
	if p == nil || p.apiFactory == nil {
		return nil, errors.New("WeChat analytics API unavailable")
	}
	api, err := p.apiFactory(ctx, project)
	if err != nil {
		return nil, err
	}
	return api.GetArticleTotalDetail(ctx, appwechat.ArticleTotalDetailRequest{BeginDate: publicationDate, EndDate: publicationDate})
}

func wechatScopeGranted(scopes []int, required int) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}
