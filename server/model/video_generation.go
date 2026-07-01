package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// VideoGeneration records the provider task and platform file closure for a
// video task. Provider URLs are intermediate metadata; Studio delivery should
// use TaskFile IDs registered through OSS-backed storage.
type VideoGeneration struct {
	ID               string                                    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID           string                                    `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID        string                                    `gorm:"type:char(36);index" json:"project_id"`
	TaskID           string                                    `gorm:"type:char(36);index" json:"task_id"`
	PlanID           string                                    `gorm:"type:char(36);index" json:"plan_id,omitempty"`
	ArkTaskID        string                                    `gorm:"type:varchar(100);index" json:"ark_task_id,omitempty"`
	Status           string                                    `gorm:"type:varchar(40);index" json:"status"`
	ResolvedParams   datatypes.JSONType[VideoTaskConfig]       `gorm:"type:json" json:"resolved_params"`
	References       datatypes.JSON                            `gorm:"type:json" json:"references,omitempty"`
	PricingBreakdown datatypes.JSONType[VideoPricingBreakdown] `gorm:"type:json" json:"pricing_breakdown"`
	CreditsCharged   int                                       `gorm:"default:0" json:"credits_charged"`
	ErrorMessage     string                                    `gorm:"type:text" json:"error_message,omitempty"`
	ProviderURLs     datatypes.JSON                            `gorm:"type:json" json:"provider_urls,omitempty"`
	TaskFileIDs      datatypes.JSON                            `gorm:"type:json" json:"task_file_ids,omitempty"`
	CreatedAt        time.Time                                 `gorm:"index" json:"created_at"`
	UpdatedAt        time.Time                                 `gorm:"index" json:"updated_at"`
}

func (VideoGeneration) TableName() string { return "video_generations" }

func (g *VideoGeneration) BeforeCreate(tx *gorm.DB) error {
	if g.Status == "" {
		g.Status = "submitted"
	}
	return nil
}
