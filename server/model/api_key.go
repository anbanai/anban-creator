package model

import "time"

// APIKey represents a per-user API key for MCP and external access.
type APIKey struct {
	ID         string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID     string     `gorm:"type:char(36);index;not null" json:"user_id"`
	Name       string     `gorm:"type:varchar(100)" json:"name"`
	KeyHash    string     `gorm:"type:char(64);uniqueIndex;not null" json:"-"`
	KeyPrefix  string     `gorm:"type:varchar(12);not null" json:"key_prefix"`
	IsManaged  bool       `gorm:"default:false" json:"is_managed"`
	RawKey     string     `gorm:"type:text" json:"-"` // stored only for managed keys; empty for user-created keys
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// TableName returns the database table name for APIKey.
func (APIKey) TableName() string { return "api_keys" }
