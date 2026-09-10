package model

import (
	"time"

	"gorm.io/datatypes"
)

// RuntimeIdentity identifies a provider-owned execution workload and instance.
type RuntimeIdentity struct {
	Scope      string
	Workload   string
	InstanceID string
}

// TaskExecution is one durable cloud execution attempt for a task.
type TaskExecution struct {
	ID                string `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID            string `gorm:"type:char(36);uniqueIndex:idx_task_attempt,priority:1;index;not null" json:"task_id"`
	Attempt           int    `gorm:"uniqueIndex:idx_task_attempt,priority:2;not null" json:"attempt"`
	ParentExecutionID string `gorm:"type:char(36);index" json:"parent_execution_id,omitempty"`
	ResumeSessionID   string `gorm:"type:varchar(128)" json:"resume_session_id,omitempty"`
	AgentPackID       string `gorm:"type:varchar(80);index" json:"agent_pack_id,omitempty"`
	AgentPackVersion  string `gorm:"type:varchar(32)" json:"agent_pack_version,omitempty"`
	AgentPackDigest   string `gorm:"type:char(64);index" json:"agent_pack_digest,omitempty"`
	// AgentPackProgressContract freezes the stage contract for resume safety.
	AgentPackProgressContract datatypes.JSON `gorm:"type:json" json:"-"`
	// AgentPackDeliveryContract freezes the user-facing delivery contract for
	// resume safety and consistent file presentation/downloads.
	AgentPackDeliveryContract datatypes.JSON `gorm:"type:json" json:"-"`
	// AgentPackRequiredArtifactContract freezes the complete set of artifacts
	// that must be present and valid before this execution can succeed.
	AgentPackRequiredArtifactContract datatypes.JSON `gorm:"type:json" json:"-"`
	RuntimeAdapter                    string         `gorm:"type:varchar(40)" json:"runtime_adapter,omitempty"`
	RuntimeProfile                    string         `gorm:"type:varchar(40)" json:"runtime_profile,omitempty"`
	RuntimeImage                      string         `gorm:"type:varchar(512)" json:"runtime_image,omitempty"`

	ExecutionProfile   string            `gorm:"type:varchar(40);not null;index" json:"execution_profile"`
	Provider           string            `gorm:"type:varchar(80);not null;index" json:"provider"`
	ProfileEnvs        map[string]string `gorm:"type:json;serializer:json;not null" json:"profile_envs"`
	ProfileFingerprint string            `gorm:"type:char(64);not null;index" json:"profile_fingerprint"`

	Target              string         `gorm:"type:varchar(20);not null" json:"target"`
	Status              string         `gorm:"type:varchar(20);index;not null" json:"status"`
	DispatchClaimToken  string         `gorm:"type:char(36);index" json:"-"`
	DispatchClaimedAt   *time.Time     `gorm:"index" json:"-"`
	RuntimeScope        string         `gorm:"column:runtime_scope;type:varchar(63)" json:"runtime_scope,omitempty"`
	RuntimeWorkload     string         `gorm:"column:runtime_workload;type:varchar(63);index" json:"runtime_workload,omitempty"`
	RuntimeInstanceID   string         `gorm:"column:runtime_instance_id;type:varchar(64)" json:"runtime_instance_id,omitempty"`
	Started             bool           `gorm:"default:false;not null" json:"started"`
	ManifestStatus      string         `gorm:"type:varchar(20);default:'';check:chk_task_execution_manifest_status,manifest_status IN ('','pending','published','collected','discarded','rejected')" json:"manifest_status,omitempty"`
	ManifestSealed      bool           `gorm:"default:false;not null" json:"-"`
	FinalizationStatus  string         `gorm:"type:varchar(20);default:'';index" json:"finalization_status,omitempty"`
	FinalizationToken   string         `gorm:"type:char(36);default:'';index" json:"-"`
	FinalizationAt      *time.Time     `json:"-"`
	CleanupStatus       string         `gorm:"type:varchar(20);default:'';index" json:"cleanup_status,omitempty"`
	CleanupToken        string         `gorm:"type:char(36);default:'';index" json:"-"`
	CleanupAt           *time.Time     `json:"-"`
	CleanupAttempts     int            `gorm:"default:0" json:"-"`
	CleanupNextAt       *time.Time     `gorm:"index" json:"-"`
	DraftDeliveryStatus string         `gorm:"type:varchar(20);default:'';index" json:"draft_delivery_status,omitempty"`
	DraftDeliveryResult datatypes.JSON `gorm:"type:json" json:"-"`
	Result              datatypes.JSON `gorm:"type:json" json:"-"`
	TerminalReason      string         `gorm:"type:varchar(80);default:''" json:"terminal_reason,omitempty"`
	Diagnostics         datatypes.JSON `gorm:"type:json" json:"diagnostics,omitempty"`
	LastHeartbeatAt     *time.Time     `json:"last_heartbeat_at,omitempty"`
	StartedAt           *time.Time     `json:"started_at,omitempty"`
	CompletedAt         *time.Time     `json:"completed_at,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

// NewTaskExecutionAgentProfile copies a frozen task profile into a durable
// execution attempt without carrying provider credentials.
func NewTaskExecutionAgentProfile(snapshot AgentProfileSnapshot, fingerprint string) TaskExecution {
	return TaskExecution{
		ExecutionProfile:   snapshot.ProfileID,
		Provider:           snapshot.Provider,
		ProfileEnvs:        RedactClaudeProfileEnvs(snapshot.Envs),
		ProfileFingerprint: fingerprint,
	}
}

const (
	TaskExecutionCreated     = "created"
	TaskExecutionDispatching = "dispatching"
	TaskExecutionStarting    = "starting"
	TaskExecutionRunning     = "running"
	TaskExecutionSucceeded   = "succeeded"
	TaskExecutionFailed      = "failed"
	TaskExecutionCancelled   = "cancelled"
	TaskExecutionTimedOut    = "timed_out"
)

const (
	TaskExecutionFinalizationTerminal      = "terminal"
	TaskExecutionFinalizationArtifacts     = "artifacts"
	TaskExecutionFinalizationResult        = "result"
	TaskExecutionFinalizationWorkflow      = "workflow"
	TaskExecutionFinalizationDraftDelivery = "draft_delivery"
	TaskExecutionFinalizationTask          = "task"
	TaskExecutionFinalizationSettlement    = "settlement"
	TaskExecutionFinalizationSlot          = "slot"
	TaskExecutionFinalizationDispatch      = "dispatch"
	TaskExecutionFinalizationNotification  = "notification"
	TaskExecutionFinalizationDone          = "done"
)

const (
	TaskExecutionCleanupPending = "pending"
	TaskExecutionCleanupDone    = "done"
)

const (
	TaskExecutionDraftDeliveryInFlight  = "in_flight"
	TaskExecutionDraftDeliverySucceeded = "succeeded"
	TaskExecutionDraftDeliverySkipped   = "skipped"
	TaskExecutionDraftDeliveryAmbiguous = "ambiguous"
)

const (
	TaskExecutionManifestPending   = "pending"
	TaskExecutionManifestPublished = "published"
	TaskExecutionManifestCollected = "collected"
	TaskExecutionManifestDiscarded = "discarded"
	TaskExecutionManifestRejected  = "rejected"
)

// ExecutionTransition contains optional fields persisted with a status CAS.
type ExecutionTransition struct {
	Started            bool
	RuntimeInstanceID  string
	ManifestStatus     string
	TerminalReason     string
	Diagnostics        datatypes.JSON
	Result             datatypes.JSON
	FinalizationStatus string
	CleanupStatus      string
}
