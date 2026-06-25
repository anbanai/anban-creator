package service

import (
	"context"
	"errors"
	"fmt"
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

type fakeSeednotePlatform struct {
	posts        []platform.SeednotePost
	metrics      platform.SeednotePostMetrics
	err          error
	metricsCalls int
}

func (f *fakeSeednotePlatform) FetchProfilePosts(ctx context.Context, profileURL string) ([]platform.SeednotePost, error) {
	return f.posts, f.err
}

func (f *fakeSeednotePlatform) FetchPostMetrics(ctx context.Context, noteURL string) (*platform.SeednotePostMetrics, error) {
	f.metricsCalls++
	return &f.metrics, f.err
}

type fakeSeednoteLLM struct {
	response string
	err      error
}

func (f *fakeSeednoteLLM) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return f.response, f.err
}

func (f *fakeSeednoteLLM) CompleteWithImage(_ context.Context, _, _, _ string) (string, error) {
	return "", fmt.Errorf("not implemented")
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
	svc := NewSeednoteTrackingService(repo, &fakeSeednotePlatform{}, &fakeSeednoteLLM{}, enq, &logger)
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
		Published: true,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return userID, projectID, taskID
}

func TestSeednoteTrackingService_EnsureTrackingForPublishedTask(t *testing.T) {
	svc, repo, enq := setupSeednoteTrackingServiceTest(t)
	userID, _, taskID := createSeednoteTrackingFixtures(t, repo)

	if err := svc.EnsureTrackingForPublishedTask(context.Background(), userID, taskID); err != nil {
		t.Fatalf("EnsureTrackingForPublishedTask: %v", err)
	}

	tracking, err := repo.SeednoteTrackings().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("FindByTaskID: %v", err)
	}
	if tracking.Status != model.SeednoteTrackingStatusWaitingDiscovery {
		t.Fatalf("Status = %q", tracking.Status)
	}
	if len(enq.delayed) != 1 || enq.delayed[0] != "seednote:discover" {
		t.Fatalf("delayed jobs = %+v", enq.delayed)
	}
}

func TestSeednoteTrackingService_EnsureTrackingForPublishedTaskMissingProfileCreatesFailedTracking(t *testing.T) {
	svc, repo, enq := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	project, err := repo.Projects().FindByID(context.Background(), projectID)
	if err != nil {
		t.Fatalf("FindByID project: %v", err)
	}
	project.ProfileURL = ""
	if err := repo.Projects().Update(context.Background(), project); err != nil {
		t.Fatalf("update project: %v", err)
	}

	if err := svc.EnsureTrackingForPublishedTask(context.Background(), userID, taskID); err != nil {
		t.Fatalf("EnsureTrackingForPublishedTask: %v", err)
	}

	tracking, err := repo.SeednoteTrackings().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("FindByTaskID: %v", err)
	}
	if tracking.Status != model.SeednoteTrackingStatusFailed {
		t.Fatalf("Status = %q, want failed", tracking.Status)
	}
	if tracking.LastError == "" {
		t.Fatal("LastError should explain missing profile URL")
	}
	if tracking.NextRunAt != nil {
		t.Fatalf("NextRunAt = %v, want nil", tracking.NextRunAt)
	}
	if len(enq.delayed) != 0 {
		t.Fatalf("delayed jobs = %+v, want none", enq.delayed)
	}
}

func TestSeednoteTrackingService_DiscoverPublishedNoteBindsAndCaptures(t *testing.T) {
	_, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	platformFake := &fakeSeednotePlatform{
		posts: []platform.SeednotePost{
			{Title: "早起效率翻倍的方法", URL: "https://www.xiaohongshu.com/explore/note-1", NoteID: "note-1", CoverURL: "https://img.example/1.jpg"},
		},
		metrics: platform.SeednotePostMetrics{LikeCount: 10, CollectCount: 3, CommentCount: 1, ShareCount: 0},
	}
	llmFake := &fakeSeednoteLLM{response: `{"matched":true,"note_url":"https://www.xiaohongshu.com/explore/note-1","note_id":"note-1","confidence":0.91,"reason":"标题和主题一致"}`}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc := NewSeednoteTrackingService(repo, platformFake, llmFake, &fakeTrackingEnqueuer{}, &logger)

	tracking := &model.SeednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ProjectID:         projectID,
		Status:            model.SeednoteTrackingStatusWaitingDiscovery,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/profile-1",
		PublishedMarkedAt: time.Now().Add(-24 * time.Hour),
	}
	if err := repo.SeednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	if err := svc.DiscoverPublishedNote(context.Background(), tracking.ID); err != nil {
		t.Fatalf("DiscoverPublishedNote: %v", err)
	}

	updated, err := repo.SeednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.Status != model.SeednoteTrackingStatusTracking || updated.NoteID != "note-1" {
		t.Fatalf("tracking = %+v", updated)
	}
	snapshots, err := repo.SeednoteMetricSnapshots().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("FindByTaskID snapshots: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].LikeCount != 10 {
		t.Fatalf("snapshots = %+v", snapshots)
	}
}

