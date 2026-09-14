package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	WechatPublicationSourceAnbanAPI      = "anban_api"
	WechatPublicationSourceWechatConsole = "wechat_console"

	WechatPublicationStatusDrafting          = "drafting"
	WechatPublicationStatusDrafted           = "drafted"
	WechatPublicationStatusPublishSubmitting = "publish_submitting"
	WechatPublicationStatusPublishing        = "publishing"
	WechatPublicationStatusPublished         = "published"
	WechatPublicationStatusNeedsSelection    = "needs_selection"
	WechatPublicationStatusPublishFailed     = "publish_failed"
	WechatPublicationStatusUnsupported       = "unsupported"
)

func IsWechatPublicationSource(value string) bool {
	return value == WechatPublicationSourceAnbanAPI || value == WechatPublicationSourceWechatConsole
}

// IsWechatPublicationStatus reports whether value is a lifecycle status.
func IsWechatPublicationStatus(value string) bool {
	for _, status := range []string{
		WechatPublicationStatusDrafting, WechatPublicationStatusDrafted, WechatPublicationStatusPublishSubmitting,
		WechatPublicationStatusPublishing, WechatPublicationStatusPublished, WechatPublicationStatusNeedsSelection,
		WechatPublicationStatusPublishFailed, WechatPublicationStatusUnsupported,
	} {
		if value == status {
			return true
		}
	}
	return false
}

// WechatPublication is the durable publication lifecycle for a single task.
// Draft delivery is tracked separately on TaskExecution; formal publication is
// represented only by this one-per-task record.
type WechatPublication struct {
	ID          string `gorm:"type:char(36);primaryKey;not null" json:"id"`
	TaskID      string `gorm:"type:char(36);uniqueIndex;not null" json:"task_id"`
	ExecutionID string `gorm:"type:char(36);index;not null;default:''" json:"execution_id"`
	UserID      string `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID   string `gorm:"type:char(36);index;not null" json:"project_id"`

	DraftMediaID            string `gorm:"type:varchar(191);not null;default:'';index" json:"draft_media_id,omitempty"`
	DraftTitle              string `gorm:"type:varchar(500);not null;default:''" json:"draft_title,omitempty"`
	DraftAuthor             string `gorm:"type:varchar(500);not null;default:''" json:"draft_author,omitempty"`
	DraftDigest             string `gorm:"type:text;not null" json:"draft_digest,omitempty"`
	DraftThumbMediaID       string `gorm:"type:varchar(191);not null;default:''" json:"draft_thumb_media_id,omitempty"`
	DraftContentFingerprint string `gorm:"type:char(64);not null;default:'';index" json:"draft_content_fingerprint,omitempty"`
	DraftRequestFingerprint string `gorm:"type:char(64);not null;default:''" json:"draft_request_fingerprint,omitempty"`

	Source           string `gorm:"type:varchar(32);index;not null;check:chk_wechat_publication_source,source IN ('anban_api','wechat_console')" json:"source"`
	Status           string `gorm:"type:varchar(32);index;not null;check:chk_wechat_publication_status,status IN ('drafting','drafted','publish_submitting','publishing','published','needs_selection','publish_failed','unsupported')" json:"status"`
	PublishID        string `gorm:"type:varchar(191);not null;default:'';index" json:"publish_id,omitempty"`
	MsgDataID        string `gorm:"type:varchar(191);not null;default:'';index" json:"msg_data_id,omitempty"`
	MsgID            string `gorm:"type:varchar(191);not null;default:'';index" json:"msg_id,omitempty"`
	ArticleID        string `gorm:"type:varchar(191);not null;default:'';index" json:"article_id,omitempty"`
	ArticleURL       string `gorm:"type:varchar(1000);not null;default:''" json:"article_url,omitempty"`
	ArticleIndex     int    `gorm:"not null;default:1" json:"article_index"`
	WechatStatusCode int    `gorm:"not null;default:0" json:"wechat_status_code"`

	DraftCreatedAt *time.Time     `gorm:"index" json:"draft_created_at,omitempty"`
	PublishedAt    *time.Time     `gorm:"index" json:"published_at,omitempty"`
	NextCheckAt    *time.Time     `gorm:"index" json:"next_check_at,omitempty"`
	LastCheckedAt  *time.Time     `gorm:"index" json:"last_checked_at,omitempty"`
	CheckAttempts  int            `gorm:"not null;default:0" json:"check_attempts"`
	LastError      string         `gorm:"type:text;not null" json:"last_error,omitempty"`
	Candidates     datatypes.JSON `gorm:"type:json" json:"candidates,omitempty"`

	ClaimToken          string     `gorm:"type:char(36);not null;default:'';index" json:"-"`
	ClaimedAt           *time.Time `gorm:"index" json:"-"`
	DraftAddAttemptedAt *time.Time `json:"-"`
	SubmitAttemptedAt   *time.Time `json:"submit_attempted_at,omitempty"`
	CreatedAt           time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"not null" json:"updated_at"`
}

func (WechatPublication) TableName() string { return "wechat_publications" }

// WechatProjectReconcileLease is the project-scoped provider-call throttle.
// It is deliberately separate from per-publication poll timestamps.
type WechatProjectReconcileLease struct {
	ProjectID        string    `gorm:"type:char(36);primaryKey;not null" json:"-"`
	LeaseUntilMicros int64     `gorm:"not null;index" json:"-"`
	CreatedAt        time.Time `gorm:"not null" json:"-"`
	UpdatedAt        time.Time `gorm:"not null" json:"-"`
}

func (WechatProjectReconcileLease) TableName() string {
	return "wechat_project_reconcile_leases"
}

// WechatPublicationBinding atomically assigns one project article to one
// publication while allowing the provider to reuse an article ID in another
// official-account project.
type WechatPublicationBinding struct {
	ProjectID     string    `gorm:"type:char(36);primaryKey;not null" json:"-"`
	ArticleID     string    `gorm:"type:varchar(191);primaryKey;not null" json:"-"`
	PublicationID string    `gorm:"type:char(36);uniqueIndex;not null" json:"-"`
	CreatedAt     time.Time `gorm:"not null" json:"-"`
}

func (WechatPublicationBinding) TableName() string { return "wechat_publication_bindings" }
