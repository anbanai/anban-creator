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
