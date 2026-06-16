package model

import "time"

// Template represents a reusable content template (poster, seednote, article).
// User-created templates store a single reference image and an auto-recognized
// style prompt; system templates may populate additional fields (Structure,
// ExampleContent, Tags) for poster/seednote/article scaffolds.
type Template struct {
	ID             string         `gorm:"type:char(36);primaryKey" json:"id"`
	UserID         string         `gorm:"type:char(36);index" json:"user_id,omitempty"`
	Visibility     string         `gorm:"type:varchar(20);default:'public'" json:"visibility"` // public | private
	Type           string         `gorm:"type:varchar(20);not null;index" json:"type"`
	Name           string         `gorm:"type:varchar(100);not null" json:"name"`
	Category       string         `gorm:"type:varchar(50);index" json:"category"`
	ThumbnailURL   string         `gorm:"type:varchar(500)" json:"thumbnail_url"`
	Structure      map[string]any `gorm:"type:json;serializer:json" json:"structure"`
	StylePrompt    string         `gorm:"type:text" json:"style_prompt"`
	ExampleContent map[string]any `gorm:"type:json;serializer:json" json:"example_content"`
	Tags           []string       `gorm:"type:json;serializer:json" json:"tags"`
	SortOrder      int            `gorm:"default:0" json:"sort_order"`
	IsActive       bool           `gorm:"default:true" json:"is_active"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func (Template) TableName() string { return "templates" }
