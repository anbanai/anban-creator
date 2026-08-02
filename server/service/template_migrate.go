package service

import (
	"context"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// MigrateTemplatePrompt copies legacy template prompts into the canonical
// column and imports them into projects that have no visual style yet. It is
// deliberately expand/backfill-only so old and new pods can overlap safely.
// The guarded contract SQL removes legacy columns after old pods are retired.
func MigrateTemplatePrompt(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil {
		return nil
	}
	migrator := db.Migrator()
	if !migrator.HasTable("templates") || !migrator.HasColumn("templates", "prompt") {
		return nil
	}

	hasLegacyTemplatePrompt := migrator.HasColumn("templates", "style_prompt")
	if hasLegacyTemplatePrompt {
		result := db.WithContext(ctx).Exec(`
			UPDATE templates
			SET prompt = style_prompt
			WHERE TRIM(COALESCE(prompt, '')) = ''
			  AND TRIM(COALESCE(style_prompt, '')) <> ''`)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 && log != nil {
			log.Info().Int64("migrated", result.RowsAffected).Msg("template prompt migration: copied style_prompt to prompt")
		}
	}

	hasLegacyProjectTemplate := migrator.HasTable("projects") &&
		migrator.HasColumn("projects", "template_id") &&
		migrator.HasColumn("projects", "style")
	if hasLegacyProjectTemplate {
		result := db.WithContext(ctx).Exec(`
			UPDATE projects
			SET style = (
				SELECT templates.prompt
				FROM templates
				WHERE templates.id = projects.template_id
				LIMIT 1
			)
			WHERE TRIM(COALESCE(projects.style, '')) = ''
			  AND TRIM(COALESCE(projects.template_id, '')) <> ''
			  AND EXISTS (
				SELECT 1
				FROM templates
				WHERE templates.id = projects.template_id
				  AND TRIM(COALESCE(templates.prompt, '')) <> ''
			  )`)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 && log != nil {
			log.Info().Int64("migrated", result.RowsAffected).Msg("template prompt migration: imported prompt into blank project visual styles")
		}
	}

	return nil
}
