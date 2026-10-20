package model

import "time"

const (
	AssistantModeProfile  = "profile"
	AssistantModeCreation = "creation"
	AssistantModeReview   = "review"

	AssistantCheckpointDraft     = "draft"
	AssistantCheckpointReady     = "ready"
	AssistantCheckpointCommitted = "committed"
	AssistantCheckpointAbandoned = "abandoned"
)

// AssistantCheckpoint is a Server-owned recovery point for an interactive
// Assistant session. It stores structured work only, never a chat transcript.
type AssistantCheckpoint struct {
	ID                    string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID                string    `gorm:"type:char(36);index:idx_assistant_checkpoint_scope,priority:1;not null" json:"user_id"`
	ModeID                string    `gorm:"type:varchar(20);index:idx_assistant_checkpoint_scope,priority:2;not null" json:"mode_id"`
	ProjectID             string    `gorm:"type:char(36);index:idx_assistant_checkpoint_scope,priority:3;not null;default:''" json:"project_id,omitempty"`
	ContentID             string    `gorm:"type:char(36);index:idx_assistant_checkpoint_scope,priority:4;not null;default:''" json:"content_id,omitempty"`
	SchemaVersion         string    `gorm:"type:varchar(32);not null" json:"schema_version"`
	PayloadDigest         string    `gorm:"-" json:"payload_digest"`
	Payload               []byte    `gorm:"type:json;not null" json:"payload"`
	Summary               string    `gorm:"type:varchar(2000);not null;default:''" json:"summary"`
	Status                string    `gorm:"type:varchar(20);index;not null" json:"status"`
	BaseProfileRevision   int64     `gorm:"not null;default:0" json:"base_profile_revision,omitempty"`
	BaseContentRevision   int64     `gorm:"not null;default:0" json:"base_content_revision,omitempty"`
	BaseAnalyticsRevision int64     `gorm:"not null;default:0" json:"base_analytics_revision,omitempty"`
	SourceSessionID       string    `gorm:"type:char(36);uniqueIndex;not null" json:"source_session_id"`
	CommitIdempotencyKey  *string   `gorm:"type:varchar(200);uniqueIndex:idx_assistant_checkpoint_user_commit,priority:2" json:"-"`
	CommitUserID          string    `gorm:"type:char(36);not null;default:'';uniqueIndex:idx_assistant_checkpoint_user_commit,priority:1" json:"-"`
	CommitResultJSON      []byte    `gorm:"type:json" json:"-"`
	ExpiresAt             time.Time `gorm:"index;not null" json:"expires_at"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func (AssistantCheckpoint) TableName() string { return "assistant_checkpoints" }

// AssistantSession stores the durable, non-secret identity and lifecycle of
// an interactive Assistant session. Runtime state is recovered from a
// structured checkpoint; transcripts and credentials are never persisted.
type AssistantSession struct {
	ID                    string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID                string     `gorm:"type:char(36);index:idx_assistant_session_scope,priority:1;not null" json:"user_id"`
	ModeID                string     `gorm:"type:varchar(20);index:idx_assistant_session_scope,priority:2;not null" json:"mode_id"`
	ProjectID             string     `gorm:"type:char(36);index:idx_assistant_session_scope,priority:3;not null;default:''" json:"project_id,omitempty"`
	ContentID             string     `gorm:"type:char(36);index:idx_assistant_session_scope,priority:4;not null;default:''" json:"content_id,omitempty"`
	Status                string     `gorm:"type:varchar(24);index;not null" json:"status"`
	CheckpointID          string     `gorm:"type:char(36);not null;default:''" json:"checkpoint_id,omitempty"`
	BaseProfileRevision   int64      `gorm:"not null;default:0" json:"base_profile_revision"`
	BaseContentRevision   int64      `gorm:"not null;default:0" json:"base_content_revision"`
	BaseAnalyticsRevision int64      `gorm:"not null;default:0" json:"base_analytics_revision"`
	ExpiresAt             time.Time  `gorm:"index;not null" json:"expires_at"`
	ClosedAt              *time.Time `json:"closed_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (AssistantSession) TableName() string { return "assistant_sessions" }

// AssistantSessionLease serializes one live runtime per user/mode/project/
// work scope across Server instances. The lease contains no runtime token.
type AssistantSessionLease struct {
	Scope      string    `gorm:"type:varchar(192);primaryKey" json:"scope"`
	SessionID  string    `gorm:"type:char(36);uniqueIndex;not null" json:"session_id"`
	UserID     string    `gorm:"type:char(36);index;not null" json:"user_id"`
	ModeID     string    `gorm:"type:varchar(20);not null" json:"mode_id"`
	ProjectID  string    `gorm:"type:char(36);not null;default:''" json:"project_id"`
	ContentID  string    `gorm:"type:char(36);not null;default:''" json:"content_id"`
	AcquiredAt time.Time `gorm:"not null" json:"acquired_at"`
	ExpiresAt  time.Time `gorm:"index;not null" json:"expires_at"`
}

func (AssistantSessionLease) TableName() string { return "assistant_session_leases" }
