package service

import (
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func setupTaskFeedbackService(t *testing.T) (repository.Repository, *model.Task) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskFeedback{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	task := &model.Task{ID: uuid.NewString(), UserID: "user-1", Type: "article", Status: model.TaskStatusCompleted}
	if err := repo.Tasks().Create(t.Context(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	return repo, task
}

func TestFeedbackServiceTaskFeedbackValidatesAndUpdates(t *testing.T) {
	repo, task := setupTaskFeedbackService(t)
	svc := NewFeedbackService(repo, nil)

	first, err := svc.UpsertTaskFeedback(t.Context(), "user-1", task.ID, 5, "  很满意  ")
	if err != nil {
		t.Fatalf("create task feedback: %v", err)
	}
	if first.Rating != 5 || first.Content != "很满意" {
		t.Fatalf("unexpected first feedback: %#v", first)
	}
	second, err := svc.UpsertTaskFeedback(t.Context(), "user-1", task.ID, 3, "需要调整标题")
	if err != nil {
		t.Fatalf("update task feedback: %v", err)
	}
	if second.ID != first.ID || second.Rating != 3 || second.Content != "需要调整标题" {
		t.Fatalf("feedback was not updated in place: first=%#v second=%#v", first, second)
	}
}

func TestFeedbackServiceTaskFeedbackRejectsInvalidStates(t *testing.T) {
	repo, task := setupTaskFeedbackService(t)
	svc := NewFeedbackService(repo, nil)

	for _, tc := range []struct {
		name    string
		user    string
		rating  int
		content string
		status  string
		want    error
	}{
		{name: "rating too low", user: "user-1", rating: 0, want: ErrTaskFeedbackInvalidRating},
		{name: "rating too high", user: "user-1", rating: 6, want: ErrTaskFeedbackInvalidRating},
		{name: "content too long", user: "user-1", rating: 4, content: string(make([]byte, 1001)), want: ErrTaskFeedbackContentTooLong},
		{name: "wrong user", user: "user-2", rating: 4, want: ErrTaskFeedbackForbidden},
		{name: "not completed", user: "user-1", rating: 4, status: model.TaskStatusRunning, want: ErrTaskFeedbackNotCompleted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.status != "" {
				task.Status = tc.status
				if err := repo.Tasks().Update(t.Context(), task); err != nil {
					t.Fatal(err)
				}
			}
			_, err := svc.UpsertTaskFeedback(t.Context(), tc.user, task.ID, tc.rating, tc.content)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			task.Status = model.TaskStatusCompleted
			_ = repo.Tasks().Update(t.Context(), task)
		})
	}
}

func TestFeedbackServiceGetTaskFeedbackReturnsNilWhenMissing(t *testing.T) {
	repo, task := setupTaskFeedbackService(t)
	svc := NewFeedbackService(repo, nil)
	got, err := svc.GetTaskFeedback(t.Context(), "user-1", task.ID)
	if err != nil {
		t.Fatalf("get task feedback: %v", err)
	}
	if got != nil {
		t.Fatalf("feedback = %#v, want nil", got)
	}
}

func TestFeedbackServiceGetTaskFeedbackRejectsNonCompletedTask(t *testing.T) {
	repo, task := setupTaskFeedbackService(t)
	svc := NewFeedbackService(repo, nil)
	if _, err := svc.UpsertTaskFeedback(t.Context(), "user-1", task.ID, 5, "完成时评价"); err != nil {
		t.Fatalf("create task feedback: %v", err)
	}
	task.Status = model.TaskStatusRunning
	if err := repo.Tasks().Update(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetTaskFeedback(t.Context(), "user-1", task.ID); !errors.Is(err, ErrTaskFeedbackNotCompleted) {
		t.Fatalf("get error = %v, want %v", err, ErrTaskFeedbackNotCompleted)
	}
}
