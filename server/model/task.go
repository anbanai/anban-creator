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
// Visual style is not duplicated here; new tasks read it from ProjectSnapshot.
//
// SelectedModules maps a module key to its quantity:
//   - main_images / detail_page / cover_banner / share_image / sku_images
//
// Task creation deducts the configured ecommerce base service fee. Selected
// modules are delivery-scope hints for the agent and drive later MCP image/vision
// operation usage, which is charged independently in credit_transactions.
type EcommerceConfig struct {
	SelectedModules map[string]int `json:"selected_modules,omitempty"`
	ProductPhotos   []string       `json:"product_photos,omitempty"`
	TargetPlatform  string         `json:"target_platform,omitempty"`
	SellingPoints   string         `json:"selling_points,omitempty"`
	// BrandBrief is the brand positioning/voice, merged from project defaults at
	// task creation unless the task supplies an explicit value. Product-specific
	// SellingPoints stay separate.
	BrandBrief string `json:"brand_brief,omitempty"`
	Language   string `json:"language,omitempty"`
	// ProviderStrategyOverride is deprecated/superseded: provider selection now
	// flows through Task.ImageModelKey (user picks the image preset at task
	// creation). Retained on the column for backward compatibility with existing
	// rows; no longer surfaced to the agent (get_project_profile returns the
	// resolved image_model instead).
	ProviderStrategyOverride string `json:"provider_strategy_override,omitempty"`
}

// StyleOverrides is legacy storage for old tasks created before project_snapshot.
// New task/plan flows do not write it.
type StyleOverrides struct {
	VisualStyle string `json:"visual_style,omitempty"` // 图片视觉 (free text)
	Writer      string `json:"writer,omitempty"`       // 写作者 YAML resource key
	Author      string `json:"author,omitempty"`       // 作者署名 (publish author)
	Theme       string `json:"theme,omitempty"`        // 排版主题 key
}

// ProjectSnapshot freezes the project/account configuration a task should use at
// creation time. Runtime surfaces (MCP/settings/UI) read this when present so
// later project edits do not change an already-created task.
type ProjectSnapshot struct {
	ProjectName       string                   `json:"project_name,omitempty"`
	Platform          string                   `json:"platform,omitempty"`
	Instructions      string                   `json:"instructions,omitempty"`
	Keywords          string                   `json:"keywords,omitempty"`
	VisualStyle       string                   `json:"visual_style,omitempty"`
	ReferenceImageURL string                   `json:"reference_image_url,omitempty"`
	ImageRatio        string                   `json:"image_ratio,omitempty"`
	Writer            string                   `json:"writer,omitempty"`
	Theme             string                   `json:"theme,omitempty"`
	Author            string                   `json:"author,omitempty"`
	EcommerceDefaults EcommerceProjectDefaults `json:"ecommerce_defaults,omitempty"`
}

