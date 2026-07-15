package model

import (
	"time"

	"gorm.io/datatypes"
)

// TaskExecution is one durable cloud execution attempt for a task.
type TaskExecution struct {
	ID                 string         `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID             string         `gorm:"type:char(36);uniqueIndex:idx_task_attempt,priority:1;index;not null" json:"task_id"`
	Attempt            int            `gorm:"uniqueIndex:idx_task_attempt,priority:2;not null" json:"attempt"`
	ParentExecutionID  string         `gorm:"type:char(36);index" json:"parent_execution_id,omitempty"`
	ResumeSessionID    string         `gorm:"type:varchar(128)" json:"resume_session_id,omitempty"`
	Target             string         `gorm:"type:varchar(20);not null" json:"target"`
	Status             string         `gorm:"type:varchar(20);index;not null" json:"status"`
	DispatchClaimToken string         `gorm:"type:char(36);index" json:"-"`
	DispatchClaimedAt  *time.Time     `gorm:"index" json:"-"`
	Namespace          string         `gorm:"type:varchar(63)" json:"namespace,omitempty"`
	JobName            string         `gorm:"type:varchar(63);index" json:"job_name,omitempty"`
	PodUID             string         `gorm:"type:varchar(64)" json:"pod_uid,omitempty"`
	Started            bool           `gorm:"default:false;not null" json:"started"`
	ManifestStatus     string         `gorm:"type:varchar(20);default:'';check:chk_task_execution_manifest_status,manifest_status IN ('','pending','published','collected','discarded','rejected')" json:"manifest_status,omitempty"`
	FinalizationStatus string         `gorm:"type:varchar(20);default:'';index" json:"finalization_status,omitempty"`
	FinalizationToken  string         `gorm:"type:char(36);default:'';index" json:"-"`
	FinalizationAt     *time.Time     `json:"-"`
	CleanupStatus      string         `gorm:"type:varchar(20);default:'';index" json:"cleanup_status,omitempty"`
	CleanupToken       string         `gorm:"type:char(36);default:'';index" json:"-"`
	CleanupAt          *time.Time     `json:"-"`
	CleanupAttempts    int            `gorm:"default:0" json:"-"`
	CleanupNextAt      *time.Time     `gorm:"index" json:"-"`
	PublishingStatus   string         `gorm:"type:varchar(20);default:'';index" json:"publishing_status,omitempty"`
	PublishingResult   datatypes.JSON `gorm:"type:json" json:"-"`
	Result             datatypes.JSON `gorm:"type:json" json:"-"`
	TerminalReason     string         `gorm:"type:varchar(80);default:''" json:"terminal_reason,omitempty"`
	Diagnostics        datatypes.JSON `gorm:"type:json" json:"diagnostics,omitempty"`
	LastHeartbeatAt    *time.Time     `json:"last_heartbeat_at,omitempty"`
	StartedAt          *time.Time     `json:"started_at,omitempty"`
	CompletedAt        *time.Time     `json:"completed_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
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
	TaskExecutionFinalizationTerminal     = "terminal"
	TaskExecutionFinalizationArtifacts    = "artifacts"
	TaskExecutionFinalizationResult       = "result"
	TaskExecutionFinalizationWorkflow     = "workflow"
	TaskExecutionFinalizationPublishing   = "publishing"
	TaskExecutionFinalizationTask         = "task"
	TaskExecutionFinalizationSettlement   = "settlement"
	TaskExecutionFinalizationSlot         = "slot"
	TaskExecutionFinalizationDispatch     = "dispatch"
	TaskExecutionFinalizationNotification = "notification"
	TaskExecutionFinalizationDone         = "done"
)

const (
	TaskExecutionCleanupPending = "pending"
	TaskExecutionCleanupDone    = "done"
)

const (
	TaskExecutionPublishingInFlight  = "in_flight"
	TaskExecutionPublishingSucceeded = "succeeded"
	TaskExecutionPublishingSkipped   = "skipped"
	TaskExecutionPublishingAmbiguous = "ambiguous"
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
	PodUID             string
	ManifestStatus     string
	TerminalReason     string
	Diagnostics        datatypes.JSON
	Result             datatypes.JSON
	FinalizationStatus string
	CleanupStatus      string
}
