package platform

import (
	"context"
	"testing"

	appwechat "github.com/anbanai/anban-creator/app/wechat"
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
	provider := &WechatOfficialAnalyticsProvider{apiFactory: func(*model.Project) (WechatArticleDetailAPI, error) { return api, nil }}
	response, err := provider.FetchArticleTotalDetail(context.Background(), &model.Project{}, "2026-08-01")
	if err != nil {
		t.Fatal(err)
	}
	if api.request.BeginDate != "2026-08-01" || api.request.EndDate != "2026-08-01" || len(response.List) != 1 || response.List[0].MsgID != "msg_1" {
		t.Fatalf("request=%#v response=%#v", api.request, response)
	}
}
