package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// MigrateDesignerRemoval drops the standalone Designer persistence model after
// its API, Studio, Miniapp, and plugin workflows have been removed.
func MigrateDesignerRemoval(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil {
		return nil
	}
	db = db.WithContext(ctx)
	for _, table := range []string{"designer_references", "image_generation_results", "image_generations"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		if err := db.Migrator().DropTable(table); err != nil {
			return fmt.Errorf("drop %s: %w", table, err)
		}
	}
	if log != nil {
		log.Info().Msg("Designer schema removal completed")
	}
	return nil
}
