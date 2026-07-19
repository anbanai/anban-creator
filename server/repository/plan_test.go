package repository

import (
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestPlanUpdateIfReferenceImageAssetID(t *testing.T) {
	repo := New(setupTestDB(t))
	plan := &model.Plan{
		ID: "plan-cas", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformArticle,
		Prompt: "before", ReferenceImageAssetID: "asset-a", CronExpr: "0 9 * * *", Status: model.PlanStatusActive,
	}
	if err := repo.Plans().Create(t.Context(), plan); err != nil {
		t.Fatal(err)
	}

	matched := *plan
	matched.Prompt = "matched"
	won, err := repo.Plans().UpdateIfReferenceImageAssetID(t.Context(), &matched, "asset-a")
	if err != nil {
		t.Fatal(err)
	}
	if !won {
		t.Fatal("matching reference CAS did not update")
	}
	got, err := repo.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "matched" || got.ReferenceImageAssetID != "asset-a" {
		t.Fatalf("matched plan = prompt %q reference %q", got.Prompt, got.ReferenceImageAssetID)
	}

	mismatch := *got
	mismatch.Prompt = "must-not-write"
	mismatch.ReferenceImageAssetID = "asset-stale"
	won, err = repo.Plans().UpdateIfReferenceImageAssetID(t.Context(), &mismatch, "asset-b")
	if err != nil {
		t.Fatal(err)
	}
	if won {
		t.Fatal("mismatching reference CAS updated")
	}
	got, err = repo.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "matched" || got.ReferenceImageAssetID != "asset-a" {
		t.Fatalf("mismatch modified plan = prompt %q reference %q", got.Prompt, got.ReferenceImageAssetID)
	}
}

func TestPlanUpdateIfReferenceImageAssetIDMatchesEmptyAndNull(t *testing.T) {
	for _, tt := range []struct {
		name    string
		id      string
		setNull bool
	}{
		{name: "empty string", id: "plan-empty"},
		{name: "null", id: "plan-null", setNull: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestDB(t)
			repo := New(db)
			plan := &model.Plan{
				ID: tt.id, UserID: "user-1", ProjectID: "project-1", Type: model.PlatformSeednote,
				Prompt: "before", CronExpr: "0 9 * * *", Status: model.PlanStatusActive,
			}
			if err := repo.Plans().Create(t.Context(), plan); err != nil {
				t.Fatal(err)
			}
			if tt.setNull {
				if err := db.Exec("UPDATE plans SET reference_image_asset_id = NULL WHERE id = ?", plan.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			loaded, err := repo.Plans().FindByID(t.Context(), plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			loaded.Prompt = "after"
			won, err := repo.Plans().UpdateIfReferenceImageAssetID(t.Context(), loaded, "")
			if err != nil {
				t.Fatal(err)
			}
			if !won {
				t.Fatal("empty expected reference did not match")
			}
			persisted, err := repo.Plans().FindByID(t.Context(), plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Prompt != "after" || persisted.ReferenceImageAssetID != "" {
				t.Fatalf("persisted plan prompt=%q reference=%q", persisted.Prompt, persisted.ReferenceImageAssetID)
			}
		})
	}
}

func TestPlanUpdateNextRunAtIfDoesNotOverwriteConcurrentFields(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	oldNext := time.Now().Add(-time.Hour).Truncate(time.Second)
	newNext := oldNext.Add(time.Hour)
	plan := &model.Plan{
		ID: "plan-next-run", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformArticle,
		Prompt: "before", ReferenceImageAssetID: "asset-a", CronExpr: "0 * * * *", Status: model.PlanStatusActive,
		NextRunAt: &oldNext,
	}
	if err := repo.Plans().Create(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Plan{}).Where("id = ?", plan.ID).Update("reference_image_asset_id", "asset-b").Error; err != nil {
		t.Fatal(err)
	}

	won, err := repo.Plans().UpdateNextRunAtIf(t.Context(), plan.ID, &newNext, &oldNext)
	if err != nil {
		t.Fatal(err)
	}
	if !won {
		t.Fatal("matching next_run_at CAS did not update")
	}
	got, err := repo.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReferenceImageAssetID != "asset-b" || got.Prompt != "before" || got.NextRunAt == nil || !got.NextRunAt.Equal(newNext) {
		t.Fatalf("updated plan = prompt %q reference %q next %v", got.Prompt, got.ReferenceImageAssetID, got.NextRunAt)
	}

	staleNext := newNext.Add(time.Hour)
	won, err = repo.Plans().UpdateNextRunAtIf(t.Context(), plan.ID, &staleNext, &oldNext)
	if err != nil {
		t.Fatal(err)
	}
	if won {
		t.Fatal("stale next_run_at CAS updated")
	}
}
