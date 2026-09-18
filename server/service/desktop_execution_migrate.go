package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const desktopRemovalResult = `{"success":false,"error":"Desktop execution was removed","terminal_reason":"infrastructure_cancelled","remote_artifacts":true,"cost_status":"unreconciled"}`

// MigrateDesktopExecutionRemoval converts rows that were owned by the removed
// local executor before dropping the task-only desktop columns. Pending local
// tasks remain eligible for the managed runtime; claimed local executions are
// made terminal so the normal execution finalizer can settle them safely.
func MigrateDesktopExecutionRemoval(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil || !db.Migrator().HasTable("tasks") || !db.Migrator().HasColumn("tasks", "execution_target") {
		return nil
	}
	db = db.WithContext(ctx)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable("task_executions") && tx.Migrator().HasColumn("task_executions", "target") {
			if err := tx.Exec(`
				UPDATE task_executions
				SET status = 'failed',
					terminal_reason = 'infrastructure_cancelled',
					result = ?,
					completed_at = COALESCE(completed_at, CURRENT_TIMESTAMP),
					finalization_status = '',
					cleanup_status = 'done'
				WHERE target = 'local_claimed'
				  AND status NOT IN ('succeeded', 'failed', 'cancelled', 'timed_out')
			`, desktopRemovalResult).Error; err != nil {
				return fmt.Errorf("terminalize local executions: %w", err)
			}
		}

		if err := tx.Exec(`
			UPDATE tasks
			SET execution_target = '', local_claim_deadline = NULL, executor_info = NULL
			WHERE execution_target = 'local'
		`).Error; err != nil {
			return fmt.Errorf("reset pending local tasks: %w", err)
		}

		for _, column := range []string{"execution_target", "local_claim_deadline", "executor_info"} {
			if !tx.Migrator().HasColumn("tasks", column) {
				continue
			}
			if err := tx.Exec(fmt.Sprintf("ALTER TABLE `tasks` DROP COLUMN `%s`", column)).Error; err != nil {
				return fmt.Errorf("drop tasks.%s: %w", column, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if log != nil {
		log.Info().Msg("desktop execution schema removal completed")
	}
	return nil
}
