package model

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// prepareTopicPoolDedupe expands and backfills the legacy table before
// AutoMigrate creates its unique index. Duplicate historical rows keep their
// IDs, task/asset links and timestamps; only their dedupe key remains NULL.
// New writes always receive a normalized key through TopicPool.BeforeSave.
// The index is the completion marker, so an interrupted upgrade is resumable.
func prepareTopicPoolDedupe(db *gorm.DB) error {
	migrator := db.Migrator()
	if !migrator.HasTable(&TopicPool{}) || migrator.HasIndex(&TopicPool{}, "idx_topic_pool_project_dedupe") {
		return nil
	}
	if !migrator.HasColumn(&TopicPool{}, "DedupeKey") {
		if err := migrator.AddColumn(&TopicPool{}, "DedupeKey"); err != nil {
			return fmt.Errorf("expand topic pool dedupe: %w", err)
		}
	} else if err := migrator.AlterColumn(&TopicPool{}, "DedupeKey"); err != nil {
		return fmt.Errorf("make legacy topic dedupe nullable: %w", err)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		var after uint
		for {
			var rows []struct {
				ID        uint
				ProjectID string
				Topic     string
			}
			if err := tx.Table("topic_pool").Select("id, project_id, topic").
				Where("id > ? AND (dedupe_key IS NULL OR dedupe_key = '')", after).
				Order("id ASC").Limit(500).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
				return fmt.Errorf("read legacy topic dedupe: %w", err)
			}
			if len(rows) == 0 {
				return nil
			}
			for _, row := range rows {
				key := NormalizeTopicDedupeKey(row.Topic)
				var existing int64
				// Use the database's comparison semantics, including its collation,
				// so backfilled keys obey the same equivalence as the unique index.
				if err := tx.Table("topic_pool").Where("project_id = ? AND dedupe_key = ? AND id <> ?", row.ProjectID, key, row.ID).Count(&existing).Error; err != nil {
					return fmt.Errorf("resolve canonical legacy topic: %w", err)
				}
				var value any
				if existing == 0 && key != "" {
					value = key
				}
				// Table + UpdateColumn intentionally bypass model hooks/timestamps.
				if err := tx.Table("topic_pool").Where("id = ?", row.ID).UpdateColumn("dedupe_key", value).Error; err != nil {
					return fmt.Errorf("backfill legacy topic dedupe: %w", err)
				}
				after = row.ID
			}
		}
	})
}
