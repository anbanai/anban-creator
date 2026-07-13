package model

import "gorm.io/gorm"

// MigrateTaskFileExecutionSchema removes the pre-attempt uniqueness constraint.
// AutoMigrate creates idx_task_file_execution_path from TaskFile's tags first.
func MigrateTaskFileExecutionSchema(db *gorm.DB) error {
	if db.Migrator().HasIndex(&TaskFile{}, "idx_task_file_task_path") {
		return db.Migrator().DropIndex(&TaskFile{}, "idx_task_file_task_path")
	}
	return nil
}
