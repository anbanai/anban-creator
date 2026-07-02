package handler

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

// TestCreatePlan_ArticleImageTogglesPersist verifies the plan handler→service→
// model→DB round-trip persists an explicit `false` for both article image
// toggles. Same regression guard as TestCreateTask_ArticleImageTogglesPersist
// (*bool / gorm:"default:true" mitigation + handler wiring omission): the
// plan-level toggles propagate to spawned tasks via CreateFromPlan, so a plan
// created with cover/content off must persist those choices. The value is
// re-read from the repo to assert the persisted state.
func TestCreatePlan_ArticleImageTogglesPersist(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "plan-toggles@example.com",
		Password:   "hashed",
		InviteCode: "plantoggles",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	planSvc := service.NewPlanService(repo, &logger)
	h := NewPlanHandler(planSvc, &logger)

	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	body := `{"project_id":"` + projectID + `","cron_expr":"0 9 * * *","prompt":"计划开关持久化测试","article_with_cover":false,"article_with_content_images":false}`
	req := httptest.NewRequest("POST", "/plans", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	plans, err := repo.Plans().FindByUserID(ctx, userID, projectID, 0, 10)
	if err != nil {
		t.Fatalf("find plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	got := plans[0]
	for label, ptr := range map[string]*bool{
		"ArticleWithCover":         got.ArticleWithCover,
		"ArticleWithContentImages": got.ArticleWithContentImages,
	} {
		switch {
		case ptr == nil:
			t.Errorf("%s = nil, want non-nil false", label)
		case *ptr:
			t.Errorf("%s = true, want false", label)
		}
	}
}
