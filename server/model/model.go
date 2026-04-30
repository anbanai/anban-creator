package model

import "gorm.io/gorm"

// AutoMigrate creates or updates all database tables.
func AutoMigrate(db *gorm.DB) error {
	err := db.AutoMigrate(
		&User{},
		&LoginSession{},
		&Channel{},
		&Plan{},
		&Task{},
		&TaskFile{},
		&CreditTransaction{},
		&APIKey{},
		&Feedback{},
		&UserModelConfig{},
		&RednotePostTracking{},
		&RednoteMetricSnapshot{},
	)
	if err != nil {
		return err
	}

	return nil
}
