package repository

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTaskFileRepositoryPublishesExecutionAtomicallyAndIdempotently(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	rows := []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "old.md", FileName: "old.md"},
		{TaskID: "t1", ExecutionID: "e2", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "new.md", FileName: "new.md"},
	}
	if err := repo.TaskFiles().BatchCreate(ctx, rows); err != nil {
		t.Fatal(err)
	}
	visible, err := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if err != nil || len(visible) != 1 || visible[0].ExecutionID != "e1" {
		t.Fatalf("visible before publish = %#v, %v", visible, err)
	}
	if err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e2"); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e2"); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	visible, err = repo.TaskFiles().FindByTaskID(ctx, "t1")
	if err != nil || len(visible) != 1 || visible[0].ExecutionID != "e2" {
		t.Fatalf("visible after publish = %#v, %v", visible, err)
	}
	old, _ := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if len(old) != 1 || old[0].State != model.TaskFileStateSuperseded {
		t.Fatalf("old rows = %#v", old)
	}
}

func TestTaskFileRepositoryPublishWithNoTargetRowsPreservesPublishedSet(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "old.md", FileName: "old.md"}); err != nil {
		t.Fatal(err)
	}
	err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e2")
	if !errors.Is(err, ErrNoPendingExecutionArtifacts) {
		t.Fatalf("error = %v, want ErrNoPendingExecutionArtifacts", err)
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 1 || visible[0].ExecutionID != "e1" {
		t.Fatalf("published set changed: %#v", visible)
	}
}

func TestTaskFileRepositoryRejectsStaleExecutionWithoutChangingPublishedSet(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e2", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "current.md", FileName: "current.md"},
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "stale.md", FileName: "stale.md"},
	}); err != nil {
		t.Fatal(err)
	}
	err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e1")
	if !errors.Is(err, ErrTaskFileExecutionNotCurrent) {
		t.Fatalf("error = %v, want stale execution", err)
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 1 || visible[0].ExecutionID != "e2" {
		t.Fatalf("published set changed: %#v", visible)
	}
}

func TestTaskFileRepositoryPublishRollbackPreservesOldSet(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "old.md", FileName: "old.md"},
		{TaskID: "t1", ExecutionID: "e2", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "new.md", FileName: "new.md"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_publish BEFORE UPDATE ON task_files WHEN OLD.execution_id = 'e2' BEGIN SELECT RAISE(ABORT, 'forced publish failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e2"); err == nil {
		t.Fatal("forced publication failure unexpectedly succeeded")
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 1 || visible[0].ExecutionID != "e1" || visible[0].State != model.TaskFileStatePublished {
		t.Fatalf("rollback visible set = %#v", visible)
	}
}

func TestTaskFileRepositoryPublishRejectsStoppedTask(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e1")
	task, _ := repo.Tasks().FindByID(ctx, "t1")
	task.Status = model.TaskStatusFailed
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "a.md", FileName: "a.md"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().PublishCurrentExecution(ctx, "t1", "e1"); !errors.Is(err, ErrTaskFileTaskNotRunning) {
		t.Fatalf("error = %v", err)
	}
}

func TestTaskFileRepositoryConcurrentAttemptsOnlyCurrentPublishes(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// SQLite's :memory: database is connection-local. A single connection also
	// serializes transactions, which is its safe equivalent to MySQL FOR UPDATE.
	sqlDB.SetMaxOpenConns(1)
	repo := New(db)
	ctx := context.Background()
	seedCurrentTaskForArtifacts(t, repo, "t1", "e2")
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "stale.md", FileName: "stale.md"},
		{TaskID: "t1", ExecutionID: "e2", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "current.md", FileName: "current.md"},
	}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, executionID := range []string{"e1", "e2"} {
		wg.Add(1)
		go func(i int, executionID string) {
			defer wg.Done()
			<-start
			errs[i] = repo.TaskFiles().PublishCurrentExecution(ctx, "t1", executionID)
		}(i, executionID)
	}
	close(start)
	wg.Wait()
	if !errors.Is(errs[0], ErrTaskFileExecutionNotCurrent) || errs[1] != nil {
		t.Fatalf("publish errors = %#v", errs)
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 1 || visible[0].ExecutionID != "e2" {
		t.Fatalf("visible = %#v", visible)
	}
}

func seedCurrentTaskForArtifacts(t *testing.T, repo Repository, taskID, executionID string) {
	t.Helper()
	ctx := context.Background()
	userID, projectID := "u-"+taskID, "p-"+taskID
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: taskID + "@example.com", Password: "x", InviteCode: taskID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "p", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning, CurrentExecutionID: &executionID}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskFileRepositoryDiscardExecutionHidesPendingRows(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "a.md", FileName: "a.md"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().DiscardExecution(ctx, "e1"); err != nil {
		t.Fatal(err)
	}
	rows, _ := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if len(rows) != 1 || rows[0].State != model.TaskFileStateSuperseded {
		t.Fatalf("rows = %#v", rows)
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, "t1")
	if len(visible) != 0 {
		t.Fatalf("discarded rows leaked: %#v", visible)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionRollsBackOnFailure(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	original := &model.TaskFile{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "old.md", FileName: "old.md"}
	if err := repo.TaskFiles().Create(ctx, original); err != nil {
		t.Fatal(err)
	}
	bad := []*model.TaskFile{
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "new.md", FileName: "new.md"},
		{TaskID: "t1", ExecutionID: "e1", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "new.md", FileName: "new.md"},
	}
	if err := repo.TaskFiles().ReplacePendingExecution(ctx, "t1", "e1", bad); err == nil {
		t.Fatal("invalid replacement unexpectedly succeeded")
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if err != nil || len(rows) != 1 || rows[0].FilePath != "old.md" {
		t.Fatalf("rollback rows = %#v, %v", rows, err)
	}
}

func TestTaskFileRepositoryReplacePendingExecutionForcesDeclaredScope(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	files := []*model.TaskFile{{TaskID: "foreign", ExecutionID: "foreign", State: model.TaskFileStatePublished, Role: model.FileRoleOther, FilePath: "output/a.md", FileName: "a.md"}}
	if err := repo.TaskFiles().ReplacePendingExecution(ctx, "t1", "e1", files); err != nil {
		t.Fatal(err)
	}
	rows, _ := repo.TaskFiles().FindByExecutionID(ctx, "e1")
	if len(rows) != 1 || rows[0].TaskID != "t1" || rows[0].State != model.TaskFileStatePending {
		t.Fatalf("scoped rows = %#v", rows)
	}
	if err := repo.TaskFiles().ReplacePendingExecution(ctx, "t1", "e1", []*model.TaskFile{{Role: model.FileRoleOther, FilePath: "../escape", FileName: "escape"}}); err == nil {
		t.Fatal("unsafe path accepted")
	}
}
