package model

import "time"

// Template represents a reusable content template (poster, seednote, article).
// The visual style lives in StylePrompt (+ ThumbnailURL reference image). The
// content scaffold — WritingStyle (写作风格/调性), Structure (内容结构, stored as
// {"text": markdown}), ExampleContent (示例, {"text": markdown}) — is delivered to
// the generation agent via get_channel_profile(task_id) when a task references
// this template. Category + Tags power the template-library filters.
type Template struct {
	ID           string         `gorm:"type:char(36);primaryKey" json:"id"`
	UserID       string         `gorm:"type:char(36);index" json:"user_id,omitempty"`
	Visibility   string         `gorm:"type:varchar(20);default:'public'" json:"visibility"` // public | private
	Type         string         `gorm:"type:varchar(20);not null;index" json:"type"`
	Name         string         `gorm:"type:varchar(100);not null" json:"name"`
	Category     string         `gorm:"type:varchar(50);index" json:"category"`
	ThumbnailURL string         `gorm:"type:varchar(500)" json:"thumbnail_url"`
	Structure    map[string]any `gorm:"type:json;serializer:json" json:"structure"`
	StylePrompt  string         `gorm:"type:text" json:"style_prompt"`
	// WritingStyle is the content scaffold's writing voice (tone/人设/调性), distinct
	// from StylePrompt (visual image style). Surfaced to the agent via
	// get_channel_profile(task_id) as template_writing_style. Empty = no override.
	WritingStyle   string         `gorm:"type:text" json:"writing_style"`
	ExampleContent map[string]any `gorm:"type:json;serializer:json" json:"example_content"`
	Tags           []string       `gorm:"type:json;serializer:json" json:"tags"`
	SortOrder      int            `gorm:"default:0" json:"sort_order"`
	IsActive       bool           `gorm:"default:true" json:"is_active"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func (Template) TableName() string { return "templates" }
