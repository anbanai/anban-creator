package service

import (
	"context"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// MigrateProjectPositioningToInstructions backfills the canonical instructions
// column from the legacy positioning column. Existing instructions always win.
func MigrateProjectPositioningToInstructions(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil {
		return nil
	}
	if !db.Migrator().HasColumn("projects", "positioning") || !db.Migrator().HasColumn("projects", "instructions") {
		return nil
	}
	res := db.WithContext(ctx).
		Table("projects").
		Where("(instructions IS NULL OR instructions = '') AND positioning <> ''").
		Update("instructions", gorm.Expr("positioning"))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		log.Info().Int64("migrated", res.RowsAffected).
			Msg("project positioning migration: copied legacy positioning to instructions")
	}
	return nil
}
