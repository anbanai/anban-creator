package platform

import (
	"context"
	"errors"
	"testing"

	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
)

type fakeWechatArticleDetailAPI struct {
	request appwechat.ArticleTotalDetailRequest
}

func (f *fakeWechatArticleDetailAPI) GetArticleTotalDetail(_ context.Context, request appwechat.ArticleTotalDetailRequest) (*appwechat.ArticleTotalDetailResponse, error) {
	f.request = request
	return &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{{MsgID: "msg_1"}}}, nil
}

func TestWechatOfficialAnalyticsProviderUsesOneDayDetailRequest(t *testing.T) {
	api := &fakeWechatArticleDetailAPI{}
	provider := &WechatOfficialAnalyticsProvider{apiFactory: func(context.Context, *model.Project) (WechatArticleDetailAPI, error) { return api, nil }}
	response, err := provider.FetchArticleTotalDetail(context.Background(), &model.Project{}, "2026-08-01")
	if err != nil {
		t.Fatal(err)
	}
	if api.request.BeginDate != "2026-08-01" || api.request.EndDate != "2026-08-01" || len(response.List) != 1 || response.List[0].MsgID != "msg_1" {
		t.Fatalf("request=%#v response=%#v", api.request, response)
	}
}

func TestWechatOfficialAnalyticsProviderUsesAuthorizerAndNeverFallsBackAfterLinkExists(t *testing.T) {
	authorization := &model.WechatOpenPlatformAuthorization{
		ProjectID: "project-1", AuthorizerAppID: "wx-authorizer", Status: model.WechatAuthorizationStatusConnected,
		GrantedScopes: []int{wechatArticleAnalyticsScope},
	}
	var authorizedCalls, legacyCalls int
	api := &fakeWechatArticleDetailAPI{}
	provider := &WechatOfficialAnalyticsProvider{
		authorizationLookup: func(context.Context, string) (*model.WechatOpenPlatformAuthorization, error) {
			return authorization, nil
		},
		legacyAPIFactory: func(context.Context, *model.Project) (WechatArticleDetailAPI, error) {
			legacyCalls++
			return nil, ErrWechatCredentialsMissing
		},
		openPlatformAPIFactory: func(_ context.Context, _ *model.Project, got *model.WechatOpenPlatformAuthorization) (WechatArticleDetailAPI, error) {
			authorizedCalls++
			if got.AuthorizerAppID != "wx-authorizer" {
				t.Fatalf("authorizer app id = %q", got.AuthorizerAppID)
			}
			return api, nil
		},
	}
	provider.apiFactory = provider.officialAPI
	if _, err := provider.FetchArticleTotalDetail(context.Background(), &model.Project{ID: "project-1"}, "2026-08-01"); err != nil {
		t.Fatal(err)
	}
	if authorizedCalls != 1 || legacyCalls != 0 {
		t.Fatalf("authorized calls=%d legacy calls=%d, want 1 and 0", authorizedCalls, legacyCalls)
	}

	authorization.Status = model.WechatAuthorizationStatusRevoked
	if _, err := provider.FetchArticleTotalDetail(context.Background(), &model.Project{ID: "project-1"}, "2026-08-01"); !errors.Is(err, ErrWechatAuthorizerInactive) {
		t.Fatalf("revoked authorization error = %v", err)
	}
	if legacyCalls != 0 {
		t.Fatalf("revoked authorization fell back to legacy credentials %d times", legacyCalls)
	}
}

func TestWechatOfficialAnalyticsProviderRequiresArticleAnalyticsScope(t *testing.T) {
	legacyCalls := 0
	provider := &WechatOfficialAnalyticsProvider{
		authorizationLookup: func(context.Context, string) (*model.WechatOpenPlatformAuthorization, error) {
			return &model.WechatOpenPlatformAuthorization{Status: model.WechatAuthorizationStatusConnected, GrantedScopes: []int{11}}, nil
		},
		legacyAPIFactory: func(context.Context, *model.Project) (WechatArticleDetailAPI, error) {
			legacyCalls++
			return nil, nil
		},
	}
	provider.apiFactory = provider.officialAPI
	if _, err := provider.FetchArticleTotalDetail(context.Background(), &model.Project{ID: "project-1"}, "2026-08-01"); !errors.Is(err, ErrWechatAnalyticsCapabilityMissing) {
		t.Fatalf("missing scope error = %v", err)
	}
	if legacyCalls != 0 {
		t.Fatalf("missing analytics scope fell back %d times", legacyCalls)
	}
}
