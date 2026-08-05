package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// MigrateGoalModeRemoval contracts the schema after the strong-goal execution
// mode was removed from tasks, plans, APIs, and agent runtimes.
func MigrateGoalModeRemoval(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil {
		return nil
	}
	db = db.WithContext(ctx)
	for _, table := range []string{"tasks", "plans"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		for _, column := range []string{"goal", "goal_mode"} {
			if !db.Migrator().HasColumn(table, column) {
				continue
			}
			statement := fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`", table, column)
			if err := db.Exec(statement).Error; err != nil {
				return fmt.Errorf("drop %s.%s: %w", table, column, err)
			}
		}
	}
	if log != nil {
		log.Info().Msg("goal mode schema removal completed")
	}
	return nil
}
