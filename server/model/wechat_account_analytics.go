package model

import (
	"fmt"
	"strings"
	"time"
)

// WechatAccountAnalyticsBatch is the immutable provenance envelope for a
// WeChat account-level analytics import. The account facts are deliberately
// separate from the generic per-content observation tables.
type WechatAccountAnalyticsBatch struct {
	ID             string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID         string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID      string     `gorm:"type:char(36);index;uniqueIndex:uq_wechat_account_analytics_batches_idempotency,priority:1;not null" json:"project_id"`
	ImportBatchID  string     `gorm:"type:char(36);index" json:"import_batch_id,omitempty"`
	AssetID        string     `gorm:"type:char(36);index" json:"asset_id,omitempty"`
	FileName       string     `gorm:"type:varchar(255);not null;default:''" json:"file_name,omitempty"`
	SHA256         string     `gorm:"type:char(64);index" json:"sha256,omitempty"`
	Source         string     `gorm:"type:varchar(255);not null;default:''" json:"source,omitempty"`
	Timezone       string     `gorm:"type:varchar(64);not null;default:'Asia/Shanghai'" json:"timezone"`
	ParserVersion  string     `gorm:"type:varchar(32);not null;default:''" json:"parser_version,omitempty"`
	Status         string     `gorm:"type:varchar(32);index;not null" json:"status"`
	IdempotencyKey string     `gorm:"type:varchar(128);index;uniqueIndex:uq_wechat_account_analytics_batches_idempotency,priority:2" json:"-"`
	RequestHash    string     `gorm:"type:char(64);index" json:"-"`
	RawProvenance  string     `gorm:"type:json" json:"raw_provenance,omitempty"`
	ReceivedAt     time.Time  `gorm:"index;not null" json:"received_at"`
	EffectiveAt    time.Time  `gorm:"index;not null" json:"effective_at"`
	DataAsOfAt     time.Time  `gorm:"index" json:"data_as_of_at,omitempty"`
	RevokedAt      *time.Time `gorm:"index" json:"revoked_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

const (
	WechatAccountAnalyticsBatchProcessing = "processing"
	WechatAccountAnalyticsBatchActive     = "active"
	WechatAccountAnalyticsBatchCompleted  = "completed"
	WechatAccountAnalyticsBatchFailed     = "failed"
	WechatAccountAnalyticsBatchRevoked    = "revoked"
)

func (WechatAccountAnalyticsBatch) TableName() string { return "wechat_account_analytics_batches" }

// WechatAccountAnalyticsDay stores one account-level calendar-day observation.
// Every metric is a pointer so an unavailable metric remains distinct from an
// observed zero.
type WechatAccountAnalyticsDay struct {
	ID                string    `gorm:"type:char(36);primaryKey" json:"id"`
	ProjectID         string    `gorm:"type:char(36);index:idx_wechat_account_analytics_days_project_date,priority:1;not null" json:"project_id"`
	BatchID           string    `gorm:"type:char(36);index:idx_wechat_account_analytics_days_project_date,priority:3;index;not null" json:"batch_id"`
	StatDate          string    `gorm:"type:char(10);index:idx_wechat_account_analytics_days_project_date,priority:2;not null" json:"stat_date"`
	ReadUsers         *int64    `json:"read_users"`
	ShareUsers        *int64    `json:"share_users"`
	OriginalReadUsers *int64    `json:"original_read_users"`
	LikeUsers         *int64    `json:"like_users"`
	CollectionUsers   *int64    `json:"collection_users"`
	CommentCount      *int64    `json:"comment_count"`
	FollowerGainCount *int64    `json:"follower_gain_count"`
	PublishedArticles *int64    `json:"published_articles"`
	Source            string    `gorm:"type:varchar(255);not null;default:''" json:"source,omitempty"`
	RawData           string    `gorm:"type:json" json:"raw_data,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (WechatAccountAnalyticsDay) TableName() string { return "wechat_account_analytics_days" }

// WechatTrafficSource is the normalized source vocabulary used by the
// account-level traffic report. Original labels remain on each fact.
type WechatTrafficSource string

const (
	WechatTrafficSourceMessage             WechatTrafficSource = "message"
	WechatTrafficSourceRecommendation      WechatTrafficSource = "recommendation"
	WechatTrafficSourceChat                WechatTrafficSource = "chat"
	WechatTrafficSourceMoments             WechatTrafficSource = "moments"
	WechatTrafficSourceOfficialAccountHome WechatTrafficSource = "official_account_home"
	WechatTrafficSourceSearch              WechatTrafficSource = "search"
	WechatTrafficSourceOther               WechatTrafficSource = "other"
)

func (s WechatTrafficSource) Valid() bool {
	switch s {
	case WechatTrafficSourceMessage, WechatTrafficSourceRecommendation, WechatTrafficSourceChat,
		WechatTrafficSourceMoments, WechatTrafficSourceOfficialAccountHome, WechatTrafficSourceSearch,
		WechatTrafficSourceOther:
		return true
	default:
		return false
	}
}

// NormalizeWechatTrafficSource accepts only documented official labels and
// stable English identifiers. Unknown labels fail preview instead of being
// silently guessed into a bucket.
func NormalizeWechatTrafficSource(label string) (WechatTrafficSource, error) {
	switch strings.TrimSpace(strings.ToLower(label)) {
	case "消息", "公众号消息", "公众号会话", "message":
		return WechatTrafficSourceMessage, nil
	case "朋友在看", "看一看", "推荐", "recommendation":
		return WechatTrafficSourceRecommendation, nil
	case "聊天", "朋友聊天", "chat":
		return WechatTrafficSourceChat, nil
	case "朋友圈", "moments":
		return WechatTrafficSourceMoments, nil
	case "公众号主页", "公众号主页访问", "official account home", "official_account_home":
		return WechatTrafficSourceOfficialAccountHome, nil
	case "搜一搜", "搜索", "search":
		return WechatTrafficSourceSearch, nil
	case "其他", "其它", "other":
		return WechatTrafficSourceOther, nil
	default:
		return "", fmt.Errorf("unsupported WeChat traffic source %q", label)
	}
}

// WechatTrafficSourceDay stores one source-attributed read fact for a day.
type WechatTrafficSourceDay struct {
	ID            string              `gorm:"type:char(36);primaryKey" json:"id"`
	ProjectID     string              `gorm:"type:char(36);index:idx_wechat_traffic_source_days_project_date,priority:1;uniqueIndex:uq_wechat_traffic_source_days_batch_date_source,priority:1;not null" json:"project_id"`
	BatchID       string              `gorm:"type:char(36);index:idx_wechat_traffic_source_days_project_date,priority:3;index;uniqueIndex:uq_wechat_traffic_source_days_batch_date_source,priority:3;not null" json:"batch_id"`
	StatDate      string              `gorm:"type:char(10);index:idx_wechat_traffic_source_days_project_date,priority:2;uniqueIndex:uq_wechat_traffic_source_days_batch_date_source,priority:2;not null" json:"stat_date"`
	Source        WechatTrafficSource `gorm:"type:varchar(40);uniqueIndex:uq_wechat_traffic_source_days_batch_date_source,priority:4;not null" json:"source"`
	OriginalLabel string              `gorm:"type:varchar(255);not null;default:''" json:"original_label,omitempty"`
	ReadUsers     *int64              `json:"read_users"`
	RawData       string              `gorm:"type:json" json:"raw_data,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

func (WechatTrafficSourceDay) TableName() string { return "wechat_traffic_source_days" }
