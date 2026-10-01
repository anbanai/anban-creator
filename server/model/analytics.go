package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"reflect"
	"regexp"
	"time"
)

// AnalyticsDecimal stores the original exact base-ten value. Text storage avoids
// SQLite's NUMERIC affinity silently converting precise decimals into float64.
// Validation uses the same DECIMAL(38,18) domain on all database engines.
type AnalyticsDecimal string

var analyticsDecimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$`)

func (d AnalyticsDecimal) Validate() error {
	if !analyticsDecimalPattern.MatchString(string(d)) {
		return fmt.Errorf("invalid exact decimal %q", d)
	}
	return nil
}
func (d AnalyticsDecimal) Value() (driver.Value, error) {
	if e := d.Validate(); e != nil {
		return nil, e
	}
	return string(d), nil
}
func (d *AnalyticsDecimal) Scan(value any) error {
	switch v := value.(type) {
	case string:
		*d = AnalyticsDecimal(v)
	case []byte:
		*d = AnalyticsDecimal(v)
	default:
		return fmt.Errorf("decimal requires exact text, got %T", value)
	}
	return d.Validate()
}
func (d AnalyticsDecimal) MarshalJSON() ([]byte, error) {
	if e := d.Validate(); e != nil {
		return nil, e
	}
	return []byte(d), nil
}
func (d *AnalyticsDecimal) UnmarshalJSON(b []byte) error {
	var n json.Number
	if e := json.Unmarshal(b, &n); e != nil {
		return e
	}
	*d = AnalyticsDecimal(n.String())
	return d.Validate()
}

type AnalyticsMetrics struct {
	DeliveredUsers         *int64            `json:"delivered_users"`
	DeliveryCompletionRate *AnalyticsDecimal `json:"delivery_completion_rate"`
	ReadUsers              *int64            `json:"read_users"`
	ShareUsers             *int64            `json:"share_users"`
	CollectionUsers        *int64            `json:"collection_users"`
	LikeUsers              *int64            `json:"like_users"`
	ZaikanUsers            *int64            `json:"zaikan_users"`
	CommentCount           *int64            `json:"comment_count"`
	ReadToFollowUsers      *int64            `json:"read_to_follow_users"`
	ExposureCount          *int64            `json:"exposure_count"`
	ViewCount              *int64            `json:"view_count"`
	LikeCount              *int64            `json:"like_count"`
	CollectCount           *int64            `json:"collect_count"`
	FollowerGainCount      *int64            `json:"follower_gain_count"`
	ShareCount             *int64            `json:"share_count"`
	BarrageCount           *int64            `json:"barrage_count"`
	ReadCompletionRate     *AnalyticsDecimal `json:"read_completion_rate"`
	AverageReadActiveTime  *AnalyticsDecimal `json:"average_read_active_time"`
	CoverClickRate         *AnalyticsDecimal `json:"cover_click_rate"`
	AvgWatchDuration       *AnalyticsDecimal `json:"avg_watch_duration"`
}

// Map preserves nil separately from an observed zero and exact decimal JSON numbers.
func (m AnalyticsMetrics) Map(channel string) map[string]any {
	out := map[string]any{}
	v := reflect.ValueOf(m)
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		key := t.Field(i).Tag.Get("json")
		if !AnalyticsMetricForChannel(key, channel) {
			continue
		}
		out[key] = nil
		if !v.Field(i).IsNil() {
			out[key] = v.Field(i).Elem().Interface()
		}
	}
	return out
}
func (m AnalyticsMetrics) Validate() error {
	v := reflect.ValueOf(m)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.IsNil() {
			continue
		}
		switch n := f.Interface().(type) {
		case *int64:
			if *n < 0 {
				return fmt.Errorf("negative analytics count")
			}
		case *AnalyticsDecimal:
			if e := n.Validate(); e != nil {
				return e
			}
		}
	}
	return nil
}
func AnalyticsMetricForChannel(key, channel string) bool {
	if channel == "" {
		return true
	}
	wechat := map[string]bool{"delivered_users": true, "delivery_completion_rate": true, "read_users": true, "share_users": true, "collection_users": true, "like_users": true, "zaikan_users": true, "comment_count": true, "read_to_follow_users": true, "read_completion_rate": true, "average_read_active_time": true}
	if channel == ChannelArticle {
		return wechat[key]
	}
	return !wechat[key] || key == "comment_count"
}

// AnalyticsMetricForPlatform is retained as a source compatibility alias for
// historical import code. New routing must pass the Task/Content channel.
func AnalyticsMetricForPlatform(key, platform string) bool {
	if platform == PlatformWechat || platform == TaskTypeWechatArticle {
		platform = ChannelArticle
	}
	return AnalyticsMetricForChannel(key, platform)
}
func AnalyticsMetricColumns() []string {
	return []string{"delivered_users", "read_users", "share_users", "collection_users", "like_users", "zaikan_users", "comment_count", "read_to_follow_users", "exposure_count", "view_count", "like_count", "collect_count", "follower_gain_count", "share_count", "barrage_count", "read_completion_rate", "average_read_active_time", "cover_click_rate", "avg_watch_duration", "delivery_completion_rate"}
}
func AnalyticsCountColumns() []string   { return AnalyticsMetricColumns()[:15] }
func AnalyticsDecimalColumns() []string { return AnalyticsMetricColumns()[15:] }

type AnalyticsState struct {
	ProjectID        string `gorm:"type:char(36);primaryKey"`
	Revision         int64  `gorm:"not null;default:0"`
	ActiveGeneration int64  `gorm:"not null;default:1"`
	Status           string `gorm:"type:varchar(32);not null;default:ready"`
	UpdatedAt        time.Time
}

type AnalyticsContent struct {
	ID            string     `gorm:"type:varchar(100);primaryKey" json:"id"`
	ProjectID     string     `gorm:"type:char(36);index:idx_analytics_content_project" json:"-"`
	Channel       string     `gorm:"type:varchar(40);index" json:"-"`
	Platform      string     `gorm:"type:varchar(20)" json:"-"` // legacy migration source only
	TaskID        string     `gorm:"type:char(36);index" json:"-"`
	PublicationID string     `gorm:"type:char(36);index" json:"-"`
	PostID        string     `gorm:"type:char(36);index" json:"-"`
	Title         string     `gorm:"type:varchar(500)" json:"title"`
	ContentType   string     `json:"content_type"`
	Status        string     `json:"status,omitempty"`
	URL           string     `gorm:"type:varchar(1000)" json:"url,omitempty"`
	Date          *time.Time `json:"date,omitempty"`
	CreatedAt     time.Time  `json:"-"`
	UpdatedAt     time.Time  `json:"-"`
}

type AnalyticsObservation struct {
	TrackingID       string     `gorm:"type:char(36);index" json:"-"`
	Sequence         uint64     `gorm:"primaryKey;autoIncrement" json:"-"`
	ID               string     `gorm:"type:varchar(100);uniqueIndex;not null" json:"id"`
	ProjectID        string     `gorm:"type:char(36);index:idx_analytics_observation_winner,priority:1;index:idx_analytics_observation_batch,priority:1;not null" json:"-"`
	ContentID        string     `gorm:"type:varchar(100);index:idx_analytics_observation_winner,priority:2;not null" json:"-"`
	BatchID          string     `gorm:"type:char(36);index:idx_analytics_observation_batch,priority:2" json:"-"`
	MetricBasis      string     `gorm:"type:varchar(16);index:idx_analytics_observation_winner,priority:3;not null" json:"metric_basis"`
	StatDate         string     `gorm:"type:char(10);index:idx_analytics_observation_winner,priority:4;not null" json:"stat_date"`
	Source           string     `gorm:"type:varchar(32);not null" json:"source"`
	SourcePriority   int        `gorm:"not null" json:"-"`
	EffectiveAt      time.Time  `gorm:"not null" json:"effective_at"`
	ReceivedAt       time.Time  `gorm:"not null" json:"received_at"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	AnalyticsMetrics `gorm:"embedded" json:"-"`
}

