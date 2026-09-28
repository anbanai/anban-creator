package service

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

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
	db, err := gorm.Open(sqlite.Open("file:trend-test?mode=memory&cache=shared"), &gorm.Config{})
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
