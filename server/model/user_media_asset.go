package model

import "time"

const (
	UserMediaAssetStatusQueued    = "queued"
	UserMediaAssetStatusAnalyzing = "analyzing"
	UserMediaAssetStatusAvailable = "available"
	UserMediaAssetStatusFailed    = "failed"
)

// UserMediaAsset is account-owned metadata for an immutable uploaded Asset.
// Removing a library row never removes the underlying Asset because historical
// task attachments may continue to reference it.
type UserMediaAsset struct {
	ID              string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID          string     `gorm:"type:char(36);index;not null" json:"user_id"`
	AssetID         string     `gorm:"type:char(36);uniqueIndex;not null" json:"asset_id"`
	ProjectID       string     `gorm:"type:char(36);index" json:"project_id,omitempty"`
	MediaType       string     `gorm:"type:varchar(16);not null;index" json:"media_type"`
	Status          string     `gorm:"type:varchar(20);not null;index" json:"status"`
	Description     string     `gorm:"type:text" json:"description,omitempty"`
	RecommendedUses []string   `gorm:"type:json;serializer:json" json:"recommended_uses,omitempty"`
	UsageNotes      string     `gorm:"type:text" json:"usage_notes,omitempty"`
	ErrorCode       string     `gorm:"type:varchar(80)" json:"error_code,omitempty"`
	ErrorMessage    string     `gorm:"type:varchar(500)" json:"error_message,omitempty"`
	RemovedAt       *time.Time `gorm:"index" json:"removed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (UserMediaAsset) TableName() string { return "user_media_assets" }

func (m *UserMediaAsset) Active() bool { return m != nil && m.RemovedAt == nil }
