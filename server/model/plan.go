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
	// Style is the plan-level visual style, populated when a template is selected
	// during plan creation. Copied to Task.Style (with task-level override winning)
	// when CreateFromPlan runs; if empty, the channel's style is used as fallback.
	Style              string     `gorm:"type:varchar(1024);default:''" json:"style,omitempty"`
	SkipReferenceImage bool       `gorm:"default:false" json:"skip_reference_image,omitempty"`
	Watermark          bool       `gorm:"default:false" json:"watermark,omitempty"`

	// Goal mode configuration propagated to tasks created from this plan.
	Goal            string `gorm:"type:text" json:"goal,omitempty"`
	GoalMode        bool   `gorm:"default:false" json:"goal_mode"`
	GoalMaxAttempts int    `gorm:"default:3" json:"goal_max_attempts"`

	NextRunAt          *time.Time `gorm:"index" json:"next_run_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// TableName returns the database table name for Plan.
func (Plan) TableName() string { return "plans" }
