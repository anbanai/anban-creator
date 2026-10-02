package worldtree

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientGetVideoInfoByURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/wechat/video/getVideoInfoByUrl" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["key"] != "secret-key" || !strings.Contains(body["url"], "channels.weixin.qq.com") {
			t.Fatalf("body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{"object_id":"video-1","url":"https://channels.weixin.qq.com/web/pages/feed?oid=abc","title":"测试视频","like_count":12,"fav_count":3,"forward_count":4,"comment_count":5}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret-key", time.Second)
	info, err := client.GetVideoInfoByURL(context.Background(), "https://channels.weixin.qq.com/web/pages/feed?oid=abc")
	if err != nil {
		t.Fatal(err)
	}
	if info.ObjectID != "video-1" || info.LikeCount != 12 || info.FavoriteCount != 3 {
		t.Fatalf("info = %+v", info)
	}
}

func TestClientRequiresKeyAndDoesNotExposeItInErrors(t *testing.T) {
	if _, err := NewClient("", "", time.Second).GetVideoInfoByURL(context.Background(), "https://weixin.qq.com/sph/test"); err != ErrNotConfigured {
		t.Fatalf("error = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":401,"msg":"invalid key"}`))
	}))
	defer server.Close()
	secret := "do-not-leak"
	_, err := NewClient(server.URL, secret, time.Second).GetVideoInfoByURL(context.Background(), "https://weixin.qq.com/sph/test")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %v", err)
	}
}
