package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

const (
	ContentAssetStatusCandidate  = "candidate"
	ContentAssetStatusResearched = "researched"
	ContentAssetStatusValidated  = "validated"
	ContentAssetStatusReady      = "ready"
	ContentAssetStatusInProgress = "in_progress"
	ContentAssetStatusExhausted  = "exhausted"
	ContentAssetStatusArchived   = "archived"

	ContentAssetLifecycleEvergreen = "evergreen"
	ContentAssetLifecycleSeasonal  = "seasonal"
	ContentAssetLifecycleExpiring  = "expiring"

	ContentAssetEvidenceExternal  = "external"
	ContentAssetEvidencePublished = "internal_published"
	ContentAssetEvidenceImported  = "imported"

	ContentAssetLinkOrigin     = "origin"
	ContentAssetLinkDerivative = "derivative"
	ContentAssetLinkReference  = "reference"

	ContentReuseRunStatusQueued    = "queued"
	ContentReuseRunStatusRunning   = "running"
	ContentReuseRunStatusPaused    = "paused"
	ContentReuseRunStatusCompleted = "completed"
	ContentReuseRunStatusFailed    = "failed"
	ContentReuseRunStatusCancelled = "cancelled"

	ContentReuseStageResearch  = "research"
	ContentReuseStageScoring   = "scoring"
	ContentReuseStageBrief     = "brief"
	ContentReuseStageProduce   = "produce"
	ContentReuseStageMaterials = "materials"
	ContentReuseStageRender    = "render"
	ContentReuseStageReview    = "review"
	ContentReuseStageSchedule  = "schedule"
	ContentReuseStagePublish   = "publish"
	ContentReuseStageObserve   = "observe"

	ContentAutopilotFailurePause = "pause"
	ContentAutopilotFailureSkip  = "skip"
)

