package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/platform"
	"github.com/anbanai/anban-creator/server/repository"
)

type fakeWechatAnalyticsPlatform struct {
	article      *platform.WechatPublishedArticle
	totals       []platform.WechatArticleTotal
	err          error
	resolveCalls int
	totalCalls   int
}

func (f *fakeWechatAnalyticsPlatform) ResolvePublishedArticle(context.Context, *model.Project, string) (*platform.WechatPublishedArticle, error) {
	f.resolveCalls++
	return f.article, f.err
}

func (f *fakeWechatAnalyticsPlatform) FetchArticleTotals(context.Context, *model.Project, string) ([]platform.WechatArticleTotal, error) {
	f.totalCalls++
	return f.totals, f.err
}

func setupWechatTrackingServiceTest(t *testing.T) (*WechatTrackingService, repository.Repository, *fakeWechatAnalyticsPlatform) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	provider := &fakeWechatAnalyticsPlatform{}
	logger := zerolog.New(io.Discard)
	return NewWechatTrackingService(repo, provider, nil, &logger), repo, provider
}

func TestWechatTrackingService_BindCapturesFirstSnapshotImmediately(t *testing.T) {
	svc, repo, provider := setupWechatTrackingServiceTest(t)
	userID, taskID := createWechatTrackingFixtures(t, repo)
	now := time.Now()
	provider.article = &platform.WechatPublishedArticle{
		ArticleID: "article-1", ArticleURL: "https://mp.weixin.qq.com/s/article-1", ArticleTitle: "首次采集", PublishedAt: now,
	}
	provider.totals = []platform.WechatArticleTotal{{
		MsgID: "article-1", Title: "首次采集",
		Details: []platform.WechatArticleMetric{{StatDate: now.In(wechatAnalyticsLocation).Format("2006-01-02"), IntPageReadCount: 88, ShareCount: 6}},
	}}

	if err := svc.BindTask(context.Background(), userID, taskID, "https://mp.weixin.qq.com/s/article-1"); err != nil {
		t.Fatal(err)
	}
	if provider.resolveCalls != 1 || provider.totalCalls != 1 {
		t.Fatalf("official calls = resolve:%d totals:%d, want 1 each", provider.resolveCalls, provider.totalCalls)
	}
	analytics, err := svc.GetTaskAnalytics(context.Background(), userID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Latest == nil || analytics.Latest.IntPageReadCount != 88 || analytics.Latest.ShareCount != 6 {
		t.Fatalf("latest metrics = %+v", analytics.Latest)
	}
	if analytics.Tracking == nil || analytics.Tracking.RunCount != 1 || analytics.Tracking.LastRunAt == nil {
		t.Fatalf("tracking = %+v", analytics.Tracking)
	}
}

func TestWechatTrackingService_BindImmediatelyRecordsWaitingWhenOfficialDataIsNotReady(t *testing.T) {
	svc, repo, provider := setupWechatTrackingServiceTest(t)
	userID, taskID := createWechatTrackingFixtures(t, repo)
	provider.article = &platform.WechatPublishedArticle{
		ArticleID: "article-1", ArticleURL: "https://mp.weixin.qq.com/s/article-1", ArticleTitle: "等待次日数据", PublishedAt: time.Now(),
	}

	if err := svc.BindTask(context.Background(), userID, taskID, "https://mp.weixin.qq.com/s/article-1"); err != nil {
		t.Fatal(err)
	}
	if provider.totalCalls != 1 {
		t.Fatalf("official totals calls = %d, want 1 synchronous call", provider.totalCalls)
	}
	analytics, err := svc.GetTaskAnalytics(context.Background(), userID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Latest != nil {
		t.Fatalf("latest metrics = %+v, want nil before official data is ready", analytics.Latest)
	}
	if analytics.Tracking == nil || analytics.Tracking.Status != model.WechatTrackingStatusWaitingData || analytics.Tracking.LastRunAt == nil {
		t.Fatalf("tracking = %+v", analytics.Tracking)
	}
}

func createWechatTrackingFixtures(t *testing.T, repo repository.Repository) (string, string) {
	t.Helper()
	ctx := context.Background()
	userID, projectID, taskID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: uuid.NewString()[:12]}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	return userID, taskID
}

func TestWechatTrackingService_RebindReplacesPreviousArticleSnapshots(t *testing.T) {
	svc, repo, provider := setupWechatTrackingServiceTest(t)
	userID, taskID := createWechatTrackingFixtures(t, repo)
	ctx := context.Background()
	now := time.Now()

	provider.article = &platform.WechatPublishedArticle{
		ArticleID: "article-a", ArticleURL: "https://mp.weixin.qq.com/s/article-a", ArticleTitle: "Article A", PublishedAt: now,
	}
	provider.totals = []platform.WechatArticleTotal{{
		MsgID: "article-a", Title: "Article A",
		Details: []platform.WechatArticleMetric{{StatDate: now.Format("2006-01-02"), IntPageReadCount: 10}},
	}}
	if err := svc.BindTask(ctx, userID, taskID, "https://mp.weixin.qq.com/s/article-a"); err != nil {
		t.Fatal(err)
	}

	provider.article = &platform.WechatPublishedArticle{
		ArticleID: "article-b", ArticleURL: "https://mp.weixin.qq.com/s/article-b", ArticleTitle: "Article B", PublishedAt: now,
	}
	provider.totals = []platform.WechatArticleTotal{{
		MsgID: "article-b", Title: "Article B",
		Details: []platform.WechatArticleMetric{{StatDate: now.Format("2006-01-02"), IntPageReadCount: 20}},
	}}
	if err := svc.BindTask(ctx, userID, taskID, "https://mp.weixin.qq.com/s/article-b"); err != nil {
		t.Fatal(err)
	}

	analytics, err := svc.GetTaskAnalytics(ctx, userID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Tracking == nil || analytics.Tracking.ArticleTitle != "Article B" {
		t.Fatalf("tracking = %+v", analytics.Tracking)
	}
	if analytics.Latest == nil || analytics.Latest.IntPageReadCount != 20 {
		t.Fatalf("latest = %+v", analytics.Latest)
	}
	if len(analytics.Series) != 1 || analytics.Series[0].IntPageReadCount != 20 {
		t.Fatalf("series = %+v", analytics.Series)
	}
}

func TestWechatTrackingService_BindRejectsForeignArticleURL(t *testing.T) {
	svc, repo, _ := setupWechatTrackingServiceTest(t)
	userID, taskID := createWechatTrackingFixtures(t, repo)
	if err := svc.BindTask(context.Background(), userID, taskID, "https://example.com/s/article"); err == nil {
		t.Fatal("expected invalid URL error")
	}
}

func TestWechatTrackingService_BindDistinguishesNotFoundFromProviderFailure(t *testing.T) {
	tests := []struct {
		name         string
		providerErr  error
		wantNotFound bool
	}{
		{name: "article not found", providerErr: platform.ErrWechatPublishedArticleNotFound, wantNotFound: true},
		{name: "upstream unavailable", providerErr: errors.New("upstream timeout")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, provider := setupWechatTrackingServiceTest(t)
			userID, taskID := createWechatTrackingFixtures(t, repo)
			provider.err = tt.providerErr
			err := svc.BindTask(context.Background(), userID, taskID, "https://mp.weixin.qq.com/s/article")
			if err == nil {
				t.Fatal("expected bind error")
			}
			if got := errors.Is(err, ErrWechatArticleNotFound); got != tt.wantNotFound {
				t.Fatalf("errors.Is(ErrWechatArticleNotFound) = %v, want %v: %v", got, tt.wantNotFound, err)
			}
		})
	}
}

func TestWechatDateInWindowUsesWechatCalendarDate(t *testing.T) {
	// 16:30 UTC is already the next calendar day for WeChat's official APIs.
	now := time.Date(2026, 8, 13, 16, 30, 0, 0, time.UTC)
	if !wechatDateInWindow("2026-08-14", now) {
		t.Fatal("expected publication date to be current in Asia/Shanghai")
	}
	if wechatDateInWindow("2026-08-11", now) {
		t.Fatal("expected the three-day official data window to be closed")
	}
}
