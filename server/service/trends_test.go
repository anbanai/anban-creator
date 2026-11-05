package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestRedFoxTrendFetcherNormalizesResponseAndUsesAPIKey(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-API-KEY")
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":2000,"data":[{"keyword":"热点","hotSpotList":[{"platName":"抖音","title":" A ","url":"https://example.test/a","maxHotScore":12345},{"platName":"微博","title":"B","url":"https://example.test/b","maxHotScore":99}]}]}`)
	}))
	defer server.Close()

	fetcher := NewRedFoxTrendFetcher(config.TrendsRedFoxConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
	items, source, err := fetcher.Fetch(context.Background(), "douyin")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "secret" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if source != "redfox" || len(items) != 1 || items[0].Title != "A" || items[0].Hot != "12345" || items[0].Rank != 1 {
		t.Fatalf("source=%q items=%#v", source, items)
	}
}

func TestRedFoxTrendFetcherSharesOneResponseAcrossPlatforms(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":2000,"data":[{"keyword":"热点","hotSpotList":[{"platName":"抖音","title":"A","url":"https://example.test/a","maxHotScore":1},{"platName":"微博","title":"B","url":"https://example.test/b","maxHotScore":2}]}]}`)
	}))
	defer server.Close()
	fetcher := NewRedFoxTrendFetcher(config.TrendsRedFoxConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
	if _, _, err := fetcher.Fetch(context.Background(), "douyin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fetcher.Fetch(context.Background(), "weibo"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls)
	}
}

func TestRedFoxTrendFetcherSharesOneResponseForConcurrentPlatforms(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":2000,"data":[{"hotSpotList":[{"platName":"抖音","title":"A","maxHotScore":1},{"platName":"微博","title":"B","maxHotScore":2}]}]}`)
	}))
	defer server.Close()
	fetcher := NewRedFoxTrendFetcher(config.TrendsRedFoxConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
	var wg sync.WaitGroup
	for _, platform := range []string{"douyin", "weibo"} {
		wg.Add(1)
		go func(platform string) {
			defer wg.Done()
			if _, _, err := fetcher.Fetch(context.Background(), platform); err != nil {
				t.Errorf("Fetch(%s): %v", platform, err)
			}
		}(platform)
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls)
	}
}

func TestRedFoxTrendFetcherRejectsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"code":3210,"msg":"insufficient credits"}`)
	}))
	defer server.Close()
	fetcher := NewRedFoxTrendFetcher(config.TrendsRedFoxConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
	_, _, err := fetcher.Fetch(context.Background(), "douyin")
	if err == nil || !strings.Contains(err.Error(), "insufficient credits") {
		t.Fatalf("error = %v", err)
	}
}

func TestRedFoxTrendFetcherExpiresInMemoryCache(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"code":2000,"data":[{"hotSpotList":[{"platName":"抖音","title":"A%d","maxHotScore":1}]}]}`, calls)
	}))
	defer server.Close()
	fetcher := NewRedFoxTrendFetcherWithTTL(config.TrendsRedFoxConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second}, time.Millisecond)
	first, _, err := fetcher.Fetch(context.Background(), "douyin")
	if err != nil || first[0].Title != "A1" {
		t.Fatalf("first fetch = %#v, %v", first, err)
	}
	time.Sleep(3 * time.Millisecond)
	second, _, err := fetcher.Fetch(context.Background(), "douyin")
	if err != nil || second[0].Title != "A2" || calls != 2 {
		t.Fatalf("expired fetch = %#v, %v, calls=%d", second, err, calls)
	}
}

func TestRedFoxTrendFetcherRejectsEmptySupportedPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":2000,"data":[{"hotSpotList":[]}]}`)
	}))
	defer server.Close()
	fetcher := NewRedFoxTrendFetcher(config.TrendsRedFoxConfig{Endpoint: server.URL, APIKey: "secret", Timeout: time.Second})
	if _, _, err := fetcher.Fetch(context.Background(), "douyin"); err == nil || !strings.Contains(err.Error(), "no supported items") {
		t.Fatalf("empty response error = %v", err)
	}
}

type trendTestFetcher struct {
	calls int
	items []TrendItem
	err   error
}

func (f *trendTestFetcher) Fetch(context.Context, string) ([]TrendItem, string, error) {
	f.calls++
	return f.items, "test", f.err
}

func trendTestService(t *testing.T, f TrendFetcher, ttl time.Duration) (*TrendService, repository.Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:trend-test-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	log := zerolog.New(io.Discard)
	return NewTrendServiceWithFetcher(repo, nil, ttl, f, &log), repo
}

