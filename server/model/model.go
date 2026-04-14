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

	// Add unique index on (user_id, name) for Channel.
	if err := ensureChannelUniqueIndex(db); err != nil {
		return err
	}

	return nil
}

// ensureChannelUniqueIndex creates a unique index on channels(user_id, name).
func ensureChannelUniqueIndex(db *gorm.DB) error {
	indexName := "idx_channels_user_id_name"

	// Check if the index already exists via GORM migrator.
	if db.Migrator().HasIndex(&Channel{}, indexName) {
		return nil
	}

	// Create the composite unique index via raw DDL.
	return db.Exec(
		"CREATE UNIQUE INDEX " + indexName + " ON channels(user_id, name)",
	).Error
}
