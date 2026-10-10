package model

import "time"

// InteractiveCommitReceipt is the durable idempotency record for a project
// creator commit. The unique user/key pair is the replay boundary.
type InteractiveCommitReceipt struct {
	ID               string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID           string     `gorm:"type:char(36);not null;uniqueIndex:idx_interactive_commit_user_key,priority:1" json:"user_id"`
	IdempotencyKey   string     `gorm:"type:varchar(200);not null;uniqueIndex:idx_interactive_commit_user_key,priority:2" json:"idempotency_key"`
	SessionID        string     `gorm:"type:char(36);not null;index" json:"session_id"`
	ConfirmationHash string     `gorm:"type:char(64);not null" json:"confirmation_hash"`
	RequestHash      string     `gorm:"type:char(64);not null;default:''" json:"-"`
	ProjectID        string     `gorm:"type:char(36);not null" json:"project_id"`
	MemoryAppend     string     `gorm:"type:text" json:"-"`
	MemoryExpected   string     `gorm:"type:text" json:"-"`
	MemoryAppliedAt  *time.Time `json:"-"`
	MemoryError      string     `gorm:"type:text" json:"-"`
	ResultJSON       []byte     `gorm:"type:json;not null" json:"-"`
	CreatedAt        time.Time  `json:"created_at"`
}
