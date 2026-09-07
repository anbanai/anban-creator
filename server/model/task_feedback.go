package model

import "time"

// TaskFeedback stores a user's evaluation of a completed task result.
type TaskFeedback struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID    string    `gorm:"type:char(36);not null;uniqueIndex:idx_task_feedback_task_user,priority:1;index" json:"task_id"`
	UserID    string    `gorm:"type:char(36);not null;uniqueIndex:idx_task_feedback_task_user,priority:2;index" json:"user_id"`
	Rating    int       `gorm:"not null" json:"rating"`
	Content   string    `gorm:"type:varchar(1000);not null" json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (TaskFeedback) TableName() string { return "task_feedbacks" }
