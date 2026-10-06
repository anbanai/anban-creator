package model

import "time"

const (
	FeedbackCadenceDaily   = "daily"
	FeedbackCadenceWeekly  = "weekly"
	FeedbackCadenceMonthly = "monthly"

	FeedbackJobQueued    = "queued"
	FeedbackJobRunning   = "running"
	FeedbackJobSucceeded = "succeeded"
	FeedbackJobFailed    = "failed"
	FeedbackJobSkipped   = "skipped"
	FeedbackJobBlocked   = "blocked"

	FeedbackSkipNoNewRevision       = "no_new_revision"
	FeedbackSkipNoMatureContent     = "no_mature_content"
	FeedbackSkipNoValidObservations = "no_valid_observations"
	FeedbackSkipSampleTooSmall      = "sample_too_small"
	FeedbackSkipAlreadySucceeded    = "already_succeeded"
	FeedbackSkipDuplicateRunning    = "duplicate_running"
	FeedbackSkipProjectPaused       = "project_paused"
	FeedbackSkipAnalyticsRebuilding = "analytics_rebuilding"
	FeedbackSkipRevokedBatch        = "revoked_batch"
	FeedbackSkipIdentityMismatch    = "identity_mismatch"
	FeedbackSkipResourceLimit       = "resource_limit"
	FeedbackSkipManualOnly          = "manual_only"
	FeedbackPromotionNone           = "none"
	FeedbackPromotionCandidate      = "candidate"
	FeedbackPromotionConfirmed      = "confirmed"
	FeedbackPromotionValidated      = "validated"
	FeedbackPromotionPromoted       = "promoted"
	FeedbackPromotionRejected       = "rejected"
	FeedbackMaxAttempts             = 4
)

