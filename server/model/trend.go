package model

import (
	"time"

	"gorm.io/datatypes"
)

// TrendSnapshot stores the latest successful public trend snapshot per platform.
type TrendSnapshot struct {
	ID              uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Platform        string         `gorm:"type:varchar(32);uniqueIndex;not null" json:"platform"`
	Items           datatypes.JSON `gorm:"type:json;not null" json:"items"`
	FetchedAt       time.Time      `gorm:"not null" json:"fetched_at"`
	LastAttemptedAt time.Time      `gorm:"not null" json:"last_attempted_at"`
	LastError       string         `gorm:"type:varchar(500)" json:"last_error,omitempty"`
	Source          string         `gorm:"type:varchar(32)" json:"source"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func (TrendSnapshot) TableName() string { return "trend_snapshots" }
