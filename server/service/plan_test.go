package service

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// setupTestDB creates an in-memory SQLite database for testing.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(&model.Plan{}, &model.Task{}, &model.UserConfig{}, &model.User{}, &model.LoginSession{}, &model.TaskFile{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

// setupTestPlanService creates a PlanService with a test database.
func setupTestPlanService(t *testing.T) (*PlanService, repository.Repository) {
	t.Helper()
	db := setupTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
	svc := NewPlanService(repo, &logger)
	return svc, repo
}

func TestPlanService_Create(t *testing.T) {
	svc, _ := setupTestPlanService(t)
	ctx := context.Background()

	tests := []struct {
		name      string
		planType  string
		cronExpr  string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "valid plan",
			planType: model.ScopeRednote,
			cronExpr: "0 9 * * 1-5",
			wantErr:  false,
		},
		{
			name:     "every minute",
			planType: model.ScopeArticle,
			cronExpr: "* * * * *",
			wantErr:  false,
		},
		{
			name:      "empty type",
			planType:  "",
			cronExpr:  "0 9 * * *",
			wantErr:   true,
			errSubstr: "type is required",
		},
		{
			name:      "empty cron",
			planType:  model.ScopeRednote,
			cronExpr:  "",
			wantErr:   true,
			errSubstr: "cron_expr is required",
		},
		{
			name:      "invalid cron",
			planType:  model.ScopeRednote,
			cronExpr:  "invalid cron",
			wantErr:   true,
			errSubstr: "invalid cron expression",
		},
		{
			name:      "invalid cron fields",
			planType:  model.ScopeXls,
			cronExpr:  "60 25 * * *",
			wantErr:   true,
			errSubstr: "invalid cron expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := svc.Create(ctx, "user-1", tt.planType, "Test Plan", "Description", tt.cronExpr, "topic hint")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if plan.ID == "" {
				t.Error("expected non-empty plan ID")
			}
			if plan.Status != model.PlanStatusActive {
				t.Errorf("expected status %q, got %q", model.PlanStatusActive, plan.Status)
			}
			if plan.NextRunAt == nil {
				t.Error("expected non-nil next_run_at")
			}
			if plan.CronExpr != tt.cronExpr {
				t.Errorf("expected cron_expr %q, got %q", tt.cronExpr, plan.CronExpr)
			}
		})
	}
}

func TestPlanService_GetByID(t *testing.T) {
	svc, _ := setupTestPlanService(t)
	ctx := context.Background()

	// Create a plan first.
	created, err := svc.Create(ctx, "user-1", model.ScopeRednote, "My Plan", "desc", "0 9 * * *", "hint")
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Get by ID.
	found, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if found.Title != "My Plan" {
		t.Errorf("expected title 'My Plan', got %q", found.Title)
	}

	// Non-existent ID.
	_, err = svc.GetByID(ctx, "non-existent-id")
	if err == nil {
		t.Error("expected error for non-existent ID")
	}
}

func TestPlanService_List(t *testing.T) {
	svc, _ := setupTestPlanService(t)
	ctx := context.Background()

	// Create multiple plans.
	for i := 0; i < 5; i++ {
		_, err := svc.Create(ctx, "user-1", model.ScopeRednote, "Plan "+string(rune('A'+i)), "desc", "0 9 * * *", "hint")
		if err != nil {
			t.Fatalf("create plan %d: %v", i, err)
		}
	}

	// Create plans for another user.
	_, err := svc.Create(ctx, "user-2", model.ScopeArticle, "Other Plan", "desc", "0 10 * * *", "hint")
	if err != nil {
		t.Fatalf("create plan for user-2: %v", err)
	}

	// List user-1 plans.
	plans, total, err := svc.List(ctx, "user-1", 0, 10)
	if err != nil {
		t.Fatalf("list plans: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(plans) != 5 {
		t.Errorf("expected 5 plans, got %d", len(plans))
	}

	// Test pagination: offset=0, limit=2.
	plans, _, err = svc.List(ctx, "user-1", 0, 2)
	if err != nil {
		t.Fatalf("list plans paginated: %v", err)
	}
	if len(plans) != 2 {
		t.Errorf("expected 2 plans, got %d", len(plans))
	}

	// List for different user.
	plans, total, err = svc.List(ctx, "user-2", 0, 10)
	if err != nil {
		t.Fatalf("list user-2 plans: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
}

func TestPlanService_Update(t *testing.T) {
	svc, _ := setupTestPlanService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, "user-1", model.ScopeRednote, "Old Title", "old desc", "0 9 * * *", "old hint")
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Update title only.
	updated, err := svc.Update(ctx, created.ID, "New Title", "new desc", "", "new hint")
	if err != nil {
		t.Fatalf("update plan: %v", err)
	}
	if updated.Title != "New Title" {
		t.Errorf("expected title 'New Title', got %q", updated.Title)
	}
	// Cron should not have changed since we passed empty.
	if updated.CronExpr != "0 9 * * *" {
		t.Errorf("expected cron_expr unchanged, got %q", updated.CronExpr)
	}

	// Update with new cron expression.
	updated, err = svc.Update(ctx, created.ID, "New Title", "new desc", "0 18 * * *", "new hint")
	if err != nil {
		t.Fatalf("update plan cron: %v", err)
	}
	if updated.CronExpr != "0 18 * * *" {
		t.Errorf("expected cron_expr '0 18 * * *', got %q", updated.CronExpr)
	}
	if updated.NextRunAt == nil {
		t.Error("expected non-nil next_run_at after cron update")
	}

	// Update with invalid cron.
	_, err = svc.Update(ctx, created.ID, "Title", "desc", "bad cron", "hint")
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestPlanService_Pause_Resume(t *testing.T) {
	svc, _ := setupTestPlanService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, "user-1", model.ScopeRednote, "Plan", "desc", "0 9 * * *", "hint")
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Pause.
	err = svc.Pause(ctx, created.ID)
	if err != nil {
		t.Fatalf("pause plan: %v", err)
	}
	paused, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get paused plan: %v", err)
	}
	if paused.Status != model.PlanStatusPaused {
		t.Errorf("expected status %q, got %q", model.PlanStatusPaused, paused.Status)
	}
	if paused.NextRunAt != nil {
		t.Error("expected nil next_run_at when paused")
	}

	// Resume.
	err = svc.Resume(ctx, created.ID)
	if err != nil {
		t.Fatalf("resume plan: %v", err)
	}
	resumed, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get resumed plan: %v", err)
	}
	if resumed.Status != model.PlanStatusActive {
		t.Errorf("expected status %q, got %q", model.PlanStatusActive, resumed.Status)
	}
	if resumed.NextRunAt == nil {
		t.Error("expected non-nil next_run_at when resumed")
	}
}

func TestPlanService_Delete(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, "user-1", model.ScopeRednote, "Plan", "desc", "0 9 * * *", "hint")
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Delete.
	err = svc.Delete(ctx, created.ID)
	if err != nil {
		t.Fatalf("delete plan: %v", err)
	}

	// Verify deletion.
	_, err = repo.Plans().FindByID(ctx, created.ID)
	if err == nil {
		t.Error("expected error when finding deleted plan")
	}
}
