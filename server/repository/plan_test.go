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
	won, err := repo.Plans().UpdateEditableIfReferenceImageAssetID(t.Context(), &matched, "asset-a", false)
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
	won, err = repo.Plans().UpdateEditableIfReferenceImageAssetID(t.Context(), &mismatch, "asset-b", false)
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
			won, err := repo.Plans().UpdateEditableIfReferenceImageAssetID(t.Context(), loaded, "", false)
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

func TestPlanUpdateEditableDoesNotOverwriteSchedulerOrProtectedFields(t *testing.T) {
	repo := New(setupTestDB(t))
	createdAt := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	oldNext := time.Now().Add(-time.Hour).Truncate(time.Second)
	schedulerNext := oldNext.Add(time.Hour)
	plan := &model.Plan{
		ID: "plan-editable", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformArticle,
		ExecutionProfile: "effective", Prompt: "before", ReferenceImageAssetID: "asset-a", CronExpr: "0 * * * *", Status: model.PlanStatusActive,
		NextRunAt: &oldNext, CreatedAt: createdAt,
	}
	if err := repo.Plans().Create(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	won, err := repo.Plans().UpdateNextRunAtIf(t.Context(), plan.ID, &schedulerNext, &oldNext)
	if err != nil || !won {
		t.Fatalf("scheduler update = %v, %v", won, err)
	}

	stale.UserID = "must-not-write-user"
	stale.ProjectID = "must-not-write-project"
	stale.Type = model.PlatformSeednote
	stale.Status = model.PlanStatusPaused
	stale.CreatedAt = createdAt.Add(time.Hour)
	stale.ExecutionProfile = "balanced"
	stale.Prompt = "after"
	stale.ArticleCoverUsePortrait = true
	if err := repo.Plans().UpdateEditable(t.Context(), stale, false); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExecutionProfile != "balanced" || got.Prompt != "after" || !got.ArticleCoverUsePortrait || got.UserID != "user-1" || got.ProjectID != "project-1" || got.Type != model.PlatformArticle || got.Status != model.PlanStatusActive || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("editable update changed protected fields: %#v", got)
	}
	if got.NextRunAt == nil || !got.NextRunAt.Equal(schedulerNext) || got.CronExpr != "0 * * * *" {
		t.Fatalf("omitted schedule = cron %q next %v, want scheduler next %v", got.CronExpr, got.NextRunAt, schedulerNext)
	}

	cronNext := schedulerNext.Add(2 * time.Hour)
	got.CronExpr = "0 */2 * * *"
	got.NextRunAt = &cronNext
	if err := repo.Plans().UpdateEditable(t.Context(), got, true); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CronExpr != "0 */2 * * *" || got.NextRunAt == nil || !got.NextRunAt.Equal(cronNext) {
		t.Fatalf("explicit schedule = cron %q next %v", got.CronExpr, got.NextRunAt)
	}
}

func TestPlanUpdateStatusAndNextRunAtDoesNotOverwriteEditableFields(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	next := time.Now().Add(time.Hour).Truncate(time.Second)
	plan := &model.Plan{
		ID: "plan-status", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformArticle,
		Prompt: "before", ReferenceImageAssetID: "asset-a", CronExpr: "0 * * * *", Status: model.PlanStatusActive,
		NextRunAt: &next,
	}
	if err := repo.Plans().Create(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Plan{}).Where("id = ?", plan.ID).Updates(map[string]interface{}{
		"topic_hint":               "concurrent prompt",
		"reference_image_asset_id": "asset-b",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Plans().UpdateStatusAndNextRunAt(t.Context(), plan.ID, model.PlanStatusPaused, nil); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.PlanStatusPaused || got.NextRunAt != nil || got.Prompt != "concurrent prompt" || got.ReferenceImageAssetID != "asset-b" {
		t.Fatalf("status update changed editable fields: %#v", got)
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

func TestPlanListActiveNextRunAtProjectsOnlyScheduledActivePlans(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	next := time.Now().Add(time.Hour).Truncate(time.Second)
	for _, plan := range []*model.Plan{
		{ID: "active-next", UserID: "user-1", Type: model.PlatformArticle, ExecutionProfile: "balanced", Status: model.PlanStatusActive, NextRunAt: &next},
		{ID: "active-nil", UserID: "user-1", Type: model.PlatformArticle, ExecutionProfile: "balanced", Status: model.PlanStatusActive},
		{ID: "paused-next", UserID: "user-1", Type: model.PlatformArticle, ExecutionProfile: "balanced", Status: model.PlanStatusPaused, NextRunAt: &next},
	} {
		if err := repo.Plans().Create(t.Context(), plan); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.Plans().ListActiveNextRunAt(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Equal(next) {
		t.Fatalf("next runs = %#v", got)
	}
}
