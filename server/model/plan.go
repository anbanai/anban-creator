package model

import "time"

// Plan represents a scheduled content generation plan.
type Plan struct {
	ID                string `gorm:"type:char(36);primaryKey" json:"id"`
	UserID            string `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID         string `gorm:"type:char(36);index" json:"project_id"`
	Type              string `gorm:"type:varchar(20);not null" json:"type"` // seednote, article
	Title             string `gorm:"type:varchar(200)" json:"title"`
	Description       string `gorm:"type:text" json:"description"`
	CronExpr          string `gorm:"type:varchar(100)" json:"cron_expr"`
	Prompt            string `gorm:"column:topic_hint;type:text" json:"prompt"`
	Status            string `gorm:"type:varchar(20);default:active" json:"status"` // active, paused, completed
	ImageModelKey     string `gorm:"type:varchar(50);default:''" json:"image_model_key,omitempty"`
	ReferenceImageURL string `gorm:"type:varchar(500)" json:"reference_image_url,omitempty"`
	// Style is the plan-level 图片视觉 (image visual style), one of three orthogonal
	// dimensions (visual / writing / theme). Resolved at plan creation with
	// precedence plan > template > project, then copied to Task.Style by
	// CreateFromPlan (task-level override wins).
	Style string `gorm:"type:varchar(1024);default:''" json:"style,omitempty"`
	// WritingStyle is the plan-level 写作风格 (writer resource key, e.g. "dan-koe").
	// Orthogonal to Style/Theme; resolved plan > template > project, copied to Task.
	WritingStyle string `gorm:"type:varchar(100);default:''" json:"writing_style,omitempty"`
	// Theme is the plan-level 排版样式 (theme resource key). Orthogonal to
	// Style/WritingStyle; resolved plan > template > project, copied to Task.
	Theme string `gorm:"type:varchar(50);default:''" json:"theme,omitempty"`
	// Author / AuthorStyleIntro / AuthorAvatarURL are the plan-level 作者（署名） +
	// 写作风格（free-text imitation） + 可选人设头像, orthogonal to Style/WritingStyle/
	// Theme. Resolved plan > template > project, copied to Task by CreateFromPlan
	// (task-level override wins). Surfaced via get_project_profile(task_id).
	Author           string `gorm:"type:varchar(50);default:''" json:"author,omitempty"`
	AuthorStyleIntro string `gorm:"type:text" json:"author_style_intro,omitempty"`
	AuthorAvatarURL  string `gorm:"type:varchar(500);default:''" json:"author_avatar_url,omitempty"`
	// TemplateID records which template was selected during plan creation. Copied
	// to Task.TemplateID by CreateFromPlan so spawned tasks surface the template's
	// content scaffold (writing style / structure / example) to the agent via
	// get_project_profile(task_id). Nullable; old rows migrate to NULL.
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
