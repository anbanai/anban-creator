package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// createBackfillProject inserts a project with exact field values, bypassing
// service defaults so the backfill logic is tested in isolation.
func createBackfillProject(t *testing.T, db *gorm.DB, platform, style, writingStyle string) *model.Project {
	t.Helper()
	ch := &model.Project{
		ID:          uuid.NewString(),
		UserID:      "user-backfill",
		Platform:    platform,
		Name:        "Backfill Test " + platform,
		VisualStyle: style,
		Writer:      writingStyle,
		Status:      model.ProjectStatusActive,
	}
	if err := db.Create(ch).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	return ch
}

func reloadProject(t *testing.T, db *gorm.DB, id string) *model.Project {
	t.Helper()
	var ch model.Project
	if err := db.First(&ch, "id = ?", id).Error; err != nil {
		t.Fatalf("reload project %s: %v", id, err)
	}
	return &ch
}

// TestMigrateArticleStyleOverload_MovesWriter — the core root-cause fix:
// an article project whose overloaded Style held a writer key ("dan-koe") must
// move it to Writer and clear VisualStyle, so the writer key no longer leaks
// into image generation as a visual-style anchor.
func TestMigrateArticleStyleOverload_MovesWriter(t *testing.T) {
	db := setupTestDB(t)
	log := zerolog.Nop()
	ch := createBackfillProject(t, db, model.PlatformArticle, "dan-koe", "")

	if err := MigrateArticleStyleOverload(context.Background(), db, &log); err != nil {
		t.Fatalf("MigrateArticleStyleOverload: %v", err)
	}

	got := reloadProject(t, db, ch.ID)
	if got.VisualStyle != "" {
		t.Errorf("VisualStyle = %q, want empty — writer key must not remain in visual style", got.VisualStyle)
	}
	if got.Writer != "dan-koe" {
		t.Errorf("Writer = %q, want %q", got.Writer, "dan-koe")
	}
}

// TestMigrateArticleStyleOverload_PreservesRealVisualStyle — a Style that is
// genuinely a visual description (not a writer key) must be left in place.
func TestMigrateArticleStyleOverload_PreservesRealVisualStyle(t *testing.T) {
	db := setupTestDB(t)
	log := zerolog.Nop()
	ch := createBackfillProject(t, db, model.PlatformArticle, "温暖自然的生活摄影，柔光大地色系", "")

	if err := MigrateArticleStyleOverload(context.Background(), db, &log); err != nil {
		t.Fatalf("MigrateArticleStyleOverload: %v", err)
	}

	got := reloadProject(t, db, ch.ID)
	if got.VisualStyle != "温暖自然的生活摄影，柔光大地色系" {
		t.Errorf("VisualStyle = %q, want the original visual description (a non-writer Style must be preserved)", got.VisualStyle)
	}
	if got.Writer != "" {
		t.Errorf("Writer = %q, want empty (no writer key to move)", got.Writer)
	}
}

// TestMigrateArticleStyleOverload_LeavesSeednoteUntouched — seednote Style is a
// real visual style; even if it happened to equal a writer key it would still
// be a visual value for seednote. The backfill only touches article projects.
func TestMigrateArticleStyleOverload_LeavesSeednoteUntouched(t *testing.T) {
	db := setupTestDB(t)
	log := zerolog.Nop()
	ch := createBackfillProject(t, db, model.PlatformSeednote, "手绘水彩插画风格", "")

	if err := MigrateArticleStyleOverload(context.Background(), db, &log); err != nil {
		t.Fatalf("MigrateArticleStyleOverload: %v", err)
	}

	got := reloadProject(t, db, ch.ID)
	if got.VisualStyle != "手绘水彩插画风格" {
		t.Errorf("seednote VisualStyle = %q, want unchanged", got.VisualStyle)
	}
}

// TestMigrateArticleStyleOverload_DoesNotClobberExistingWritingStyle — if a
// project already has a Writer set, the writer key in VisualStyle is moved away
// (VisualStyle cleared) but the existing Writer is preserved.
func TestMigrateArticleStyleOverload_DoesNotClobberExistingWritingStyle(t *testing.T) {
	db := setupTestDB(t)
	log := zerolog.Nop()
	ch := createBackfillProject(t, db, model.PlatformArticle, "casual-science", "cultural-depth")

	if err := MigrateArticleStyleOverload(context.Background(), db, &log); err != nil {
		t.Fatalf("MigrateArticleStyleOverload: %v", err)
	}

	got := reloadProject(t, db, ch.ID)
	if got.VisualStyle != "" {
		t.Errorf("VisualStyle = %q, want empty (stale writer key cleared)", got.VisualStyle)
	}
	if got.Writer != "cultural-depth" {
		t.Errorf("Writer = %q, want %q (must not be clobbered)", got.Writer, "cultural-depth")
	}
}

// TestMigrateArticleStyleOverload_Idempotent — running twice is a no-op: after
// the first pass the row no longer matches the query.
func TestMigrateArticleStyleOverload_Idempotent(t *testing.T) {
	db := setupTestDB(t)
	log := zerolog.Nop()
	ch := createBackfillProject(t, db, model.PlatformArticle, "dan-koe", "")

	for i := 0; i < 2; i++ {
		if err := MigrateArticleStyleOverload(context.Background(), db, &log); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}

	got := reloadProject(t, db, ch.ID)
	if got.VisualStyle != "" || got.Writer != "dan-koe" {
		t.Errorf("after 2 passes: VisualStyle=%q Writer=%q, want VisualStyle empty + Writer=dan-koe", got.VisualStyle, got.Writer)
	}
}
