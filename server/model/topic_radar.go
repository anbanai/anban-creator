package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/datatypes"
)

const (
	TaskKindTopicDigest = "topic_digest"
	TaskKindTopicScript = "topic_script"
	TaskTypeTopicDigest = "topic-digest"
	TaskTypeTopicScript = "topic-script"
)

const (
	TopicDigestStatusQueued     = "queued"
	TopicDigestStatusPreparing  = "preparing"
	TopicDigestStatusDispatched = "dispatched"
	TopicDigestStatusRunning    = "running"
	TopicDigestStatusFinalizing = "finalizing"
	TopicDigestStatusSucceeded  = "succeeded"
	TopicDigestStatusFailed     = "failed"
)

const (
	TopicDigestDispatchPending  = "pending"
	TopicDigestDispatchEnqueued = "enqueued"
	TopicDigestDispatchClaimed  = "claimed"
	TopicDigestDispatchFailed   = "failed"
)

const (
	TopicAdoptionPending    = "pending"
	TopicAdoptionProcessing = "processing"
	TopicAdoptionAdopted    = "adopted"
	TopicAdoptionDuplicate  = "duplicate"
	TopicAdoptionFailed     = "failed"
)

const (
	TopicScriptJobQueued    = "queued"
	TopicScriptJobRunning   = "running"
	TopicScriptJobSucceeded = "succeeded"
	TopicScriptJobFailed    = "failed"
)

const (
	TopicNotificationProviderFeishu   = "feishu"
	TopicNotificationProviderWeCom    = "wecom"
	TopicNotificationProviderDingTalk = "dingtalk"
)

const (
	TopicNotificationPending = "pending"
	TopicNotificationSending = "sending"
	TopicNotificationSent    = "sent"
	TopicNotificationFailed  = "failed"
	TopicNotificationInvalid = "invalid"
)

