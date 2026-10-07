package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func TestFeedbackDashboardCountsProjectCoverage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	repo := repository.New(db)
	project := &model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformWechat, Name: "项目"}
	if err := repo.Projects().Create(t.Context(), project); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	observations := []model.AnalyticsObservation{
		{ID: "observation-project-1", ProjectID: project.ID, ContentID: "content-1", MetricBasis: "cumulative", StatDate: "2026-10-07", Source: "test", EffectiveAt: now, ReceivedAt: now},
		{ID: "observation-project-2", ProjectID: "project-2", ContentID: "content-2", MetricBasis: "cumulative", StatDate: "2026-10-07", Source: "test", EffectiveAt: now, ReceivedAt: now},
		{ID: "observation-revoked", ProjectID: project.ID, ContentID: "content-3", MetricBasis: "cumulative", StatDate: "2026-10-07", Source: "test", EffectiveAt: now, ReceivedAt: now, RevokedAt: &now},
		{ID: "observation-empty-content", ProjectID: project.ID, MetricBasis: "cumulative", StatDate: "2026-10-07", Source: "test", EffectiveAt: now, ReceivedAt: now},
	}
	if err := db.Create(&observations).Error; err != nil {
		t.Fatal(err)
	}
	logger := zerolog.Nop()
	h := NewFeedbackDashboardHandler(service.NewProjectService(repo, &logger), repo, nil)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", "user-1"); return c.Next() })
	app.Get("/projects/:id/feedback", h.Get)

	response, err := app.Test(httptest.NewRequest("GET", "/projects/project-1/feedback", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("GET status = %d, want %d", response.StatusCode, fiber.StatusOK)
	}
	var body struct {
		Data struct {
			Analytics struct {
				ContentCount          int64 `json:"content_count"`
				ValidObservationCount int64 `json:"valid_observation_count"`
			} `json:"analytics"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Analytics.ContentCount != 0 || body.Data.Analytics.ValidObservationCount != 1 {
		t.Fatalf("coverage counts = (%d, %d), want (0, 1)", body.Data.Analytics.ContentCount, body.Data.Analytics.ValidObservationCount)
	}
}
