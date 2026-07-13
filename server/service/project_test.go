package service

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type projectMemoryLifecycleFake struct {
	ids []string
	err error
}

func (f *projectMemoryLifecycleFake) DeleteProjectMemory(_ context.Context, projectID string) error {
	f.ids = append(f.ids, projectID)
	return f.err
}

func TestProjectDeleteRemovesDeterministicMemoryPVC(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := NewProjectService(repo, &logger)
	memory := &projectMemoryLifecycleFake{}
	svc.SetProjectMemoryLifecycle(memory)
	ctx := context.Background()
	user := &model.User{ID: uuid.NewString(), Email: uuid.NewString() + "@example.com", Password: "x", InviteCode: uuid.NewString()[:12]}
	if err := repo.Users().Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	project := &model.Project{ID: uuid.NewString(), UserID: user.ID, Name: "delete", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, user.ID, project.ID); err != nil {
		t.Fatal(err)
	}
	if len(memory.ids) != 1 || memory.ids[0] != project.ID {
		t.Fatalf("memory deletions=%v", memory.ids)
	}
	if _, err := repo.Projects().FindByID(ctx, project.ID); err == nil {
		t.Fatal("project still exists")
	}
}

func TestProjectDeleteSurfacesMemoryIdentityMismatchAfterDatabaseDelete(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := NewProjectService(repo, &logger)
	memory := &projectMemoryLifecycleFake{err: errors.New("PVC identity mismatch")}
	svc.SetProjectMemoryLifecycle(memory)
	ctx := context.Background()
	user := &model.User{ID: uuid.NewString(), Email: uuid.NewString() + "@example.com", Password: "x", InviteCode: uuid.NewString()[:12]}
	if err := repo.Users().Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	project := &model.Project{ID: uuid.NewString(), UserID: user.ID, Name: "delete", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, user.ID, project.ID); err == nil {
		t.Fatal("expected memory identity mismatch")
	}
	if len(memory.ids) != 1 {
		t.Fatalf("memory deletions=%v", memory.ids)
	}
}
