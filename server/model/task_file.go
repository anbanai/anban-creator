package model

import "time"

// TaskFile stores files produced or consumed by a task.
type TaskFile struct {
	ID        string    `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID    string    `gorm:"type:char(26);index;not null" json:"task_id"`
	Role      string    `gorm:"type:varchar(20);not null" json:"role"`
	FilePath  string    `gorm:"type:varchar(500)" json:"file_path"`
	MediaID   string    `gorm:"type:varchar(200)" json:"media_id"`
	WechatURL string    `gorm:"type:varchar(500)" json:"wechat_url"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName returns the database table name for TaskFile.
func (TaskFile) TableName() string { return "task_files" }
