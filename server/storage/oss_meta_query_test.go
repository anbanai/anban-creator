package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ossv2 "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/anbanai/anban-creator/server/config"
)

func TestOSSMetaQuerySemanticSearchUsesVideoAndUserPrefix(t *testing.T) {
	var body string
	var mode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		mode = r.URL.Query().Get("mode")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<MetaQuery><Files><File><Filename>assets/users/user-1/a.mp4</Filename><Size>12</Size><ETag>"etag"</ETag><FileModifiedTime>2026-10-31T12:00:00Z</FileModifiedTime><Insights><Video><Caption>店员制作咖啡</Caption><Description>视频展示店员在吧台制作咖啡的过程。</Description></Video></Insights></File></Files></MetaQuery>`)
	}))
	defer srv.Close()

	provider, err := NewOSSMetaQueryProvider(config.StorageConfig{
		Endpoint: srv.URL, AccessKeyID: "ak", AccessKeySecret: "sk", BucketName: "bucket", Region: "cn-hangzhou",
		AIContentAwareness: config.AIContentAwarenessConfig{Enabled: true, VideoEnabled: true, QueryTimeout: time.Second, MaxResults: 10},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.Search(context.Background(), "有人制作咖啡", "assets/users/user-1/", "video", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ObjectKey != "assets/users/user-1/a.mp4" || got[0].Summary != "店员制作咖啡" || got[0].Description == "" {
		t.Fatalf("results = %#v", got)
	}
	if mode != "semantic" || !strings.Contains(body, "<MediaType>video</MediaType>") || !strings.Contains(body, "assets/users/user-1/") {
		t.Fatalf("request body = %s", body)
	}
}

func TestOSSMetaQueryLookupReturnsPendingWhenObjectIsNotIndexed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<MetaQuery><Files></Files></MetaQuery>`)
	}))
	defer srv.Close()
	provider, err := NewOSSMetaQueryProvider(config.StorageConfig{Endpoint: srv.URL, AccessKeyID: "ak", AccessKeySecret: "sk", BucketName: "bucket", AIContentAwareness: config.AIContentAwarenessConfig{Enabled: true, VideoEnabled: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Lookup(context.Background(), "assets/users/user-1/a.mp4", "video")
	if !errors.Is(err, ErrSemanticIndexPending) {
		t.Fatalf("lookup error = %v, want pending", err)
	}
}

func TestMapMetaQueryFilesFallsBackToVideoCaption(t *testing.T) {
	caption := "店员制作咖啡"
	key := "assets/users/user-1/a.mp4"
	got := mapMetaQueryFiles([]ossv2.MetaQueryFile{{Filename: &key, Insights: &ossv2.MetaQueryFileInsights{Video: &ossv2.MetaQueryFileInsightsVideo{Caption: &caption}}}})
	if len(got) != 1 || got[0].Summary != caption || got[0].Description != caption {
		t.Fatalf("mapped caption-only result = %#v", got)
	}
}
