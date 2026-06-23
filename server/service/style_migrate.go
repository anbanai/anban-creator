package service

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/resources"
	"gorm.io/gorm"
)

// MigrateArticleStyleOverload is a one-time, idempotent data backfill.
//
// Historically Channel.Style was overloaded: article channels stored the
// WRITER key (e.g. "dan-koe") there, and the server injected it verbatim as a
// "视觉风格要求", which made image generation read the writer's cover style
// (dan-koe → 维多利亚木刻). After splitting the field into three orthogonal
// dimensions — Style (图片视觉) / WritingStyle (写作风格) / Theme (排版样式) —
// any article channel whose Style still holds a known writer key must be
// moved to WritingStyle and Style cleared. Otherwise the writer key would be
// read as a visual-style anchor and re-trigger the bug on existing channels.
//
// Seednote channels are untouched: their Style is a genuine visual description.
// Channels whose Style does NOT match a writer key are also untouched (it is a
// real visual style). Safe to run on every startup — a migrated row no longer
// matches the query, so it converges immediately.
func MigrateArticleStyleOverload(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	mgr := resources.Manager()
	if mgr == nil || db == nil {
		return nil // resources unavailable (embedded load failed) or no DB.
	}

	var channels []*model.Channel
	// Only article channels with a non-empty Style can hold a stale writer key.
	if err := db.WithContext(ctx).
		Where("platform = ? AND style <> ?", model.PlatformArticle, "").
		Find(&channels).Error; err != nil {
		return err
	}

	migrated := 0
	for _, ch := range channels {
		if mgr.Get(resources.CategoryWriter, ch.Style) == nil {
			continue // Style is not a writer key — it is a real visual style; keep it.
		}
		// Move the writer key into WritingStyle (only when empty, to avoid
		// clobbering an explicitly configured writer) and clear Style. Use a
		// map so the empty-string Style is actually written (gorm skips zero
		// values in struct Updates but writes them in map Updates).
		updates := map[string]any{"style": ""}
		if ch.WritingStyle == "" {
			updates["writing_style"] = ch.Style
		}
		if err := db.Model(&model.Channel{}).Where("id = ?", ch.ID).Updates(updates).Error; err != nil {
			log.Error().Err(err).Str("channel_id", ch.ID).Str("style", ch.Style).
				Msg("style backfill: failed to update channel")
			continue
		}
		migrated++
	}

	if migrated > 0 {
		log.Info().Int("migrated", migrated).
			Msg("style backfill: moved stale writer keys from style to writing_style (visual dimension cleared)")
	}
	return nil
}
