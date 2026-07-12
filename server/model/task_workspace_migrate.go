package model

import (
	"fmt"

	"gorm.io/gorm"
)

// MigrateDurableTaskWorkspaceSchema removes the API-server cleanup marker now
// that Kubernetes task workspaces live durably on NAS.
func MigrateDurableTaskWorkspaceSchema(db *gorm.DB) error {
	m := db.Migrator()
	if !m.HasColumn("tasks", "cleaned_up_at") {
		return nil
	}
	if m.HasIndex("tasks", "idx_tasks_cleaned_up_at") {
		if err := m.DropIndex("tasks", "idx_tasks_cleaned_up_at"); err != nil {
			return fmt.Errorf("drop tasks.cleaned_up_at index: %w", err)
		}
	}
	if err := db.Exec("ALTER TABLE `tasks` DROP COLUMN `cleaned_up_at`").Error; err != nil {
		return fmt.Errorf("drop tasks.cleaned_up_at column: %w", err)
	}
	return nil
}
