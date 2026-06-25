package model

import "time"

const (
	TopicStatusUnused = "unused"
	TopicStatusUsed   = "used"
)

// TopicPool represents a user-managed topic in a project's topic pool.
type TopicPool struct {
	ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID string     `gorm:"type:char(36);index;not null" json:"project_id"`
	Topic     string     `gorm:"type:varchar(500);not null" json:"topic"`
	Status    string     `gorm:"type:varchar(20);default:'unused'" json:"status"`
	TaskID    *string    `gorm:"type:char(36)" json:"task_id,omitempty"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// TableName returns the database table name for TopicPool.
func (TopicPool) TableName() string { return "topic_pool" }
