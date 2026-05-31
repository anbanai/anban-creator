package handler

import (
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

func setupTaskHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func TestTaskCreateRejectsGenerateVideo(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "video-disabled@example.com",
		Password:   "hashed",
		InviteCode: "vidoff",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Channels().Create(ctx, &model.Channel{
		ID:       channelID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ChannelStatusActive,
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil)
	handler := NewTaskHandler(taskSvc, &logger)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return handler.Create(c)
	})

	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(`{
		"channel_id": "`+channelID+`",
		"prompt": "test",
		"generate_video": true
	}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestTaskCreatePromptLengthLimit(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "prompt-limit@example.com",
		Password:   "hashed",
		InviteCode: "promptlimit",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Channels().Create(ctx, &model.Channel{
		ID:       channelID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ChannelStatusActive,
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil)
	handler := NewTaskHandler(taskSvc, &logger)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return handler.Create(c)
	})

	tests := []struct {
		name       string
		prompt     string
		wantStatus int
	}{
		{"allows 5120 characters", strings.Repeat("a", 5120), fiber.StatusOK},
		{"rejects more than 5120 characters", strings.Repeat("a", 5121), fiber.StatusBadRequest},
		{"counts unicode characters", strings.Repeat("汉", 5120), fiber.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"channel_id":"` + channelID + `","prompt":"` + tt.prompt + `"}`
			req := httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
