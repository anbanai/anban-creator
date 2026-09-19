package model

import "time"

const (
	WechatCapabilityDraftAdd              = "draft_add"
	WechatCapabilityFreePublishSubmit     = "freepublish_submit"
	WechatCapabilityFreePublishQuery      = "freepublish_query"
	WechatCapabilityPublishedArticleQuery = "published_article_query"
	WechatCapabilityDataCubeArticleStats  = "datacube_article_stats"

	WechatCapabilityUnknown                = "unknown"
	WechatCapabilityAvailable              = "available"
	WechatCapabilityDenied                 = "denied"
	WechatCapabilityTemporarilyUnavailable = "temporarily_unavailable"
)

var WechatCapabilities = []string{
	WechatCapabilityDraftAdd,
	WechatCapabilityFreePublishSubmit,
	WechatCapabilityFreePublishQuery,
	WechatCapabilityPublishedArticleQuery,
	WechatCapabilityDataCubeArticleStats,
}

type WechatAccountCapability struct {
	ProjectID      string     `gorm:"type:char(36);primaryKey;not null" json:"project_id"`
	Capability     string     `gorm:"type:varchar(64);primaryKey;not null" json:"capability"`
	Status         string     `gorm:"type:varchar(32);index;not null" json:"status"`
	LastCheckedAt  *time.Time `gorm:"index" json:"last_checked_at,omitempty"`
	LastWechatCode int        `gorm:"not null;default:0" json:"last_wechat_code"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (WechatAccountCapability) TableName() string { return "wechat_account_capabilities" }
