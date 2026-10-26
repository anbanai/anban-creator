package wechat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	wechatcache "github.com/silenceper/wechat/v2/cache"
	"github.com/silenceper/wechat/v2/officialaccount"
	wechatconfig "github.com/silenceper/wechat/v2/officialaccount/config"
	"github.com/silenceper/wechat/v2/officialaccount/freepublish"
)

type failingWechatAccessTokenHandle struct{ err error }

func (h failingWechatAccessTokenHandle) GetAccessToken() (string, error) { return "", h.err }

func TestOpenPlatformServiceUsesServerOwnedAuthorizerAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("access_token") != "authorizer-access" {
			t.Errorf("access_token = %q", r.URL.Query().Get("access_token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"media_id":"draft-media"}`))
	}))
	defer server.Close()

	oa := officialaccount.NewOfficialAccount(&wechatconfig.Config{Cache: wechatcache.NewMemory()})
	service := NewServiceWithOfficialAccount(oa, func(context.Context) (string, error) {
		return "authorizer-access", nil
	}, nil)
	api := NewOfficialAPI(server.Client(), server.URL, func(ctx context.Context) (string, error) {
		return service.accessToken(ctx)
	})
	response, err := api.AddDraft(context.Background(), DraftAddRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.MediaID != "draft-media" {
		t.Fatalf("draft media ID = %q", response.MediaID)
	}
}

func TestOpenPlatformServiceInjectsServerTokenIntoSDKDraftAndMaterialClients(t *testing.T) {
	legacyTokenErr := errors.New("SDK credential token source must not be used")
	oa := officialaccount.NewOfficialAccount(&wechatconfig.Config{AppID: "wx-authorizer", Cache: wechatcache.NewMemory()})
	oa.SetAccessTokenHandle(failingWechatAccessTokenHandle{err: legacyTokenErr})
	called := 0
	service := NewServiceWithOfficialAccount(oa, func(context.Context) (string, error) {
		called++
		return "authorizer-access", nil
	}, nil)

	token, err := service.getOfficialAccount().GetDraft().GetAccessToken()
	if err != nil || token != "authorizer-access" {
		t.Fatalf("draft SDK access token = %q, %v", token, err)
	}
	token, err = service.getOfficialAccount().GetMaterial().GetAccessToken()
	if err != nil || token != "authorizer-access" {
		t.Fatalf("material SDK access token = %q, %v", token, err)
	}
	if called != 2 {
		t.Fatalf("Server token source calls = %d, want one call per SDK operation", called)
	}
}

func TestMapPublishedItemsIncludesEveryArticleInAMultiArticlePublish(t *testing.T) {
	items := []freepublish.ArticleListItem{{
		ArticleID:  "publish-1",
		UpdateTime: 1723600000,
		Content: freepublish.ArticleListContent{NewsItem: []freepublish.Article{
			{Title: "头条", URL: "https://mp.weixin.qq.com/s/first"},
			{Title: "次条", URL: "https://mp.weixin.qq.com/s/second"},
		}},
	}}

	got := mapPublishedItems(items)
	if len(got) != 2 {
		t.Fatalf("item count = %d, want 2", len(got))
	}
	if got[1].ArticleID != "publish-1" || got[1].Title != "次条" || got[1].URL != "https://mp.weixin.qq.com/s/second" {
		t.Fatalf("second article = %+v", got[1])
	}
}

func TestMapOfficialPublishedItemsIncludesNewspicMediaIDs(t *testing.T) {
	items := []FreePublishBatchItem{{
		ArticleID:  "publish-picture-1",
		UpdateTime: 1723600000,
		Content: FreePublishContent{NewspicInfo: &DraftArticle{
			Title: "图片消息",
			ImageInfo: &DraftImageInfo{ImageList: []DraftImage{
				{ImageMediaID: "media-cover"},
				{ImageMediaID: "media-page-1"},
			}},
		}},
	}}

	got := mapOfficialPublishedItems(items)
	if len(got) != 1 || got[0].ArticleType != "newspic" || got[0].Title != "图片消息" {
		t.Fatalf("published picture mapping = %#v", got)
	}
	if len(got[0].ImageMediaIDs) != 2 || got[0].ImageMediaIDs[0] != "media-cover" || got[0].ImageMediaIDs[1] != "media-page-1" {
		t.Fatalf("published picture media IDs = %#v", got[0].ImageMediaIDs)
	}
}
