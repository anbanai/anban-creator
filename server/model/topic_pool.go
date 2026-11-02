package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	TopicStatusUnused = "unused"
	TopicStatusUsed   = "used"
)

// TopicPool represents a user-managed topic in a project's topic pool.
type TopicPool struct {
	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    string `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID string `gorm:"type:char(36);index;not null;uniqueIndex:idx_topic_pool_project_dedupe" json:"project_id"`
	Topic     string `gorm:"type:varchar(500);not null" json:"topic"`
	// Legacy duplicate rows retain NULL keys so their IDs and history survive;
	// the earliest canonical row and every new topic receive a normalized key.
	DedupeKey          string     `gorm:"type:varchar(500);uniqueIndex:idx_topic_pool_project_dedupe" json:"-"`
	SourceDigestID     *string    `gorm:"type:char(36);index" json:"source_digest_id,omitempty"`
	SourceSuggestionID *string    `gorm:"type:char(36);index" json:"source_suggestion_id,omitempty"`
	SourceLabel        string     `gorm:"type:varchar(120)" json:"source_label,omitempty"`
	Status             string     `gorm:"type:varchar(20);default:'unused'" json:"status"`
	TaskID             *string    `gorm:"type:char(36)" json:"task_id,omitempty"`
	ContentAssetID     *string    `gorm:"type:char(36);index" json:"content_asset_id,omitempty"`
	UsedAt             *time.Time `json:"used_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// TableName returns the database table name for TopicPool.
func (TopicPool) TableName() string { return "topic_pool" }

func (t *TopicPool) BeforeSave(_ *gorm.DB) error {
	t.Topic = strings.TrimSpace(t.Topic)
	if t.DedupeKey == "" {
		t.DedupeKey = NormalizeTopicDedupeKey(t.Topic)
	}
	return nil
}
