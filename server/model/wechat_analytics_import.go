package model

import "time"

const (
	WechatAnalyticsImportBatchProcessing  = "processing"
	WechatAnalyticsImportBatchNeedsReview = "needs_review"
	WechatAnalyticsImportBatchCompleted   = "completed"
	WechatAnalyticsImportBatchFailed      = "failed"

	WechatAnalyticsImportRowMatched     = "matched"
	WechatAnalyticsImportRowNeedsReview = "needs_review"
	WechatAnalyticsImportRowUnmatched   = "unmatched"
	WechatAnalyticsImportRowInvalid     = "invalid"
)

// WechatAnalyticsImportBatch is an immutable import of a WeChat console export.
type WechatAnalyticsImportBatch struct {
	ID            string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID        string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID     string     `gorm:"type:char(36);index;not null" json:"project_id"`
	AssetID       string     `gorm:"type:char(36);index;not null" json:"asset_id"`
	FileName      string     `gorm:"type:varchar(255);not null" json:"file_name"`
	ContentType   string     `gorm:"type:varchar(120);not null" json:"content_type"`
	FileSize      int64      `gorm:"not null" json:"file_size"`
	SHA256        string     `gorm:"type:char(64);index;not null" json:"sha256"`
	Source        string     `gorm:"type:varchar(255);not null;default:''" json:"source,omitempty"`
	ReceivedAt    time.Time  `gorm:"index;not null" json:"received_at"`
	DataAsOfAt    time.Time  `gorm:"index;not null" json:"data_as_of_at"`
	Timezone      string     `gorm:"type:varchar(64);not null" json:"timezone"`
	ParserVersion string     `gorm:"type:varchar(32);not null" json:"parser_version"`
	Status        string     `gorm:"type:varchar(32);index;not null" json:"status"`
	TotalRows     int        `json:"total_rows"`
	MatchedRows   int        `json:"matched_rows"`
	ReviewRows    int        `json:"review_rows"`
	UnmatchedRows int        `json:"unmatched_rows"`
	InvalidRows   int        `json:"invalid_rows"`
	RevokedAt     *time.Time `gorm:"index" json:"revoked_at,omitempty"`
	ErrorSummary  string     `gorm:"type:text" json:"error_summary,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (WechatAnalyticsImportBatch) TableName() string { return "wechat_analytics_import_batches" }

type WechatAnalyticsImportRow struct {
	ID                     string     `gorm:"type:char(36);primaryKey" json:"id"`
	BatchID                string     `gorm:"type:char(36);index;not null" json:"batch_id"`
	ProjectID              string     `gorm:"type:char(36);index;not null" json:"project_id"`
	SourceRow              int        `gorm:"not null" json:"source_row"`
	RawData                string     `gorm:"type:json;not null" json:"raw_data"`
	Source                 string     `gorm:"type:varchar(255);not null;default:''" json:"source,omitempty"`
	Title                  string     `gorm:"type:varchar(500);not null" json:"title"`
	NormalizedTitle        string     `gorm:"type:varchar(500);index;not null" json:"normalized_title"`
	PublishedDate          *time.Time `gorm:"index" json:"published_date,omitempty"`
	ArticleURL             string     `gorm:"type:varchar(1000);not null;default:'';index" json:"article_url,omitempty"`
	ReadUsers              *int64     `json:"read_users,omitempty"`
	ShareUsers             *int64     `json:"share_users,omitempty"`
	ReadToFollowUsers      *int64     `json:"read_to_follow_users,omitempty"`
	DeliveredUsers         *int64     `json:"delivered_users,omitempty"`
	DeliveryCompletionRate *float64   `json:"delivery_completion_rate,omitempty"`
	ReadCompletionRate     *float64   `json:"read_completion_rate,omitempty"`
	ParseError             string     `gorm:"type:text" json:"parse_error,omitempty"`
	MatchStatus            string     `gorm:"type:varchar(32);index;not null" json:"match_status"`
	CandidatePublicationID string     `gorm:"type:char(36);index" json:"candidate_publication_id,omitempty"`
	PublicationID          string     `gorm:"type:char(36);index" json:"publication_id,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func (WechatAnalyticsImportRow) TableName() string { return "wechat_analytics_import_rows" }

// WechatAnalyticsSnapshot preserves each imported point-in-time observation.
type WechatAnalyticsSnapshot struct {
	ID                     string    `gorm:"type:char(36);primaryKey" json:"id"`
	ProjectID              string    `gorm:"type:char(36);index;not null" json:"project_id"`
	PublicationID          string    `gorm:"type:char(36);index;not null" json:"publication_id"`
	BatchID                string    `gorm:"type:char(36);index;not null" json:"batch_id"`
	ImportRowID            string    `gorm:"type:char(36);uniqueIndex;not null" json:"import_row_id"`
	Source                 string    `gorm:"type:varchar(255);not null;default:''" json:"source,omitempty"`
	DataAsOfAt             time.Time `gorm:"index;not null" json:"data_as_of_at"`
	ImportedAt             time.Time `gorm:"index;not null" json:"imported_at"`
	ReadUsers              *int64    `json:"read_users,omitempty"`
	ShareUsers             *int64    `json:"share_users,omitempty"`
	ReadToFollowUsers      *int64    `json:"read_to_follow_users,omitempty"`
	DeliveredUsers         *int64    `json:"delivered_users,omitempty"`
	DeliveryCompletionRate *float64  `json:"delivery_completion_rate,omitempty"`
	ReadCompletionRate     *float64  `json:"read_completion_rate,omitempty"`
	RawData                string    `gorm:"type:json;not null" json:"raw_data"`
	CreatedAt              time.Time `json:"created_at"`
}

func (WechatAnalyticsSnapshot) TableName() string { return "wechat_analytics_snapshots" }
