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

type fakeSeednotePlatform struct {
	posts        []platform.SeednotePost
	metrics      platform.SeednotePostMetrics
	err          error
	profileCalls int
	metricsCalls int
}

func (f *fakeSeednotePlatform) FetchProfilePosts(ctx context.Context, profileURL string) ([]platform.SeednotePost, error) {
	f.profileCalls++
	return f.posts, f.err
}

func (f *fakeSeednotePlatform) FetchPostMetrics(ctx context.Context, noteURL string) (*platform.SeednotePostMetrics, error) {
	f.metricsCalls++
	return &f.metrics, f.err
}

type fakeTrackingEnqueuer struct {
	delayed []string
	delays  []time.Duration
	now     []string
	err     error
}

func (f *fakeTrackingEnqueuer) Enqueue(taskType string, payload []byte) error {
	f.now = append(f.now, taskType)
	return nil
}

func (f *fakeTrackingEnqueuer) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	if f.err != nil {
		err := f.err
		f.err = nil
		return err
	}
	f.delayed = append(f.delayed, taskType)
	f.delays = append(f.delays, delay)
	return nil
}

func setupSeednoteTrackingServiceTest(t *testing.T) (*SeednoteTrackingService, repository.Repository, *fakeTrackingEnqueuer) {
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
	svc := NewSeednoteTrackingService(repo, &fakeSeednotePlatform{}, enq, &logger)
	return svc, repo, enq
}

func createSeednoteTrackingFixtures(t *testing.T, repo repository.Repository) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	taskID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Nickname: "User", Password: "hashed"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:         projectID,
		UserID:     userID,
		Platform:   model.PlatformSeednote,
		Name:       "SeedNote",
		ProfileURL: "https://www.xiaohongshu.com/user/profile/profile-1",
		Status:     model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Title:     "早起效率翻倍的方法",
		Prompt:    "早起效率",
		Published: false,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return userID, projectID, taskID
}

