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

// ModelTokenUsage is terminal per-model token evidence retained for provider
// cost reconciliation. It never represents a user-wallet charge.
type ModelTokenUsage struct {
	Provider                 string `json:"provider"`
	Model                    string `json:"model"`
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
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
	ProjectName           string                   `json:"project_name,omitempty"`
	Platform              string                   `json:"platform,omitempty"`
	Instructions          string                   `json:"instructions,omitempty"`
	Keywords              string                   `json:"keywords,omitempty"`
	VisualStyle           string                   `json:"visual_style,omitempty"`
	ReferenceImageAssetID string                   `json:"reference_image_asset_id,omitempty"`
	ImageRatio            string                   `json:"image_ratio,omitempty"`
	Writer                string                   `json:"writer,omitempty"`
	Theme                 string                   `json:"theme,omitempty"`
	Author                string                   `json:"author,omitempty"`
	EcommerceDefaults     EcommerceProjectDefaults `json:"ecommerce_defaults,omitempty"`
	MontageDefaults       MontageDefaults          `json:"montage_defaults,omitempty"`
}

// Task represents a content generation task.
type Task struct {
	ID                    string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID                string     `gorm:"type:char(36);index:idx_user_status,priority:1;index:idx_user_created,priority:1;not null" json:"user_id"`
	ProjectID             string     `gorm:"type:char(36);index" json:"project_id"`
	PlanID                *string    `gorm:"type:char(36);index" json:"plan_id,omitempty"`
	Type                  string     `gorm:"type:varchar(20);not null" json:"type"`
	Status                string     `gorm:"type:varchar(20);default:pending;index:idx_user_status,priority:2" json:"status"`
	Prompt                string     `gorm:"column:topic;type:varchar(5120)" json:"prompt"`
	Title                 string     `gorm:"type:varchar(200)" json:"title,omitempty"`
	ImageRatio            string     `gorm:"type:varchar(10);default:''" json:"image_ratio,omitempty"`
	ImageModelKey         string     `gorm:"type:varchar(50);default:''" json:"image_model_key,omitempty"`
	ReferenceImageAssetID string     `gorm:"type:char(36);index" json:"-"`
	ReferenceImage        *AssetView `gorm:"-" json:"reference_image,omitempty"`
	// InputSourceTaskID records the root task whose immutable OSS inputs a clone
	// may reuse. It is internal ownership provenance, not a client-controlled field.
	InputSourceTaskID string `gorm:"type:char(36);index" json:"-"`
	// InputSourceProjectID records the project that owns InputSourceTaskID's
	// immutable OSS inputs. It is internal ownership provenance.
	InputSourceProjectID string `gorm:"type:char(36);index" json:"-"`
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
	Ecommerce             datatypes.JSONType[EcommerceConfig]   `gorm:"type:json" json:"ecommerce"`
	InputAttachments      datatypes.JSONType[[]EntryAttachment] `gorm:"type:json" json:"input_attachments"`
	MontageInput          datatypes.JSONType[MontageInput]      `gorm:"type:json" json:"montage_input"`
	ProgressLog           string                                `gorm:"type:longtext" json:"progress_log,omitempty"`
	Progress              int                                   `gorm:"default:0" json:"progress,omitempty"`
	LatestProgress        datatypes.JSONType[ProgressPayload]   `gorm:"type:json" json:"latest_progress"`
	Result                *string                               `gorm:"type:json" json:"result,omitempty"`
	TerminalModelUsage    datatypes.JSONType[[]ModelTokenUsage] `gorm:"type:json" json:"-"`
	CostStatus            string                                `gorm:"type:varchar(20);default:'';index" json:"-"`
	BillingQuoteID        string                                `gorm:"type:char(36);index" json:"billing_quote_id,omitempty"`
	BillingCatalogID      string                                `gorm:"type:varchar(128);index" json:"billing_catalog_id,omitempty"`
	BillingSKUID          string                                `gorm:"type:varchar(128);index" json:"billing_sku_id,omitempty"`
	BillingPricingTier    string                                `gorm:"type:varchar(20);index" json:"billing_pricing_tier,omitempty"`
	BillingChargeID       *string                               `gorm:"type:char(36);uniqueIndex" json:"billing_charge_id,omitempty"`
	BillingPriceCredits   int64                                 `gorm:"not null;default:0" json:"billing_price_credits"`
	ExecutionProfile      string                                `gorm:"type:varchar(40);not null;index:idx_tasks_execution_profile" json:"execution_profile"`
	AgentProfileSnapshot  AgentProfileSnapshot                  `gorm:"type:json;serializer:json;not null" json:"agent_profile_snapshot"`
	BillingTerminalReason string                                `gorm:"type:varchar(64);index" json:"billing_terminal_reason,omitempty"`
	InputTokens           *int64                                `json:"-"`
	OutputTokens          *int64                                `json:"-"`
	CacheReadTokens       *int64                                `json:"-"`
	CacheCreationTokens   *int64                                `json:"-"`
	ErrorMessage          string                                `gorm:"type:text" json:"error_message,omitempty"`
	StartedAt             *time.Time                            `gorm:"index" json:"started_at"`
	CompletedAt           *time.Time                            `gorm:"index" json:"completed_at"`
	LastHeartbeatAt       *time.Time                            `gorm:"index" json:"last_heartbeat_at,omitempty"`
	RetryCount            int                                   `gorm:"default:0" json:"retry_count"`
	MaxRetries            int                                   `gorm:"default:3" json:"max_retries"`
	RateLimitRetryCount   int                                   `gorm:"default:0" json:"rate_limit_retry_count"`

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
	// resume path (ApprovePublish) can publish without re-extracting from an
	// executor filesystem. Opaque datatypes.JSON rather than
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
	CurrentExecutionID *string                          `gorm:"type:char(36);index" json:"current_execution_id,omitempty"`
	LocalClaimDeadline *time.Time                       `gorm:"index" json:"local_claim_deadline,omitempty"`
	ExecutorInfo       datatypes.JSONType[ExecutorMeta] `gorm:"type:json" json:"executor_info"`
	DeletingAt         *time.Time                       `gorm:"index" json:"-"`

	CreatedAt time.Time `gorm:"index:idx_user_created,priority:2" json:"created_at"`
	UpdatedAt time.Time `gorm:"index" json:"updated_at"`
	Plan      *Plan     `gorm:"foreignKey:PlanID;references:ID" json:"plan,omitempty"`
}

