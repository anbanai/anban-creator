package service

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// cancelingExecutor cancels the parent execution ctx the moment Execute runs,
// then returns a successful result. This reproduces the production failure
// mode where the asynq deadline expires mid-pipeline: by the time the
// post-execution finalize phase runs, the execution ctx is dead. The finalize
// writes must use the decoupled persistCtx (not the expired ctx), otherwise
// the task stays stuck in "running" and all completed work is lost.
type cancelingExecutor struct {
	parentCancel context.CancelFunc
	result       *agent.ExecutionResult
}

func (e *cancelingExecutor) Execute(ctx context.Context, opts *agent.ExecutionOptions) (*agent.ExecutionResult, error) {
	e.parentCancel()
	return e.result, nil
}

// TestHandleExecution_PersistsOutcomeOnExpiredContext is the regression test for
// the "context deadline exceeded" cascade. Before the fix, the cancel-path DB
// writes ran on the (now-expired) asynq ctx and silently failed, leaving the
// task stuck in "running". With persistCtx, the task reaches a terminal state.
func TestHandleExecution_PersistsOutcomeOnExpiredContext(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	bgCtx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	// Workspace with a real output file so uploadMissingTaskFiles has work to do.
	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "content.md"), []byte("# 标题\n\n正文"), 0644); err != nil {
		t.Fatalf("write content: %v", err)
	}

	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(bgCtx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	execCtx, cancel := context.WithCancel(context.Background())
	exec := &cancelingExecutor{
		parentCancel: cancel,
		result:       &agent.ExecutionResult{Success: true, WorkDir: workDir},
	}
	svc := NewTaskService(repo, exec, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(execCtx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}

	found, err := repo.Tasks().FindByID(bgCtx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	// execCtx was canceled, so the task lands in "cancelled" — the point is it
	// reached a TERMINAL state. Without persistCtx, the writes would run on the
	// dead ctx and the task would stay "running" (the original bug).
	if found.Status == model.TaskStatusRunning {
		t.Fatalf("task stuck in running — post-execution writes failed on expired ctx (decoupling bug); status=%q", found.Status)
	}
	if found.CompletedAt == nil {
		t.Fatalf("completed_at not set — cancel-path writes failed on expired ctx; status=%q", found.Status)
	}
}
