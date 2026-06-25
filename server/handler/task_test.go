package handler

import (
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

type noopTaskEnqueuer struct{}

func (noopTaskEnqueuer) Enqueue(string, []byte) error {
	return nil
}

func (noopTaskEnqueuer) EnqueueIn(string, []byte, time.Duration) error {
	return nil
}

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

func TestTaskCreatePromptLengthLimit(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "prompt-limit@example.com",
		Password:   "hashed",
		InviteCode: "promptlimit",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
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
			body := `{"project_id":"` + projectID + `","prompt":"` + tt.prompt + `"}`
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

// TestCreateTask_TemplateIDValidation verifies that the handler:
//   - rejects malformed template_id with 400
//   - accepts a valid UUID and persists it on the created task
func TestCreateTask_TemplateIDValidation(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "tmpl-validate@example.com",
		Password:   "hashed",
		InviteCode: "tmplvalidate",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	t.Run("rejects malformed template_id", func(t *testing.T) {
		body := `{"project_id":"` + projectID + `","template_id":"not-a-uuid"}`
		req := httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("accepts valid template_id and persists it", func(t *testing.T) {
		templateID := uuid.New().String()
		body := `{"project_id":"` + projectID + `","template_id":"` + templateID + `"}`
		req := httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		// Verify the created task carries the template_id.
		tasks, err := repo.Tasks().FindByUserID(ctx, userID, "", 0, 50)
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		var latest *model.Task
		for i := range tasks {
			if tasks[i].TemplateID != nil && *tasks[i].TemplateID == templateID {
				latest = tasks[i]
				break
			}
		}
		if latest == nil {
			t.Fatalf("no task with template_id=%s was persisted", templateID)
		}
	})

	t.Run("omits template_id when not provided", func(t *testing.T) {
		body := `{"project_id":"` + projectID + `"}`
		req := httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})
}

// TestCreateTask_ImageModelKeyTierForbidden verifies that the handler returns
// 403 when a Free-tier user tries to create a task with a Pro-tier image preset,
// or with "custom" (Enterprise-only). Also covers the fail-closed path:
// Free users CAN still create tasks with Free-tier or empty keys.
func TestCreateTask_ImageModelKeyTierForbidden(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "tier-forbidden@example.com",
		Password:   "hashed",
		InviteCode: "tierforbidden",
		Tier:       model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	presets := []config.ImageModelPreset{
		{Key: "volcengine-standard", Provider: "volcengine", Model: "doubao-seedream", MinTier: "free"},
		{Key: "gemini-pro", Provider: "gemini", Model: "gemini-3-pro", MinTier: "pro"},
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetImagePresets(presets)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "free tier + free preset accepted",
			body:       `{"project_id":"` + projectID + `","image_model_key":"volcengine-standard"}`,
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "free tier + empty key accepted",
			body:       `{"project_id":"` + projectID + `"}`,
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "free tier + pro preset rejected",
			body:       `{"project_id":"` + projectID + `","image_model_key":"gemini-pro"}`,
			wantStatus: fiber.StatusForbidden,
		},
		{
			name:       "free tier + custom rejected",
			body:       `{"project_id":"` + projectID + `","image_model_key":"custom"}`,
			wantStatus: fiber.StatusForbidden,
		},
		{
			name:       "free tier + unknown key rejected",
			body:       `{"project_id":"` + projectID + `","image_model_key":"made-up"}`,
			wantStatus: fiber.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/tasks", strings.NewReader(tt.body))
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
