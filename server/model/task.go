package model

import (
	"time"

	"gorm.io/datatypes"
)

// ProgressPayload mirrors the MCP update_task_progress tool schema. Persisted
// to Task.LatestProgress at every stage transition so Studio can render the
// current stage across reloads without parsing the mixed progress_log column
// (which also contains "Using tool: ..." noise from the agent executor).
type ProgressPayload struct {
	Stage       string `json:"stage,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Percent     int    `json:"percent,omitempty"`
}

// EcommerceConfig carries the per-task inputs for an e-commerce image-generation
// package (project platform = "ecommerce"): which deliverable modules the buyer
// selected and in what quantity, the uploaded product photos, target platform,
// selling points, language, and an optional provider-strategy override. Stored on
// Task.Ecommerce as a typed JSON column (datatypes.JSONType) so the agent reads it
// via get_project_profile(task_id, scope="ecommerce") and Studio renders it.
//
// Visual style is NOT duplicated here — it flows through the orthogonal Task.Style
// dimension (resolved task > template > project), exactly like seednote/article.
//
// SelectedModules maps a module key to its quantity:
//   - main_images / detail_page / cover_banner / share_image / sku_images
//
// The task's credit cost is sum(module unit price × quantity) over this map, billed
// once at creation via CreditService.DeductForTaskWithAmount and refunded on failure
// via RefundForTask (amount-agnostic, keyed by task_id).
type EcommerceConfig struct {
	SelectedModules          map[string]int `json:"selected_modules,omitempty"`
	ProductPhotos            []string       `json:"product_photos,omitempty"`
	TargetPlatform           string         `json:"target_platform,omitempty"`
	SellingPoints            string         `json:"selling_points,omitempty"`
	// BrandBrief is the brand positioning/voice, resolved from the selected
	// e-commerce template's BrandBrief at task creation (task-level override
	// rare; this is template/project-level brand context, distinct from the
	// product-specific SellingPoints). Surfaced to the agent via
	// get_project_profile(scope="ecommerce") so product imagery respects brand.
	BrandBrief string `json:"brand_brief,omitempty"`
	Language   string `json:"language,omitempty"`
	// ProviderStrategyOverride is deprecated/superseded: provider selection now
	// flows through Task.ImageModelKey (user picks the image preset at task
	// creation). Retained on the column for backward compatibility with existing
	// rows; no longer surfaced to the agent (get_project_profile returns the
	// resolved image_model instead).
	ProviderStrategyOverride string `json:"provider_strategy_override,omitempty"`
}

// Task represents a content generation task.
type Task struct {
	ID                string `gorm:"type:char(36);primaryKey" json:"id"`
	UserID            string `gorm:"type:char(36);index:idx_user_status,priority:1;index:idx_user_created,priority:1;not null" json:"user_id"`
	ProjectID         string `gorm:"type:char(36);index" json:"project_id"`
	PlanID            *uint  `gorm:"index" json:"plan_id"`
	Type              string `gorm:"type:varchar(20);not null" json:"type"`
	Status            string `gorm:"type:varchar(20);default:pending;index:idx_user_status,priority:2" json:"status"`
	Prompt            string `gorm:"column:topic;type:varchar(5120)" json:"prompt"`
	Title             string `gorm:"type:varchar(200)" json:"title,omitempty"`
	ImageRatio        string `gorm:"type:varchar(10);default:''" json:"image_ratio,omitempty"`
	ImageModelKey     string `gorm:"type:varchar(50);default:''" json:"image_model_key,omitempty"`
	ReferenceImageURL string `gorm:"type:varchar(500)" json:"reference_image_url,omitempty"`
	// Style is the effective 图片视觉 (image visual style) for this task, one of
	// three orthogonal dimensions (visual / writing / theme). Resolved at creation
	// with precedence task > template > plan > project. Propagated to the agent
	// via the user prompt (BuildUserPrompt).
	Style string `gorm:"type:varchar(1024);default:''" json:"style,omitempty"`
	// WritingStyle is the effective 写作风格 (writer resource key, e.g. "dan-koe"),
	// orthogonal to Style/Theme. Resolved task > template > plan > project.
	// Consumed by write_article / GenerateOutline (never injected as visual style).
	WritingStyle string `gorm:"type:varchar(100);default:''" json:"writing_style,omitempty"`
	// Theme is the effective 排版样式 (theme resource key), orthogonal to
	// Style/WritingStyle. Resolved task > template > plan > project. Consumed by
	// the deterministic HTML renderer (convert_markdown / render_template).
	Theme string `gorm:"type:varchar(50);default:''" json:"theme,omitempty"`
	// Author is the effective 作者（署名 byline）for this task, surfaced as the
	// top-level `author` via get_project_profile(task_id). Resolved task > template
	// > plan > project. Independent of the writing-imitation intro below.
	Author string `gorm:"type:varchar(50);default:''" json:"author,omitempty"`
	// AuthorStyleIntro is the effective 写作风格 (free-text writing imitation),
	// surfaced as template_writing_style via get_project_profile(task_id). Resolved
	// task > template > plan > project. Orthogonal to Style/Theme and to the byline.
	AuthorStyleIntro string `gorm:"type:text" json:"author_style_intro,omitempty"`
	// AuthorAvatarURL is the optional 写作风格 persona avatar (not part of the
	// byline), surfaced as template_author_avatar. Resolved task > template > plan
	// > project.
	AuthorAvatarURL string `gorm:"type:varchar(500);default:''" json:"author_avatar_url,omitempty"`
	// TemplateID records which template was selected when creating this task
	// (manual task, or a plan-spawned task inheriting plan.TemplateID). It does
	// NOT enter the style resolution chain (visual style lives in Style) —
	// instead the agent surfaces the template's content scaffold (writing style /
	// structure / example) via get_project_profile(task_id). Old rows → NULL.
	TemplateID         *string `gorm:"type:char(36);index" json:"template_id,omitempty"`
	SkipReferenceImage bool    `gorm:"default:false" json:"skip_reference_image,omitempty"`
	Watermark          bool    `gorm:"default:false" json:"watermark,omitempty"`
	// HasContentImage / HasTailImage control seednote image composition. Cover is
	// always generated; these two flags decide whether image_01.png and tail.png
	// follow. Default matches the seednote form default (content on, tail off).
	// Propagated to the agent via BuildUserPrompt; non-seednote task types ignore them.
	HasContentImage bool `gorm:"default:true;not null" json:"has_content_image"`
	HasTailImage    bool `gorm:"default:false;not null" json:"has_tail_image"`
	// Ecommerce carries the e-commerce package config for platform="ecommerce"
	// tasks (selected deliverable modules, product photos, target platform, selling
	// points, language, provider-strategy override). Zero value for non-ecommerce
	// tasks. Read by the agent via get_project_profile(task_id, scope="ecommerce").
	Ecommerce           datatypes.JSONType[EcommerceConfig] `gorm:"type:json" json:"ecommerce"`
	ProgressLog         string                              `gorm:"type:longtext" json:"progress_log,omitempty"`
	Progress            int                                 `gorm:"default:0" json:"progress,omitempty"`
	LatestProgress      datatypes.JSONType[ProgressPayload] `gorm:"type:json" json:"latest_progress"`
	Result              *string                             `gorm:"type:json" json:"result,omitempty"`
	InputTokens         *int64                              `json:"input_tokens,omitempty"`
	OutputTokens        *int64                              `json:"output_tokens,omitempty"`
	CacheReadTokens     *int64                              `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens *int64                              `json:"cache_creation_tokens,omitempty"`
	TotalCostUSD        *float64                            `json:"total_cost_usd,omitempty"`
	ErrorMessage        string                              `gorm:"type:text" json:"error_message,omitempty"`
	StartedAt           *time.Time                          `gorm:"index" json:"started_at"`
	CompletedAt         *time.Time                          `gorm:"index" json:"completed_at"`
	CleanedUpAt         *time.Time                          `gorm:"index" json:"cleaned_up_at"`
	LastHeartbeatAt     *time.Time                          `gorm:"index" json:"last_heartbeat_at,omitempty"`
	RetryCount          int                                 `gorm:"default:0" json:"retry_count"`
	MaxRetries          int                                 `gorm:"default:3" json:"max_retries"`
	RateLimitRetryCount int                                 `gorm:"default:0" json:"rate_limit_retry_count"`

	// Goal mode: when GoalMode is true, Goal is prepended to the user prompt as
	// a /goal slash command so Claude Code's built-in goal loop drives
	// turn-by-turn evaluation inside a single agent session. No server-side
	// retry / evaluation log; the loop is opaque to the server.
	Goal     string `gorm:"type:text" json:"goal,omitempty"`
	GoalMode bool   `gorm:"default:false" json:"goal_mode"`

	Published      bool       `gorm:"default:false" json:"published"`
	PublishedAt    *time.Time `gorm:"index" json:"published_at,omitempty"`
	WorkflowStatus *string    `gorm:"type:json" json:"workflow_status,omitempty"`
	CreatedAt      time.Time  `gorm:"index:idx_user_created,priority:2" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"index" json:"updated_at"`
	Plan           *Plan      `gorm:"foreignKey:PlanID" json:"plan,omitempty"`
}

// TableName returns the database table name for Task.
func (Task) TableName() string { return "tasks" }

// SetEcommerce stores an e-commerce package config into the Ecommerce JSON
// column. Thin wrapper over datatypes.NewJSONType so call sites (task creation,
// plan-spawned tasks) don't each need to import gorm.io/datatypes.
func (t *Task) SetEcommerce(ec EcommerceConfig) {
	t.Ecommerce = datatypes.NewJSONType(ec)
}
