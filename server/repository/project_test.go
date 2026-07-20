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

func TestProjectUpdateIfReferenceImageAssetIDMatchesEmptyAndNull(t *testing.T) {
	for _, tt := range []struct {
		name    string
		id      string
		setNull bool
	}{
		{name: "empty string", id: "project-empty"},
		{name: "null", id: "project-null", setNull: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			repo := New(db)
			project := &model.Project{
				ID: tt.id, UserID: "user-1", Platform: model.PlatformArticle,
				Name: "before", Status: model.ProjectStatusActive,
			}
			if err := repo.Projects().Create(t.Context(), project); err != nil {
				t.Fatal(err)
			}
			if tt.setNull {
				if err := db.Exec("UPDATE projects SET reference_image_asset_id = NULL WHERE id = ?", project.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			loaded, err := repo.Projects().FindByID(t.Context(), project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.ReferenceImageAssetID != "" {
				t.Fatalf("loaded reference = %q, want empty", loaded.ReferenceImageAssetID)
			}
			if tt.setNull {
				wrong := *loaded
				wrong.Name = "must-not-write"
				won, err := repo.Projects().UpdateIfReferenceImageAssetID(t.Context(), &wrong, "asset-nonempty")
				if err != nil {
					t.Fatal(err)
				}
				if won {
					t.Fatal("nonempty expected reference matched null")
				}
			}
			loaded.Name = "after"
			won, err := repo.Projects().UpdateIfReferenceImageAssetID(t.Context(), loaded, "")
			if err != nil {
				t.Fatal(err)
			}
			if !won {
				t.Fatal("empty expected reference did not match")
			}
			persisted, err := repo.Projects().FindByID(t.Context(), project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Name != "after" || persisted.ReferenceImageAssetID != "" {
				t.Fatalf("persisted project name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
			}
		})
	}
}
