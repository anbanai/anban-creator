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
	)
	if err != nil {
		return err
	}

	// Drop legacy unique index on (user_id, name) for Channel — names should not be unique.
	_ = db.Migrator().DropIndex(&Channel{}, "idx_channels_user_id_name")

	return nil
}
