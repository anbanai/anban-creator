package model

import "time"

const (
	ChannelsTrackingStatusTracking = "tracking"
	ChannelsTrackingStatusStopped  = "stopped"
	ChannelsTrackingStatusFailed   = "failed"
)

const (
	ChannelsStopReasonMaxDurationReached = "max_duration_reached"
	ChannelsStopReasonTooManyFailures    = "too_many_failures"
	ChannelsTrackingMaxDays              = 14
	ChannelsTrackingMaxFailures          = 5
)

type ChannelsVideoTracking struct {
	ID                string     `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID            string     `gorm:"type:char(36);uniqueIndex;not null" json:"task_id"`
	UserID            string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID         string     `gorm:"type:char(36);index;not null" json:"project_id"`
	Status            string     `gorm:"type:varchar(32);index;not null" json:"status"`
	VideoURL          string     `gorm:"type:varchar(1000);not null" json:"video_url"`
	VideoID           string     `gorm:"type:varchar(191);index;not null" json:"video_id"`
	VideoTitle        string     `gorm:"type:text" json:"video_title"`
	AuthorUsername    string     `gorm:"type:varchar(500);index" json:"author_username"`
	AuthorName        string     `gorm:"type:varchar(500)" json:"author_name"`
	CoverURL          string     `gorm:"type:text" json:"cover_url"`
	PublishedAt       *time.Time `gorm:"index" json:"published_at,omitempty"`
	BoundAt           time.Time  `gorm:"index" json:"bound_at"`
	LastRunAt         *time.Time `gorm:"index" json:"last_run_at,omitempty"`
	NextRunAt         *time.Time `gorm:"index" json:"next_run_at,omitempty"`
	CaptureClaimUntil *time.Time `gorm:"index" json:"-"`
	CaptureClaimToken string     `gorm:"type:char(36);index" json:"-"`
	TrackingStoppedAt *time.Time `gorm:"index" json:"tracking_stopped_at,omitempty"`
	RunCount          int        `gorm:"default:0" json:"run_count"`
	FailureCount      int        `gorm:"default:0" json:"failure_count"`
	StopReason        string     `gorm:"type:varchar(64)" json:"stop_reason"`
	LastError         string     `gorm:"type:text" json:"last_error"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (ChannelsVideoTracking) TableName() string { return "channels_video_trackings" }

type ChannelsMetricSnapshot struct {
	ID            string    `gorm:"type:char(36);primaryKey" json:"id"`
	TrackingID    string    `gorm:"type:char(36);uniqueIndex:idx_channels_tracking_date,priority:1;index;not null" json:"tracking_id"`
	TaskID        string    `gorm:"type:char(36);index;not null" json:"task_id"`
	CapturedAt    time.Time `gorm:"index" json:"captured_at"`
	CapturedDate  string    `gorm:"type:char(10);uniqueIndex:idx_channels_tracking_date,priority:2;not null" json:"captured_date"`
	LikeCount     int       `json:"like_count"`
	FavoriteCount int       `json:"favorite_count"`
	CommentCount  int       `json:"comment_count"`
	ForwardCount  int       `json:"forward_count"`
	RawData       string    `gorm:"type:json" json:"raw_data,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (ChannelsMetricSnapshot) TableName() string { return "channels_metric_snapshots" }
