package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/worldtree"
)

type fakeChannelsPlatform struct {
	info  *worldtree.VideoInfo
	err   error
	calls int
}

type failingChannelsEnqueuer struct{ err error }

func (f *failingChannelsEnqueuer) Enqueue(string, []byte) error { return f.err }
func (f *failingChannelsEnqueuer) EnqueueIn(string, []byte, time.Duration) error {
	return f.err
}

func (f *fakeChannelsPlatform) GetVideoInfoByURL(context.Context, string) (*worldtree.VideoInfo, error) {
	f.calls++
	return f.info, f.err
}

func setupChannelsTrackingServiceTest(t *testing.T) (*ChannelsTrackingService, repository.Repository, *fakeChannelsPlatform, *fakeTrackingEnqueuer) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })
	provider := &fakeChannelsPlatform{}
	enqueuer := &fakeTrackingEnqueuer{}
	logger := zerolog.New(io.Discard)
	return NewChannelsTrackingService(repo, provider, enqueuer, &logger), repo, provider, enqueuer
}

func createChannelsTrackingFixtures(t *testing.T, repo repository.Repository, taskType string) (string, string) {
	t.Helper()
	ctx := context.Background()
	userID, projectID, taskID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: uuid.NewString()[:12]}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformMontage, Name: "Video", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: taskType, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	return userID, taskID
}

func TestNormalizeChannelsVideoURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"https://channels.weixin.qq.com/web/pages/feed?oid=abc&utm_source=share#x", "https://channels.weixin.qq.com/web/pages/feed?oid=abc"},
		{"https://channels.weixin.qq.com/web/pages/feed?eid=export%2Fabc&foo=bar", "https://channels.weixin.qq.com/web/pages/feed?eid=export%2Fabc"},
		{"https://weixin.qq.com/sph/AoaRLaRI8?from=share", "https://weixin.qq.com/sph/AoaRLaRI8"},
	}
	for _, tt := range tests {
		got, err := NormalizeChannelsVideoURL(tt.raw)
		if err != nil || got != tt.want {
			t.Errorf("NormalizeChannelsVideoURL(%q) = %q, %v", tt.raw, got, err)
		}
	}
	for _, raw := range []string{"http://weixin.qq.com/sph/x", "https://example.com/sph/x", "https://channels.weixin.qq.com/web/pages/feed", "https://weixin.qq.com/not-sph/x"} {
		if _, err := NormalizeChannelsVideoURL(raw); !errors.Is(err, ErrChannelsVideoURLInvalid) {
			t.Errorf("NormalizeChannelsVideoURL(%q) error = %v", raw, err)
		}
	}
}

