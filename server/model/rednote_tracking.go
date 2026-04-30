package model

import "time"

const (
	RednoteTrackingStatusWaitingDiscovery = "waiting_discovery"
	RednoteTrackingStatusTracking         = "tracking"
	RednoteTrackingStatusStopped          = "stopped"
	RednoteTrackingStatusFailed           = "failed"
)

const (
	RednoteStopReasonMaxDurationReached = "max_duration_reached"
	RednoteStopReasonLowGrowth          = "low_growth"
	RednoteStopReasonDiscoveryTimeout   = "discovery_timeout"
	RednoteStopReasonTooManyFailures    = "too_many_failures"
	RednoteStopReasonManualStop         = "manual_stop"
)

const (
	RednoteTrackingMaxDays              = 14
	RednoteDiscoveryMaxAttempts         = 7
	RednoteTrackingMaxFailures          = 5
	RednoteLowGrowthThreshold           = 3
	RednoteLowGrowthConsecutiveCaptures = 3
)

type RednotePostTracking struct {
	ID                        string     `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID                    string     `gorm:"type:char(36);uniqueIndex;not null" json:"task_id"`
	UserID                    string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ChannelID                 string     `gorm:"type:char(36);index;not null" json:"channel_id"`
	Status                    string     `gorm:"type:varchar(32);index;not null" json:"status"`
	ProfileURL                string     `gorm:"type:varchar(500)" json:"profile_url"`
	NoteID                    string     `gorm:"type:varchar(100);index" json:"note_id"`
	NoteURL                   string     `gorm:"type:varchar(500)" json:"note_url"`
	NoteTitle                 string     `gorm:"type:varchar(500)" json:"note_title"`
	NoteCoverURL              string     `gorm:"type:varchar(500)" json:"note_cover_url"`
	PublishedMarkedAt         time.Time  `gorm:"index" json:"published_marked_at"`
	DiscoveredAt              *time.Time `gorm:"index" json:"discovered_at,omitempty"`
	TrackingStartedAt         *time.Time `gorm:"index" json:"tracking_started_at,omitempty"`
	TrackingStoppedAt         *time.Time `gorm:"index" json:"tracking_stopped_at,omitempty"`
	NextRunAt                 *time.Time `gorm:"index" json:"next_run_at,omitempty"`
	LastRunAt                 *time.Time `gorm:"index" json:"last_run_at,omitempty"`
	RunCount                  int        `gorm:"default:0" json:"run_count"`
	ConsecutiveLowGrowthCount int        `gorm:"default:0" json:"consecutive_low_growth_count"`
	FailureCount              int        `gorm:"default:0" json:"failure_count"`
	DiscoveryAttemptCount     int        `gorm:"default:0" json:"discovery_attempt_count"`
	MatchConfidence           float64    `gorm:"default:0" json:"match_confidence"`
	MatchReason               string     `gorm:"type:text" json:"match_reason"`
	StopReason                string     `gorm:"type:varchar(64)" json:"stop_reason"`
	LastError                 string     `gorm:"type:text" json:"last_error"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

func (RednotePostTracking) TableName() string { return "rednote_post_trackings" }

type RednoteMetricSnapshot struct {
	ID           string    `gorm:"type:char(36);primaryKey" json:"id"`
	TrackingID   string    `gorm:"type:char(36);uniqueIndex:idx_rednote_tracking_date,priority:1;index;not null" json:"tracking_id"`
	TaskID       string    `gorm:"type:char(36);index;not null" json:"task_id"`
	CapturedAt   time.Time `gorm:"index" json:"captured_at"`
	CapturedDate string    `gorm:"type:char(10);uniqueIndex:idx_rednote_tracking_date,priority:2;not null" json:"captured_date"`
	LikeCount    int       `json:"like_count"`
	CollectCount int       `json:"collect_count"`
	CommentCount int       `json:"comment_count"`
	ShareCount   int       `json:"share_count"`
	ViewCount    *int      `json:"view_count,omitempty"`
	RawData      string    `gorm:"type:json" json:"raw_data,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (RednoteMetricSnapshot) TableName() string { return "rednote_metric_snapshots" }

func RednoteCapturedDate(t time.Time) string {
	return t.Format("2006-01-02")
}
