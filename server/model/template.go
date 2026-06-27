package model

import (
	"time"

	"gorm.io/datatypes"
)

// Template is a PROJECT STARTER — a frozen snapshot of a project's config used
// to pre-fill a new project at creation time, then detached. It does NOT enter
// the runtime resolution chain (project is the single source of truth; Project
// keeps only a created_from_template_id audit link).
//
// Its field shape mirrors Project so a template is literally "a project config
// you can apply". Field surface differs by type:
//   - 小红书 (seednote): only 图片视觉 (VisualStyle + ThumbnailURL).
//   - 公众号 (article):  图片视觉 (VisualStyle) + 作者 (Byline, the published byline)
//   - 写作笔迹 (WritingVoice free text + optional PersonaAvatar persona avatar)
//   - 排版 (Theme).
//   - 海报 (poster):    legacy content scaffold (WriterKey/Structure/Example/...).
//   - 电商 (ecommerce): 图片视觉 (VisualStyle) + Ecommerce defaults (default modules /
//     target platform / brand brief / image model key).
//
// Byline and WritingVoice are kept STRICTLY SEPARATE (byline = published name;
// WritingVoice = writing imitation). Field names are crisp and non-overloaded;
// DB column names are retained from the pre-refactor schema (gorm:"column:...")
// so this rename needs no data migration.
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
	// WriterKey is the 写作者 YAML resource key (e.g. "dan-koe") used by poster
	// projects. The article form does NOT set it (it uses WritingVoice instead).
	WriterKey string `gorm:"column:writing_style;type:text" json:"writer_key"`
	// Theme is the 排版 (layout/typesetting) resource key (e.g. "autumn-warm").
	Theme string `gorm:"type:varchar(50);default:''" json:"theme"`
	// Byline is the 作者（署名）— the published WeChat author name. JUST a name for
	// 署名; strictly independent of WritingVoice below.
	Byline string `gorm:"column:author_name;type:varchar(100)" json:"byline"`
	// WritingVoice is the 写作笔迹 (free-text writing imitation: 框架/方式/笔迹),
	// defined inline (NOT a writer resource key, NOT the byline).
	WritingVoice string `gorm:"column:author_style_intro;type:text" json:"writing_voice"`
	// PersonaAvatar is the optional 人设头像 (persona avatar, never the byline).
	PersonaAvatar  string         `gorm:"column:author_avatar_url;type:varchar(500)" json:"persona_avatar"`
	ExampleContent map[string]any `gorm:"type:json;serializer:json" json:"example_content"`
	Tags           []string       `gorm:"type:json;serializer:json" json:"tags"`
	// Ecommerce carries the e-commerce template defaults (default deliverable
	// modules + quantities, target platform, brand brief, default image model key)
	// for type="ecommerce" templates. Zero value for non-ecommerce templates.
	Ecommerce datatypes.JSONType[EcommerceTemplateDefaults] `gorm:"type:json" json:"ecommerce,omitempty"`
	SortOrder int                                           `gorm:"default:0" json:"sort_order"`
	IsActive  bool                                          `gorm:"default:true" json:"is_active"`
	CreatedAt time.Time                                     `json:"created_at"`
	UpdatedAt time.Time                                     `json:"updated_at"`
}

func (Template) TableName() string { return "templates" }

// EcommerceTemplateDefaults carries the reusable defaults an e-commerce template
// bakes into every project created from it: which deliverable modules to
// pre-select (and quantity), the default target platform, a brand brief, and the
// default image model key (e.g. "openai-gpt-image"). Stored on Template.Ecommerce
// as a typed JSON column. Product photos are intentionally NOT here.
type EcommerceTemplateDefaults struct {
	DefaultSelectedModules map[string]int `json:"default_selected_modules,omitempty"`
	TargetPlatform         string         `json:"target_platform,omitempty"`
	BrandBrief             string         `json:"brand_brief,omitempty"`
	ImageModelKey          string         `json:"image_model_key,omitempty"`
}

// SetEcommerce stores e-commerce template defaults into the Ecommerce JSON column.
// Thin wrapper over datatypes.NewJSONType so handler/service call sites don't each
// need to import gorm.io/datatypes.
func (t *Template) SetEcommerce(ec EcommerceTemplateDefaults) {
	t.Ecommerce = datatypes.NewJSONType(ec)
}