func TestChannelsTrackingBindCapturesImmediately(t *testing.T) {
	svc, repo, provider, enqueuer := setupChannelsTrackingServiceTest(t)
	userID, taskID := createChannelsTrackingFixtures(t, repo, model.PlatformMontage)
	provider.info = &worldtree.VideoInfo{
		ObjectID: "video-1", URL: "https://channels.weixin.qq.com/web/pages/feed?oid=abc", Title: "首次采集",
		AuthorName: "作者", CreateTime: time.Now().Add(-time.Hour).Unix(), LikeCount: 12, FavoriteCount: 3, CommentCount: 4, ForwardCount: 5,
	}
	if err := svc.BindTask(context.Background(), userID, taskID, provider.info.URL); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	analytics, err := svc.GetTaskAnalytics(context.Background(), userID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Latest == nil || analytics.Latest.LikeCount != 12 || analytics.Latest.FavoriteCount != 3 {
		t.Fatalf("latest = %+v", analytics.Latest)
	}
	if analytics.Tracking == nil || analytics.Tracking.RunCount != 1 || analytics.Tracking.ProviderName != "世界树科技" {
		t.Fatalf("tracking = %+v", analytics.Tracking)
	}
	if len(enqueuer.delayed) != 1 || enqueuer.delayed[0] != ChannelsCaptureMetricsTaskType {
		t.Fatalf("delayed tasks = %#v", enqueuer.delayed)
	}
}

func TestChannelsTrackingBindSucceedsWhenDelayedEnqueueFails(t *testing.T) {
	svc, repo, provider, _ := setupChannelsTrackingServiceTest(t)
	userID, taskID := createChannelsTrackingFixtures(t, repo, model.PlatformMontage)
	provider.info = &worldtree.VideoInfo{ObjectID: "video-1", LikeCount: 12}
	svc.enqueuer = &failingChannelsEnqueuer{err: errors.New("redis unavailable")}

	if err := svc.BindTask(context.Background(), userID, taskID, "https://weixin.qq.com/sph/video-1"); err != nil {
		t.Fatal(err)
	}
	analytics, err := svc.GetTaskAnalytics(context.Background(), userID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Latest == nil || analytics.Latest.LikeCount != 12 {
		t.Fatalf("latest = %+v", analytics.Latest)
	}
	if analytics.Tracking == nil || analytics.Tracking.NextRunAt == nil || analytics.Tracking.LastError == "" {
		t.Fatalf("tracking = %+v", analytics.Tracking)
	}
}

func TestChannelsTrackingRebindReplacesSnapshots(t *testing.T) {
	svc, repo, provider, _ := setupChannelsTrackingServiceTest(t)
	userID, taskID := createChannelsTrackingFixtures(t, repo, model.TaskTypeLiveSlicer)
	provider.info = &worldtree.VideoInfo{ObjectID: "video-a", Title: "A", LikeCount: 1}
	if err := svc.BindTask(context.Background(), userID, taskID, "https://weixin.qq.com/sph/video-a"); err != nil {
		t.Fatal(err)
	}
	provider.info = &worldtree.VideoInfo{ObjectID: "video-b", Title: "B", LikeCount: 20}
	if err := svc.BindTask(context.Background(), userID, taskID, "https://weixin.qq.com/sph/video-b"); err != nil {
		t.Fatal(err)
	}
	analytics, err := svc.GetTaskAnalytics(context.Background(), userID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Tracking == nil || analytics.Tracking.VideoTitle != "B" || analytics.Latest == nil || analytics.Latest.LikeCount != 20 || len(analytics.Series) != 1 {
		t.Fatalf("analytics = %+v", analytics)
	}
}

func TestChannelsTrackingRecoversLifecycleWithoutRefetchingSameDay(t *testing.T) {
	svc, repo, provider, enqueuer := setupChannelsTrackingServiceTest(t)
	userID, taskID := createChannelsTrackingFixtures(t, repo, model.PlatformMontage)
	provider.info = &worldtree.VideoInfo{ObjectID: "video-1", LikeCount: 8}
	if err := svc.BindTask(context.Background(), userID, taskID, "https://weixin.qq.com/sph/video-1"); err != nil {
		t.Fatal(err)
	}
	tracking, err := repo.ChannelsTrackings().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	tracking.LastRunAt = nil
	due := time.Now().Add(-time.Minute)
	tracking.NextRunAt = &due
	tracking.RunCount = 0
	if err := repo.ChannelsTrackings().Update(context.Background(), tracking); err != nil {
		t.Fatal(err)
	}
	beforeCalls := provider.calls
	beforeEnqueues := len(enqueuer.delayed)
	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != beforeCalls {
		t.Fatalf("provider calls = %d, want %d", provider.calls, beforeCalls)
	}
	recovered, err := repo.ChannelsTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.LastRunAt == nil || recovered.NextRunAt == nil || recovered.RunCount != 1 {
		t.Fatalf("recovered tracking = %+v", recovered)
	}
	if len(enqueuer.delayed) != beforeEnqueues+1 {
		t.Fatalf("delayed tasks = %#v", enqueuer.delayed)
	}
}

func TestChannelsTrackingSkipsCaptureWhileAnotherWorkerHoldsClaim(t *testing.T) {
	svc, repo, provider, _ := setupChannelsTrackingServiceTest(t)
	userID, taskID := createChannelsTrackingFixtures(t, repo, model.PlatformMontage)
	provider.info = &worldtree.VideoInfo{ObjectID: "video-1", LikeCount: 8}
	if err := svc.BindTask(context.Background(), userID, taskID, "https://weixin.qq.com/sph/video-1"); err != nil {
		t.Fatal(err)
	}
	tracking, err := repo.ChannelsTrackings().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ChannelsMetricSnapshots().DeleteByTrackingID(context.Background(), tracking.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ChannelsTrackings().TryClaimCapture(context.Background(), tracking.ID, "worker-a", time.Now(), time.Now().Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	beforeCalls := provider.calls
	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != beforeCalls {
		t.Fatalf("provider calls = %d, want %d", provider.calls, beforeCalls)
	}
}

func TestChannelsTrackingClaimReleaseIsTokenScoped(t *testing.T) {
	_, repo, _, _ := setupChannelsTrackingServiceTest(t)
	_, taskID := createChannelsTrackingFixtures(t, repo, model.PlatformMontage)
	tracking := &model.ChannelsVideoTracking{
		ID: uuid.NewString(), TaskID: taskID, UserID: uuid.NewString(), ProjectID: uuid.NewString(),
		Status: model.ChannelsTrackingStatusTracking, VideoURL: "https://weixin.qq.com/sph/video", VideoID: "video", BoundAt: time.Now(),
	}
	if err := repo.ChannelsTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	claimed, err := repo.ChannelsTrackings().TryClaimCapture(context.Background(), tracking.ID, "worker-a", now, now.Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("worker-a claim = %v, %v", claimed, err)
	}
	if err := repo.ChannelsTrackings().ReleaseCaptureClaim(context.Background(), tracking.ID, "worker-a"); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ChannelsTrackings().TryClaimCapture(context.Background(), tracking.ID, "worker-b", now, now.Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("worker-b claim = %v, %v", claimed, err)
	}
	if err := repo.ChannelsTrackings().ReleaseCaptureClaim(context.Background(), tracking.ID, "worker-a"); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ChannelsTrackings().TryClaimCapture(context.Background(), tracking.ID, "worker-c", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("stale worker release cleared the active worker-b claim")
	}
}

func TestChannelsTrackingStopsBeforePaidFetchAtDurationLimit(t *testing.T) {
	svc, repo, provider, _ := setupChannelsTrackingServiceTest(t)
	userID, taskID := createChannelsTrackingFixtures(t, repo, model.PlatformMontage)
	provider.info = &worldtree.VideoInfo{ObjectID: "video-1", LikeCount: 8}
	if err := svc.BindTask(context.Background(), userID, taskID, "https://weixin.qq.com/sph/video-1"); err != nil {
		t.Fatal(err)
	}
	tracking, err := repo.ChannelsTrackings().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	tracking.BoundAt = time.Now().Add(-model.ChannelsTrackingMaxDays*24*time.Hour - time.Minute)
	if err := repo.ChannelsTrackings().Update(context.Background(), tracking); err != nil {
		t.Fatal(err)
	}
	beforeCalls := provider.calls
	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != beforeCalls {
		t.Fatalf("provider calls = %d, want %d", provider.calls, beforeCalls)
	}
	stopped, err := repo.ChannelsTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != model.ChannelsTrackingStatusStopped || stopped.StopReason != model.ChannelsStopReasonMaxDurationReached || stopped.NextRunAt != nil {
		t.Fatalf("stopped tracking = %+v", stopped)
	}
}

func TestChannelsTrackingRejectsNonVideoTaskAndProviderFailure(t *testing.T) {
	svc, repo, provider, _ := setupChannelsTrackingServiceTest(t)
	userID, taskID := createChannelsTrackingFixtures(t, repo, model.PlatformArticle)
	if err := svc.BindTask(context.Background(), userID, taskID, "https://weixin.qq.com/sph/video"); err == nil || !strings.Contains(err.Error(), "not a video task") {
		t.Fatalf("error = %v", err)
	}

	videoUserID, videoTaskID := createChannelsTrackingFixtures(t, repo, model.PlatformMontage)
	provider.err = errors.New("upstream timeout")
	if err := svc.BindTask(context.Background(), videoUserID, videoTaskID, "https://weixin.qq.com/sph/video"); err == nil || errors.Is(err, ErrChannelsVideoURLInvalid) {
		t.Fatalf("error = %v", err)
	}
}
