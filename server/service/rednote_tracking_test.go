package service

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/platform"
	"github.com/royalrick/anbanwriter/server/repository"
)

type fakeRednotePlatform struct {
	posts        []platform.RednotePost
	metrics      platform.RednotePostMetrics
	err          error
	metricsCalls int
}

func (f *fakeRednotePlatform) FetchProfilePosts(ctx context.Context, profileURL string) ([]platform.RednotePost, error) {
	return f.posts, f.err
}

func (f *fakeRednotePlatform) FetchPostMetrics(ctx context.Context, noteURL string) (*platform.RednotePostMetrics, error) {
	f.metricsCalls++
	return &f.metrics, f.err
}

type fakeRednoteLLM struct {
	response string
	err      error
}

func (f *fakeRednoteLLM) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return f.response, f.err
}

type fakeTrackingEnqueuer struct {
	delayed []string
	now     []string
}

func (f *fakeTrackingEnqueuer) Enqueue(taskType string, payload []byte) error {
	f.now = append(f.now, taskType)
	return nil
}

func (f *fakeTrackingEnqueuer) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	f.delayed = append(f.delayed, taskType)
	return nil
}

func setupRednoteTrackingServiceTest(t *testing.T) (*RednoteTrackingService, repository.Repository, *fakeTrackingEnqueuer) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{}
	svc := NewRednoteTrackingService(repo, &fakeRednotePlatform{}, &fakeRednoteLLM{}, enq, &logger)
	return svc, repo, enq
}

func createRednoteTrackingFixtures(t *testing.T, repo repository.Repository) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := uuid.New().String()
	taskID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Nickname: "User", Password: "hashed"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Channels().Create(ctx, &model.Channel{
		ID:         channelID,
		UserID:     userID,
		Platform:   model.PlatformRednote,
		Name:       "RedNote",
		ProfileURL: "https://www.xiaohongshu.com/user/profile/profile-1",
		Status:     model.ChannelStatusActive,
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformRednote,
		Status:    model.TaskStatusCompleted,
		Title:     "早起效率翻倍的方法",
		Prompt:    "早起效率",
		Published: true,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return userID, channelID, taskID
}

func TestRednoteTrackingService_EnsureTrackingForPublishedTask(t *testing.T) {
	svc, repo, enq := setupRednoteTrackingServiceTest(t)
	userID, _, taskID := createRednoteTrackingFixtures(t, repo)

	if err := svc.EnsureTrackingForPublishedTask(context.Background(), userID, taskID); err != nil {
		t.Fatalf("EnsureTrackingForPublishedTask: %v", err)
	}

	tracking, err := repo.RednoteTrackings().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("FindByTaskID: %v", err)
	}
	if tracking.Status != model.RednoteTrackingStatusWaitingDiscovery {
		t.Fatalf("Status = %q", tracking.Status)
	}
	if len(enq.delayed) != 1 || enq.delayed[0] != "rednote:discover" {
		t.Fatalf("delayed jobs = %+v", enq.delayed)
	}
}

func TestRednoteTrackingService_DiscoverPublishedNoteBindsAndCaptures(t *testing.T) {
	_, repo, _ := setupRednoteTrackingServiceTest(t)
	userID, channelID, taskID := createRednoteTrackingFixtures(t, repo)
	platformFake := &fakeRednotePlatform{
		posts: []platform.RednotePost{
			{Title: "早起效率翻倍的方法", URL: "https://www.xiaohongshu.com/explore/note-1", NoteID: "note-1", CoverURL: "https://img.example/1.jpg"},
		},
		metrics: platform.RednotePostMetrics{LikeCount: 10, CollectCount: 3, CommentCount: 1, ShareCount: 0},
	}
	llmFake := &fakeRednoteLLM{response: `{"matched":true,"note_url":"https://www.xiaohongshu.com/explore/note-1","note_id":"note-1","confidence":0.91,"reason":"标题和主题一致"}`}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc := NewRednoteTrackingService(repo, platformFake, llmFake, &fakeTrackingEnqueuer{}, &logger)

	tracking := &model.RednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ChannelID:         channelID,
		Status:            model.RednoteTrackingStatusWaitingDiscovery,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/profile-1",
		PublishedMarkedAt: time.Now().Add(-24 * time.Hour),
	}
	if err := repo.RednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	if err := svc.DiscoverPublishedNote(context.Background(), tracking.ID); err != nil {
		t.Fatalf("DiscoverPublishedNote: %v", err)
	}

	updated, err := repo.RednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.Status != model.RednoteTrackingStatusTracking || updated.NoteID != "note-1" {
		t.Fatalf("tracking = %+v", updated)
	}
	snapshots, err := repo.RednoteMetricSnapshots().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("FindByTaskID snapshots: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].LikeCount != 10 {
		t.Fatalf("snapshots = %+v", snapshots)
	}
}

