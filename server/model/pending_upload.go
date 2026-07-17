package model

import (
	"errors"
	"time"
)

const (
	PendingUploadStatusPending   = "pending"
	PendingUploadStatusExpiring  = "expiring"
	PendingUploadStatusFinalized = "finalized"
	PendingUploadStatusExpired   = "expired"
)

var (
	ErrPendingUploadNotFound      = errors.New("pending upload not found")
	ErrPendingUploadClaimRejected = errors.New("pending upload claim rejected")
)

type PendingUploadClaim struct {
	UploadID        string
	UserID          string
	Key             string
	FinalizedKey    string
	AllowedPurposes []string
}

// PendingUpload tracks browser-direct OSS uploads until a project, task, or plan
// submit claims them. Unclaimed rows are eligible for cleanup after ExpiresAt.
type PendingUpload struct {
	ID               string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID           string     `gorm:"type:char(36);index;not null" json:"user_id"`
	Purpose          string     `gorm:"type:varchar(50);index;not null" json:"purpose"`
	Key              string     `gorm:"type:varchar(500);uniqueIndex;not null" json:"key"`
	FinalizedKey     string     `gorm:"type:varchar(500);index" json:"finalized_key,omitempty"`
	PublicURL        string     `gorm:"type:varchar(800);not null" json:"public_url"`
	FileName         string     `gorm:"type:varchar(255);not null" json:"file_name"`
	ContentType      string     `gorm:"type:varchar(120)" json:"content_type"`
	Size             int64      `gorm:"default:0" json:"size"`
	Status           string     `gorm:"type:varchar(20);index;not null;default:pending" json:"status"`
	ExpiresAt        time.Time  `gorm:"index;not null" json:"expires_at"`
	FinalizedAt      *time.Time `json:"finalized_at,omitempty"`
	CleanupClaimID   string     `gorm:"type:char(36);index" json:"cleanup_claim_id,omitempty"`
	CleanupClaimedAt *time.Time `gorm:"index" json:"cleanup_claimed_at,omitempty"`
	ExpiredAt        *time.Time `json:"expired_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (PendingUpload) TableName() string { return "pending_uploads" }
