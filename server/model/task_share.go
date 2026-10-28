package model

import "time"

// TaskShare is private persistence; API responses use explicit reading DTOs.
type TaskShare struct {
	TaskID    string     `gorm:"type:char(36);primaryKey" json:"-"`
	UserID    string     `gorm:"type:char(36);index;not null" json:"-"`
	Token     string     `gorm:"type:varchar(64);uniqueIndex:idx_task_share_token;not null" json:"-"`
	ExpiresAt time.Time  `gorm:"not null" json:"-"`
	RevokedAt *time.Time `json:"-"`
	CreatedAt time.Time  `json:"-"`
	UpdatedAt time.Time  `json:"-"`
}

func (TaskShare) TableName() string { return "task_shares" }