func TestRednoteTrackingService_DiscoverPublishedNoteRejectsMismatchedIdentifiers(t *testing.T) {
	_, repo, _ := setupRednoteTrackingServiceTest(t)
	userID, channelID, taskID := createRednoteTrackingFixtures(t, repo)
	url1 := "https://www.xiaohongshu.com/explore/note-1"
	url2 := "https://www.xiaohongshu.com/explore/note-2"
	platformFake := &fakeRednotePlatform{
		posts: []platform.RednotePost{
			{Title: "早起效率翻倍的方法", URL: url1, NoteID: "note-1", CoverURL: "https://img.example/1.jpg"},
			{Title: "午后精力恢复技巧", URL: url2, NoteID: "note-2", CoverURL: "https://img.example/2.jpg"},
		},
		metrics: platform.RednotePostMetrics{LikeCount: 99, CollectCount: 9, CommentCount: 9, ShareCount: 9},
	}
	llmFake := &fakeRednoteLLM{response: `{"matched":true,"note_url":"https://www.xiaohongshu.com/explore/note-2","note_id":"note-1","confidence":0.91,"reason":"标题和主题一致"}`}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{}
	svc := NewRednoteTrackingService(repo, platformFake, llmFake, enq, &logger)

	tracking := &model.RednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ChannelID:         channelID,
		Status:            model.RednoteTrackingStatusWaitingDiscovery,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/profile-1",
		PublishedMarkedAt: time.Now().Add(-24 * time.Hour),
	}
	if err := repo.RednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	if err := svc.DiscoverPublishedNote(context.Background(), tracking.ID); err != nil {
		t.Fatalf("DiscoverPublishedNote: %v", err)
	}

	updated, err := repo.RednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.Status != model.RednoteTrackingStatusWaitingDiscovery {
		t.Fatalf("Status = %q, want %q", updated.Status, model.RednoteTrackingStatusWaitingDiscovery)
	}
	if updated.NoteID != "" || updated.NoteURL != "" {
		t.Fatalf("matched note fields should remain empty, got note_id=%q note_url=%q", updated.NoteID, updated.NoteURL)
	}
	snapshots, err := repo.RednoteMetricSnapshots().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("FindByTaskID snapshots: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("snapshots = %+v, want none", snapshots)
	}
	if len(enq.delayed) != 1 || enq.delayed[0] != RednoteDiscoverTaskType {
		t.Fatalf("delayed jobs = %+v", enq.delayed)
	}
}

func TestRednoteTrackingService_StaleJobsNoopForTerminalStatuses(t *testing.T) {
	_, repo, _ := setupRednoteTrackingServiceTest(t)
	userID, channelID, taskID := createRednoteTrackingFixtures(t, repo)
	platformFake := &fakeRednotePlatform{
		posts: []platform.RednotePost{
			{Title: "早起效率翻倍的方法", URL: "https://www.xiaohongshu.com/explore/note-1", NoteID: "note-1"},
		},
		metrics: platform.RednotePostMetrics{LikeCount: 10},
	}
	llmFake := &fakeRednoteLLM{response: `{"matched":true,"note_url":"https://www.xiaohongshu.com/explore/note-1","note_id":"note-1","confidence":0.91,"reason":"标题和主题一致"}`}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{}
	svc := NewRednoteTrackingService(repo, platformFake, llmFake, enq, &logger)

	tracking := &model.RednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ChannelID:         channelID,
		Status:            model.RednoteTrackingStatusStopped,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/profile-1",
		NoteURL:           "https://www.xiaohongshu.com/explore/note-1",
		PublishedMarkedAt: time.Now().Add(-24 * time.Hour),
		RunCount:          2,
	}
	if err := repo.RednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	if err := svc.DiscoverPublishedNote(context.Background(), tracking.ID); err != nil {
		t.Fatalf("DiscoverPublishedNote: %v", err)
	}
	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err != nil {
		t.Fatalf("CaptureMetrics: %v", err)
	}

	updated, err := repo.RednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.Status != model.RednoteTrackingStatusStopped || updated.RunCount != 2 {
		t.Fatalf("tracking changed after stale jobs: %+v", updated)
	}
	if platformFake.metricsCalls != 0 {
		t.Fatalf("metricsCalls = %d, want 0", platformFake.metricsCalls)
	}
	if len(enq.delayed) != 0 {
		t.Fatalf("delayed jobs = %+v, want none", enq.delayed)
	}
}

