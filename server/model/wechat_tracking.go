package model

import (
	"time"
)

const (
	WechatTrackingStatusWaitingData = "waiting_data"
	WechatTrackingStatusTracking    = "tracking"
	WechatTrackingStatusExpired     = "expired"
	WechatTrackingStatusUnsupported = "unsupported"
	WechatTrackingStatusError       = "error"
)

const WechatTrackingWindow = 30 * 24 * time.Hour

type WechatArticleTracking struct {
	ID            string `gorm:"type:char(36);primaryKey;not null" json:"id"`
	TaskID        string `gorm:"type:char(36);uniqueIndex;not null" json:"task_id"`
	UserID        string `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID     string `gorm:"type:char(36);index;not null" json:"project_id"`
	PublicationID string `gorm:"type:char(36);uniqueIndex;not null" json:"publication_id"`
	Source        string `gorm:"type:varchar(32);index;not null;check:chk_wechat_tracking_source,source IN ('anban_api','wechat_console')" json:"source"`
	Status        string `gorm:"type:varchar(32);index;not null;check:chk_wechat_tracking_status,status IN ('waiting_data','tracking','expired','unsupported','error')" json:"status"`

	ArticleID  string `gorm:"type:varchar(191);index;not null;default:''" json:"article_id,omitempty"`
	MsgDataID  string `gorm:"type:varchar(191);index;not null;default:''" json:"msg_data_id,omitempty"`
	MsgID      string `gorm:"type:varchar(191);index;not null;default:''" json:"msg_id,omitempty"`
	ArticleURL string `gorm:"type:varchar(1000);not null;default:''" json:"article_url,omitempty"`

	PublishedAt        time.Time  `gorm:"index;not null" json:"published_at"`
	ExpiresAt          time.Time  `gorm:"index;not null" json:"expires_at"`
	LastFetchAt        *time.Time `gorm:"index" json:"last_fetch_at,omitempty"`
	NextFetchAt        *time.Time `gorm:"index" json:"next_fetch_at,omitempty"`
	ExpiredAt          *time.Time `gorm:"index" json:"expired_at,omitempty"`
	RecoveryClaimToken string     `gorm:"type:char(36);index;not null;default:''" json:"-"`
	RecoveryClaimedAt  *time.Time `gorm:"index" json:"-"`
	RunCount           int        `gorm:"not null;default:0" json:"run_count"`
	FailureCount       int        `gorm:"not null;default:0" json:"failure_count"`
	LastError          string     `gorm:"type:text;not null" json:"last_error,omitempty"`
	CreatedAt          time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"not null" json:"updated_at"`
}

func (WechatArticleTracking) TableName() string { return "wechat_article_trackings" }

type WechatMetricSnapshot struct {
	ID         string    `gorm:"type:char(36);primaryKey;not null" json:"id"`
	TrackingID string    `gorm:"type:char(36);uniqueIndex:idx_wechat_tracking_stat_date,priority:1;index;not null" json:"tracking_id"`
	TaskID     string    `gorm:"type:char(36);index;not null" json:"task_id"`
	StatDate   string    `gorm:"type:char(10);uniqueIndex:idx_wechat_tracking_stat_date,priority:2;not null" json:"stat_date"`
	CapturedAt time.Time `gorm:"index;not null" json:"captured_at"`

	ReadUsers             int       `gorm:"not null;default:0" json:"read_users"`
	ShareUsers            int       `gorm:"not null;default:0" json:"share_users"`
	CollectionUsers       int       `gorm:"not null;default:0" json:"collection_users"`
	LikeUsers             int       `gorm:"not null;default:0" json:"like_users"`
	ZaikanUsers           int       `gorm:"not null;default:0" json:"zaikan_users"`
	CommentCount          int       `gorm:"not null;default:0" json:"comment_count"`
	ReadFinishRate        float64   `gorm:"type:decimal(10,6);not null;default:0" json:"read_finish_rate"`
	AverageReadActiveTime float64   `gorm:"type:decimal(14,3);not null;default:0" json:"average_read_active_time"`
	ReadToSubscribeUsers  int       `gorm:"not null;default:0" json:"read_to_subscribe_users"`
	RawResponse           []byte    `gorm:"type:longblob;not null" json:"raw_response"`
	CreatedAt             time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt             time.Time `gorm:"not null" json:"updated_at"`
}

func (WechatMetricSnapshot) TableName() string { return "wechat_metric_snapshots" }
