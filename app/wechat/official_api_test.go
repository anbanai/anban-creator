package wechat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficialAPIUsesOfficialPublicationEndpointsAndStringIdentifiers(t *testing.T) {
	tests := []struct {
		name     string
		call     func(context.Context, *OfficialAPI) error
		path     string
		body     string
		response string
	}{
		{
			name: "draft add", path: "/cgi-bin/draft/add", body: `{"articles":[{"title":"title"}]}`,
			response: `{"media_id":"draft-media"}`,
			call: func(ctx context.Context, api *OfficialAPI) error {
				_, err := api.AddDraft(ctx, DraftAddRequest{Articles: []DraftArticle{{Title: "title"}}})
				return err
			},
		},
		{
			name: "draft batchget", path: "/cgi-bin/draft/batchget", body: `{"offset":0,"count":20,"no_content":false}`,
			response: `{"total_count":1,"item_count":1,"item":[{"media_id":"draft-media","content":{"news_item":[{"title":"title"}]}}]}`,
			call: func(ctx context.Context, api *OfficialAPI) error {
				_, err := api.BatchGetDrafts(ctx, DraftBatchGetRequest{Count: 20})
				return err
			},
		},
		{
			name: "freepublish submit", path: "/cgi-bin/freepublish/submit", body: `{"media_id":"draft-media"}`,
			response: `{"publish_id":"publish-123","msg_data_id":"submit-msg-data-123"}`,
			call: func(ctx context.Context, api *OfficialAPI) error {
				result, err := api.SubmitFreePublish(ctx, FreePublishSubmitRequest{MediaID: "draft-media"})
				if err == nil && (result.PublishID != "publish-123" || result.MsgDataID != "submit-msg-data-123") {
					t.Fatalf("submit result = %#v", result)
				}
				return err
			},
		},
		{
			name: "freepublish get", path: "/cgi-bin/freepublish/get", body: `{"publish_id":"publish-123"}`,
			response: `{"publish_id":"publish-123","publish_status":0,"article_id":"article-1","article_detail":{"count":1,"item":[{"idx":1,"article_url":"https://mp.weixin.qq.com/s/example"}]}}`,
			call: func(ctx context.Context, api *OfficialAPI) error {
				result, err := api.GetFreePublish(ctx, FreePublishGetRequest{PublishID: "publish-123"})
				if err == nil && result.PublishID != "publish-123" {
					t.Fatalf("publish id = %q", result.PublishID)
				}
				return err
			},
		},
		{
			name: "freepublish batchget", path: "/cgi-bin/freepublish/batchget", body: `{"offset":0,"count":20,"no_content":false}`,
			response: `{"total_count":1,"item_count":1,"item":[{"article_id":"article-1","update_time":123,"content":{"news_item":[{"title":"title","author":"author","digest":"digest","content":"<p>body</p>","thumb_media_id":"thumb","url":"https://mp.weixin.qq.com/s/article"}]}}]}`,
			call: func(ctx context.Context, api *OfficialAPI) error {
				result, err := api.BatchGetFreePublishes(ctx, FreePublishBatchGetRequest{Count: 20})
				if err == nil {
					article := result.Items[0].Content.NewsItems[0]
					if article.URL == "" || article.Author == "" || article.Digest == "" || article.Content == "" || article.ThumbMediaID == "" || result.Items[0].UpdateTime != 123 {
						t.Fatalf("published article contract = %#v", result.Items[0])
					}
				}
				return err
			},
		},
		{
			name: "article total detail", path: "/datacube/getarticletotaldetail", body: `{"begin_date":"2026-08-02","end_date":"2026-08-02"}`,
			response: `{"is_delay":true,"list":[{"ref_date":"2026-08-02","msgid":"published-msgid_1","content_url":"https://mp.weixin.qq.com/s/published-example","title":"title","publish_type":1,"detail_list":[{"stat_date":"2026-08-02","read_user":12,"read_user_source":[{"source_name":"公众号会话","read_user":3}],"share_user":4,"zaikan_user":5,"like_user":6,"comment_count":7,"collection_user":8,"praise_money":9,"read_subscribe_user":10,"read_delivery_rate":0.11,"read_finish_rate":0.12,"read_avg_activetime":13.5,"read_jump_position":{"first_screen":14,"article_end":2}}]}]}`,
			call: func(ctx context.Context, api *OfficialAPI) error {
				result, err := api.GetArticleTotalDetail(ctx, ArticleTotalDetailRequest{BeginDate: "2026-08-02", EndDate: "2026-08-02"})
				if err != nil {
					return err
				}
				item := result.List[0]
				metric := item.DetailList[0]
				if !result.IsDelay || item.MsgID != "published-msgid_1" || item.ContentURL != "https://mp.weixin.qq.com/s/published-example" || item.PublishType != 1 || metric.ReadUser != 12 || metric.ReadFinishRate != 0.12 || len(metric.ReadUserSource) != 1 || metric.ReadUserSource[0]["source_name"] != "公众号会话" || metric.ReadUserSource[0]["read_user"] != float64(3) || metric.ReadJumpPosition["first_screen"] != float64(14) || metric.ReadJumpPosition["article_end"] != float64(2) {
					t.Fatalf("detail contract = %#v", result.List[0])
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Fatalf("path = %q, want %q", r.URL.Path, tt.path)
				}
				if r.URL.Query().Get("access_token") != "token" {
					t.Fatalf("token = %q", r.URL.Query().Get("access_token"))
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				var got, want any
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if err := json.Unmarshal([]byte(tt.body), &want); err != nil {
					t.Fatal(err)
				}
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(want)
				if string(gotJSON) != string(wantJSON) {
					t.Fatalf("body = %s, want %s", gotJSON, wantJSON)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()
			api := NewOfficialAPI(server.Client(), server.URL, func(context.Context) (string, error) { return "token", nil })
			if err := tt.call(context.Background(), api); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOfficialAPIReturnsTypedWechatError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"errcode":48001,"errmsg":"api unauthorized"}`)
	}))
	defer server.Close()
	api := NewOfficialAPI(server.Client(), server.URL, func(context.Context) (string, error) { return "token", nil })
	_, err := api.SubmitFreePublish(context.Background(), FreePublishSubmitRequest{MediaID: "draft"})
	if err == nil || !strings.Contains(err.Error(), "48001") {
		t.Fatalf("error = %v, want WeChat 48001", err)
	}
}

func TestFreePublishStatusContract(t *testing.T) {
	tests := []struct {
		status int
		want   int
	}{
		{FreePublishStatusSucceeded, 0},
		{FreePublishStatusPublishing, 1},
		{FreePublishStatusOriginalFailed, 2},
		{FreePublishStatusFailed, 3},
		{FreePublishStatusAuditRejected, 4},
		{FreePublishStatusUserDeleted, 5},
		{FreePublishStatusSystemBanned, 6},
	}
	for _, tt := range tests {
		if tt.status != tt.want {
			t.Fatalf("official publish status = %d, want %d", tt.status, tt.want)
		}
	}
}