func TestParseTrendPayloadVariants(t *testing.T) {
	bodies := []string{"[{\"word\":\"A\",\"hot_value\":12,\"link\":\"https://a\"}]", "{\"list\":[{\"title\":\"A-root-list\"}]}", "{\"data\":[{\"title\":\"B\",\"num\":\"9\"}]}", "{\"data\":{\"list\":[{\"name\":\"C\",\"mobil_url\":\"https://c\"}]}}", "{\"data\":{\"data\":[{\"keyword\":\"D\"}]}}"}
	for _, body := range bodies {
		items, err := parseTrendPayload([]byte(body))
		if err != nil || len(items) != 1 {
			t.Fatalf("parse %s: %v %#v", body, err, items)
		}
	}
}

func TestTrendServiceTTLAndStaleFallback(t *testing.T) {
	f := &trendTestFetcher{items: []TrendItem{{Title: "fresh"}}}
	svc, repo := trendTestService(t, f, time.Hour)
	first, err := svc.List(context.Background(), []string{"weibo"}, 12, false)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 || first.Items[0].Items[0].Title != "fresh" {
		t.Fatalf("first=%+v calls=%d", first, f.calls)
	}
	second, _ := svc.List(context.Background(), []string{"weibo"}, 12, false)
	if f.calls != 1 || second.Items[0].Stale {
		t.Fatalf("expected ttl hit: calls=%d result=%+v", f.calls, second)
	}
	row, _ := repo.TrendSnapshots().FindByPlatform(context.Background(), "weibo")
	row.FetchedAt = time.Now().Add(-2 * time.Hour)
	_ = repo.TrendSnapshots().Upsert(context.Background(), row)
	previousFetchedAt := row.FetchedAt
	previousAttempt := row.LastAttemptedAt
	f.err = io.ErrUnexpectedEOF
	result, _ := svc.List(context.Background(), []string{"weibo"}, 12, false)
	if !result.Items[0].Stale || result.Items[0].LastError == "" {
		t.Fatalf("expected stale error: %+v", result)
	}
	row, _ = repo.TrendSnapshots().FindByPlatform(context.Background(), "weibo")
	if !row.FetchedAt.Equal(previousFetchedAt) || !row.LastAttemptedAt.After(previousAttempt) || row.LastError == "" {
		t.Fatalf("failed refresh must preserve fetched_at and record attempt: %+v", row)
	}
}

func TestTrendServiceIncludesProviderMetadata(t *testing.T) {
	f := &trendTestFetcher{items: []TrendItem{{Title: "fresh"}}}
	svc, _ := trendTestService(t, f, time.Hour)
	result, err := svc.List(context.Background(), []string{"weibo"}, 12, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != "default" {
		t.Fatalf("provider = %q, want default", result.Provider)
	}
}

func TestTrendServiceDoesNotServeSnapshotFromAnotherProvider(t *testing.T) {
	f := &trendTestFetcher{items: []TrendItem{{Title: "new"}}, err: io.ErrUnexpectedEOF}
	svc, repo := trendTestService(t, f, time.Hour)
	svc.provider = "redfox"
	fetchedAt := time.Now().UTC()
	if err := repo.TrendSnapshots().Upsert(context.Background(), &model.TrendSnapshot{
		Platform: "weibo", Items: []byte(`[{"title":"old"}]`), FetchedAt: fetchedAt,
		LastAttemptedAt: fetchedAt, Source: "60s",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.List(context.Background(), []string{"weibo"}, 12, false)
	if err != nil {
		t.Fatal(err)
	}
	item := result.Items[0]
	if f.calls != 1 || !item.Stale || item.Items[0].Title != "old" || item.LastError == "" {
		t.Fatalf("provider switch must refresh and mark the old snapshot stale: calls=%d result=%+v", f.calls, result)
	}
}

func TestTrendServiceLimitAndForceRefresh(t *testing.T) {
	f := &trendTestFetcher{items: []TrendItem{{Title: "a"}, {Title: "b"}}}
	svc, _ := trendTestService(t, f, time.Hour)
	r, _ := svc.List(context.Background(), []string{"weibo"}, 1, false)
	if len(r.Items[0].Items) != 1 {
		t.Fatal("limit not applied")
	}
	_, _ = svc.List(context.Background(), []string{"weibo"}, 2, true)
	if f.calls != 2 {
		t.Fatalf("force refresh calls=%d", f.calls)
	}
}

func TestTrendServiceReturnsEmptyItemsWhenInitialRefreshFails(t *testing.T) {
	f := &trendTestFetcher{err: io.ErrUnexpectedEOF}
	svc, _ := trendTestService(t, f, time.Hour)

	result, err := svc.List(context.Background(), []string{"toutiao"}, 12, false)
	if err != nil {
		t.Fatalf("list should expose stale result instead of failing: %v", err)
	}
	if result.Items[0].Items == nil {
		t.Fatalf("expected an empty items array, got nil: %+v", result.Items[0])
	}
	if len(result.Items[0].Items) != 0 {
		t.Fatalf("expected no items, got %+v", result.Items[0].Items)
	}
}
