package model

import "gorm.io/gorm"

// AutoMigrate creates or updates all database tables.
func AutoMigrate(db *gorm.DB) error {
	err := db.AutoMigrate(
		&User{},
		&LoginSession{},
		&UserConfig{},
		&Channel{},
		&Plan{},
		&Task{},
		&TaskFile{},
	)
	if err != nil {
		return err
	}

	// Add unique index on (user_id, scope) for UserConfig.
	// AutoMigrate does not create composite unique indexes from struct tags,
	// so we create it explicitly.
	if err := ensureUserConfigUniqueIndex(db); err != nil {
		return err
	}

	// Add unique index on (user_id, name) for Channel.
	if err := ensureChannelUniqueIndex(db); err != nil {
		return err
	}

	return nil
}

// ensureUserConfigUniqueIndex creates a unique index on user_configs(user_id, scope).
func ensureUserConfigUniqueIndex(db *gorm.DB) error {
	indexName := "idx_user_configs_user_id_scope"

	// Check if the index already exists via GORM migrator.
	if db.Migrator().HasIndex(&UserConfig{}, indexName) {
		return nil
	}

	// Create the composite unique index via raw DDL.
	// The syntax works for MySQL (the primary target).
	return db.Exec(
		"CREATE UNIQUE INDEX " + indexName + " ON user_configs(user_id, scope)",
	).Error
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
