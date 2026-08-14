package model

import "time"

const (
	WechatTrackingStatusWaitingData = "waiting_data"
	WechatTrackingStatusTracking    = "tracking"
	WechatTrackingStatusStopped     = "stopped"
	WechatTrackingStatusFailed      = "failed"
)

const (
	WechatStopReasonDataWindowEnded = "data_window_ended"
	WechatStopReasonTooManyFailures = "too_many_failures"
	WechatTrackingMaxDays           = 3
	WechatTrackingMaxFailures       = 3
)

type WechatArticleTracking struct {
	ID                string     `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID            string     `gorm:"type:char(36);uniqueIndex;not null" json:"task_id"`
	UserID            string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID         string     `gorm:"type:char(36);index;not null" json:"project_id"`
	Status            string     `gorm:"type:varchar(32);index;not null" json:"status"`
	ArticleID         string     `gorm:"type:varchar(191);index" json:"article_id"`
	MsgID             string     `gorm:"type:varchar(191);index" json:"msg_id"`
	ArticleURL        string     `gorm:"type:varchar(1000);not null" json:"article_url"`
	ArticleTitle      string     `gorm:"type:varchar(500)" json:"article_title"`
	PublishedDate     string     `gorm:"type:char(10);index;not null" json:"published_date"`
	BoundAt           time.Time  `gorm:"index" json:"bound_at"`
	LastRunAt         *time.Time `gorm:"index" json:"last_run_at,omitempty"`
	NextRunAt         *time.Time `gorm:"index" json:"next_run_at,omitempty"`
	TrackingStoppedAt *time.Time `gorm:"index" json:"tracking_stopped_at,omitempty"`
	RunCount          int        `gorm:"default:0" json:"run_count"`
	FailureCount      int        `gorm:"default:0" json:"failure_count"`
	StopReason        string     `gorm:"type:varchar(64)" json:"stop_reason"`
	LastError         string     `gorm:"type:text" json:"last_error"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (WechatArticleTracking) TableName() string { return "wechat_article_trackings" }

type WechatMetricSnapshot struct {
	ID               string    `gorm:"type:char(36);primaryKey" json:"id"`
	TrackingID       string    `gorm:"type:char(36);uniqueIndex:idx_wechat_tracking_date,priority:1;index;not null" json:"tracking_id"`
	TaskID           string    `gorm:"type:char(36);index;not null" json:"task_id"`
	CapturedAt       time.Time `gorm:"index" json:"captured_at"`
	CapturedDate     string    `gorm:"type:char(10);uniqueIndex:idx_wechat_tracking_date,priority:2;not null" json:"captured_date"`
	StatDate         string    `gorm:"type:char(10);index" json:"stat_date"`
	TargetUser       int       `json:"target_user"`
	IntPageReadUser  int       `json:"int_page_read_user"`
	IntPageReadCount int       `json:"int_page_read_count"`
	OriPageReadUser  int       `json:"ori_page_read_user"`
	OriPageReadCount int       `json:"ori_page_read_count"`
	ShareUser        int       `json:"share_user"`
	ShareCount       int       `json:"share_count"`
	AddToFavUser     int       `json:"add_to_fav_user"`
	AddToFavCount    int       `json:"add_to_fav_count"`
	RawData          string    `gorm:"type:json" json:"raw_data,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

func (WechatMetricSnapshot) TableName() string { return "wechat_metric_snapshots" }
