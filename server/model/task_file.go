package model

import "time"

// TaskFile stores files produced or consumed by a task.
type TaskFile struct {
	ID              string    `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID          string    `gorm:"type:char(36);index;not null" json:"task_id"`
	Role            string    `gorm:"type:varchar(20);not null" json:"role"`
	FilePath        string    `gorm:"type:varchar(500)" json:"-"`
	FileName        string    `gorm:"type:varchar(255);not null" json:"file_name"`
	MimeType        string    `gorm:"type:varchar(100)" json:"mime_type"`
	FileSize        int64     `gorm:"default:0" json:"file_size"`
	MediaID         string    `gorm:"type:varchar(200)" json:"media_id"`
	WechatURL       string    `gorm:"type:varchar(500)" json:"wechat_url"`
	OSSKey          string    `gorm:"type:varchar(500)" json:"oss_key"`
	OSSURL          string    `gorm:"type:varchar(500)" json:"oss_url"`
	StorageProvider string    `gorm:"type:varchar(20);default:local" json:"storage_provider"`
	CreatedAt       time.Time `json:"created_at"`
}

// TableName returns the database table name for TaskFile.
func (TaskFile) TableName() string { return "task_files" }
