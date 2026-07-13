package repository

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTaskFileRepositoryPublishesExecutionAtomicallyAndIdempotently(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
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
	if err := repo.TaskFiles().PublishExecution(ctx, "t1", "e2"); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().PublishExecution(ctx, "t1", "e2"); err != nil {
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
