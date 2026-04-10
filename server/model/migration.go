package model

import (
	"log"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MigrateUserConfigsToChannels migrates existing user_configs rows into channels
// rows. It also backfills channel_id on existing tasks and plans that lack it.
//
// This function is idempotent: calling it multiple times is safe. It uses
// GORM operations rather than raw SQL so it works with both MySQL (production)
// and SQLite (tests).
//
// It should be called from main.go after AutoMigrate, NOT during tests.
func MigrateUserConfigsToChannels(db *gorm.DB) error {
	// 1. Migrate user_configs -> channels.
	var configs []UserConfig
	if err := db.Find(&configs).Error; err != nil {
		return err
	}

	for _, cfg := range configs {
		// Check if a channel already exists for this user_id + platform.
		var existing Channel
		err := db.Where("user_id = ? AND platform = ?", cfg.UserID, cfg.Scope).First(&existing).Error
		if err == nil {
			// Channel already exists for this (user_id, platform) -- skip.
			continue
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}

		// Build the channel name: use the config name if non-empty, otherwise
		// derive from scope.
		name := cfg.Name
		if name == "" {
			name = cfg.Scope + "频道"
		}

		ch := Channel{
			ID:          strings.ReplaceAll(uuid.New().String(), "-", ""),
			UserID:      cfg.UserID,
			Platform:    cfg.Scope,
			Name:        name,
			Keywords:    cfg.Keywords,
			Positioning: cfg.Positioning,
			Style:       cfg.Style,
			Theme:       cfg.Theme,
			Author:      cfg.Author,
			Config: ChannelConfig{
				WechatAppID:  cfg.WechatAppID,
				WechatSecret: cfg.WechatSecret,
			},
			Status: ChannelStatusActive,
		}
		if err := db.Create(&ch).Error; err != nil {
			// If a unique constraint race occurred (two processes running
			// migration simultaneously), skip this row.
			if strings.Contains(err.Error(), "UNIQUE constraint failed") ||
				strings.Contains(err.Error(), "Duplicate entry") {
				continue
			}
			return err
		}
	}

	// 2. Backfill channel_id on tasks that have type matching a channel's platform.
	if err := backfillTaskChannelID(db); err != nil {
		return err
	}

	// 3. Backfill channel_id on plans.
	if err := backfillPlanChannelID(db); err != nil {
		return err
	}

	return nil
}

// backfillTaskChannelID sets channel_id on tasks where it is empty,
// by matching (user_id, type) to (user_id, platform) in channels.
func backfillTaskChannelID(db *gorm.DB) error {
	// Find all tasks missing a channel_id.
	var tasks []Task
	if err := db.Where("channel_id = '' OR channel_id IS NULL").Find(&tasks).Error; err != nil {
		return err
	}

	for _, task := range tasks {
		var ch Channel
		err := db.Where("user_id = ? AND platform = ? AND status = ?", task.UserID, task.Type, ChannelStatusActive).
			First(&ch).Error
		if err != nil {
			// No matching channel -- skip this task.
			continue
		}
		if err := db.Model(&Task{}).Where("id = ?", task.ID).Update("channel_id", ch.ID).Error; err != nil {
			log.Printf("migration: failed to backfill channel_id for task %s: %v", task.ID, err)
		}
	}

	return nil
}

// backfillPlanChannelID sets channel_id on plans where it is empty,
// by matching (user_id, type) to (user_id, platform) in channels.
func backfillPlanChannelID(db *gorm.DB) error {
	var plans []Plan
	if err := db.Where("channel_id = '' OR channel_id IS NULL").Find(&plans).Error; err != nil {
		return err
	}

	for _, plan := range plans {
		var ch Channel
		err := db.Where("user_id = ? AND platform = ? AND status = ?", plan.UserID, plan.Type, ChannelStatusActive).
			First(&ch).Error
		if err != nil {
			continue
		}
		if err := db.Model(&Plan{}).Where("id = ?", plan.ID).Update("channel_id", ch.ID).Error; err != nil {
			log.Printf("migration: failed to backfill channel_id for plan %s: %v", plan.ID, err)
		}
	}

	return nil
}
