package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type taskWorkspaceLifecycleFake struct {
	tasks []*model.Task
	err   error
}

func (f *taskWorkspaceLifecycleFake) DeleteTaskWorkspace(_ context.Context, task *model.Task) error {
	f.tasks = append(f.tasks, task)
	return f.err
}

func TestTaskDeleteRemovesDeterministicWorkspacePVC(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil)
	workspace := &taskWorkspaceLifecycleFake{}
	svc.SetTaskWorkspaceLifecycle(workspace)
	ctx := context.Background()
	task := &model.Task{ID: uuid.NewString(), UserID: uuid.NewString(), ProjectID: uuid.NewString(), Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if len(workspace.tasks) != 1 || workspace.tasks[0].ID != task.ID || workspace.tasks[0].ProjectID != task.ProjectID || workspace.tasks[0].UserID != task.UserID {
		t.Fatalf("workspace deletions = %#v", workspace.tasks)
	}
}

func TestTaskDeleteSurfacesWorkspaceIdentityMismatch(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil)
	svc.SetTaskWorkspaceLifecycle(&taskWorkspaceLifecycleFake{err: errors.New("PVC identity mismatch")})
	ctx := context.Background()
	task := &model.Task{ID: uuid.NewString(), UserID: uuid.NewString(), ProjectID: uuid.NewString(), Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	err := svc.Delete(ctx, task.ID)
	if err == nil || !strings.Contains(err.Error(), "PVC identity mismatch") {
		t.Fatalf("Delete error = %v, want workspace identity mismatch", err)
	}
	if _, err := repo.Tasks().FindByID(ctx, task.ID); err != nil {
		t.Fatalf("task was deleted after workspace ownership failure: %v", err)
	}
}
