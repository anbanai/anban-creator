package model

import (
	"time"

	"gorm.io/datatypes"
)

// Template is a visual template: a reusable image reference plus visual prompt
// that can be imported into a project. It does NOT enter task/plan/runtime
// resolution. Legacy columns remain on the model so old rows can still be read,
// but new handlers only write visual metadata.
type Template struct {
	ID           string         `gorm:"type:char(36);primaryKey" json:"id"`
	UserID       string         `gorm:"type:char(36);index" json:"user_id,omitempty"`
	Visibility   string         `gorm:"type:varchar(20);default:'public'" json:"visibility"` // public | private
	Type         string         `gorm:"type:varchar(20);not null;index" json:"type"`
	Name         string         `gorm:"type:varchar(100);not null" json:"name"`
	Category     string         `gorm:"type:varchar(50);index" json:"category"`
	ThumbnailURL string         `gorm:"type:varchar(500)" json:"thumbnail_url"`
	Structure    map[string]any `gorm:"type:json;serializer:json" json:"structure"`
	// VisualStyle is the 图片视觉 (image visual style, free text) dimension.
	VisualStyle string `gorm:"column:style_prompt;type:text" json:"visual_style"`
	// Legacy content fields. New visual-template flows no longer write or read
	// these as business config.
	Writer         string                                        `gorm:"type:text" json:"writer"`
	Theme          string                                        `gorm:"type:varchar(50);default:''" json:"theme"`
	Author         string                                        `gorm:"column:author;type:varchar(100)" json:"author"`
	ExampleContent map[string]any                                `gorm:"type:json;serializer:json" json:"example_content"`
	Tags           []string                                      `gorm:"type:json;serializer:json" json:"tags"`
	Ecommerce      datatypes.JSONType[EcommerceTemplateDefaults] `gorm:"type:json" json:"ecommerce,omitempty"`
	SortOrder      int                                           `gorm:"default:0" json:"sort_order"`
	IsActive       bool                                          `gorm:"default:true" json:"is_active"`
	CreatedAt      time.Time                                     `json:"created_at"`
	UpdatedAt      time.Time                                     `json:"updated_at"`
}

func (Template) TableName() string { return "templates" }

// EcommerceTemplateDefaults is a legacy payload kept so old template rows can be
// decoded. E-commerce defaults now live on projects.
type EcommerceTemplateDefaults struct {
	DefaultSelectedModules map[string]int `json:"default_selected_modules,omitempty"`
	TargetPlatform         string         `json:"target_platform,omitempty"`
	BrandBrief             string         `json:"brand_brief,omitempty"`
	ImageModelKey          string         `json:"image_model_key,omitempty"`
}

// SetEcommerce stores legacy e-commerce template defaults into the JSON column.
func (t *Template) SetEcommerce(ec EcommerceTemplateDefaults) {
	t.Ecommerce = datatypes.NewJSONType(ec)
}
