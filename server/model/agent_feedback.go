package model

import "time"

// AgentFeedback stores post-execution feedback from an agent (scores, errors, optimizations).
type AgentFeedback struct {
	ID            string    `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID        string    `gorm:"type:char(36);index;not null" json:"task_id"`
	AgentName     string    `gorm:"type:varchar(30);not null" json:"agent_name"`
	Scores        string    `gorm:"type:json" json:"scores,omitempty"`
	Errors        string    `gorm:"type:text" json:"errors,omitempty"`
	Optimizations string    `gorm:"type:text" json:"optimizations,omitempty"`
	Summary       string    `gorm:"type:varchar(500)" json:"summary,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// TableName returns the database table name for AgentFeedback.
func (AgentFeedback) TableName() string { return "agent_feedbacks" }