const (
	TaskBillingTerminalCompleted               = "completed"
	TaskBillingTerminalUserCancelled           = "user_cancelled"
	TaskBillingTerminalPlatformError           = "platform_error"
	TaskBillingTerminalProviderError           = "provider_error"
	TaskBillingTerminalExecutionTimeout        = "execution_timeout"
	TaskBillingTerminalInfrastructureCancelled = "infrastructure_cancelled"
)

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

func (t *Task) SetInputAttachments(attachments []EntryAttachment) {
	t.InputAttachments = datatypes.NewJSONType(attachments)
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
		ProjectName:           p.Name,
		Platform:              p.Platform,
		Instructions:          p.Instructions,
		Keywords:              p.Keywords,
		VisualStyle:           p.VisualStyle,
		ReferenceImageAssetID: p.ReferenceImageAssetID,
		ImageRatio:            p.ImageRatio,
		Writer:                p.Writer,
		Theme:                 p.Theme,
		Author:                p.Author,
		EcommerceDefaults:     p.EcommerceDefaults.Data(),
		MontageDefaults:       p.MontageDefaults.Data(),
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
	p.ReferenceImageAssetID = snap.ReferenceImageAssetID
	p.ImageRatio = snap.ImageRatio
	p.Writer = snap.Writer
	p.Theme = snap.Theme
	p.Author = snap.Author
	p.SetEcommerceDefaults(snap.EcommerceDefaults)
	p.SetMontageDefaults(snap.MontageDefaults)
	return &p
}
