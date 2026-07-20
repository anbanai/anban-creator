package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestAssetRepositoryFindOwnedByIDIsOpaque(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()

	asset := &model.Asset{
		ID:          "asset-1",
		UserID:      "user-1",
		Purpose:     "project_reference",
		StorageKey:  "assets/user-1/asset-1/reference.png",
		FileName:    "reference.png",
		ContentType: "image/png",
		Size:        1024,
		ETag:        "etag-1",
	}
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.Assets().FindOwnedByID(ctx, asset.ID, asset.UserID)
	if err != nil {
		t.Fatalf("owned FindOwnedByID: %v", err)
	}
	if found.ID != asset.ID || found.StorageKey != asset.StorageKey {
		t.Fatalf("found asset = %+v, want ID %q and StorageKey %q", found, asset.ID, asset.StorageKey)
	}

	if _, err := repo.Assets().FindOwnedByID(ctx, asset.ID, "user-2"); !errors.Is(err, model.ErrAssetNotFound) {
		t.Fatalf("foreign FindOwnedByID error = %v, want ErrAssetNotFound", err)
	}
	if _, err := repo.Assets().FindOwnedByID(ctx, "missing", asset.UserID); !errors.Is(err, model.ErrAssetNotFound) {
		t.Fatalf("missing FindOwnedByID error = %v, want ErrAssetNotFound", err)
	}
}
