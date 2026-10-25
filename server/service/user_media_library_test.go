package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func newUserMediaLibraryTestService(t *testing.T) (repository.Repository, *UserMediaLibraryService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Asset{}, &model.Project{}, &model.UserMediaAsset{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	return repo, NewUserMediaLibraryService(repo, &imageAnalysisStorageFake{data: map[string][]byte{}}, nil)
}

func seedUserMediaLibraryAsset(t *testing.T, repo repository.Repository, userID, id, projectID, status string) *model.UserMediaAsset {
	t.Helper()
	assetID := uuid.NewString()
	if err := repo.Assets().Create(context.Background(), &model.Asset{ID: assetID, UserID: userID, Purpose: DirectUploadPurposeMediaLibrary, StorageKey: id + ".png", FileName: id + ".png", ContentType: "image/png", Size: 12, ETag: id}); err != nil {
		t.Fatal(err)
	}
	row := &model.UserMediaAsset{ID: id, UserID: userID, AssetID: assetID, ProjectID: projectID, MediaType: "image", Status: status, Description: id + " 描述", UsageNotes: id + " 说明"}
	if err := repo.UserMediaAssets().Create(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestUserMediaLibrarySearchCandidatesIsolatesUsersAndReturnsReadyProjectMatches(t *testing.T) {
	repo, svc := newUserMediaLibraryTestService(t)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformWechat, Name: "我的项目", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	seedUserMediaLibraryAsset(t, repo, "user-1", "same-project", "project-1", model.UserMediaAssetStatusAvailable)
	seedUserMediaLibraryAsset(t, repo, "user-1", "other-project", "project-2", model.UserMediaAssetStatusAvailable)
	seedUserMediaLibraryAsset(t, repo, "user-1", "queued", "project-1", model.UserMediaAssetStatusQueued)
	seedUserMediaLibraryAsset(t, repo, "user-2", "foreign", "project-1", model.UserMediaAssetStatusAvailable)

	items, err := svc.SearchCandidates(ctx, "user-1", "project-1", "描述", "image", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "same-project" || items[1].ID != "other-project" {
		t.Fatalf("candidate ids = %#v", []string{items[0].ID, items[1].ID})
	}
	for _, item := range items {
		if item.Status != model.UserMediaAssetStatusAvailable || item.PreviewURL == "" || item.OptionLabel != "media_asset:"+item.ID {
			t.Fatalf("candidate view = %#v", item)
		}
	}
}

func TestUserMediaLibraryRemoveStopsCandidatesButKeepsHistoricalAsset(t *testing.T) {
	repo, svc := newUserMediaLibraryTestService(t)
	ctx := context.Background()
	row := seedUserMediaLibraryAsset(t, repo, "user-1", "media-1", "", model.UserMediaAssetStatusAvailable)
	if err := svc.Remove(ctx, "user-1", row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, "user-1", row.ID); !errors.Is(err, ErrUserMediaNotFound) {
		t.Fatalf("Get removed = %v", err)
	}
	if items, err := svc.SearchCandidates(ctx, "user-1", "", "", "", 10); err != nil || len(items) != 0 {
		t.Fatalf("removed candidates = %#v, %v", items, err)
	}
	asset, err := repo.Assets().FindByID(ctx, row.AssetID)
	if err != nil || asset == nil {
		t.Fatalf("historical asset = %#v, %v", asset, err)
	}
}

func TestUserMediaLibraryUpdateKeepsUserNotesAndRejectsForeignProject(t *testing.T) {
	repo, svc := newUserMediaLibraryTestService(t)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "owned-project", UserID: "user-1", Platform: model.PlatformWechat, Name: "owned", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: "foreign-project", UserID: "user-2", Platform: model.PlatformWechat, Name: "foreign", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	row := seedUserMediaLibraryAsset(t, repo, "user-1", "media-1", "", model.UserMediaAssetStatusAvailable)
	notes := "仅用于春季新品"
	view, err := svc.Update(ctx, UserMediaUpdateRequest{UserID: "user-1", ProjectID: userMediaStringPtr("owned-project"), UsageNotes: &notes}, row.ID)
	if err != nil || view.ProjectID != "owned-project" || view.UsageNotes != notes {
		t.Fatalf("updated view = %#v, %v", view, err)
	}
	if _, err := svc.Update(ctx, UserMediaUpdateRequest{UserID: "user-1", ProjectID: userMediaStringPtr("foreign-project")}, row.ID); !errors.Is(err, ErrUserMediaProjectDenied) {
		t.Fatalf("foreign project update = %v", err)
	}
	if view.UpdatedAt.Before(view.CreatedAt.Add(-time.Second)) {
		t.Fatalf("invalid timestamps: %#v", view)
	}
	cleared := ""
	view, err = svc.Update(ctx, UserMediaUpdateRequest{UserID: "user-1", ProjectID: &cleared}, row.ID)
	if err != nil || view.ProjectID != "" || view.UsageNotes != notes {
		t.Fatalf("cleared project tag = %#v, %v", view, err)
	}
}

func TestValidateUserMediaAssetForCreationRequiresReadyActiveRow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Asset{}, &model.UserMediaAsset{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	for _, tc := range []struct {
		name   string
		status string
		remove bool
	}{
		{name: "queued", status: model.UserMediaAssetStatusQueued},
		{name: "failed", status: model.UserMediaAssetStatusFailed},
		{name: "removed", status: model.UserMediaAssetStatusAvailable, remove: true},
	} {
		assetID := "asset-" + tc.name
		if err := repo.Assets().Create(t.Context(), &model.Asset{ID: assetID, UserID: "user-1", Purpose: DirectUploadPurposeMediaLibrary, StorageKey: assetID + ".png", FileName: "source.png", ContentType: "image/png", Size: 1, ETag: assetID}); err != nil {
			t.Fatal(err)
		}
		row := &model.UserMediaAsset{ID: "media-" + tc.name, UserID: "user-1", AssetID: assetID, MediaType: "image", Status: tc.status}
		if tc.remove {
			removedAt := time.Now().UTC()
			row.RemovedAt = &removedAt
		}
		if err := repo.UserMediaAssets().Create(t.Context(), row); err != nil {
			t.Fatal(err)
		}
	}
	ready := &model.Asset{ID: "asset-ready", UserID: "user-1", Purpose: DirectUploadPurposeMediaLibrary, StorageKey: "asset-ready.png", FileName: "source.png", ContentType: "image/png", Size: 1, ETag: "asset-ready"}
	if err := repo.Assets().Create(t.Context(), ready); err != nil {
		t.Fatal(err)
	}
	if err := repo.UserMediaAssets().Create(t.Context(), &model.UserMediaAsset{ID: "media-ready", UserID: "user-1", AssetID: ready.ID, MediaType: "image", Status: model.UserMediaAssetStatusAvailable}); err != nil {
		t.Fatal(err)
	}

	if err := ValidateUserMediaAssetForCreation(t.Context(), repo, "user-1", ready); err != nil {
		t.Fatalf("ready media asset rejected: %v", err)
	}
	for _, tc := range []string{"queued", "failed", "removed"} {
		t.Run(tc, func(t *testing.T) {
			asset, err := repo.Assets().FindOwnedByID(t.Context(), "asset-"+tc, "user-1")
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateUserMediaAssetForCreation(t.Context(), repo, "user-1", asset); !errors.Is(err, ErrUserMediaUnavailable) {
				t.Fatalf("validation error = %v, want ErrUserMediaUnavailable", err)
			}
		})
	}
}

func userMediaStringPtr(value string) *string { return &value }
