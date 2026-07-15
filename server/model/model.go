package model

import "gorm.io/gorm"

// AutoMigrate creates or updates all database tables.
func AutoMigrate(db *gorm.DB) error {
	// Drop the legacy task/path uniqueness constraint before GORM creates the
	// execution-scoped replacement. This order is required by MySQL as well as
	// SQLite and makes retry attempts with the same path representable.
	if err := MigrateTaskFileExecutionSchema(db); err != nil {
		return err
	}
	if err := MigrateTaskArtifactCollectionSchema(db); err != nil {
		return err
	}
	if err := MigrateAgentFeedbackIdempotencySchema(db); err != nil {
		return err
	}
	err := db.AutoMigrate(
		&User{},
		&LoginSession{},
		&Project{},
		&Plan{},
		&Task{},
		&TaskExecution{},
		&TaskFile{},
		&PendingUpload{},
		&CreditTransaction{},
		&APIKey{},
		&Feedback{},
		&UserModelConfig{},
		&SeednotePostTracking{},
		&SeednoteMetricSnapshot{},
		&Template{},
		&ViralAnalysis{},
		&PosterTask{},
		&TopicPool{},
		&ImageGeneration{},
		&ImageGenerationResult{},
		&VideoGeneration{},
		&VideoGenerationSegment{},
		&AgentFeedback{},
		&IlinkBinding{},
		&IlinkNotification{},
	)
	if err != nil {
		return err
	}

	if err := MigrateDurableTaskWorkspaceSchema(db); err != nil {
		return err
	}
	return nil
}
