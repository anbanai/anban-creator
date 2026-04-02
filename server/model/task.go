package model

import "time"

// Task represents a content generation task.
type Task struct {
	ID           string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID       string     `gorm:"type:char(36);index;not null" json:"user_id"`
	PlanID       *uint      `gorm:"index" json:"plan_id"`
	Type         string     `gorm:"type:varchar(20);not null" json:"type"`
	Status       string     `gorm:"type:varchar(20);default:pending" json:"status"`
	Topic        string     `gorm:"type:varchar(500)" json:"topic"`
	ProgressLog  string     `gorm:"type:longtext" json:"progress_log,omitempty"`
	Result       string     `gorm:"type:json" json:"result,omitempty"`
	ErrorMessage string     `gorm:"type:text" json:"error_message,omitempty"`
	StartedAt    *time.Time `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	CreatedAt    time.Time  `json:"created_at"`
	Plan         *Plan      `gorm:"foreignKey:PlanID" json:"plan,omitempty"`
}

// TableName returns the database table name for Task.
func (Task) TableName() string { return "tasks" }
