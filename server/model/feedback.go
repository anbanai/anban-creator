package model

import "time"

// Feedback represents a user feedback submission.
type Feedback struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID    string    `gorm:"type:char(36);index;not null" json:"user_id"`
	Type      string    `gorm:"type:varchar(20);not null" json:"type"` // bug, suggestion
	Content   string    `gorm:"type:varchar(1000);not null" json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the database table name for Feedback.
func (Feedback) TableName() string { return "feedbacks" }

// Feedback type constants.
const (
	FeedbackTypeBug        = "bug"
	FeedbackTypeSuggestion = "suggestion"
)
