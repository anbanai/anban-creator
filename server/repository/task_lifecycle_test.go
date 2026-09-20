package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/anbanai/anban-creator/server/model"
)

func TestFinalizeTaskForExecutionNormalizesLifecycleAtomically(t *testing.T) {
	repo := New(setupTestDB(t))
	executionID := uuid.NewString()
	now := time.Now().Add(-time.Minute)
	task := &model.Task{
		ID: uuid.NewString(), UserID: uuid.NewString(), Type: model.PlatformArticle,
		Status: model.TaskStatusRunning, CurrentExecutionID: &executionID,
		Lifecycle: datatypes.NewJSONType(model.TaskLifecycle{
			Version: 1, Revision: 3, ExecutionID: executionID, UpdatedAt: now,
			Stages: []model.TaskLifecycleStage{
				{ID: "research", Title: "研究", Source: "agent", Kind: "work", State: "complete", StartedAt: &now, CompletedAt: &now},
				{ID: "writing", Title: "写作", Source: "agent", Kind: "work", State: "active", StartedAt: &now},
				{ID: "review", Title: "复核", Source: "agent", Kind: "work", State: "pending"},
				{ID: "system_draft", Title: "创建公众号草稿", Source: "server", Kind: "draft", State: "pending"},
			},
		}),
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	won, err := repo.Tasks().FinalizeTaskForExecution(context.Background(), task.ID, executionID, model.TaskStatusCompleted, "", model.LifecycleTerminalWork)
	if err != nil || !won {
		t.Fatalf("finalize = %v, %v", won, err)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := persisted.Lifecycle.Data()
	if lifecycle.Revision != 4 || lifecycle.Stages[1].State != model.TaskLifecycleStateComplete || lifecycle.Stages[1].CompletedAt == nil || lifecycle.Stages[2].State != model.TaskLifecycleStateSkipped {
		t.Fatalf("normalized lifecycle = %#v", lifecycle)
	}
	if lifecycle.Stages[3].State != model.TaskLifecycleStatePending {
		t.Fatalf("server publication stage was finalized with agent work: %#v", lifecycle.Stages[3])
	}
}
