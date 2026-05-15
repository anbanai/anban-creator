package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

func setupSeednoteAnalyticsHandlerTest(t *testing.T) (*fiber.App, repository.Repository) {
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
	trackingSvc := service.NewSeednoteTrackingService(repo, nil, nil, nil, &logger)
	handler := NewSeednoteAnalyticsHandler(trackingSvc, &logger)
	app := fiber.New()
	app.Get("/tasks/:id/seednote-analytics", func(c fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-User-ID"))
		return handler.GetTaskAnalytics(c)
	})
	return app, repo
}

func TestSeednoteAnalyticsHandler_GetTaskAnalytics(t *testing.T) {
	app, repo := setupSeednoteAnalyticsHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	taskID := uuid.New().String()
	channelID := uuid.New().String()
	now := time.Now().UTC()
	next := now.Add(24 * time.Hour)
	viewCount := 1200

	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "owner@example.com", Nickname: "Owner", Password: "hashed", InviteCode: "owner1"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Channels().Create(ctx, &model.Channel{ID: channelID, UserID: userID, Platform: model.PlatformSeednote, Name: "SeedNote", Status: model.ChannelStatusActive}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ChannelID: channelID, Type: model.PlatformSeednote, Status: model.TaskStatusCompleted, Published: true}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	tracking := &model.SeednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ChannelID:         channelID,
		Status:            model.SeednoteTrackingStatusTracking,
		NoteURL:           "https://www.xiaohongshu.com/explore/note-1",
		NoteTitle:         "早起效率翻倍的方法",
		NoteCoverURL:      "https://img.example/cover.jpg",
		LastRunAt:         &now,
		NextRunAt:         &next,
		RunCount:          2,
		DiscoveredAt:      &now,
		PublishedMarkedAt: now.Add(-48 * time.Hour),
	}
	if err := repo.SeednoteTrackings().Create(ctx, tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}
	for i, likes := range []int{10, 18} {
		capturedAt := now.Add(time.Duration(i-1) * 24 * time.Hour)
		if err := repo.SeednoteMetricSnapshots().Create(ctx, &model.SeednoteMetricSnapshot{
			ID:           uuid.New().String(),
			TrackingID:   tracking.ID,
			TaskID:       taskID,
			CapturedAt:   capturedAt,
			CapturedDate: model.SeednoteCapturedDate(capturedAt),
			LikeCount:    likes,
			CollectCount: 3 + i,
			CommentCount: 1,
			ShareCount:   i,
			ViewCount:    &viewCount,
		}); err != nil {
			t.Fatalf("create snapshot: %v", err)
		}
	}

	req := httptest.NewRequest("GET", "/tasks/"+taskID+"/seednote-analytics", nil)
	req.Header.Set("X-User-ID", userID)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	defer resp.Body.Close()

	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	raw, err := json.Marshal(body.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var analytics service.SeednoteAnalytics
	if err := json.Unmarshal(raw, &analytics); err != nil {
		t.Fatalf("decode analytics: %v", err)
	}
	if analytics.Tracking == nil || analytics.Tracking.Status != model.SeednoteTrackingStatusTracking {
		t.Fatalf("tracking = %+v", analytics.Tracking)
	}
	if analytics.Latest == nil || analytics.Latest.LikeCount != 18 || analytics.Latest.ViewCount == nil {
		t.Fatalf("latest = %+v", analytics.Latest)
	}
	if analytics.Deltas == nil || analytics.Deltas.LikeCount != 8 || analytics.Deltas.CollectCount != 1 {
		t.Fatalf("deltas = %+v", analytics.Deltas)
	}
	if len(analytics.Series) != 2 {
		t.Fatalf("series length = %d, want 2", len(analytics.Series))
	}
}

func TestSeednoteAnalyticsHandler_RejectsNonOwner(t *testing.T) {
	app, repo := setupSeednoteAnalyticsHandlerTest(t)
	ctx := context.Background()
	ownerID := uuid.New().String()
	otherID := uuid.New().String()
	taskID := uuid.New().String()
	channelID := uuid.New().String()

	if err := repo.Users().Create(ctx, &model.User{ID: ownerID, Email: "owner@example.com", Nickname: "Owner", Password: "hashed", InviteCode: "owner2"}); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := repo.Users().Create(ctx, &model.User{ID: otherID, Email: "other@example.com", Nickname: "Other", Password: "hashed", InviteCode: "other2"}); err != nil {
		t.Fatalf("create other: %v", err)
	}
	if err := repo.Channels().Create(ctx, &model.Channel{ID: channelID, UserID: ownerID, Platform: model.PlatformSeednote, Name: "SeedNote", Status: model.ChannelStatusActive}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: ownerID, ChannelID: channelID, Type: model.PlatformSeednote, Status: model.TaskStatusCompleted, Published: true}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := httptest.NewRequest("GET", "/tasks/"+taskID+"/seednote-analytics", nil)
	req.Header.Set("X-User-ID", otherID)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}
