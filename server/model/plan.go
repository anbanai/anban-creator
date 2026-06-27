package model

import "time"

// Plan represents a scheduled content generation plan.
//
// Under the "project = single source of truth" model, a plan is a pure
// SCHEDULER under a project — it no longer carries any style/persona/theme
// fields. Spawned tasks inherit everything from the project (task > project)
// and may override per dimension via Task.Overrides. A plan only owns scheduling
// (cron / topic hint / status), goal-mode, and a few per-plan image defaults
// that flow to the tasks it spawns.
type Plan struct {
	ID          string `gorm:"type:char(36);primaryKey" json:"id"`
	UserID      string `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID   string `gorm:"type:char(36);index" json:"project_id"`
	Type        string `gorm:"type:varchar(20);not null" json:"type"` // seednote, article, ecommerce
	Title       string `gorm:"type:varchar(200)" json:"title"`
	Description string `gorm:"type:text" json:"description"`
	CronExpr    string `gorm:"type:varchar(100)" json:"cron_expr"`
	Prompt      string `gorm:"column:topic_hint;type:text" json:"prompt"`
	Status      string `gorm:"type:varchar(20);default:active" json:"status"` // active, paused, completed
	// ImageModelKey / ReferenceImageURL are per-plan image defaults copied to each
	// spawned task (task-level values, when set, win). They are scheduling-adjacent
	// "what to produce" params, not style/persona/theme dimensions.
	ImageModelKey      string `gorm:"type:varchar(50);default:''" json:"image_model_key,omitempty"`
	ReferenceImageURL  string `gorm:"type:varchar(500)" json:"reference_image_url,omitempty"`
	SkipReferenceImage bool   `gorm:"default:false" json:"skip_reference_image,omitempty"`
	Watermark          bool   `gorm:"default:false" json:"watermark,omitempty"`
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
