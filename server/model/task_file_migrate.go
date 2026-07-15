package model

import "gorm.io/gorm"

// MigrateTaskFileExecutionSchema removes the pre-attempt uniqueness constraint
// before AutoMigrate creates idx_task_file_execution_path from TaskFile's tags.
func MigrateTaskFileExecutionSchema(db *gorm.DB) error {
	if db.Migrator().HasIndex(&TaskFile{}, "idx_task_file_task_path") {
		return db.Migrator().DropIndex(&TaskFile{}, "idx_task_file_task_path")
	}
	return nil
}

// MigrateTaskArtifactCollectionSchema removes the legacy state constraints so
// AutoMigrate can recreate them with the collected terminal state.
func MigrateTaskArtifactCollectionSchema(db *gorm.DB) error {
	constraints := []struct {
		model any
		name  string
	}{
		{model: &TaskFile{}, name: "chk_task_file_state"},
		{model: &TaskExecution{}, name: "chk_task_execution_manifest_status"},
	}
	for _, constraint := range constraints {
		if db.Migrator().HasConstraint(constraint.model, constraint.name) {
			if err := db.Migrator().DropConstraint(constraint.model, constraint.name); err != nil {
				return err
			}
		}
	}
	return nil
}