// Task represents a content generation task.
type Task struct {
	ID                string  `gorm:"type:char(36);primaryKey" json:"id"`
	UserID            string  `gorm:"type:char(36);index:idx_user_status,priority:1;index:idx_user_created,priority:1;not null" json:"user_id"`
	ProjectID         string  `gorm:"type:char(36);index" json:"project_id"`
	PlanID            *string `gorm:"type:char(36);index" json:"plan_id,omitempty"`
	Type              string  `gorm:"type:varchar(20);not null" json:"type"`
	Status            string  `gorm:"type:varchar(20);default:pending;index:idx_user_status,priority:2" json:"status"`
	Prompt            string  `gorm:"column:topic;type:varchar(5120)" json:"prompt"`
	Title             string  `gorm:"type:varchar(200)" json:"title,omitempty"`
	ImageRatio        string  `gorm:"type:varchar(10);default:''" json:"image_ratio,omitempty"`
	ImageModelKey     string  `gorm:"type:varchar(50);default:''" json:"image_model_key,omitempty"`
	ReferenceImageURL string  `gorm:"type:varchar(500)" json:"reference_image_url,omitempty"`
	// Overrides is legacy storage for old task-level style/author/theme overrides.
	// New tasks use ProjectSnapshot as the runtime fact source.
	Overrides          datatypes.JSONType[StyleOverrides]  `gorm:"type:json" json:"overrides"`
	ProjectSnapshot    datatypes.JSONType[ProjectSnapshot] `gorm:"type:json" json:"project_snapshot"`
	SkipReferenceImage bool                                `gorm:"default:false" json:"skip_reference_image,omitempty"`
	Watermark          bool                                `gorm:"default:false" json:"watermark,omitempty"`
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
	Ecommerce               datatypes.JSONType[EcommerceConfig] `gorm:"type:json" json:"ecommerce"`
	VideoConfig             datatypes.JSONType[VideoTaskConfig] `gorm:"type:json" json:"video_config"`
	VideoGenerationID       string                              `gorm:"type:varchar(100);default:''" json:"video_generation_id,omitempty"`
	VideoEstimatedCredits   int                                 `gorm:"default:0" json:"video_estimated_credits,omitempty"`
	VideoCreditsCharged     int                                 `gorm:"default:0" json:"video_credits_charged,omitempty"`
	ProgressLog             string                              `gorm:"type:longtext" json:"progress_log,omitempty"`
	Progress                int                                 `gorm:"default:0" json:"progress,omitempty"`
	LatestProgress          datatypes.JSONType[ProgressPayload] `gorm:"type:json" json:"latest_progress"`
	Result                  *string                             `gorm:"type:json" json:"result,omitempty"`
	InputTokens             *int64                              `json:"input_tokens,omitempty"`
	OutputTokens            *int64                              `json:"output_tokens,omitempty"`
	CacheReadTokens         *int64                              `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens     *int64                              `json:"cache_creation_tokens,omitempty"`
	TotalCostUSD            *float64                            `json:"total_cost_usd,omitempty"`
	BillingStatus           string                              `gorm:"type:varchar(32);default:settled" json:"billing_status,omitempty"`
	BillingShortfallCredits int                                 `gorm:"default:0" json:"billing_shortfall_credits,omitempty"`
	ErrorMessage            string                              `gorm:"type:text" json:"error_message,omitempty"`
	StartedAt               *time.Time                          `gorm:"index" json:"started_at"`
	CompletedAt             *time.Time                          `gorm:"index" json:"completed_at"`
	CleanedUpAt             *time.Time                          `gorm:"index" json:"cleaned_up_at"`
	LastHeartbeatAt         *time.Time                          `gorm:"index" json:"last_heartbeat_at,omitempty"`
	RetryCount              int                                 `gorm:"default:0" json:"retry_count"`
	MaxRetries              int                                 `gorm:"default:3" json:"max_retries"`
	RateLimitRetryCount     int                                 `gorm:"default:0" json:"rate_limit_retry_count"`

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
	Plan      *Plan     `gorm:"foreignKey:PlanID;references:ID" json:"plan,omitempty"`
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

func (t *Task) SetVideoConfig(vc VideoTaskConfig) {
	t.VideoConfig = datatypes.NewJSONType(vc)
}

// SetOverrides stores legacy per-task style/author/theme overrides.
func (t *Task) SetOverrides(o StyleOverrides) {
	t.Overrides = datatypes.NewJSONType(o)
}

// SetProjectSnapshot stores a frozen project/account snapshot.
func (t *Task) SetProjectSnapshot(s ProjectSnapshot) {
	t.ProjectSnapshot = datatypes.NewJSONType(s)
}

// SnapshotProject builds the runtime snapshot for a task from the current
// project. The article writer default is applied here so snapshots are complete.
func SnapshotProject(p *Project) ProjectSnapshot {
	if p == nil {
		return ProjectSnapshot{}
	}
	return ProjectSnapshot{
		ProjectName:       p.Name,
		Platform:          p.Platform,
		Instructions:      p.Instructions,
		Keywords:          p.Keywords,
		VisualStyle:       p.VisualStyle,
		ReferenceImageURL: p.ReferenceImageURL,
		ImageRatio:        p.ImageRatio,
		Writer:            p.Writer,
		Theme:             p.Theme,
		Author:            p.Author,
		EcommerceDefaults: p.EcommerceDefaults.Data(),
	}
}

// ProjectFromSnapshot returns a project-shaped view backed by a task snapshot.
// Credentials and persistence metadata intentionally remain from base.
func ProjectFromSnapshot(base *Project, snap ProjectSnapshot) *Project {
	if base == nil {
		return nil
	}
	if snap.Platform == "" {
		return base
	}
	p := *base
	p.Name = snap.ProjectName
	p.Platform = snap.Platform
	p.Instructions = snap.Instructions
	p.Keywords = snap.Keywords
	p.VisualStyle = snap.VisualStyle
	p.ReferenceImageURL = snap.ReferenceImageURL
	p.ImageRatio = snap.ImageRatio
	p.Writer = snap.Writer
	p.Theme = snap.Theme
	p.Author = snap.Author
	p.SetEcommerceDefaults(snap.EcommerceDefaults)
	return &p
}
