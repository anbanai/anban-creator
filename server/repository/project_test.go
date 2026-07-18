package repository

import (
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestProjectUpdateIfReferenceImageAssetID(t *testing.T) {
	repo := New(setupTestDB(t))
	project := &model.Project{
		ID: "project-cas", UserID: "user-1", Platform: model.PlatformArticle,
		Name: "before", ReferenceImageAssetID: "asset-a", Status: model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(t.Context(), project); err != nil {
		t.Fatal(err)
	}

	matched := *project
	matched.Name = "matched"
	won, err := repo.Projects().UpdateIfReferenceImageAssetID(t.Context(), &matched, "asset-a")
	if err != nil {
		t.Fatal(err)
	}
	if !won {
		t.Fatal("matching reference CAS did not update")
	}
	got, err := repo.Projects().FindByID(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "matched" || got.ReferenceImageAssetID != "asset-a" {
		t.Fatalf("matched project = name %q reference %q", got.Name, got.ReferenceImageAssetID)
	}

	mismatch := *got
	mismatch.Name = "must-not-write"
	mismatch.ReferenceImageAssetID = "asset-stale"
	won, err = repo.Projects().UpdateIfReferenceImageAssetID(t.Context(), &mismatch, "asset-b")
	if err != nil {
		t.Fatal(err)
	}
	if won {
		t.Fatal("mismatching reference CAS updated")
	}
	got, err = repo.Projects().FindByID(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "matched" || got.ReferenceImageAssetID != "asset-a" {
		t.Fatalf("mismatch modified project = name %q reference %q", got.Name, got.ReferenceImageAssetID)
	}
}
