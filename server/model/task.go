package model

import "time"

// Task represents a content generation task.
type Task struct {
	ID                  string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID              string     `gorm:"type:char(36);index:idx_user_status,priority:1;index:idx_user_created,priority:1;not null" json:"user_id"`
	ChannelID           string     `gorm:"type:char(36);index" json:"channel_id"`
	PlanID              *uint      `gorm:"index" json:"plan_id"`
	Type                string     `gorm:"type:varchar(20);not null" json:"type"`
	Status              string     `gorm:"type:varchar(20);default:pending;index:idx_user_status,priority:2" json:"status"`
	Prompt              string     `gorm:"column:topic;type:varchar(500)" json:"prompt"`
	Title               string     `gorm:"type:varchar(200)" json:"title,omitempty"`
	ImageRatio          string     `gorm:"type:varchar(10);default:''" json:"image_ratio,omitempty"`
	GenerateVideo       bool       `gorm:"default:false" json:"generate_video,omitempty"`
	ProgressLog         string     `gorm:"type:longtext" json:"progress_log,omitempty"`
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
	Published           bool       `gorm:"default:false" json:"published"`
	PublishedAt         *time.Time `gorm:"index" json:"published_at,omitempty"`
	WorkflowStatus      *string    `gorm:"type:json" json:"workflow_status,omitempty"`
	CreatedAt           time.Time  `gorm:"index:idx_user_created,priority:2" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"index" json:"updated_at"`
	Plan                *Plan      `gorm:"foreignKey:PlanID" json:"plan,omitempty"`
}

// TableName returns the database table name for Task.
func (Task) TableName() string { return "tasks" }