// TopicSourceRef is a user-visible, server-validated source reference.
type TopicSourceRef struct {
	Type      string    `json:"type"`
	Label     string    `json:"label,omitempty"`
	Platform  string    `json:"platform,omitempty"`
	RefID     string    `json:"ref_id,omitempty"`
	Title     string    `json:"title,omitempty"`
	URL       string    `json:"url,omitempty"`
	Rank      int       `json:"rank,omitempty"`
	Hot       float64   `json:"hot,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
	Freshness string    `json:"freshness,omitempty"`
}

func (r TopicSourceRef) Resolvable() bool {
	return strings.TrimSpace(r.URL) != "" || strings.TrimSpace(r.RefID) != ""
}

type TopicScoreWeights map[string]float64

func DefaultTopicRadarWeights() TopicScoreWeights {
	return TopicScoreWeights{
		"traffic_potential": 0.25,
		"account_fit":       0.20,
		"differentiation":   0.15,
		"timeliness":        0.10,
		"monetization":      0.15,
		"production_cost":   0.08,
		"compliance_risk":   0.07,
	}
}

type TopicSubscription struct {
	ID           string         `gorm:"type:char(36);primaryKey" json:"id"`
	UserID       string         `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID    string         `gorm:"type:char(36);index;not null" json:"project_id"`
	Name         string         `gorm:"type:varchar(120);not null" json:"name"`
	Enabled      bool           `gorm:"not null;default:false" json:"enabled"`
	Timezone     string         `gorm:"type:varchar(64);not null;default:'Asia/Shanghai'" json:"timezone"`
	Schedule     datatypes.JSON `gorm:"type:json;not null" json:"schedule"`
	SourceConfig datatypes.JSON `gorm:"type:json;not null" json:"source_config"`
	Weights      datatypes.JSON `gorm:"type:json;not null" json:"weights"`
	ScriptPolicy datatypes.JSON `gorm:"type:json;not null" json:"script_policy"`
	NextRunAt    *time.Time     `gorm:"index" json:"next_run_at,omitempty"`
	LastRunAt    *time.Time     `json:"last_run_at,omitempty"`
	DeletedAt    *time.Time     `gorm:"index" json:"-"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func (TopicSubscription) TableName() string { return "topic_subscriptions" }

type TopicDigest struct {
	EffectsQueuedAt  *time.Time     `gorm:"index" json:"effects_queued_at,omitempty"`
	ID               string         `gorm:"type:char(36);primaryKey" json:"id"`
	SubscriptionID   string         `gorm:"type:char(36);index;not null" json:"subscription_id"`
	UserID           string         `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID        string         `gorm:"type:char(36);index;not null" json:"project_id"`
	OccurrenceKey    string         `gorm:"type:varchar(255);uniqueIndex;not null" json:"occurrence_key"`
	RequestKey       string         `gorm:"type:varchar(255);index;not null" json:"request_key"`
	PlannedAt        time.Time      `gorm:"index;not null" json:"planned_at"`
	Status           string         `gorm:"type:varchar(24);index;not null" json:"status"`
	DispatchStatus   string         `gorm:"type:varchar(24);index;not null" json:"dispatch_status"`
	ActiveKey        *string        `gorm:"type:varchar(36);uniqueIndex" json:"-"`
	InputSnapshot    datatypes.JSON `gorm:"type:json;not null" json:"-"`
	InputHash        string         `gorm:"type:char(64);not null" json:"input_hash"`
	AgentTaskID      *string        `gorm:"type:char(36);index" json:"agent_task_id,omitempty"`
	AgentExecutionID *string        `gorm:"type:char(36);index" json:"agent_execution_id,omitempty"`
	CandidateCount   int            `gorm:"not null;default:0" json:"candidate_count"`
	Error            string         `gorm:"type:text" json:"error,omitempty"`
	StartedAt        *time.Time     `json:"started_at,omitempty"`
	FinishedAt       *time.Time     `json:"finished_at,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

func (TopicDigest) TableName() string { return "topic_digests" }

func NewTopicDigestInput(snapshot any) (datatypes.JSON, string, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(data)
	return datatypes.JSON(data), hex.EncodeToString(hash[:]), nil
}

type TopicSuggestion struct {
	ContentAssetID string         `gorm:"type:char(36);index" json:"asset_id"`
	ID             string         `gorm:"type:char(36);primaryKey" json:"id"`
	DigestID       string         `gorm:"type:char(36);index;not null" json:"digest_id"`
	ProjectID      string         `gorm:"type:char(36);index;not null" json:"project_id"`
	Title          string         `gorm:"type:varchar(500);not null" json:"title"`
	Introduction   string         `gorm:"type:text;not null" json:"introduction"`
	SourceSummary  string         `gorm:"type:text;not null" json:"source_summary"`
	SourceRefs     datatypes.JSON `gorm:"type:json;not null" json:"source_refs"`
	Scores         datatypes.JSON `gorm:"type:json;not null" json:"scores"`
	VideoFit       string         `gorm:"type:text" json:"video_fit"`
	Rank           int            `gorm:"not null" json:"rank"`
	AdoptionStatus string         `gorm:"type:varchar(24);index;not null" json:"adoption_status"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func (TopicSuggestion) TableName() string { return "topic_suggestions" }

type TopicSuggestionAdoption struct {
	ID           string     `gorm:"type:char(36);primaryKey" json:"id"`
	SuggestionID string     `gorm:"type:char(36);uniqueIndex;not null" json:"suggestion_id"`
	DigestID     string     `gorm:"type:char(36);index;not null" json:"digest_id"`
	UserID       string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID    string     `gorm:"type:char(36);index;not null" json:"project_id"`
	Status       string     `gorm:"type:varchar(24);index;not null" json:"status"`
	TopicPoolID  *uint      `gorm:"index" json:"-"`
	Attempt      int        `gorm:"not null;default:0" json:"attempt"`
	LeaseToken   string     `gorm:"type:char(36);index" json:"-"`
	LeaseUntil   *time.Time `gorm:"index" json:"-"`
	Error        string     `gorm:"type:text" json:"error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (TopicSuggestionAdoption) TableName() string { return "topic_suggestion_adoptions" }

type TopicScriptJob struct {
	ContentAssetID  string         `gorm:"type:char(36);index" json:"asset_id"`
	InputSnapshot   datatypes.JSON `gorm:"type:json" json:"-"`
	ID              string         `gorm:"type:char(36);primaryKey" json:"id"`
	SuggestionID    string         `gorm:"type:char(36);index;not null" json:"suggestion_id"`
	DigestID        string         `gorm:"type:char(36);index;not null" json:"digest_id"`
	UserID          string         `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID       string         `gorm:"type:char(36);index;not null" json:"project_id"`
	TargetPlatform  string         `gorm:"type:varchar(48);not null" json:"target_platform"`
	DurationSeconds int            `gorm:"not null" json:"duration_seconds"`
	AspectRatio     string         `gorm:"type:varchar(24);not null" json:"aspect_ratio"`
	Fingerprint     string         `gorm:"type:char(64);uniqueIndex;not null" json:"fingerprint"`
	Status          string         `gorm:"type:varchar(24);index;not null" json:"status"`
	Attempt         int            `gorm:"not null;default:0" json:"attempt"`
	TaskID          *string        `gorm:"type:char(36);index" json:"task_id,omitempty"`
	ExecutionID     *string        `gorm:"type:char(36);index" json:"execution_id,omitempty"`
	ArtifactPath    string         `gorm:"type:varchar(500)" json:"artifact_path,omitempty"`
	ArtifactSHA256  string         `gorm:"type:char(64)" json:"artifact_sha256,omitempty"`
	Error           string         `gorm:"type:text" json:"error,omitempty"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	FinishedAt      *time.Time     `json:"finished_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func (TopicScriptJob) TableName() string { return "topic_script_jobs" }

type TopicNotificationChannel struct {
	ID         string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID     string    `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID  string    `gorm:"type:char(36);index;not null" json:"project_id"`
	Provider   string    `gorm:"type:varchar(24);not null" json:"provider"`
	Name       string    `gorm:"type:varchar(120);not null" json:"name"`
	WebhookURL string    `gorm:"type:text;not null" json:"-"`
	Enabled    bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (TopicNotificationChannel) TableName() string { return "topic_notification_channels" }

type TopicNotificationDelivery struct {
	NextAttemptAt  *time.Time     `gorm:"index" json:"next_attempt_at,omitempty"`
	ID             string         `gorm:"type:char(36);primaryKey" json:"id"`
	DigestID       string         `gorm:"type:char(36);index;not null;uniqueIndex:idx_topic_delivery_identity" json:"digest_id"`
	ChannelID      string         `gorm:"type:char(36);index;not null;uniqueIndex:idx_topic_delivery_identity" json:"channel_id"`
	PayloadVersion string         `gorm:"type:varchar(24);not null;uniqueIndex:idx_topic_delivery_identity" json:"payload_version"`
	Payload        datatypes.JSON `gorm:"type:json;not null" json:"-"`
	Status         string         `gorm:"type:varchar(24);index;not null" json:"status"`
	Attempt        int            `gorm:"not null;default:0" json:"attempt"`
	LeaseToken     string         `gorm:"type:char(36);index" json:"-"`
	LeaseUntil     *time.Time     `gorm:"index" json:"-"`
	LastError      string         `gorm:"type:text" json:"last_error,omitempty"`
	SentAt         *time.Time     `json:"sent_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func (TopicNotificationDelivery) TableName() string { return "topic_notification_deliveries" }

func ScheduledTopicOccurrenceKey(subscriptionID string, planned time.Time) string {
	return "sched:" + strings.TrimSpace(subscriptionID) + ":" + planned.UTC().Format(time.RFC3339)
}

func NormalizeTopicDedupeKey(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(title)), " "))
}

func CanTransitionTopicDigest(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case TopicDigestStatusQueued:
		return to == TopicDigestStatusPreparing
	case TopicDigestStatusPreparing:
		return to == TopicDigestStatusDispatched || to == TopicDigestStatusFailed
	case TopicDigestStatusDispatched:
		return to == TopicDigestStatusRunning || to == TopicDigestStatusFailed
	case TopicDigestStatusRunning:
		return to == TopicDigestStatusFinalizing || to == TopicDigestStatusFailed
	case TopicDigestStatusFinalizing:
		return to == TopicDigestStatusSucceeded || to == TopicDigestStatusFailed
	default:
		return false
	}
}
