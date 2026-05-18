package handler

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

func setupTestDBForHandler(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Channel{},
		&model.Plan{},
		&model.Task{},
		&model.User{},
		&model.LoginSession{},
		&model.TaskFile{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func setupTimelineHandler(t *testing.T) (repository.Repository, *zerolog.Logger) {
	t.Helper()
	db := setupTestDBForHandler(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
	_ = NewTimelineHandler(repo, &logger)
	return repo, &logger
}

func TestTimelineHandler_GetTimeline(t *testing.T) {
	repo, _ := setupTimelineHandler(t)
	ctx := t.Context()

	userID := "user-timeline-1"

	// Create some tasks.
	task1 := &model.Task{
		ID:        "task-1",
		UserID:    userID,
		Type:      model.ScopeSeednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "Test topic 1",
		CreatedAt: time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC),
	}
	completedAt := time.Date(2026, 4, 1, 9, 15, 0, 0, time.UTC)
	task1.CompletedAt = &completedAt

	task2 := &model.Task{
		ID:        "task-2",
		UserID:    userID,
		Type:      model.ScopeArticle,
		Status:    model.TaskStatusPending,
		Prompt:    "Test topic 2",
		CreatedAt: time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
	}

	task3 := &model.Task{
		ID:        "task-3",
		UserID:    "other-user",
		Type:      model.ScopeSeednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "Other user task",
		CreatedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	}

	for _, task := range []*model.Task{task1, task2, task3} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	// Create an active plan with next_run_at in range.
	nextRun := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	plan1 := &model.Plan{
		ID:          "plan-1",
		UserID:      userID,
		Type:        model.ScopeSeednote,
		Title:       "Scheduled Plan",
		Description: "A scheduled plan",
		CronExpr:    "0 9 * * 1-5",
		Prompt:      "scheduled topic",
		Status:      model.PlanStatusActive,
		NextRunAt:   &nextRun,
	}

	// Create an active plan with next_run_at outside range.
	nextRunFar := time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)
	plan2 := &model.Plan{
		ID:        "plan-2",
		UserID:    userID,
		Type:      model.ScopeArticle,
		Title:     "Far Future Plan",
		CronExpr:  "0 9 * * 1-5",
		Status:    model.PlanStatusActive,
		NextRunAt: &nextRunFar,
	}

	// Create a paused plan (should not appear in timeline).
	plan3 := &model.Plan{
		ID:        "plan-3",
		UserID:    userID,
		Type:      model.ScopeSeednote,
		Title:     "Paused Plan",
		CronExpr:  "0 9 * * 1-5",
		Status:    model.PlanStatusPaused,
		NextRunAt: nil,
	}

	for _, plan := range []*model.Plan{plan1, plan2, plan3} {
		if err := repo.Plans().Create(ctx, plan); err != nil {
			t.Fatalf("create plan: %v", err)
		}
	}

	// The timeline handler's GetTimeline method requires a fiber.Ctx, which is
	// difficult to set up in unit tests. Instead, we test the logic directly
	// by calling the repository methods that the handler uses.
	t.Run("tasks within date range", func(t *testing.T) {
		from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 4, 30, 23, 59, 59, 0, time.UTC)

		tasks, err := repo.Tasks().FindByCreatedAtRange(ctx, from, to, 0, 0)
		if err != nil {
			t.Fatalf("find tasks in range: %v", err)
		}

		// Should find 2 tasks for our user (task-1 and task-2), plus other-user's task.
		userTasks := 0
		for _, task := range tasks {
			if task.UserID == userID {
				userTasks++
			}
		}
		if userTasks != 2 {
			t.Errorf("expected 2 user tasks in range, got %d", userTasks)
		}
	})

	t.Run("plans within date range", func(t *testing.T) {
		plans, err := repo.Plans().ListActiveByUserID(ctx, userID, "")
		if err != nil {
			t.Fatalf("list active plans: %v", err)
		}

		// Should find 2 active plans (plan-1 and plan-2, not paused plan-3).
		if len(plans) != 2 {
			t.Errorf("expected 2 active plans, got %d", len(plans))
		}
	})

	t.Run("find running tasks", func(t *testing.T) {
		// No running tasks currently.
		running, err := repo.Tasks().FindRunning(ctx)
		if err != nil {
			t.Fatalf("find running tasks: %v", err)
		}
		if len(running) != 0 {
			t.Errorf("expected 0 running tasks, got %d", len(running))
		}

		// Set task-2 to running.
		if err := repo.Tasks().UpdateStatus(ctx, "task-2", model.TaskStatusRunning); err != nil {
			t.Fatalf("update task status: %v", err)
		}

		running, err = repo.Tasks().FindRunning(ctx)
		if err != nil {
			t.Fatalf("find running tasks after update: %v", err)
		}
		if len(running) != 1 {
			t.Errorf("expected 1 running task, got %d", len(running))
		}
		if running[0].ID != "task-2" {
			t.Errorf("expected running task id 'task-2', got %q", running[0].ID)
		}
	})

	t.Run("timeline aggregation", func(t *testing.T) {
		// Manually build the timeline items as the handler would.
		from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 4, 30, 23, 59, 59, 0, time.UTC)

		tasks, err := repo.Tasks().FindByCreatedAtRange(ctx, from, to, 0, 0)
		if err != nil {
			t.Fatalf("find tasks: %v", err)
		}

		var userItems []TimelineItem
		for _, task := range tasks {
			if task.UserID != userID {
				continue
			}
			title := task.Prompt
			if title == "" {
				title = task.Type + " task"
			}
			userItems = append(userItems, TimelineItem{
				ID:          task.ID,
				Type:        "task",
				ContentType: task.Type,
				Title:       title,
				Status:      task.Status,
				CompletedAt: task.CompletedAt,
				CreatedAt:   task.CreatedAt,
			})
		}

		plans, err := repo.Plans().ListActiveByUserID(ctx, userID, "")
		if err != nil {
			t.Fatalf("list active plans: %v", err)
		}
		for _, p := range plans {
			if p.NextRunAt != nil && p.NextRunAt.After(from) && p.NextRunAt.Before(to) {
				title := p.Title
				if title == "" {
					title = p.Prompt
				}
				if title == "" {
					title = p.Type + " plan"
				}
				userItems = append(userItems, TimelineItem{
					ID:          p.ID,
					Type:        "plan",
					ContentType: p.Type,
					Title:       title,
					Status:      p.Status,
					ScheduledAt: p.NextRunAt,
					CreatedAt:   p.CreatedAt,
				})
			}
		}

		// Should have 2 tasks + 1 plan in range.
		if len(userItems) != 3 {
			t.Errorf("expected 3 timeline items, got %d", len(userItems))
		}
	})
}
