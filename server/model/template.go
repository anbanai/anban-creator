package model

import "time"

// Template represents a reusable content template (poster, seednote, article).
//
// A 公众号 template bundles three orthogonal visual-expression dimensions:
//   - 图片视觉 (image visual style): StylePrompt (+ ThumbnailURL reference image)
//   - 写作风格 (writing voice):      WritingStyle (writer 资源 key)
//   - 排版样式 (layout/typesetting): Theme (theme 资源 key)
//
// The three are independent — WritingStyle never drives image style. The content
// scaffold — Structure (内容结构, {"text": markdown}), ExampleContent (示例,
// {"text": markdown}) — plus the three dimensions are delivered to the agent via
// get_channel_profile(task_id) when a task references this template. Category +
// Tags power the template-library filters.
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
	// Theme is the template's 排版样式 (theme resource key, e.g. "autumn-warm"),
	// the third orthogonal dimension alongside StylePrompt (visual) and
	// WritingStyle (voice). Surfaced to the agent as template_theme and resolved
	// into Task.Theme at creation. Empty = no override (channel theme used).
	Theme          string         `gorm:"type:varchar(50);default:''" json:"theme"`
	ExampleContent map[string]any `gorm:"type:json;serializer:json" json:"example_content"`
	Tags           []string       `gorm:"type:json;serializer:json" json:"tags"`
	SortOrder      int            `gorm:"default:0" json:"sort_order"`
	IsActive       bool           `gorm:"default:true" json:"is_active"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func (Template) TableName() string { return "templates" }