func TestRednoteTrackingService_CaptureMetricsNoopsWhenTodaySnapshotExists(t *testing.T) {
	_, repo, _ := setupRednoteTrackingServiceTest(t)
	userID, channelID, taskID := createRednoteTrackingFixtures(t, repo)
	platformFake := &fakeRednotePlatform{metrics: platform.RednotePostMetrics{LikeCount: 99}}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{}
	svc := NewRednoteTrackingService(repo, platformFake, &fakeRednoteLLM{}, enq, &logger)
	now := time.Now()
	lastRun := now.Add(-time.Hour)
	nextRun := now.Add(time.Hour)
	tracking := &model.RednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ChannelID:         channelID,
		Status:            model.RednoteTrackingStatusTracking,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/profile-1",
		NoteURL:           "https://www.xiaohongshu.com/explore/note-1",
		PublishedMarkedAt: now.Add(-24 * time.Hour),
		TrackingStartedAt: &lastRun,
		LastRunAt:         &lastRun,
		NextRunAt:         &nextRun,
		RunCount:          3,
	}
	if err := repo.RednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	if err := repo.RednoteMetricSnapshots().Create(context.Background(), &model.RednoteMetricSnapshot{
		ID:           uuid.New().String(),
		TrackingID:   tracking.ID,
		TaskID:       taskID,
		CapturedAt:   now.Add(-30 * time.Minute),
		CapturedDate: model.RednoteCapturedDate(now),
		LikeCount:    10,
	}); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err != nil {
		t.Fatalf("CaptureMetrics: %v", err)
	}

	updated, err := repo.RednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.RunCount != 3 || !updated.LastRunAt.Equal(lastRun) || !updated.NextRunAt.Equal(nextRun) {
		t.Fatalf("tracking changed after duplicate same-day capture: %+v", updated)
	}
	if platformFake.metricsCalls != 0 {
		t.Fatalf("metricsCalls = %d, want 0", platformFake.metricsCalls)
	}
	if len(enq.delayed) != 0 {
		t.Fatalf("delayed jobs = %+v, want none", enq.delayed)
	}
}

func TestRednoteTrackingService_ResetHidesOldSnapshotsFromAnalytics(t *testing.T) {
	svc, repo, _ := setupRednoteTrackingServiceTest(t)
	userID, channelID, taskID := createRednoteTrackingFixtures(t, repo)
	oldStartedAt := time.Now().Add(-72 * time.Hour)
	oldCapturedAt := oldStartedAt.Add(time.Hour)
	tracking := &model.RednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ChannelID:         channelID,
		Status:            model.RednoteTrackingStatusStopped,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/old",
		NoteURL:           "https://www.xiaohongshu.com/explore/note-old",
		PublishedMarkedAt: oldStartedAt,
		TrackingStartedAt: &oldStartedAt,
		RunCount:          1,
	}
	if err := repo.RednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	if err := repo.RednoteMetricSnapshots().Create(context.Background(), &model.RednoteMetricSnapshot{
		ID:           uuid.New().String(),
		TrackingID:   tracking.ID,
		TaskID:       taskID,
		CapturedAt:   oldCapturedAt,
		CapturedDate: model.RednoteCapturedDate(oldCapturedAt),
		LikeCount:    42,
	}); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	if err := svc.EnsureTrackingForPublishedTask(context.Background(), userID, taskID); err != nil {
		t.Fatalf("EnsureTrackingForPublishedTask: %v", err)
	}
	analytics, err := svc.GetTaskAnalytics(context.Background(), userID, taskID)
	if err != nil {
		t.Fatalf("GetTaskAnalytics: %v", err)
	}
	if len(analytics.Series) != 0 || analytics.Latest != nil || analytics.Deltas != nil {
		t.Fatalf("analytics includes old snapshots after reset: %+v", analytics)
	}
}

func TestRednoteTrackingService_EnsureTrackingForPublishedTaskRejectsForeignChannel(t *testing.T) {
	svc, repo, _ := setupRednoteTrackingServiceTest(t)
	userID, channelID, taskID := createRednoteTrackingFixtures(t, repo)
	otherUserID := uuid.New().String()
	if err := repo.Users().Create(context.Background(), &model.User{ID: otherUserID, Email: otherUserID + "@example.com", Nickname: "Other", Password: "hashed", InviteCode: "other123"}); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	channel, err := repo.Channels().FindByID(context.Background(), channelID)
	if err != nil {
		t.Fatalf("FindByID channel: %v", err)
	}
	channel.UserID = otherUserID
	if err := repo.Channels().Update(context.Background(), channel); err != nil {
		t.Fatalf("update channel: %v", err)
	}

	if err := svc.EnsureTrackingForPublishedTask(context.Background(), userID, taskID); err == nil {
		t.Fatal("expected channel ownership error")
	}
	if _, err := repo.RednoteTrackings().FindByTaskID(context.Background(), taskID); err == nil {
		t.Fatal("tracking should not be created for a foreign channel")
	}
}

func TestRednoteTrackingService_ShouldStopForLowGrowth(t *testing.T) {
	if !shouldStopForLowGrowth(3, model.RednoteLowGrowthConsecutiveCaptures) {
		t.Fatal("expected low growth stop")
	}
	if shouldStopForLowGrowth(2, model.RednoteLowGrowthConsecutiveCaptures) {
		t.Fatal("did not expect low growth stop")
	}
}
