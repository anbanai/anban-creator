package model

import "gorm.io/gorm"

// AutoMigrate creates or updates all database tables.
func AutoMigrate(db *gorm.DB) error {
	err := db.AutoMigrate(
		&User{},
		&LoginSession{},
		&Project{},
		&Plan{},
		&Task{},
		&TaskFile{},
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
		&AgentFeedback{},
		&IlinkBinding{},
		&IlinkNotification{},
	)
	if err != nil {
		return err
	}

	return nil
}