func TestSeednoteTrackingService_BindCapturesFirstSnapshotImmediately(t *testing.T) {
	svc, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, _, taskID := createSeednoteTrackingFixtures(t, repo)
	platformFake := svc.platform.(*fakeSeednotePlatform)
	viewCount := 321
	platformFake.metrics = platform.SeednotePostMetrics{
		LikeCount: 12, CollectCount: 8, CommentCount: 3, ShareCount: 2, ViewCount: &viewCount,
	}

	if err := svc.BindTask(context.Background(), userID, taskID, SeednotePublicationIdentity{NoteID: "note-1"}); err != nil {
		t.Fatal(err)
	}
	if platformFake.metricsCalls != 1 {
		t.Fatalf("metrics calls = %d, want 1 synchronous call", platformFake.metricsCalls)
	}
	analytics, err := svc.GetTaskAnalytics(context.Background(), userID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Latest == nil || analytics.Latest.LikeCount != 12 || analytics.Latest.ViewCount == nil || *analytics.Latest.ViewCount != viewCount {
		t.Fatalf("latest metrics = %+v", analytics.Latest)
	}
	if analytics.Tracking == nil || analytics.Tracking.RunCount != 1 || analytics.Tracking.LastRunAt == nil {
		t.Fatalf("tracking = %+v", analytics.Tracking)
	}
}

func TestSeednoteTrackingService_BindSucceedsWhenDelayedEnqueueFails(t *testing.T) {
	svc, repo, enqueuer := setupSeednoteTrackingServiceTest(t)
	userID, _, taskID := createSeednoteTrackingFixtures(t, repo)
	platformFake := svc.platform.(*fakeSeednotePlatform)
	platformFake.metrics = platform.SeednotePostMetrics{LikeCount: 12}
	enqueuer.err = errors.New("queue temporarily unavailable")

	if err := svc.BindTask(context.Background(), userID, taskID, SeednotePublicationIdentity{NoteID: "note-1"}); err != nil {
		t.Fatal(err)
	}
	analytics, err := svc.GetTaskAnalytics(context.Background(), userID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Latest == nil || analytics.Latest.LikeCount != 12 {
		t.Fatalf("latest metrics = %+v", analytics.Latest)
	}
	if analytics.Tracking == nil || analytics.Tracking.NextRunAt == nil || analytics.Tracking.LastError == "" {
		t.Fatalf("tracking = %+v", analytics.Tracking)
	}
}

func TestBindTaskUsesDeterministicIdentity(t *testing.T) {
	tests := []struct {
		name, noteID, noteURL, wantID, wantURL, wantStatus string
		wantErr, wantEnqueue, wantMetrics                  bool
	}{
		{name: "url only", noteURL: "https://www.xiaohongshu.com/explore/note-1?xsec_token=abc", wantID: "note-1", wantURL: "https://www.xiaohongshu.com/explore/note-1?xsec_token=abc", wantStatus: model.SeednoteTrackingStatusTracking, wantEnqueue: true, wantMetrics: true},
		{name: "id only", noteID: "note-1", wantID: "note-1", wantURL: "https://www.xiaohongshu.com/explore/note-1", wantStatus: model.SeednoteTrackingStatusTracking, wantEnqueue: true, wantMetrics: true},
		{name: "matching id and url", noteID: "note-1", noteURL: "https://www.xiaohongshu.com/explore/note-1", wantID: "note-1", wantURL: "https://www.xiaohongshu.com/explore/note-1", wantStatus: model.SeednoteTrackingStatusTracking, wantEnqueue: true, wantMetrics: true},
		{name: "conflicting identity", noteID: "note-2", noteURL: "https://www.xiaohongshu.com/explore/note-1", wantErr: true},
		{name: "foreign host", noteURL: "https://example.com/explore/note-1", wantErr: true},
		{name: "note id only in query", noteURL: "https://www.xiaohongshu.com/user/profile/user-1?next=/explore/note-1", wantErr: true},
		{name: "missing identity", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, enqueuer := setupSeednoteTrackingServiceTest(t)
			userID, _, taskID := createSeednoteTrackingFixtures(t, repo)
			err := svc.BindTask(context.Background(), userID, taskID, SeednotePublicationIdentity{NoteID: tt.noteID, NoteURL: tt.noteURL})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected identity error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tracking, err := repo.SeednoteTrackings().FindByTaskID(context.Background(), taskID)
			if err != nil {
				t.Fatal(err)
			}
			if tracking.NoteID != tt.wantID || tracking.NoteURL != tt.wantURL || tracking.Status != tt.wantStatus {
				t.Fatalf("tracking = %#v", tracking)
			}
			calls := len(enqueuer.now) + len(enqueuer.delayed)
			if (calls > 0) != tt.wantEnqueue {
				t.Fatalf("enqueue calls = %d", calls)
			}
			platformFake := svc.platform.(*fakeSeednotePlatform)
			if platformFake.profileCalls != 0 {
				t.Fatalf("profile calls = %d, want 0", platformFake.profileCalls)
			}
			if (platformFake.metricsCalls > 0) != tt.wantMetrics {
				t.Fatalf("metrics calls = %d", platformFake.metricsCalls)
			}
		})
	}
}

func TestSeednoteTrackingService_CaptureMetricsNoopsWhenTodaySnapshotExists(t *testing.T) {
	_, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	platformFake := &fakeSeednotePlatform{metrics: platform.SeednotePostMetrics{LikeCount: 99}}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{}
	svc := NewSeednoteTrackingService(repo, platformFake, enq, &logger)
	now := time.Now()
	lastRun := now
	nextRun := now.Add(time.Hour)
	tracking := &model.SeednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ProjectID:         projectID,
		Status:            model.SeednoteTrackingStatusTracking,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/profile-1",
		NoteURL:           "https://www.xiaohongshu.com/explore/note-1",
		PublishedMarkedAt: now.Add(-24 * time.Hour),
		TrackingStartedAt: &lastRun,
		LastRunAt:         &lastRun,
		NextRunAt:         &nextRun,
		RunCount:          3,
	}
	if err := repo.SeednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	if err := repo.SeednoteMetricSnapshots().Create(context.Background(), &model.SeednoteMetricSnapshot{
		ID:           uuid.New().String(),
		TrackingID:   tracking.ID,
		TaskID:       taskID,
		CapturedAt:   now,
		CapturedDate: model.SeednoteCapturedDate(now),
		LikeCount:    10,
	}); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err != nil {
		t.Fatalf("CaptureMetrics: %v", err)
	}

	updated, err := repo.SeednoteTrackings().FindByID(context.Background(), tracking.ID)
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

func TestSeednoteTrackingService_CaptureMetricsRepairsIncompleteTodaySnapshotLifecycle(t *testing.T) {
	_, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	platformFake := &fakeSeednotePlatform{metrics: platform.SeednotePostMetrics{LikeCount: 99}}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{}
	svc := NewSeednoteTrackingService(repo, platformFake, enq, &logger)
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	tracking := &model.SeednotePostTracking{
		ID:                        uuid.New().String(),
		TaskID:                    taskID,
		UserID:                    userID,
		ProjectID:                 projectID,
		Status:                    model.SeednoteTrackingStatusTracking,
		ProfileURL:                "https://www.xiaohongshu.com/user/profile/profile-1",
		NoteURL:                   "https://www.xiaohongshu.com/explore/note-1",
		PublishedMarkedAt:         now.Add(-48 * time.Hour),
		TrackingStartedAt:         &yesterday,
		LastRunAt:                 &yesterday,
		RunCount:                  0,
		ConsecutiveLowGrowthCount: model.SeednoteLowGrowthConsecutiveCaptures - 1,
	}
	if err := repo.SeednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	if err := repo.SeednoteMetricSnapshots().Create(context.Background(), &model.SeednoteMetricSnapshot{
		ID:           uuid.New().String(),
		TrackingID:   tracking.ID,
		TaskID:       taskID,
		CapturedAt:   now.Add(-30 * time.Minute),
		CapturedDate: model.SeednoteCapturedDate(now),
		LikeCount:    10,
	}); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err != nil {
		t.Fatalf("CaptureMetrics: %v", err)
	}

	updated, err := repo.SeednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if platformFake.metricsCalls != 0 {
		t.Fatalf("metricsCalls = %d, want 0", platformFake.metricsCalls)
	}
	if updated.RunCount != 1 {
		t.Fatalf("RunCount = %d, want 1", updated.RunCount)
	}
	if updated.Status != model.SeednoteTrackingStatusTracking {
		t.Fatalf("Status = %q, want tracking", updated.Status)
	}
	if updated.ConsecutiveLowGrowthCount != model.SeednoteLowGrowthConsecutiveCaptures-1 {
		t.Fatalf("ConsecutiveLowGrowthCount = %d, want unchanged", updated.ConsecutiveLowGrowthCount)
	}
	if updated.LastRunAt == nil || model.SeednoteCapturedDate(*updated.LastRunAt) != model.SeednoteCapturedDate(now) {
		t.Fatalf("LastRunAt = %v, want today", updated.LastRunAt)
	}
	if updated.NextRunAt == nil {
		t.Fatal("NextRunAt should be set")
	}
	if len(enq.delayed) != 1 || enq.delayed[0] != SeednoteCaptureMetricsTaskType {
		t.Fatalf("delayed jobs = %+v", enq.delayed)
	}
}

func TestSeednoteTrackingService_CaptureMetricsRetriesEnqueueAfterSameDayEnqueueFailure(t *testing.T) {
	_, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	platformFake := &fakeSeednotePlatform{metrics: platform.SeednotePostMetrics{LikeCount: 99}}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{err: errors.New("queue temporarily unavailable")}
	svc := NewSeednoteTrackingService(repo, platformFake, enq, &logger)
	now := time.Now()
	startedAt := now.Add(-24 * time.Hour)
	tracking := &model.SeednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ProjectID:         projectID,
		Status:            model.SeednoteTrackingStatusTracking,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/profile-1",
		NoteURL:           "https://www.xiaohongshu.com/explore/note-1",
		PublishedMarkedAt: now.Add(-48 * time.Hour),
		TrackingStartedAt: &startedAt,
	}
	if err := repo.SeednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err != nil {
		t.Fatalf("CaptureMetrics: %v", err)
	}
	afterFailure, err := repo.SeednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID after failure: %v", err)
	}
	if afterFailure.LastRunAt == nil || model.SeednoteCapturedDate(*afterFailure.LastRunAt) != model.SeednoteCapturedDate(now) {
		t.Fatalf("LastRunAt = %v, want today after partial failure", afterFailure.LastRunAt)
	}
	if afterFailure.LastError == "" {
		t.Fatal("LastError should record enqueue failure")
	}
	if afterFailure.NextRunAt == nil {
		t.Fatal("NextRunAt should be set after enqueue failure")
	}
	if platformFake.metricsCalls != 1 {
		t.Fatalf("metricsCalls = %d, want 1", platformFake.metricsCalls)
	}

	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err != nil {
		t.Fatalf("CaptureMetrics retry: %v", err)
	}
	updated, err := repo.SeednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID after retry: %v", err)
	}
	if updated.LastError != "" {
		t.Fatalf("LastError = %q, want cleared", updated.LastError)
	}
	if platformFake.metricsCalls != 1 {
		t.Fatalf("metricsCalls = %d, want no second fetch", platformFake.metricsCalls)
	}
	if len(enq.delayed) != 1 || enq.delayed[0] != SeednoteCaptureMetricsTaskType {
		t.Fatalf("delayed jobs = %+v", enq.delayed)
	}
	if len(enq.delays) != 1 {
		t.Fatalf("delays = %+v, want one delay", enq.delays)
	}
	if enq.delays[0] >= 24*time.Hour {
		t.Fatalf("delay = %s, want remaining time before original NextRunAt", enq.delays[0])
	}
	if enq.delays[0] <= 23*time.Hour {
		t.Fatalf("delay = %s, want close to original NextRunAt", enq.delays[0])
	}
}

