package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// createBackfillProject inserts a project with exact field values, bypassing
// service defaults so the backfill logic is tested in isolation.
func createBackfillProject(t *testing.T, db *gorm.DB, platform, style, writingStyle string) *model.Project {
	t.Helper()
	ch := &model.Project{
		ID:           uuid.NewString(),
		UserID:       "user-backfill",
		Platform:     platform,
		Name:         "Backfill Test " + platform,
		Style:        style,
		WritingStyle: writingStyle,
		Status:       model.ProjectStatusActive,
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

// TestMigrateArticleStyleOverload_MovesWriterKey — the core root-cause fix:
// an article project whose overloaded Style held a writer key ("dan-koe") must
// move it to WritingStyle and clear Style (visual), so the writer key no longer
// leaks into image generation as a visual-style anchor.
func TestMigrateArticleStyleOverload_MovesWriterKey(t *testing.T) {
	db := setupTestDB(t)
	log := zerolog.Nop()
	ch := createBackfillProject(t, db, model.PlatformArticle, "dan-koe", "")

	if err := MigrateArticleStyleOverload(context.Background(), db, &log); err != nil {
		t.Fatalf("MigrateArticleStyleOverload: %v", err)
	}

	got := reloadProject(t, db, ch.ID)
	if got.Style != "" {
		t.Errorf("Style (visual) = %q, want empty — writer key must not remain in visual style", got.Style)
	}
	if got.WritingStyle != "dan-koe" {
		t.Errorf("WritingStyle = %q, want %q", got.WritingStyle, "dan-koe")
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
	if got.Style != "温暖自然的生活摄影，柔光大地色系" {
		t.Errorf("Style = %q, want the original visual description (a non-writer Style must be preserved)", got.Style)
	}
	if got.WritingStyle != "" {
		t.Errorf("WritingStyle = %q, want empty (no writer key to move)", got.WritingStyle)
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
	if got.Style != "手绘水彩插画风格" {
		t.Errorf("seednote Style = %q, want unchanged", got.Style)
	}
}

// TestMigrateArticleStyleOverload_DoesNotClobberExistingWritingStyle — if a
// project already has a WritingStyle set, the writer key in Style is moved away
// (Style cleared) but the existing WritingStyle is preserved.
func TestMigrateArticleStyleOverload_DoesNotClobberExistingWritingStyle(t *testing.T) {
	db := setupTestDB(t)
	log := zerolog.Nop()
	ch := createBackfillProject(t, db, model.PlatformArticle, "casual-science", "cultural-depth")

	if err := MigrateArticleStyleOverload(context.Background(), db, &log); err != nil {
		t.Fatalf("MigrateArticleStyleOverload: %v", err)
	}

	got := reloadProject(t, db, ch.ID)
	if got.Style != "" {
		t.Errorf("Style = %q, want empty (stale writer key cleared)", got.Style)
	}
	if got.WritingStyle != "cultural-depth" {
		t.Errorf("WritingStyle = %q, want %q (must not be clobbered)", got.WritingStyle, "cultural-depth")
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
	if got.Style != "" || got.WritingStyle != "dan-koe" {
		t.Errorf("after 2 passes: Style=%q WritingStyle=%q, want Style empty + WritingStyle=dan-koe", got.Style, got.WritingStyle)
	}
}
