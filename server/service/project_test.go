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

func setupProjectServiceTest(t *testing.T) (*ProjectService, repository.Repository, context.Context, string) {
	t.Helper()
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := NewProjectService(repo, &logger)
	ctx := context.Background()
	userID := uuid.NewString()
	user := &model.User{
		ID:         userID,
		Email:      uuid.NewString() + "@example.com",
		Password:   "x",
		InviteCode: uuid.NewString()[:12],
	}
	if err := repo.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return svc, repo, ctx, userID
}

func TestProjectServiceMontageDefaultsSurviveCreateAndUpdate(t *testing.T) {
	svc, repo, ctx, userID := setupProjectServiceTest(t)
	project := &model.Project{
		Platform: model.PlatformMontage,
		Name:     "launch",
	}
	project.SetMontageDefaults(model.MontageDefaults{
		DefaultPipeline: "social-short",
		Preferences: model.MontagePreferences{
			AspectRatio:     "9:16",
			DurationSeconds: 45,
			Style:           "documentary",
			MusicPrompt:     "minimal electronic",
			SubtitleMode:    "burned-in",
			VoiceoverMode:   "narrated",
		},
		AssetGuidance:   "prefer uploaded footage",
		DeliveryTargets: []string{"final_video", "subtitles"},
	})
	project.MontageDefaultsSet = true

	created, err := svc.Create(ctx, userID, project)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	stored, err := repo.Projects().FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	got := stored.MontageDefaults.Data()
	if got.DefaultPipeline != "social-short" || got.Preferences.DurationSeconds != 45 || got.AssetGuidance != "prefer uploaded footage" {
		t.Fatalf("created MontageDefaults = %#v", got)
	}
	if len(got.DeliveryTargets) != 2 || got.DeliveryTargets[1] != "subtitles" {
		t.Fatalf("created DeliveryTargets = %#v", got.DeliveryTargets)
	}

	update := &model.Project{Platform: model.PlatformMontage}
	update.SetMontageDefaults(model.MontageDefaults{
		DefaultPipeline: "product-demo",
		Preferences:     model.MontagePreferences{AspectRatio: "16:9", DurationSeconds: 30},
		DeliveryTargets: []string{"final_video"},
	})
	update.MontageDefaultsSet = true
	updated, err := svc.Update(ctx, userID, created.ID, update)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got = updated.MontageDefaults.Data()
	if got.DefaultPipeline != "product-demo" || got.Preferences.AspectRatio != "16:9" || got.Preferences.DurationSeconds != 30 {
		t.Fatalf("updated MontageDefaults = %#v", got)
	}
}

func TestProjectServiceValidatesMontageDefaults(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		duration int64
		wantErr  string
	}{
		{name: "rejects defaults on article", platform: model.PlatformArticle, duration: 30, wantErr: "montage_defaults"},
		{name: "rejects negative duration", platform: model.PlatformMontage, duration: -1, wantErr: "duration_seconds"},
		{name: "accepts empty duration", platform: model.PlatformMontage, duration: 0},
		{name: "accepts maximum duration", platform: model.PlatformMontage, duration: 600},
		{name: "rejects excessive duration", platform: model.PlatformMontage, duration: 601, wantErr: "duration_seconds"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, ctx, userID := setupProjectServiceTest(t)
			project := &model.Project{Platform: tt.platform, Name: tt.name}
			project.SetMontageDefaults(model.MontageDefaults{
				Preferences: model.MontagePreferences{DurationSeconds: tt.duration},
			})
			project.MontageDefaultsSet = true

			_, err := svc.Create(ctx, userID, project)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Create error = %v, want substring %q", err, tt.wantErr)
			}
		})
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
