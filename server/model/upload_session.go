package model

import (
	"errors"
	"time"
)

const (
	UploadSessionPending    = "pending"
	UploadSessionFinalizing = "finalizing"
	UploadSessionFinalized  = "finalized"
	UploadSessionExpiring   = "expiring"
	UploadSessionExpired    = "expired"
)

var (
	ErrUploadSessionNotFound      = errors.New("upload session not found")
	ErrUploadSessionClaimRejected = errors.New("upload session claim rejected")
)

// UploadSession tracks a browser-direct upload until it is finalized into an
// immutable asset or expires from staging storage.
type UploadSession struct {
	ID                    string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID                string     `gorm:"type:char(36);index;not null" json:"user_id"`
	Purpose               string     `gorm:"type:varchar(50);index;not null" json:"purpose"`
	StagingKey            string     `gorm:"type:varchar(500);uniqueIndex;not null" json:"staging_key"`
	FileName              string     `gorm:"type:varchar(255);not null" json:"file_name"`
	ContentType           string     `gorm:"type:varchar(120);not null" json:"content_type"`
	Size                  int64      `gorm:"not null" json:"size"`
	Status                string     `gorm:"type:varchar(20);index;not null;default:pending" json:"status"`
	ExpiresAt             time.Time  `gorm:"index;not null" json:"expires_at"`
	FinalizationToken     string     `gorm:"type:char(36);index" json:"-"`
	FinalizationClaimedAt *time.Time `gorm:"index" json:"-"`
	PromotionSourceETag   string     `gorm:"column:promotion_source_etag;type:varchar(255);not null;default:''" json:"-"`
	FinalizationETag      string     `gorm:"column:finalization_etag;type:varchar(255);not null;default:''" json:"-"`
	AssetID               string     `gorm:"type:char(36);index" json:"asset_id,omitempty"`
	FinalizedAt           *time.Time `json:"finalized_at,omitempty"`
	CleanupClaimID        string     `gorm:"type:char(36);index" json:"-"`
	CleanupClaimedAt      *time.Time `gorm:"index" json:"-"`
	ExpiredAt             *time.Time `json:"expired_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (UploadSession) TableName() string { return "upload_sessions" }
