package model

import "time"

// Plan represents a scheduled content generation plan.
type Plan struct {
	ID                string `gorm:"type:char(36);primaryKey" json:"id"`
	UserID            string `gorm:"type:char(36);index;not null" json:"user_id"`
	ChannelID         string `gorm:"type:char(36);index" json:"channel_id"`
	Type              string `gorm:"type:varchar(20);not null" json:"type"` // seednote, article
	Title             string `gorm:"type:varchar(200)" json:"title"`
	Description       string `gorm:"type:text" json:"description"`
	CronExpr          string `gorm:"type:varchar(100)" json:"cron_expr"`
	Prompt            string `gorm:"column:topic_hint;type:text" json:"prompt"`
	Status            string `gorm:"type:varchar(20);default:active" json:"status"` // active, paused, completed
	ImageModelKey     string `gorm:"type:varchar(50);default:''" json:"image_model_key,omitempty"`
	ReferenceImageURL string `gorm:"type:varchar(500)" json:"reference_image_url,omitempty"`
	// Style is the plan-level visual style, populated when a template is selected
	// during plan creation. Copied to Task.Style (with task-level override winning)
	// when CreateFromPlan runs; if empty, the channel's style is used as fallback.
	Style string `gorm:"type:varchar(1024);default:''" json:"style,omitempty"`
	// TemplateID records which template was selected during plan creation. Copied
	// to Task.TemplateID by CreateFromPlan so spawned tasks surface the template's
	// content scaffold (writing style / structure / example) to the agent via
	// get_channel_profile(task_id). Nullable; old rows migrate to NULL.
	TemplateID         *string `gorm:"type:char(36);index" json:"template_id,omitempty"`
	SkipReferenceImage bool    `gorm:"default:false" json:"skip_reference_image,omitempty"`
	Watermark          bool    `gorm:"default:false" json:"watermark,omitempty"`
	// HasContentImage / HasTailImage are plan-level seednote image composition
	// flags copied to Task on CreateFromPlan. Cover is always on; content defaults
	// to on, tail defaults to off — matches the seednote form default.
	HasContentImage bool `gorm:"default:true;not null" json:"has_content_image"`
	HasTailImage    bool `gorm:"default:false;not null" json:"has_tail_image"`

	// Goal mode configuration propagated to tasks created from this plan.
	Goal     string `gorm:"type:text" json:"goal,omitempty"`
	GoalMode bool   `gorm:"default:false" json:"goal_mode"`

	NextRunAt *time.Time `gorm:"index" json:"next_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// TableName returns the database table name for Plan.
func (Plan) TableName() string { return "plans" }