// FeedbackJob is the durable, idempotent unit of periodic feedback work.
// Skipped rows are retained as an audit trail and never dispatch an Agent.
type FeedbackJob struct {
	ID                string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID            string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID         string     `gorm:"type:char(36);index;not null" json:"project_id"`
	Platform          string     `gorm:"type:varchar(20);index;not null" json:"platform"`
	AccountID         string     `gorm:"type:varchar(128);index;not null;default:''" json:"account_id"`
	Operation         string     `gorm:"type:varchar(40);index;not null" json:"operation"`
	Cadence           string     `gorm:"type:varchar(16);index;not null" json:"cadence"`
	PeriodStart       string     `gorm:"type:char(10);not null" json:"period_start"`
	PeriodEnd         string     `gorm:"type:char(10);not null" json:"period_end"`
	MaturityCutoff    string     `gorm:"type:char(10);not null;default:''" json:"maturity_cutoff,omitempty"`
	AnalyticsRevision int64      `gorm:"not null;default:0" json:"analytics_revision"`
	ContentSetDigest  string     `gorm:"type:char(64);not null;default:''" json:"content_set_digest"`
	StrategyRevision  int64      `gorm:"not null;default:0" json:"strategy_revision"`
	TargetContentID   string     `gorm:"type:varchar(100);index;not null;default:''" json:"target_content_id,omitempty"`
	Trigger           string     `gorm:"column:trigger_source;type:varchar(32);index;not null;default:''" json:"trigger,omitempty"`
	TaskID            string     `gorm:"type:char(36);index;not null;default:''" json:"task_id,omitempty"`
	ExecutionID       string     `gorm:"type:char(36);index;not null;default:''" json:"execution_id,omitempty"`
	Fingerprint       string     `gorm:"type:char(64);uniqueIndex;not null" json:"fingerprint"`
	Status            string     `gorm:"type:varchar(20);index;not null" json:"status"`
	SkipReason        string     `gorm:"type:varchar(64);index;not null;default:''" json:"skip_reason,omitempty"`
	SampleCount       int        `gorm:"not null;default:0" json:"sample_count"`
	Coverage          string     `gorm:"type:varchar(20);not null;default:'unknown'" json:"coverage"`
	Attempts          int        `gorm:"not null;default:0" json:"attempts"`
	LastError         string     `gorm:"type:text;not null" json:"last_error,omitempty"`
	QueuedAt          *time.Time `gorm:"index" json:"queued_at,omitempty"`
	StartedAt         *time.Time `gorm:"index" json:"started_at,omitempty"`
	CompletedAt       *time.Time `gorm:"index" json:"completed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (FeedbackJob) TableName() string { return "feedback_jobs" }

// FeedbackLease is a durable, short-lived execution lease. Account leases
// enforce the one-running-job-per-account rule across scheduler/worker
// instances; expiry makes abandoned leases recoverable after a crash.
type FeedbackLease struct {
	Scope      string    `gorm:"type:varchar(192);primaryKey" json:"scope"`
	JobID      string    `gorm:"type:char(36);index;not null" json:"job_id"`
	AcquiredAt time.Time `gorm:"not null" json:"acquired_at"`
	ExpiresAt  time.Time `gorm:"index;not null" json:"expires_at"`
}

func (FeedbackLease) TableName() string { return "feedback_leases" }

// FeedbackInsight stores explainable deterministic facts or LLM interpretations.
type FeedbackInsight struct {
	ID                   string     `gorm:"type:char(36);primaryKey" json:"id"`
	JobID                string     `gorm:"type:char(36);uniqueIndex:idx_feedback_insight_job_kind;index;not null" json:"job_id"`
	ExecutionID          string     `gorm:"type:char(36);index;not null;default:''" json:"execution_id,omitempty"`
	ProjectID            string     `gorm:"type:char(36);index;not null" json:"project_id"`
	AnalyticsRevision    int64      `gorm:"not null" json:"analytics_revision"`
	TargetContentID      string     `gorm:"type:varchar(100);index;not null;default:''" json:"target_content_id,omitempty"`
	BaselineScope        string     `gorm:"type:varchar(64);not null;default:''" json:"baseline_scope,omitempty"`
	Trigger              string     `gorm:"column:trigger_source;type:varchar(32);index;not null;default:''" json:"trigger,omitempty"`
	Kind                 string     `gorm:"type:varchar(40);uniqueIndex:idx_feedback_insight_job_kind;index;not null" json:"kind"`
	EvidenceJSON         string     `gorm:"type:json;not null" json:"evidence"`
	Summary              string     `gorm:"type:text;not null" json:"summary"`
	Confidence           string     `gorm:"type:varchar(16);not null;default:'unknown'" json:"confidence"`
	Limitations          string     `gorm:"type:text;not null" json:"limitations"`
	PromotionStatus      string     `gorm:"type:varchar(16);index;not null;default:'none'" json:"promotion_status"`
	ConfirmedBy          string     `gorm:"type:char(36);index;not null;default:''" json:"confirmed_by,omitempty"`
	ConfirmedAt          *time.Time `json:"confirmed_at,omitempty"`
	ValidatedAt          *time.Time `json:"validated_at,omitempty"`
	MemoryRevision       int64      `gorm:"not null;default:0" json:"memory_revision,omitempty"`
	AgentPackDigest      string     `gorm:"type:char(64);not null;default:''" json:"agent_pack_digest,omitempty"`
	AnalysisArtifactPath string     `gorm:"type:varchar(500);not null;default:''" json:"-"`
	EvidenceArtifactPath string     `gorm:"type:varchar(500);not null;default:''" json:"-"`
	AnalysisArtifactHash string     `gorm:"type:char(64);not null;default:''" json:"-"`
	EvidenceArtifactHash string     `gorm:"type:char(64);not null;default:''" json:"-"`
	CreatedAt            time.Time  `json:"created_at"`
}

func (FeedbackInsight) TableName() string { return "feedback_insights" }

// StrategySnapshot is advisory input frozen into future executions.
type StrategySnapshot struct {
	ID              string     `gorm:"type:char(36);primaryKey" json:"id"`
	ProjectID       string     `gorm:"type:char(36);index;not null" json:"project_id"`
	Platform        string     `gorm:"type:varchar(20);not null" json:"platform"`
	Revision        int64      `gorm:"not null" json:"revision"`
	SourceRevision  int64      `gorm:"not null" json:"source_revision"`
	Digest          string     `gorm:"type:char(64);not null" json:"digest"`
	Status          string     `gorm:"type:varchar(16);index;not null" json:"status"`
	ApplicableTasks string     `gorm:"type:json;not null" json:"applicable_tasks"`
	Recommendations string     `gorm:"type:json;not null" json:"recommendations"`
	Evidence        string     `gorm:"type:json;not null" json:"evidence"`
	Confidence      string     `gorm:"type:varchar(16);not null" json:"confidence"`
	Limitations     string     `gorm:"type:text;not null" json:"limitations"`
	ExpiresAt       *time.Time `gorm:"index" json:"expires_at,omitempty"`
	SupersedesID    string     `gorm:"type:char(36);index;not null;default:''" json:"supersedes_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	ActivatedAt     *time.Time `json:"activated_at,omitempty"`
	RetiredAt       *time.Time `json:"retired_at,omitempty"`
}

func (StrategySnapshot) TableName() string { return "strategy_snapshots" }

// GenerationContext records which advisory strategy a task execution consumed.
type GenerationContext struct {
	ID                 string    `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID             string    `gorm:"type:char(36);uniqueIndex;not null" json:"task_id"`
	ExecutionID        string    `gorm:"type:char(36);uniqueIndex;not null" json:"execution_id"`
	ProjectID          string    `gorm:"type:char(36);index;not null" json:"project_id"`
	StrategySnapshotID string    `gorm:"type:char(36);index;not null;default:''" json:"strategy_snapshot_id"`
	StrategyRevision   int64     `gorm:"not null;default:0" json:"strategy_revision"`
	StrategyDigest     string    `gorm:"type:char(64);not null;default:''" json:"strategy_digest"`
	StrategyMode       string    `gorm:"type:varchar(16);not null;default:'advisory'" json:"strategy_mode"`
	UnavailableReason  string    `gorm:"type:varchar(64);not null;default:''" json:"unavailable_reason,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

func (GenerationContext) TableName() string { return "generation_contexts" }
