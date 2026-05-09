package model

import (
	"encoding/json"
	"time"
)

// PosterTask represents a commercial poster generation task.
type PosterTask struct {
	ID              string          `gorm:"type:char(36);primaryKey" json:"id"`
	UserID          string          `gorm:"type:char(36);not null;index" json:"user_id"`
	TemplateID      *string         `gorm:"type:char(36)" json:"template_id"`
	InputContent    json.RawMessage `gorm:"type:json" json:"input_content"`
	StylePreference string          `gorm:"type:varchar(50)" json:"style_preference"`
	Images          json.RawMessage `gorm:"type:json" json:"images"`
	Conversation    json.RawMessage `gorm:"type:json" json:"conversation"`
	Status          string          `gorm:"type:varchar(20);not null;default:'drafting'" json:"status"`
	ErrorMessage    string          `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt       time.Time       `gorm:"index" json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func (PosterTask) TableName() string { return "poster_tasks" }
