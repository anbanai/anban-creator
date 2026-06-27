package model

import (
	"time"

	"gorm.io/datatypes"
)

// ProjectConfig holds platform-specific configuration stored as JSON.
type ProjectConfig struct {
	WechatAppID      string `json:"wechat_app_id,omitempty"`
	WechatSecret     string `json:"wechat_secret,omitempty"`
	EnablePublishing bool   `json:"enable_publishing"`
	// RequirePublishApproval gates auto-publishing behind a human review step.
	// Only meaningful when EnablePublishing=true: when set, a completed article
	// task freezes its draft data into Task.PendingDraftArticles and enters the
	// "pending" approval state instead of immediately landing in the WeChat draft
	// box. Orthogonal to EnablePublishing — old projects default to false, so
	// auto-publish behavior is unchanged unless explicitly opted in.
	RequirePublishApproval bool `json:"require_publish_approval"`
}

// EcommerceProjectDefaults holds the reusable e-commerce defaults for a project
// (platform="ecommerce"): deliverable modules + quantities, target platform,
// brand brief, and default image model key. Product photos stay per-task
// (Task.Ecommerce) since each task ships a distinct product set. Stored on
// Project.EcommerceDefaults as a typed JSON column; tasks inherit these unless
// the task overrides them.
type EcommerceProjectDefaults struct {
	DefaultSelectedModules map[string]int `json:"default_selected_modules,omitempty"`
	TargetPlatform         string         `json:"target_platform,omitempty"`
	BrandBrief             string         `json:"brand_brief,omitempty"`
	ImageModelKey          string         `json:"image_model_key,omitempty"`
}

// Project is the SINGLE SOURCE OF TRUTH for every style/persona/theme/ecommerce
// setting. Tasks and plans inherit from it with precedence task > project; a task
// may override individual dimensions via Task.Overrides.
//
// DB column names are retained from the pre-refactor schema (gorm:"column:...")
// so this rename needs no data migration — only the Go field names and JSON keys
// change, which removes the `Style` overload from every code/API surface.
type Project struct {
	ID          string `gorm:"type:char(36);primaryKey" json:"id"`
	UserID      string `gorm:"type:char(36);index;not null" json:"user_id"`
	Platform    string `gorm:"type:varchar(20);not null" json:"platform"` // article, seednote, ecommerce
	Name        string `gorm:"type:varchar(100);not null" json:"name"`
	AvatarURL   string `gorm:"type:varchar(500)" json:"avatar_url"`
	ProfileURL  string `gorm:"type:varchar(500)" json:"profile_url"` // 平台主页链接
	Positioning string `gorm:"type:text" json:"positioning"`         // 项目定位
	Keywords    string `gorm:"type:text" json:"keywords"`            // 关键词
	// VisualStyle is the 图片视觉 (image visual style, free text) dimension.
	VisualStyle string `gorm:"column:style;type:text" json:"visual_style"`
	// WriterKey is the 写作者 YAML resource key (e.g. "dan-koe") for the app/writer
	// styled-writing pipeline. Orthogonal to VisualStyle/Theme.
	WriterKey string `gorm:"column:writing_style;type:varchar(100);default:''" json:"writer_key"`
	// Theme is the 排版 (layout/typesetting) resource key (e.g. "autumn-warm").
	Theme string `gorm:"type:varchar(50)" json:"theme"`
	// Byline is the 作者（署名）— the published author name, passed to publish_draft.
	// STRICTLY independent of WritingVoice (the writing-imitation dimension).
	Byline string `gorm:"column:author;type:varchar(50)" json:"byline"`
	// WritingVoice is the 写作笔迹 (free-text writing imitation: 框架/方式/笔迹).
	WritingVoice string `gorm:"column:author_style_intro;type:text" json:"writing_voice"`
	// PersonaAvatar is the optional 人设头像 (persona avatar, never part of the byline).
	PersonaAvatar string `gorm:"column:author_avatar_url;type:varchar(500)" json:"persona_avatar"`
	// CreatedFromTemplateID records the starter template used to create this project
	// (audit only). Templates are project-creation starters, imported once then
	// detached — this id does NOT enter the resolution chain.
	CreatedFromTemplateID string        `gorm:"column:template_id;type:char(36);default:''" json:"created_from_template_id"`
	ReferenceImageURL     string        `gorm:"type:varchar(500)" json:"reference_image_url"`    // 品牌视觉参考图 URL
	ImageRatio            string        `gorm:"type:varchar(10);default:''" json:"image_ratio"`  // 图片比例: "3:4", "1:1", "4:3", "16:9"
	MaxConcurrentTasks    int           `gorm:"type:int;default:10" json:"max_concurrent_tasks"` // 最大并发任务数
	Config                ProjectConfig `gorm:"type:json;serializer:json" json:"config"`         // 平台特有配置
	// EcommerceDefaults carries the reusable e-commerce defaults for platform=
	// "ecommerce" projects. Zero value for non-ecommerce projects.
	EcommerceDefaults datatypes.JSONType[EcommerceProjectDefaults] `gorm:"type:json" json:"ecommerce_defaults"`
	Status            string                                       `gorm:"type:varchar(20);default:active" json:"status"` // active, archived
	CreatedAt         time.Time                                    `json:"created_at"`
	UpdatedAt         time.Time                                    `json:"updated_at"`
}

func (Project) TableName() string { return "projects" }

// GetWechatAppID returns the WeChat App ID from config.
func (p *Project) GetWechatAppID() string {
	return p.Config.WechatAppID
}

// GetWechatSecret returns the WeChat Secret from config.
func (p *Project) GetWechatSecret() string {
	return p.Config.WechatSecret
}

// GetEnablePublishing returns whether auto-publishing is enabled for this project.
func (p *Project) GetEnablePublishing() bool {
	return p.Config.EnablePublishing
}

// GetRequirePublishApproval returns whether auto-published drafts must pass a
// human approval gate before touching the WeChat account. No-op unless
// GetEnablePublishing() is also true.
func (p *Project) GetRequirePublishApproval() bool {
	return p.Config.RequirePublishApproval
}
