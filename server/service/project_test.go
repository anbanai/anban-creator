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

type rejectingProjectCASRepository struct {
	repository.ProjectRepository
}

func (r rejectingProjectCASRepository) UpdateIfReferenceImageAssetID(context.Context, *model.Project, string) (bool, error) {
	return false, nil
}

type projectCASRepositoryOverride struct {
	repository.Repository
	projects repository.ProjectRepository
}

func (r projectCASRepositoryOverride) Projects() repository.ProjectRepository {
	return r.projects
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

func TestProjectUpdateReferenceImageAssetIDOnlyWhenExplicitlySet(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := NewProjectService(repo, &logger)
	ctx := context.Background()
	userID := uuid.NewString()
	project := &model.Project{
		ID: uuid.NewString(), UserID: userID, Name: "brand", Platform: model.PlatformArticle,
		ReferenceImageAssetID: "asset-old", Status: model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Update(ctx, userID, project.ID, &model.Project{ReferenceImageAssetID: "ignored"}); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Projects().FindByID(ctx, project.ID)
	if got.ReferenceImageAssetID != "asset-old" {
		t.Fatalf("implicit update changed reference to %q", got.ReferenceImageAssetID)
	}

	if _, err := svc.Update(ctx, userID, project.ID, &model.Project{ReferenceImageAssetID: "asset-new", ReferenceImageSet: true}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Projects().FindByID(ctx, project.ID)
	if got.ReferenceImageAssetID != "asset-new" {
		t.Fatalf("explicit update left reference at %q", got.ReferenceImageAssetID)
	}

	if _, err := svc.Update(ctx, userID, project.ID, &model.Project{ReferenceImageSet: true}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Projects().FindByID(ctx, project.ID)
	if got.ReferenceImageAssetID != "" {
		t.Fatalf("explicit clear left reference at %q", got.ReferenceImageAssetID)
	}
}

func TestProjectServiceUpdateIfReferenceImageAssetIDReturnsConflictWithoutWriting(t *testing.T) {
	base := repository.New(setupTaskTestDB(t))
	repo := projectCASRepositoryOverride{
		Repository: base,
		projects:   rejectingProjectCASRepository{ProjectRepository: base.Projects()},
	}
	logger := zerolog.New(io.Discard)
	svc := NewProjectService(repo, &logger)
	project := &model.Project{
		ID: uuid.NewString(), UserID: "user-1", Name: "before", Platform: model.PlatformArticle,
		ReferenceImageAssetID: "asset-a", Status: model.ProjectStatusActive,
	}
	if err := base.Projects().Create(t.Context(), project); err != nil {
		t.Fatal(err)
	}

	_, err := svc.UpdateIfReferenceImageAssetID(t.Context(), project.UserID, project.ID, &model.Project{Name: "after"}, "asset-a")
	if !errors.Is(err, ErrProjectUpdateConflict) {
		t.Fatalf("error = %v, want ErrProjectUpdateConflict", err)
	}
	persisted, err := base.Projects().FindByID(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != "before" || persisted.ReferenceImageAssetID != "asset-a" {
		t.Fatalf("conflicting update modified project: name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
	}
}
