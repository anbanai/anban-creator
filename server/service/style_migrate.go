package service

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/resources"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// MigrateArticleStyleOverload is a one-time, idempotent data backfill.
//
// Historically Project.Style was overloaded: article projects stored the
// WRITER key (e.g. "dan-koe") there, and the server injected it verbatim as a
// "视觉风格要求", which made image generation read the writer's cover style
// (dan-koe → 维多利亚木刻). After splitting the field into three orthogonal
// dimensions — Style (图片视觉) / Writer (写作风格) / Theme (排版样式) —
// any article project whose Style still holds a known writer key must be
// moved to Writer and Style cleared. Otherwise the writer key would be
// read as a visual-style anchor and re-trigger the bug on existing projects.
//
// Seednote projects are untouched: their Style is a genuine visual description.
// Projects whose Style does NOT match a writer key are also untouched (it is a
// real visual style). Safe to run on every startup — a migrated row no longer
// matches the query, so it converges immediately.
func MigrateArticleStyleOverload(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	mgr := resources.Manager()
	if mgr == nil || db == nil {
		return nil // resources unavailable (embedded load failed) or no DB.
	}

	var projects []*model.Project
	// Only article projects with a non-empty Style can hold a stale writer key.
	if err := db.WithContext(ctx).
		Where("platform = ? AND style <> ?", model.PlatformArticle, "").
		Find(&projects).Error; err != nil {
		return err
	}

	migrated := 0
	for _, ch := range projects {
		if mgr.Get(resources.CategoryWriter, ch.VisualStyle) == nil {
			continue // VisualStyle is not a writer key — it is a real visual style; keep it.
		}
		// Move the writer key into Writer (only when empty, to avoid
		// clobbering an explicitly configured writer) and clear VisualStyle. Use a
		// map so the empty-string style is actually written (gorm skips zero
		// values in struct Updates but writes them in map Updates).
		updates := map[string]any{"style": ""}
		if ch.Writer == "" {
			updates["writer"] = ch.VisualStyle
		}
		if err := db.Model(&model.Project{}).Where("id = ?", ch.ID).Updates(updates).Error; err != nil {
			log.Error().Err(err).Str("project_id", ch.ID).Str("style", ch.VisualStyle).
				Msg("style backfill: failed to update project")
			continue
		}
		migrated++
	}

	if migrated > 0 {
		log.Info().Int("migrated", migrated).
			Msg("style backfill: moved stale writer keys from style to writer (visual dimension cleared)")
	}
	return nil
}
