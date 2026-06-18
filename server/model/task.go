package model

import (
	"time"
)

// Task represents a content generation task.
type Task struct {
	ID                string `gorm:"type:char(36);primaryKey" json:"id"`
	UserID            string `gorm:"type:char(36);index:idx_user_status,priority:1;index:idx_user_created,priority:1;not null" json:"user_id"`
	ChannelID         string `gorm:"type:char(36);index" json:"channel_id"`
	PlanID            *uint  `gorm:"index" json:"plan_id"`
	Type              string `gorm:"type:varchar(20);not null" json:"type"`
	Status            string `gorm:"type:varchar(20);default:pending;index:idx_user_status,priority:2" json:"status"`
	Prompt            string `gorm:"column:topic;type:varchar(5120)" json:"prompt"`
	Title             string `gorm:"type:varchar(200)" json:"title,omitempty"`
	ImageRatio        string `gorm:"type:varchar(10);default:''" json:"image_ratio,omitempty"`
	ImageModelKey     string `gorm:"type:varchar(50);default:''" json:"image_model_key,omitempty"`
	ReferenceImageURL string `gorm:"type:varchar(500)" json:"reference_image_url,omitempty"`
	// Style is the effective visual style for this task's image generation,
	// resolved at creation time with precedence task > plan > channel. Propagated
	// to the agent via the user prompt (BuildUserPrompt); the channel-level style
	// in settings.json is cleared by the executor when this is non-empty, so the
	// agent has a single source of truth and no prompt-vs-config ambiguity.
	Style               string     `gorm:"type:varchar(1024);default:''" json:"style,omitempty"`
	// TemplateID records which template was selected when creating this task.
	// Nullable: only set for manually-created tasks where the user picked a template.
	// Not used by the style resolution chain (task > plan > channel) — only for UI
	// attribution in Studio. Old rows migrate to NULL automatically.
	TemplateID          *string    `gorm:"type:char(36);index" json:"template_id,omitempty"`
	SkipReferenceImage  bool       `gorm:"default:false" json:"skip_reference_image,omitempty"`
	Watermark           bool       `gorm:"default:false" json:"watermark,omitempty"`
	ProgressLog         string     `gorm:"type:longtext" json:"progress_log,omitempty"`
	Progress            int        `gorm:"default:0" json:"progress,omitempty"`
	Result              *string    `gorm:"type:json" json:"result,omitempty"`
	InputTokens         *int64     `json:"input_tokens,omitempty"`
	OutputTokens        *int64     `json:"output_tokens,omitempty"`
	CacheReadTokens     *int64     `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens *int64     `json:"cache_creation_tokens,omitempty"`
	TotalCostUSD        *float64   `json:"total_cost_usd,omitempty"`
	ErrorMessage        string     `gorm:"type:text" json:"error_message,omitempty"`
	StartedAt           *time.Time `gorm:"index" json:"started_at"`
	CompletedAt         *time.Time `gorm:"index" json:"completed_at"`
	CleanedUpAt         *time.Time `gorm:"index" json:"cleaned_up_at"`
	LastHeartbeatAt     *time.Time `gorm:"index" json:"last_heartbeat_at,omitempty"`
	RetryCount          int        `gorm:"default:0" json:"retry_count"`
	MaxRetries          int        `gorm:"default:3" json:"max_retries"`
	RateLimitRetryCount int        `gorm:"default:0" json:"rate_limit_retry_count"`

	// Goal mode: when GoalMode is true, Goal is prepended to the user prompt as
	// a /goal slash command so Claude Code's built-in goal loop drives
	// turn-by-turn evaluation inside a single agent session. No server-side
	// retry / evaluation log; the loop is opaque to the server.
	Goal     string `gorm:"type:text" json:"goal,omitempty"`
	GoalMode bool   `gorm:"default:false" json:"goal_mode"`

	Published      bool       `gorm:"default:false" json:"published"`
	PublishedAt    *time.Time `gorm:"index" json:"published_at,omitempty"`
	WorkflowStatus *string    `gorm:"type:json" json:"workflow_status,omitempty"`
	CreatedAt      time.Time  `gorm:"index:idx_user_created,priority:2" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"index" json:"updated_at"`
	Plan           *Plan      `gorm:"foreignKey:PlanID" json:"plan,omitempty"`
}

// TableName returns the database table name for Task.
func (Task) TableName() string { return "tasks" }
