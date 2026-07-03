package model

import "time"

const (
	IlinkBindingStatusPending = "pending"
	IlinkBindingStatusActive  = "active"
)

const (
	IlinkNotificationStatusPending   = "pending"
	IlinkNotificationStatusDelivered = "delivered"
	IlinkNotificationStatusFailed    = "failed"
)

// IlinkBinding maps a Studio user to a WeChat contact talking to a platform
// assistant account. wcflink remains the transport implementation; ilink is the
// platform channel name.
type IlinkBinding struct {
	ID                string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID            string     `gorm:"type:char(36);uniqueIndex;not null" json:"user_id"`
	PlatformAccountID *string    `gorm:"type:varchar(128);index:idx_ilink_contact,unique" json:"platform_account_id,omitempty"`
	ExternalUserID    *string    `gorm:"type:varchar(128);index:idx_ilink_contact,unique" json:"external_user_id,omitempty"`
	BindCode          *string    `gorm:"type:varchar(32);uniqueIndex" json:"-"`
	BindCodeExpiresAt *time.Time `gorm:"index" json:"-"`
	DefaultProjectID  string     `gorm:"type:char(36);index" json:"default_project_id"`
	Status            string     `gorm:"type:varchar(20);default:pending" json:"status"`
	BoundAt           *time.Time `gorm:"index" json:"bound_at"`
	LastSeenAt        time.Time  `json:"last_seen_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (IlinkBinding) TableName() string { return "ilink_bindings" }

// IlinkNotification is a reliable outbox row for ilink task terminal messages.
type IlinkNotification struct {
	ID                string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID            string     `gorm:"type:char(36);index;not null" json:"user_id"`
	TaskID            string     `gorm:"type:char(36);index:idx_ilink_task_status,unique;not null" json:"task_id"`
	TaskStatus        string     `gorm:"type:varchar(32);index:idx_ilink_task_status,unique;not null" json:"task_status"`
	PlatformAccountID string     `gorm:"type:varchar(128);index" json:"platform_account_id"`
	ExternalUserID    string     `gorm:"type:varchar(128)" json:"external_user_id"`
	Body              string     `gorm:"type:text" json:"body"`
	Status            string     `gorm:"type:varchar(20);index;default:pending" json:"status"`
	Attempts          int        `json:"attempts"`
	NextAttemptAt     time.Time  `gorm:"index" json:"next_attempt_at"`
	LastError         string     `gorm:"type:text" json:"last_error"`
	DeliveredAt       *time.Time `json:"delivered_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (IlinkNotification) TableName() string { return "ilink_notification_outbox" }