func TestSeednoteTrackingService_ResetHidesOldSnapshotsFromAnalytics(t *testing.T) {
	svc, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	oldStartedAt := time.Now().Add(-72 * time.Hour)
	oldCapturedAt := oldStartedAt.Add(time.Hour)
	tracking := &model.SeednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ProjectID:         projectID,
		Status:            model.SeednoteTrackingStatusStopped,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/old",
		NoteURL:           "https://www.xiaohongshu.com/explore/note-old",
		PublishedMarkedAt: oldStartedAt,
		TrackingStartedAt: &oldStartedAt,
		RunCount:          1,
	}
	if err := repo.SeednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	if err := repo.SeednoteMetricSnapshots().Create(context.Background(), &model.SeednoteMetricSnapshot{
		ID:           uuid.New().String(),
		TrackingID:   tracking.ID,
		TaskID:       taskID,
		CapturedAt:   oldCapturedAt,
		CapturedDate: model.SeednoteCapturedDate(oldCapturedAt),
		LikeCount:    42,
	}); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	if err := svc.BindTask(context.Background(), userID, taskID, SeednotePublicationIdentity{NoteID: "note-new"}); err != nil {
		t.Fatalf("BindTask: %v", err)
	}
	analytics, err := svc.GetTaskAnalytics(context.Background(), userID, taskID)
	if err != nil {
		t.Fatalf("GetTaskAnalytics: %v", err)
	}
	if len(analytics.Series) != 1 || analytics.Latest == nil || analytics.Latest.LikeCount != 0 {
		t.Fatalf("analytics does not contain only the fresh binding snapshot: %+v", analytics)
	}
}

