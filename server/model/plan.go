package model

import "time"

// Plan represents a scheduled content generation plan.
//
// A plan is a scheduler under a project. It owns scheduling (cron / topic hint /
// status), goal-mode, and a few per-plan image defaults that flow to spawned
// tasks. Project/account style config is snapshotted from the project when the
// task is created; plans no longer override visual/writer/author/theme.
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
	// "what to produce" params, not style/author/theme dimensions.
	ImageModelKey      string `gorm:"type:varchar(50);default:''" json:"image_model_key,omitempty"`
	ReferenceImageURL  string `gorm:"type:varchar(500)" json:"reference_image_url,omitempty"`
	SkipReferenceImage bool   `gorm:"default:false" json:"skip_reference_image,omitempty"`
	Watermark          bool   `gorm:"default:false" json:"watermark,omitempty"`
	// HasContentImage / HasTailImage are plan-level seednote image composition
	// flags copied to Task on CreateFromPlan. Cover is always on; content defaults
	// to on, tail defaults to off — matches the seednote form default.
	HasContentImage bool `gorm:"default:true;not null" json:"has_content_image"`
	HasTailImage    bool `gorm:"default:false;not null" json:"has_tail_image"`
	// ArticleWithCover / ArticleWithContentImages are plan-level 公众号 article image
	// toggles copied to Task on CreateFromPlan. Nullable *bool, DB default true (nil =
	// generate). See model.Task.ArticleWithCover for why *bool is required.
	ArticleWithCover         *bool `gorm:"default:true;not null" json:"article_with_cover"`
	ArticleWithContentImages *bool `gorm:"default:true;not null" json:"article_with_content_images"`

	// Legacy style/author/theme columns. New code no longer writes or reads these;
	// task runtime config is frozen from the owning project into Task.ProjectSnapshot.
	VisualStyle string `gorm:"type:varchar(1024);default:''" json:"visual_style,omitempty"` // 图片视觉 (free text)
	Writer      string `gorm:"type:varchar(100);default:''" json:"writer,omitempty"`        // 写作者 YAML resource key
	Author      string `gorm:"type:varchar(200);default:''" json:"author,omitempty"`        // 作者署名 (publish author — never a writer persona name)
	Theme       string `gorm:"type:varchar(50);default:''" json:"theme,omitempty"`          // 排版主题 key

	// Goal mode configuration propagated to tasks created from this plan.
	Goal     string `gorm:"type:text" json:"goal,omitempty"`
	GoalMode bool   `gorm:"default:false" json:"goal_mode"`

	NextRunAt *time.Time `gorm:"index" json:"next_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// TableName returns the database table name for Plan.
func (Plan) TableName() string { return "plans" }
