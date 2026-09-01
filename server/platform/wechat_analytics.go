package platform

import (
	"context"
	"errors"

	appconfig "github.com/anbanai/anban-creator/app/config"
	appwechat "github.com/anbanai/anban-creator/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
)

var ErrWechatCredentialsMissing = errors.New("project missing WeChat credentials")

type WechatArticleDetailAPI interface {
	GetArticleTotalDetail(context.Context, appwechat.ArticleTotalDetailRequest) (*appwechat.ArticleTotalDetailResponse, error)
}

type WechatOfficialAnalyticsProvider struct {
	logger     *zerolog.Logger
	apiFactory func(*model.Project) (WechatArticleDetailAPI, error)
}

func NewWechatOfficialAnalyticsProvider(logger *zerolog.Logger) *WechatOfficialAnalyticsProvider {
	provider := &WechatOfficialAnalyticsProvider{logger: logger}
	provider.apiFactory = provider.officialAPI
	return provider
}

func (p *WechatOfficialAnalyticsProvider) officialAPI(project *model.Project) (WechatArticleDetailAPI, error) {
	if project == nil || project.GetWechatAppID() == "" || project.GetWechatSecret() == "" {
		return nil, ErrWechatCredentialsMissing
	}
	cfg := &appconfig.Config{}
	cfg.Wechat.AppID = project.GetWechatAppID()
	cfg.Wechat.Secret = project.GetWechatSecret()
	return appwechat.NewService(cfg, p.logger).OfficialAPI(), nil
}

func (p *WechatOfficialAnalyticsProvider) FetchArticleTotalDetail(ctx context.Context, project *model.Project, publicationDate string) (*appwechat.ArticleTotalDetailResponse, error) {
	if p == nil || p.apiFactory == nil {
		return nil, errors.New("WeChat analytics API unavailable")
	}
	api, err := p.apiFactory(project)
	if err != nil {
		return nil, err
	}
	return api.GetArticleTotalDetail(ctx, appwechat.ArticleTotalDetailRequest{BeginDate: publicationDate, EndDate: publicationDate})
}
