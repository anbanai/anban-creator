package model

import "time"

// TaskFileObjectCleanup durably tracks uploaded objects that lost a task-file
// publication race and could not be deleted immediately.
type TaskFileObjectCleanup struct {
	ID              string `gorm:"type:char(36);primaryKey"`
	TaskFileID      string `gorm:"type:char(36);index;not null"`
	StorageProvider string `gorm:"type:varchar(20);uniqueIndex:idx_task_file_object_cleanup_key,priority:1;not null"`
	OSSKey          string `gorm:"type:varchar(500);uniqueIndex:idx_task_file_object_cleanup_key,priority:2;not null"`
	CreatedAt       time.Time
}

func (TaskFileObjectCleanup) TableName() string { return "task_file_object_cleanups" }