// ContentAsset is a user-owned reusable content idea. It is intentionally
// independent of a single project so related platform projects can share it.
type ContentAsset struct {
	ID              string         `gorm:"type:char(36);primaryKey" json:"id"`
	UserID          string         `gorm:"type:char(36);index;not null" json:"user_id"`
	Title           string         `gorm:"type:varchar(500);not null" json:"title"`
	CoreQuestion    string         `gorm:"type:text" json:"core_question,omitempty"`
	Audience        string         `gorm:"type:text" json:"audience,omitempty"`
	SemanticKernel  string         `gorm:"type:text" json:"semantic_kernel,omitempty"`
	Keywords        datatypes.JSON `gorm:"type:json" json:"keywords,omitempty"`
	Lifecycle       string         `gorm:"type:varchar(20);not null;default:'evergreen'" json:"lifecycle"`
	Status          string         `gorm:"type:varchar(20);not null;default:'candidate';index" json:"status"`
	EvidenceQuality string         `gorm:"type:varchar(20)" json:"evidence_quality,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func (ContentAsset) TableName() string { return "content_assets" }

func NewContentAsset(userID, title string) *ContentAsset {
	return &ContentAsset{ID: uuid.NewString(), UserID: userID, Title: title, Lifecycle: ContentAssetLifecycleEvergreen, Status: ContentAssetStatusCandidate}
}

type ContentAssetEvidence struct {
	ID         string         `gorm:"type:char(36);primaryKey" json:"id"`
	AssetID    string         `gorm:"type:char(36);index;not null" json:"asset_id"`
	UserID     string         `gorm:"type:char(36);index;not null" json:"user_id"`
	SourceType string         `gorm:"type:varchar(30);not null" json:"source_type"`
	Platform   string         `gorm:"type:varchar(40)" json:"platform,omitempty"`
	ProjectID  string         `gorm:"type:char(36);index" json:"project_id,omitempty"`
	SourceKind string         `gorm:"type:varchar(40)" json:"source_kind,omitempty"`
	SourceID   string         `gorm:"type:varchar(100)" json:"source_id,omitempty"`
	Title      string         `gorm:"type:varchar(500)" json:"title,omitempty"`
	URL        string         `gorm:"type:text" json:"url,omitempty"`
	Metrics    datatypes.JSON `gorm:"type:json" json:"metrics,omitempty"`
	DataAsOf   *time.Time     `json:"data_as_of,omitempty"`
	DataSource string         `gorm:"type:varchar(100)" json:"data_source,omitempty"`
	Freshness  string         `gorm:"type:varchar(20)" json:"freshness,omitempty"`
	Quality    string         `gorm:"type:varchar(20)" json:"quality,omitempty"`
	Notes      string         `gorm:"type:text" json:"notes,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

func (ContentAssetEvidence) TableName() string { return "content_asset_evidence" }

type ContentAssetLink struct {
	ID          string    `gorm:"type:char(36);primaryKey" json:"id"`
	AssetID     string    `gorm:"type:char(36);index;uniqueIndex:uq_content_asset_link,priority:1;not null" json:"asset_id"`
	UserID      string    `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID   string    `gorm:"type:char(36);index" json:"project_id,omitempty"`
	ContentKind string    `gorm:"type:varchar(40);uniqueIndex:uq_content_asset_link,priority:2;not null" json:"content_kind"`
	ContentID   string    `gorm:"type:varchar(100);uniqueIndex:uq_content_asset_link,priority:3;not null" json:"content_id"`
	Platform    string    `gorm:"type:varchar(40)" json:"platform,omitempty"`
	Relation    string    `gorm:"type:varchar(20);uniqueIndex:uq_content_asset_link,priority:4;not null" json:"relation"`
	Angle       string    `gorm:"type:text" json:"angle,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (ContentAssetLink) TableName() string { return "content_asset_links" }

// ContentAssetScoreSnapshot is an immutable, platform-local recommendation
// record tied to one analytics projection revision.
type ContentAssetScoreSnapshot struct {
	ID                string         `gorm:"type:char(36);primaryKey" json:"id"`
	AssetID           string         `gorm:"type:char(36);index;not null" json:"asset_id"`
	UserID            string         `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID         string         `gorm:"type:char(36);index;not null" json:"project_id"`
	Platform          string         `gorm:"type:varchar(40);index;not null" json:"platform"`
	AnalyticsRevision int64          `gorm:"not null" json:"analytics_revision"`
	Status            string         `gorm:"type:varchar(30);not null;index" json:"status"`
	Score             float64        `json:"score"`
	Confidence        string         `gorm:"type:varchar(20);not null" json:"confidence"`
	DataAsOf          *time.Time     `json:"data_as_of,omitempty"`
	Snapshot          datatypes.JSON `gorm:"type:json;not null" json:"snapshot"`
	CreatedAt         time.Time      `gorm:"index" json:"created_at"`
}

func (ContentAssetScoreSnapshot) TableName() string { return "content_asset_score_snapshots" }

// ContentReuseOperationBudget reserves cap capacity while a provider is running.
// Wallet charges remain the only source of monetary accounting.
type ContentReuseOperationBudget struct {
	RunID        string `gorm:"type:char(36);primaryKey"`
	OperationKey string `gorm:"type:char(64);primaryKey"`
	Fingerprint  string `gorm:"type:char(64);not null"`
	Amount       int64  `gorm:"not null"`
	Status       string `gorm:"type:varchar(20);not null;default:invoking"`
	CreatedAt    time.Time
}

func (ContentReuseOperationBudget) TableName() string { return "content_reuse_operation_budgets" }

type ContentReuseRun struct {
	Version            int64          `gorm:"not null;default:0" json:"version"`
	ID                 string         `gorm:"type:char(36);primaryKey" json:"id"`
	UserID             string         `gorm:"type:char(36);index;not null;uniqueIndex:idx_content_reuse_user_key,priority:1" json:"user_id"`
	AssetID            string         `gorm:"type:char(36);index;not null" json:"asset_id"`
	SourceContentKind  string         `gorm:"type:varchar(40)" json:"source_content_kind,omitempty"`
	SourceContentID    string         `gorm:"type:varchar(100)" json:"source_content_id,omitempty"`
	TargetProjectID    string         `gorm:"type:char(36);index;not null" json:"target_project_id"`
	TargetPlatform     string         `gorm:"type:varchar(40);not null" json:"target_platform"`
	TargetAgentID      string         `gorm:"type:varchar(80)" json:"target_agent_id,omitempty"`
	Stage              string         `gorm:"type:varchar(30);not null" json:"stage"`
	Status             string         `gorm:"type:varchar(20);not null;index" json:"status"`
	IdempotencyKey     string         `gorm:"type:varchar(150);not null;uniqueIndex:idx_content_reuse_user_key,priority:2" json:"idempotency_key"`
	TaskID             string         `gorm:"type:char(36);index" json:"task_id,omitempty"`
	RequestFingerprint string         `gorm:"type:char(64);index;not null;default:''" json:"-"`
	PolicySnapshot     datatypes.JSON `gorm:"type:json" json:"policy_snapshot,omitempty"`
	ScoreSnapshot      datatypes.JSON `gorm:"type:json" json:"score_snapshot,omitempty"`
	BillingQuoteID     string         `gorm:"type:char(36);index" json:"-"`
	BillingFingerprint string         `gorm:"type:char(64)" json:"-"`
	BudgetDate         *time.Time     `gorm:"index" json:"budget_date,omitempty"`
	BudgetReserved     int64          `json:"budget_reserved"`
	CostCredits        int64          `json:"cost_credits"`
	RetryCount         int            `json:"retry_count"`
	ErrorCode          string         `gorm:"type:varchar(80)" json:"error_code,omitempty"`
	ErrorMessage       string         `gorm:"type:text" json:"error_message,omitempty"`
	NextRunAt          *time.Time     `gorm:"index" json:"next_run_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	// StageExecutions and AuditEvents are read-only projections populated by
	// the Server run-detail endpoint. They are never persisted through a run
	// update and therefore cannot be used to mutate workflow state.
	StageExecutions []*ContentReuseStageExecution `gorm:"-" json:"stage_executions,omitempty"`
	AuditEvents     []*ContentReuseAuditEvent     `gorm:"-" json:"audit_events,omitempty"`
}

const (
	ContentStageQueued    = "queued"
	ContentStageRunning   = "running"
	ContentStageCompleted = "completed"
	ContentStageFailed    = "failed"
	ContentStagePaused    = "paused"
)

// ContentReuseStageExecution freezes one independently recoverable unit of a
// reuse run. ArtifactRefs contain exact task/execution/path/hash provenance.
type ContentReuseStageExecution struct {
	ID                  string         `gorm:"type:char(36);primaryKey" json:"id"`
	RunID               string         `gorm:"type:char(36);index;uniqueIndex:uq_content_reuse_stage,priority:1;not null" json:"run_id"`
	Stage               string         `gorm:"type:varchar(30);uniqueIndex:uq_content_reuse_stage,priority:2;not null" json:"stage"`
	Version             int64          `gorm:"not null;default:0" json:"version"`
	Status              string         `gorm:"type:varchar(20);index;not null" json:"status"`
	TaskID              *string        `gorm:"type:char(36);uniqueIndex" json:"task_id,omitempty"`
	InputSnapshot       datatypes.JSON `gorm:"type:json" json:"input_snapshot,omitempty"`
	InputSHA256         string         `gorm:"type:char(64)" json:"input_sha256,omitempty"`
	AcceptedExecutionID string         `gorm:"type:char(36)" json:"accepted_execution_id,omitempty"`
	ArtifactRefs        datatypes.JSON `gorm:"type:json" json:"artifact_refs,omitempty"`
	QuoteID             string         `gorm:"type:char(36)" json:"quote_id,omitempty"`
	ChargeID            string         `gorm:"type:char(36)" json:"charge_id,omitempty"`
	ReservedCredits     int64          `json:"reserved_credits"`
	SettledCredits      int64          `json:"settled_credits"`
	RetryCount          int            `json:"retry_count"`
	ErrorCode           string         `gorm:"type:varchar(80)" json:"error_code,omitempty"`
	ErrorMessage        string         `gorm:"type:text" json:"error_message,omitempty"`
	RecoveryPoint       string         `gorm:"type:varchar(80)" json:"recovery_point,omitempty"`
	NextRunAt           *time.Time     `gorm:"index" json:"next_run_at,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

func (ContentReuseStageExecution) TableName() string { return "content_reuse_stage_executions" }

// ContentReuseAuditEvent is append-only evidence for a state transition.
type ContentReuseAuditEvent struct {
	ID               string         `gorm:"type:char(36);primaryKey" json:"id"`
	UserID           string         `gorm:"type:char(36);index;not null" json:"user_id"`
	RunID            string         `gorm:"type:char(36);index;not null" json:"run_id"`
	StageExecutionID string         `gorm:"type:char(36);index" json:"stage_execution_id,omitempty"`
	EntityKind       string         `gorm:"type:varchar(30);not null" json:"entity_kind"`
	EntityID         string         `gorm:"type:char(36);index;not null" json:"entity_id"`
	FromState        string         `gorm:"type:varchar(30)" json:"from_state,omitempty"`
	ToState          string         `gorm:"type:varchar(30);not null" json:"to_state"`
	PreviousVersion  int64          `json:"previous_version"`
	NewVersion       int64          `json:"new_version"`
	TaskID           string         `gorm:"type:char(36)" json:"task_id,omitempty"`
	ExecutionID      string         `gorm:"type:char(36)" json:"execution_id,omitempty"`
	ResultSHA256     string         `gorm:"type:char(64)" json:"result_sha256,omitempty"`
	PolicyRevision   int            `json:"policy_revision"`
	ReasonCode       string         `gorm:"type:varchar(80)" json:"reason_code,omitempty"`
	Details          datatypes.JSON `gorm:"type:json" json:"details,omitempty"`
	CreatedAt        time.Time      `gorm:"index" json:"created_at"`
}

func (ContentReuseAuditEvent) TableName() string { return "content_reuse_audit_events" }

// ContentAssetResearchJob represents research before a content asset exists.
type ContentAssetResearchJob struct {
	ID                  string         `gorm:"type:char(36);primaryKey" json:"id"`
	UserID              string         `gorm:"type:char(36);index;uniqueIndex:uq_content_research_user_fingerprint,priority:1;not null" json:"user_id"`
	ProjectID           string         `gorm:"type:char(36);index;not null" json:"project_id"`
	CandidateAssetID    string         `gorm:"type:char(36);index" json:"candidate_asset_id,omitempty"`
	SourceRevision      int64          `json:"source_revision"`
	PolicyRevision      int            `json:"policy_revision"`
	Fingerprint         string         `gorm:"type:char(64);uniqueIndex:uq_content_research_user_fingerprint,priority:2;not null" json:"fingerprint"`
	FrozenContext       datatypes.JSON `gorm:"type:json;not null" json:"frozen_context"`
	FrozenContextSHA256 string         `gorm:"type:char(64);not null;default:''" json:"frozen_context_sha256"`
	TaskID              *string        `gorm:"type:char(36);uniqueIndex" json:"task_id,omitempty"`
	AcceptedExecutionID string         `gorm:"type:char(36)" json:"accepted_execution_id,omitempty"`
	ArtifactRefs        datatypes.JSON `gorm:"type:json" json:"artifact_refs,omitempty"`
	Status              string         `gorm:"type:varchar(20);index;not null" json:"status"`
	Version             int64          `gorm:"not null;default:0" json:"version"`
	RetryCount          int            `json:"retry_count"`
	ErrorCode           string         `gorm:"type:varchar(80)" json:"error_code,omitempty"`
	ErrorMessage        string         `gorm:"type:text" json:"error_message,omitempty"`
	NextRunAt           *time.Time     `gorm:"index" json:"next_run_at,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

func (ContentAssetResearchJob) TableName() string { return "content_asset_research_jobs" }

func NewContentReuseStageExecution(runID, stage string) *ContentReuseStageExecution {
	return &ContentReuseStageExecution{ID: uuid.NewString(), RunID: runID, Stage: stage, Status: ContentStageQueued}
}

func NewContentAssetResearchJob(userID, projectID, fingerprint string, frozenContext datatypes.JSON) *ContentAssetResearchJob {
	return &ContentAssetResearchJob{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Fingerprint: fingerprint, FrozenContext: frozenContext, Status: ContentStageQueued}
}

func (ContentReuseRun) TableName() string { return "content_reuse_runs" }

func NewContentReuseRun(userID, assetID, projectID, platform string) *ContentReuseRun {
	id := uuid.NewString()
	return &ContentReuseRun{ID: id, UserID: userID, AssetID: assetID, TargetProjectID: projectID, TargetPlatform: platform, Stage: ContentReuseStageResearch, Status: ContentReuseRunStatusQueued, IdempotencyKey: "reuse:" + id}
}

type ContentAutopilotPolicy struct {
	UserID                          string         `gorm:"type:char(36);primaryKey" json:"user_id"`
	Enabled                         bool           `json:"enabled"`
	ProjectIDs                      datatypes.JSON `gorm:"type:json" json:"project_ids,omitempty"`
	Platforms                       datatypes.JSON `gorm:"type:json" json:"platforms,omitempty"`
	DailyBudgetCredits              int64          `json:"daily_budget_credits"`
	MaxRunCostCredits               int64          `json:"max_run_cost_credits"`
	MinRecommendationScore          int            `json:"min_recommendation_score"`
	PlatformMinRecommendationScores datatypes.JSON `gorm:"type:json" json:"platform_min_recommendation_scores,omitempty"`
	MinVideoConfidence              int            `json:"min_video_confidence"`
	VideoFirst                      bool           `json:"video_first"`
	PublishWindows                  datatypes.JSON `gorm:"type:json" json:"publish_windows,omitempty"`
	DraftEnabled                    bool           `json:"draft_enabled"`
	PublishEnabled                  bool           `json:"publish_enabled"`
	ComplianceVersion               string         `gorm:"type:varchar(50)" json:"compliance_version,omitempty"`
	FailureMode                     string         `gorm:"type:varchar(20);not null;default:'pause'" json:"failure_mode"`
	Revision                        int            `json:"revision"`
	CreatedAt                       time.Time      `json:"created_at"`
	UpdatedAt                       time.Time      `json:"updated_at"`
}

func (ContentAutopilotPolicy) TableName() string { return "content_autopilot_policies" }

func NewContentAutopilotPolicy(userID string) *ContentAutopilotPolicy {
	return &ContentAutopilotPolicy{UserID: userID, FailureMode: ContentAutopilotFailurePause, Revision: 1}
}
