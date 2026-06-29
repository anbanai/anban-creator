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
// Visual style is NOT duplicated here — it flows through the orthogonal
// Task.Overrides.VisualStyle dimension (resolved task > project), exactly like
// seednote/article.
//
// SelectedModules maps a module key to its quantity:
//   - main_images / detail_page / cover_banner / share_image / sku_images
//
// The task's credit cost is sum(module unit price × quantity) over this map, billed
// once at creation via CreditService.DeductForTaskWithAmount and refunded on failure
// via RefundForTask (amount-agnostic, keyed by task_id).
type EcommerceConfig struct {
	SelectedModules map[string]int `json:"selected_modules,omitempty"`
	ProductPhotos   []string       `json:"product_photos,omitempty"`
	TargetPlatform  string         `json:"target_platform,omitempty"`
	SellingPoints   string         `json:"selling_points,omitempty"`
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

// StyleOverrides holds per-task overrides for the dimensions a task otherwise
// inherits from its project. Only fields the user explicitly overrode are set;
// empty/absent = inherit the project value. Resolution at execution is the
// two-layer task.Overrides.X ?? project.X (no template/plan layer).
//
// Stored on Task.Overrides as a typed JSON column (datatypes.JSONType) so the
// zero value serializes to a valid JSON null and the "only overridden keys"
// semantics are expressed by presence rather than by nullable columns.
type StyleOverrides struct {
	VisualStyle   string `json:"visual_style,omitempty"`   // 图片视觉 (free text)
	WriterKey     string `json:"writer_key,omitempty"`     // 写作者 YAML resource key
	WritingVoice  string `json:"writing_voice,omitempty"`  // 写作笔迹 (free-text imitation)
	Byline        string `json:"byline,omitempty"`         // 作者署名 (publish byline)
	PersonaAvatar string `json:"persona_avatar,omitempty"` // 人设头像 (never part of byline)
	Theme         string `json:"theme,omitempty"`          // 排版主题 key
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
	// Overrides carries per-task overrides for the style/persona/theme dimensions
	// the task otherwise inherits from its project. Only keys the user explicitly
	// overrode are set; empty/absent = inherit the project value. Effective value
	// at execution = task.Overrides.X ?? project.X (two-layer, no template/plan
	// layer). See StyleOverrides.
	Overrides          datatypes.JSONType[StyleOverrides] `gorm:"type:json" json:"overrides"`
	SkipReferenceImage bool                               `gorm:"default:false" json:"skip_reference_image,omitempty"`
	Watermark          bool                               `gorm:"default:false" json:"watermark,omitempty"`
	// HasContentImage / HasTailImage control seednote image composition. Cover is
	// always generated; these two flags decide whether image_01.png and tail.png
	// follow. Default matches the seednote form default (content on, tail off).
	// Propagated to the agent via BuildUserPrompt; non-seednote task types ignore them.
	HasContentImage bool `gorm:"default:true;not null" json:"has_content_image"`
	HasTailImage    bool `gorm:"default:false;not null" json:"has_tail_image"`
	// ArticleWithCover / ArticleWithContentImages toggle 公众号 article image
	// generation independently (unlike seednote, the article cover is NOT mandatory).
	// Nullable *bool with a DB default of true: nil (and the DDL default applied to
	// pre-existing rows) = generate. A non-nil false is honored — plain bool with
	// default:true cannot represent "off" because GORM auto-fills the zero value
	// (false) back to the default at Create time. Propagated to the agent via
	// BuildUserPrompt; non-article task types ignore them.
	ArticleWithCover         *bool `gorm:"default:true;not null" json:"article_with_cover"`
	ArticleWithContentImages *bool `gorm:"default:true;not null" json:"article_with_content_images"`
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

	Published   bool       `gorm:"default:false" json:"published"`
	PublishedAt *time.Time `gorm:"index" json:"published_at,omitempty"`
	// PublishApprovalState drives the publish-approval gate (Batch 4A): "", then
	// "pending" when a completed article task froze its draft data awaiting human
	// review, "approved"/"rejected" once acted on. Only meaningful when the owning
	// project has RequirePublishApproval && EnablePublishing. Empty for all other
	// tasks and all pre-gate projects, so existing rows need no migration.
	PublishApprovalState string `gorm:"type:varchar(20);default:''" json:"publish_approval_state,omitempty"`
	// PendingDraftArticles is the frozen []service.DraftArticleInput (marshaled)
	// captured at task completion when the approval gate holds. Stored here so the
	// resume path (ApprovePublish) can publish without re-extracting from the
	// (possibly cleaned-up) workspace. Opaque datatypes.JSON rather than
	// JSONType[T] because the element type lives in package service — model may
	// not import service (cycle). The service boundary (un)marshals it.
	PendingDraftArticles datatypes.JSON `gorm:"type:json" json:"pending_draft_articles,omitempty"`
	WorkflowStatus       *string        `gorm:"type:json" json:"workflow_status,omitempty"`

	// Local-execution (desktop) claim protocol. ExecutionTarget selects where the
	// task runs: ExecutionTargetCloud (empty, default) → cloud Asynq/Docker;
	// ExecutionTargetLocal → awaiting claim by a desktop local executor (not
	// enqueued to Asynq); ExecutionTargetLocalClaimed → a desktop claimed it and
	// is running it locally. LocalClaimDeadline is set at creation for "local"
	// tasks; the fallback worker (service.ReclaimExpiredLocalTasks) flips expired
	// unclaimed ones back to cloud so tasks never get stuck when no desktop is
	// online. ExecutorInfo records which desktop claimed the task (diagnostics).
	ExecutionTarget    string                           `gorm:"type:varchar(20);default:''" json:"execution_target,omitempty"`
	LocalClaimDeadline *time.Time                       `gorm:"index" json:"local_claim_deadline,omitempty"`
	ExecutorInfo       datatypes.JSONType[ExecutorMeta] `gorm:"type:json" json:"executor_info"`

	CreatedAt time.Time `gorm:"index:idx_user_created,priority:2" json:"created_at"`
	UpdatedAt time.Time `gorm:"index" json:"updated_at"`
	Plan      *Plan     `gorm:"foreignKey:PlanID" json:"plan,omitempty"`
}

// ExecutionTarget values selecting where a task runs.
const (
	ExecutionTargetCloud        = ""              // default: cloud Asynq/Docker
	ExecutionTargetLocal        = "local"         // awaiting desktop local-executor claim
	ExecutionTargetLocalClaimed = "local_claimed" // claimed by a desktop, running locally
)

// ExecutorMeta records which local executor claimed a task, for diagnostics.
// Stored on Task.ExecutorInfo as a typed JSON column (datatypes.JSONType) so
// the zero value serializes to a valid JSON null.
type ExecutorMeta struct {
	Hostname  string `json:"hostname,omitempty"`
	Version   string `json:"version,omitempty"`
	ClaimedAt string `json:"claimed_at,omitempty"`
}

// TableName returns the database table name for Task.
func (Task) TableName() string { return "tasks" }

// SetEcommerce stores an e-commerce package config into the Ecommerce JSON
// column. Thin wrapper over datatypes.NewJSONType so call sites (task creation,
// plan-spawned tasks) don't each need to import gorm.io/datatypes.
func (t *Task) SetEcommerce(ec EcommerceConfig) {
	t.Ecommerce = datatypes.NewJSONType(ec)
}

// SetOverrides stores per-task style/persona/theme overrides into the Overrides
// JSON column. Thin wrapper over datatypes.NewJSONType so call sites (task
// creation, retry) don't each need to import gorm.io/datatypes.
func (t *Task) SetOverrides(o StyleOverrides) {
	t.Overrides = datatypes.NewJSONType(o)
}