func TestSeednoteTrackingService_DiscoverPublishedNoteRejectsMismatchedIdentifiers(t *testing.T) {
	_, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	url1 := "https://www.xiaohongshu.com/explore/note-1"
	url2 := "https://www.xiaohongshu.com/explore/note-2"
	platformFake := &fakeSeednotePlatform{
		posts: []platform.SeednotePost{
			{Title: "早起效率翻倍的方法", URL: url1, NoteID: "note-1", CoverURL: "https://img.example/1.jpg"},
			{Title: "午后精力恢复技巧", URL: url2, NoteID: "note-2", CoverURL: "https://img.example/2.jpg"},
		},
		metrics: platform.SeednotePostMetrics{LikeCount: 99, CollectCount: 9, CommentCount: 9, ShareCount: 9},
	}
	llmFake := &fakeSeednoteLLM{response: `{"matched":true,"note_url":"https://www.xiaohongshu.com/explore/note-2","note_id":"note-1","confidence":0.91,"reason":"标题和主题一致"}`}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{}
	svc := NewSeednoteTrackingService(repo, platformFake, llmFake, enq, &logger)

	tracking := &model.SeednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ProjectID:         projectID,
		Status:            model.SeednoteTrackingStatusWaitingDiscovery,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/profile-1",
		PublishedMarkedAt: time.Now().Add(-24 * time.Hour),
	}
	if err := repo.SeednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	if err := svc.DiscoverPublishedNote(context.Background(), tracking.ID); err != nil {
		t.Fatalf("DiscoverPublishedNote: %v", err)
	}

	updated, err := repo.SeednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.Status != model.SeednoteTrackingStatusWaitingDiscovery {
		t.Fatalf("Status = %q, want %q", updated.Status, model.SeednoteTrackingStatusWaitingDiscovery)
	}
	if updated.NoteID != "" || updated.NoteURL != "" {
		t.Fatalf("matched note fields should remain empty, got note_id=%q note_url=%q", updated.NoteID, updated.NoteURL)
	}
	snapshots, err := repo.SeednoteMetricSnapshots().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("FindByTaskID snapshots: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("snapshots = %+v, want none", snapshots)
	}
	if len(enq.delayed) != 1 || enq.delayed[0] != SeednoteDiscoverTaskType {
		t.Fatalf("delayed jobs = %+v", enq.delayed)
	}
}

func TestSeednoteTrackingService_StaleJobsNoopForTerminalStatuses(t *testing.T) {
	_, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	platformFake := &fakeSeednotePlatform{
		posts: []platform.SeednotePost{
			{Title: "早起效率翻倍的方法", URL: "https://www.xiaohongshu.com/explore/note-1", NoteID: "note-1"},
		},
		metrics: platform.SeednotePostMetrics{LikeCount: 10},
	}
	llmFake := &fakeSeednoteLLM{response: `{"matched":true,"note_url":"https://www.xiaohongshu.com/explore/note-1","note_id":"note-1","confidence":0.91,"reason":"标题和主题一致"}`}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{}
	svc := NewSeednoteTrackingService(repo, platformFake, llmFake, enq, &logger)

	tracking := &model.SeednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ProjectID:         projectID,
		Status:            model.SeednoteTrackingStatusStopped,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/profile-1",
		NoteURL:           "https://www.xiaohongshu.com/explore/note-1",
		PublishedMarkedAt: time.Now().Add(-24 * time.Hour),
		RunCount:          2,
	}
	if err := repo.SeednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	if err := svc.DiscoverPublishedNote(context.Background(), tracking.ID); err != nil {
		t.Fatalf("DiscoverPublishedNote: %v", err)
	}
	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err != nil {
		t.Fatalf("CaptureMetrics: %v", err)
	}

	updated, err := repo.SeednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.Status != model.SeednoteTrackingStatusStopped || updated.RunCount != 2 {
		t.Fatalf("tracking changed after stale jobs: %+v", updated)
	}
	if platformFake.metricsCalls != 0 {
		t.Fatalf("metricsCalls = %d, want 0", platformFake.metricsCalls)
	}
	if len(enq.delayed) != 0 {
		t.Fatalf("delayed jobs = %+v, want none", enq.delayed)
	}
}

func TestSeednoteTrackingService_CaptureMetricsNoopsWhenTodaySnapshotExists(t *testing.T) {
	_, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	platformFake := &fakeSeednotePlatform{metrics: platform.SeednotePostMetrics{LikeCount: 99}}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{}
	svc := NewSeednoteTrackingService(repo, platformFake, &fakeSeednoteLLM{}, enq, &logger)
	now := time.Now()
	lastRun := now.Add(-time.Hour)
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
	svc := NewSeednoteTrackingService(repo, platformFake, &fakeSeednoteLLM{}, enq, &logger)
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
	svc := NewSeednoteTrackingService(repo, platformFake, &fakeSeednoteLLM{}, enq, &logger)
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

	if err := svc.CaptureMetrics(context.Background(), tracking.ID); err == nil {
		t.Fatal("expected enqueue failure")
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

func TestSeednoteTrackingService_EnsureTrackingForPublishedTaskRejectsForeignProject(t *testing.T) {
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

	if err := svc.EnsureTrackingForPublishedTask(context.Background(), userID, taskID); err == nil {
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