type AnalyticsRawPayload struct {
	ObservationID string `gorm:"type:varchar(100);primaryKey"`
	Payload       string `gorm:"type:longtext;not null"`
}

// Empty ContentID is the account projection; all other rows are per content.
type AnalyticsBucket struct {
	ProjectID        string `gorm:"type:char(36);primaryKey;index:idx_analytics_bucket_range,priority:1"`
	Generation       int64  `gorm:"primaryKey;index:idx_analytics_bucket_range,priority:2"`
	ContentID        string `gorm:"type:varchar(100);primaryKey;index:idx_analytics_bucket_range,priority:6"`
	MetricBasis      string `gorm:"type:varchar(16);primaryKey;index:idx_analytics_bucket_range,priority:3"`
	Granularity      string `gorm:"type:varchar(8);primaryKey;index:idx_analytics_bucket_range,priority:4"`
	BucketStart      string `gorm:"type:char(10);primaryKey;index:idx_analytics_bucket_range,priority:5"`
	LastStatDate     string `gorm:"type:char(10)"`
	ObservationID    string `gorm:"type:varchar(100)"`
	Coverage         int64
	AnalyticsMetrics `gorm:"embedded"`
}

type AnalyticsIdempotency struct {
	ProjectID   string `gorm:"type:char(36);primaryKey"`
	Key         string `gorm:"column:idempotency_key;type:varchar(128);primaryKey"`
	RequestHash string `gorm:"type:char(64);not null"`
	BatchID     string `gorm:"type:char(36);not null"`
	CreatedAt   time.Time
}

func (AnalyticsState) TableName() string      { return "analytics_project_states" }
func (AnalyticsRawPayload) TableName() string { return "analytics_observation_payloads" }

func (AnalyticsDecimal) GormDataType() string { return "string" }
func (AnalyticsDecimal) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return "decimal(38,18)"
	}
	return "text"
}

type AnalyticsRebuildJob struct {
	ID              string    `gorm:"type:char(36);primaryKey" json:"id"`
	ProjectID       string    `gorm:"type:char(36);index;not null" json:"project_id"`
	Generation      int64     `gorm:"not null" json:"generation"`
	Status          string    `gorm:"type:varchar(24);index;not null" json:"status"`
	CursorContentID string    `gorm:"type:varchar(100)" json:"cursor_content_id,omitempty"`
	CursorBasis     string    `gorm:"type:varchar(16)" json:"cursor_basis,omitempty"`
	CursorDate      string    `gorm:"type:char(10)" json:"cursor_date,omitempty"`
	CursorSource    string    `gorm:"type:varchar(32)" json:"cursor_source,omitempty"`
	Processed       int64     `gorm:"not null;default:0" json:"processed"`
	Error           string    `gorm:"type:text" json:"error,omitempty"`
	ReportJSON      string    `gorm:"type:json" json:"report,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (AnalyticsRebuildJob) TableName() string { return "analytics_rebuild_jobs" }
