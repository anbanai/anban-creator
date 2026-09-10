package model

import "time"

// TaskFile stores files produced or consumed by a task.
type TaskFile struct {
	ID              string    `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID          string    `gorm:"type:char(36);uniqueIndex:idx_task_file_execution_path,priority:1;not null" json:"task_id"`
	ExecutionID     string    `gorm:"type:char(36);uniqueIndex:idx_task_file_execution_path,priority:2;index;not null;default:''" json:"execution_id,omitempty"`
	State           string    `gorm:"type:varchar(20);index;not null;default:published;check:chk_task_file_state,state IN ('pending','published','collected','superseded')" json:"state"`
	Role            string    `gorm:"type:varchar(20);not null" json:"role"`
	FilePath        string    `gorm:"type:varchar(500);uniqueIndex:idx_task_file_execution_path,priority:3" json:"-"`
	FileName        string    `gorm:"type:varchar(255);not null" json:"file_name"`
	MimeType        string    `gorm:"type:varchar(100)" json:"mime_type"`
	FileSize        int64     `gorm:"default:0" json:"file_size"`
	ContentHash     string    `gorm:"type:char(64);index" json:"-"`
	MediaID         string    `gorm:"type:varchar(200)" json:"media_id,omitempty"`
	WechatURL       string    `gorm:"type:varchar(500)" json:"wechat_url,omitempty"`
	OSSKey          string    `gorm:"type:varchar(500)" json:"-"`
	CleanupOSSKey   string    `gorm:"type:varchar(500);index" json:"-"`
	OSSURL          string    `gorm:"type:varchar(500)" json:"-"`
	StorageProvider string    `gorm:"type:varchar(20);default:local" json:"-"`
	URL             string    `gorm:"-" json:"url"`
	IsDeliverable   bool      `gorm:"-" json:"is_deliverable"`
	DeliveryRole    string    `gorm:"-" json:"delivery_role,omitempty"`
	PreviewURL      string    `gorm:"-" json:"preview_url"`
	DownloadURL     string    `gorm:"-" json:"download_url,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

const (
	TaskFileStatePending    = "pending"
	TaskFileStatePublished  = "published"
	TaskFileStateCollected  = "collected"
	TaskFileStateSuperseded = "superseded"
)

// TableName returns the database table name for TaskFile.
func (TaskFile) TableName() string { return "task_files" }
