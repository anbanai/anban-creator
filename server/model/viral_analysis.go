package model

import (
	"encoding/json"
	"time"
)

// ViralAnalysis represents a viral content analysis report.
type ViralAnalysis struct {
	ID             string          `gorm:"type:char(36);primaryKey" json:"id"`
	UserID         string          `gorm:"type:char(36);not null;index" json:"user_id"`
	SourceType     string          `gorm:"type:varchar(20);not null" json:"source_type"`
	SourceURL      string          `gorm:"type:varchar(500);not null" json:"source_url"`
	SourceData     json.RawMessage `gorm:"type:json" json:"source_data"`
	AnalysisResult json.RawMessage `gorm:"type:json" json:"analysis_result"`
	Status         string          `gorm:"type:varchar(20);not null;default:'pending'" json:"status"`
	ErrorMessage   string          `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt      time.Time       `gorm:"index" json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func (ViralAnalysis) TableName() string { return "viral_analyses" }
