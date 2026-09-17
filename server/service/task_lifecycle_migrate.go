package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// MigrateTaskLifecycle contracts the legacy percentage schema after the
// revisioned lifecycle column has been created by AutoMigrate.
func MigrateTaskLifecycle(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil {
		return nil
	}
	db = db.WithContext(ctx)
	columns := map[string][]string{
		"tasks":           {"progress", "progress_sequence", "latest_progress"},
		"task_executions": {"agent_pack_progress_contract"},
	}
	for table, names := range columns {
		if !db.Migrator().HasTable(table) {
			continue
		}
		for _, column := range names {
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
		log.Info().Msg("task lifecycle schema migration completed")
	}
	return nil
}