func TestSeednoteTrackingService_BindTaskRejectsForeignProject(t *testing.T) {
	svc, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	otherUserID := uuid.New().String()
	if err := repo.Users().Create(context.Background(), &model.User{ID: otherUserID, Email: otherUserID + "@example.com", Nickname: "Other", Password: "hashed", InviteCode: "other123"}); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	project, err := repo.Projects().FindByID(context.Background(), projectID)
	if err != nil {
		t.Fatalf("FindByID project: %v", err)
	}
	project.UserID = otherUserID
	if err := repo.Projects().Update(context.Background(), project); err != nil {
		t.Fatalf("update project: %v", err)
	}

	if err := svc.BindTask(context.Background(), userID, taskID, SeednotePublicationIdentity{NoteID: "note-1"}); err == nil {
		t.Fatal("expected project ownership error")
	}
	if _, err := repo.SeednoteTrackings().FindByTaskID(context.Background(), taskID); err == nil {
		t.Fatal("tracking should not be created for a foreign project")
	}
}

func TestSeednoteTrackingService_ShouldStopForLowGrowth(t *testing.T) {
	if !shouldStopForLowGrowth(3, model.SeednoteLowGrowthConsecutiveCaptures) {
		t.Fatal("expected low growth stop")
	}
	if shouldStopForLowGrowth(2, model.SeednoteLowGrowthConsecutiveCaptures) {
		t.Fatal("did not expect low growth stop")
	}
}
