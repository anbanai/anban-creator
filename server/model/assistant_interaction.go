package model

import (
	"encoding/json"
	"time"
)

const (
	AssistantInteractionActive            = "active"
	AssistantInteractionReconnecting      = "reconnecting"
	AssistantInteractionNeedsConfirmation = "needs_confirmation"
	AssistantInteractionCompleted         = "completed"
	AssistantInteractionAbandoned         = "abandoned"
	AssistantInteractionRuntimeStarting   = "starting"
	AssistantInteractionRuntimeRunning    = "running"
	AssistantInteractionRuntimeClosed     = "closed"
	AssistantInteractionRuntimeFailed     = "failed"
	AssistantInteractionDraftPending      = "pending"
	AssistantInteractionDraftConfirmed    = "confirmed"
	AssistantInteractionDraftConsumed     = "consumed"
	AssistantInteractionDraftSuperseded   = "superseded"
)

// AssistantInteraction is the durable, user-visible conversation boundary.
// Runtime/session identifiers are execution details and are intentionally not
// exposed through this model's JSON contract.
type AssistantInteraction struct {
	ID              string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID          string     `gorm:"type:char(36);index;not null" json:"-"`
	ModeID          string     `gorm:"type:varchar(20);not null" json:"mode_id"`
	ProjectID       string     `gorm:"type:char(36);index;not null;default:''" json:"project_id,omitempty"`
	ContentID       string     `gorm:"type:char(36);index;not null;default:''" json:"content_id,omitempty"`
	Lifecycle       string     `gorm:"type:varchar(32);index;not null" json:"lifecycle"`
	Generation      int64      `gorm:"not null;default:1" json:"generation"`
	CommandSequence uint64     `gorm:"not null;default:0" json:"-"`
	ExpiresAt       time.Time  `gorm:"index;not null" json:"expires_at"`
	LastActivityAt  time.Time  `gorm:"index;not null" json:"last_activity_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ClosedAt        *time.Time `json:"closed_at,omitempty"`
}

func (AssistantInteraction) TableName() string { return "assistant_interactions" }

type AssistantInteractionRuntime struct {
	ID             string     `gorm:"type:char(36);primaryKey" json:"-"`
	InteractionID  string     `gorm:"type:char(36);index:idx_assistant_interaction_runtime,priority:1;not null" json:"interaction_id"`
	Generation     int64      `gorm:"index:idx_assistant_interaction_runtime,priority:2;not null" json:"generation"`
	ServerInstance string     `gorm:"type:varchar(128);not null" json:"-"`
	RuntimeID      string     `gorm:"type:varchar(128);not null" json:"-"`
	LogicalSession string     `gorm:"type:char(36);not null" json:"-"`
	TokenHash      string     `gorm:"type:char(64);not null" json:"-"`
	Status         string     `gorm:"type:varchar(24);index;not null" json:"status"`
	StartedAt      time.Time  `json:"started_at"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (AssistantInteractionRuntime) TableName() string { return "assistant_interaction_runtimes" }

type AssistantInteractionDraft struct {
	ID                string          `gorm:"type:char(36);primaryKey" json:"id"`
	InteractionID     string          `gorm:"type:char(36);index:idx_assistant_interaction_draft,priority:1;not null" json:"interaction_id"`
	ModeID            string          `gorm:"type:varchar(20);index:idx_assistant_interaction_draft,priority:2;not null" json:"mode_id"`
	Revision          int64           `gorm:"index:idx_assistant_interaction_draft,priority:3;not null" json:"revision"`
	Payload           json.RawMessage `gorm:"type:json;not null" json:"payload"`
	PayloadDigest     string          `gorm:"type:char(64);not null" json:"payload_digest"`
	Ready             bool            `gorm:"not null;default:false" json:"ready"`
	ConfirmationState string          `gorm:"type:varchar(24);index;not null" json:"confirmation_state"`
	SourceGeneration  int64           `gorm:"not null" json:"source_generation"`
	ExpiresAt         time.Time       `gorm:"index;not null" json:"expires_at"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

func (AssistantInteractionDraft) TableName() string { return "assistant_interaction_drafts" }

type AssistantInteractionEvent struct {
	ID            string          `gorm:"type:char(36);primaryKey" json:"id"`
	InteractionID string          `gorm:"type:char(36);index:idx_assistant_interaction_event,priority:1;not null" json:"interaction_id"`
	Sequence      uint64          `gorm:"index:idx_assistant_interaction_event,priority:2;not null" json:"sequence"`
	Type          string          `gorm:"type:varchar(64);not null" json:"type"`
	Data          json.RawMessage `gorm:"type:json;not null" json:"data"`
	CreatedAt     time.Time       `gorm:"index;not null" json:"created_at"`
}

func (AssistantInteractionEvent) TableName() string { return "assistant_interaction_events" }

type AssistantInteractionAttempt struct {
	ID            string          `gorm:"type:char(36);primaryKey" json:"id"`
	InteractionID string          `gorm:"type:char(36);index:idx_assistant_interaction_attempt,priority:1;not null" json:"interaction_id"`
	AttemptID     string          `gorm:"type:varchar(128);index:idx_assistant_interaction_attempt,priority:2;not null" json:"attempt_id"`
	Operation     string          `gorm:"type:varchar(24);not null" json:"operation"`
	Generation    int64           `gorm:"not null" json:"generation"`
	PayloadDigest string          `gorm:"type:char(64);not null" json:"payload_digest"`
	Result        json.RawMessage `gorm:"type:json" json:"result,omitempty"`
	ErrorCode     string          `gorm:"type:varchar(96);not null;default:''" json:"error_code,omitempty"`
	CreatedAt     time.Time       `gorm:"index;not null" json:"created_at"`
	ExpiresAt     time.Time       `gorm:"index;not null" json:"expires_at"`
}

func (AssistantInteractionAttempt) TableName() string { return "assistant_interaction_attempts" }
