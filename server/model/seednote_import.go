package model

import "time"

const (
	SeednoteImportBatchStatusProcessing  = "processing"
	SeednoteImportBatchStatusNeedsReview = "needs_review"
	SeednoteImportBatchStatusCompleted   = "completed"
	SeednoteImportBatchStatusFailed      = "failed"

	SeednoteImportRowStatusAutoMatched = "auto_matched"
	SeednoteImportRowStatusNeedsReview = "needs_review"
	SeednoteImportRowStatusMatched     = "matched"
	SeednoteImportRowStatusCreated     = "created"
	SeednoteImportRowStatusSkipped     = "skipped"
	SeednoteImportRowStatusInvalid     = "invalid"
)

// SeednoteImportBatch records one immutable official export import.
type SeednoteImportBatch struct {
	ID                 string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID             string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID          string     `gorm:"type:char(36);index;not null" json:"project_id"`
	AssetID            string     `gorm:"type:char(36);index;not null" json:"asset_id"`
	FileName           string     `gorm:"type:varchar(255);not null" json:"file_name"`
	ContentType        string     `gorm:"type:varchar(120);not null" json:"content_type"`
	FileSize           int64      `gorm:"not null" json:"file_size"`
	SHA256             string     `gorm:"type:char(64);index;not null" json:"sha256"`
	ReceivedAt         time.Time  `gorm:"index;not null" json:"received_at"`
	ClientModifiedAt   *time.Time `gorm:"index" json:"client_modified_at,omitempty"`
	SourceCreatedAt    *time.Time `gorm:"index" json:"source_created_at,omitempty"`
	SourceModifiedAt   *time.Time `gorm:"index" json:"source_modified_at,omitempty"`
	SourceMetadataJSON string     `gorm:"type:json" json:"source_metadata_json,omitempty"`
	DataAsOfAt         time.Time  `gorm:"index;not null" json:"data_as_of_at"`
	Timezone           string     `gorm:"type:varchar(64);not null" json:"timezone"`
	ParserVersion      string     `gorm:"type:varchar(32);not null" json:"parser_version"`
	Status             string     `gorm:"type:varchar(32);index;not null" json:"status"`
	TotalRows          int        `json:"total_rows"`
	ResolvedRows       int        `json:"resolved_rows"`
	ReviewRows         int        `json:"review_rows"`
	InvalidRows        int        `json:"invalid_rows"`
	ErrorSummary       string     `gorm:"type:text" json:"error_summary,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (SeednoteImportBatch) TableName() string { return "seednote_import_batches" }

type SeednoteImportRow struct {
	ID                  string     `gorm:"type:char(36);primaryKey" json:"id"`
	BatchID             string     `gorm:"type:char(36);index;not null" json:"batch_id"`
	ProjectID           string     `gorm:"type:char(36);index;not null" json:"project_id"`
	SourceRow           int        `gorm:"not null" json:"source_row"`
	RawData             string     `gorm:"type:json;not null" json:"raw_data"`
	NormalizedTitle     string     `gorm:"type:varchar(500);index;not null" json:"normalized_title"`
	Title               string     `gorm:"type:varchar(500);not null" json:"title"`
	FirstPublishedAt    *time.Time `gorm:"index" json:"first_published_at,omitempty"`
	Genre               string     `gorm:"type:varchar(50)" json:"genre,omitempty"`
	ExposureCount       *int64     `json:"exposure_count,omitempty"`
	ViewCount           *int64     `json:"view_count,omitempty"`
	CoverClickRate      *float64   `json:"cover_click_rate,omitempty"`
	LikeCount           *int64     `json:"like_count,omitempty"`
	CommentCount        *int64     `json:"comment_count,omitempty"`
	CollectCount        *int64     `json:"collect_count,omitempty"`
	FollowerGainCount   *int64     `json:"follower_gain_count,omitempty"`
	ShareCount          *int64     `json:"share_count,omitempty"`
	AvgWatchDuration    *float64   `json:"avg_watch_duration,omitempty"`
	BarrageCount        *int64     `json:"barrage_count,omitempty"`
	ParseError          string     `gorm:"type:text" json:"parse_error,omitempty"`
	MatchStatus         string     `gorm:"type:varchar(32);index;not null" json:"match_status"`
	CandidatePostID     string     `gorm:"type:char(36);index" json:"candidate_post_id,omitempty"`
	CandidateConfidence float64    `json:"candidate_confidence"`
	PostID              string     `gorm:"type:char(36);index" json:"post_id,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (SeednoteImportRow) TableName() string { return "seednote_import_rows" }

type SeednotePost struct {
	ID               string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID           string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ProjectID        string     `gorm:"type:char(36);index;not null" json:"project_id"`
	Title            string     `gorm:"type:varchar(500);not null" json:"title"`
	NormalizedTitle  string     `gorm:"type:varchar(500);index;not null" json:"normalized_title"`
	FirstPublishedAt *time.Time `gorm:"index" json:"first_published_at,omitempty"`
	Genre            string     `gorm:"type:varchar(50)" json:"genre,omitempty"`
	NoteID           string     `gorm:"type:varchar(100);index" json:"note_id,omitempty"`
	NoteURL          string     `gorm:"type:varchar(500)" json:"note_url,omitempty"`
	TaskID           string     `gorm:"type:char(36);index" json:"task_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (SeednotePost) TableName() string { return "seednote_posts" }

type SeednotePostAlias struct {
	ID               string     `gorm:"type:char(36);primaryKey" json:"id"`
	PostID           string     `gorm:"type:char(36);index;not null" json:"post_id"`
	NormalizedTitle  string     `gorm:"type:varchar(500);index;not null" json:"normalized_title"`
	FirstPublishedAt *time.Time `gorm:"index" json:"first_published_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

func (SeednotePostAlias) TableName() string { return "seednote_post_aliases" }

type SeednoteMetricVersion struct {
	ID                string    `gorm:"type:char(36);primaryKey" json:"id"`
	PostID            string    `gorm:"type:char(36);index;not null" json:"post_id"`
	BatchID           string    `gorm:"type:char(36);index;not null" json:"batch_id"`
	ImportRowID       string    `gorm:"type:char(36);index;not null" json:"import_row_id"`
	DataAsOfAt        time.Time `gorm:"index;not null" json:"data_as_of_at"`
	ImportedAt        time.Time `gorm:"index;not null" json:"imported_at"`
	ExposureCount     *int64    `json:"exposure_count,omitempty"`
	ViewCount         *int64    `json:"view_count,omitempty"`
	CoverClickRate    *float64  `json:"cover_click_rate,omitempty"`
	LikeCount         *int64    `json:"like_count,omitempty"`
	CommentCount      *int64    `json:"comment_count,omitempty"`
	CollectCount      *int64    `json:"collect_count,omitempty"`
	FollowerGainCount *int64    `json:"follower_gain_count,omitempty"`
	ShareCount        *int64    `json:"share_count,omitempty"`
	AvgWatchDuration  *float64  `json:"avg_watch_duration,omitempty"`
	BarrageCount      *int64    `json:"barrage_count,omitempty"`
	RawData           string    `gorm:"type:json;not null" json:"raw_data"`
	CreatedAt         time.Time `json:"created_at"`
}

func (SeednoteMetricVersion) TableName() string { return "seednote_metric_versions" }
