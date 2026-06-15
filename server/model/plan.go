package model

import "time"

// Plan represents a scheduled content generation plan.
type Plan struct {
	ID                 string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID             string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ChannelID          string     `gorm:"type:char(36);index" json:"channel_id"`
	Type               string     `gorm:"type:varchar(20);not null" json:"type"` // seednote, article
	Title              string     `gorm:"type:varchar(200)" json:"title"`
	Description        string     `gorm:"type:text" json:"description"`
	CronExpr           string     `gorm:"type:varchar(100)" json:"cron_expr"`
	Prompt             string     `gorm:"column:topic_hint;type:text" json:"prompt"`
	Status             string     `gorm:"type:varchar(20);default:active" json:"status"` // active, paused, completed
	ImageModelKey      string     `gorm:"type:varchar(50);default:''" json:"image_model_key,omitempty"`
	ReferenceImageURL  string     `gorm:"type:varchar(500)" json:"reference_image_url,omitempty"`
	SkipReferenceImage bool       `gorm:"default:false" json:"skip_reference_image,omitempty"`
	Watermark          bool       `gorm:"default:false" json:"watermark,omitempty"`
	NextRunAt          *time.Time `gorm:"index" json:"next_run_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// TableName returns the database table name for Plan.
func (Plan) TableName() string { return "plans" }
