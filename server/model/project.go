package model

import (
	"time"

	"gorm.io/datatypes"
)

// ProjectConfig holds platform-specific configuration stored as JSON.
type ProjectConfig struct {
	WechatAppID       string `json:"wechat_app_id,omitempty"`
	WechatSecret      string `json:"wechat_secret,omitempty"`
	WechatPublishMode string `json:"wechat_publish_mode,omitempty"`
}

// EcommerceProjectDefaults holds the reusable e-commerce defaults for a project
// (platform="ecommerce"): deliverable modules + quantities, target platform,
// brand brief, and default image model key. Product photos stay per-task
// (Task.Ecommerce) since each task ships a distinct product set.
type EcommerceProjectDefaults struct {
	DefaultSelectedModules map[string]int `json:"default_selected_modules,omitempty"`
	TargetPlatform         string         `json:"target_platform,omitempty"`
	BrandBrief             string         `json:"brand_brief,omitempty"`
	ImageCapabilityKey     string         `json:"image_capability_key,omitempty"`
}

// SetEcommerceDefaults stores reusable e-commerce defaults into the
// EcommerceDefaults JSON column. Thin wrapper over datatypes.NewJSONType so
// handler/service call sites don't each need to import gorm.io/datatypes.
func (p *Project) SetEcommerceDefaults(ec EcommerceProjectDefaults) {
	p.EcommerceDefaults = datatypes.NewJSONType(ec)
}

// Project is the single source of truth for style/author/theme/e-commerce
// defaults. New tasks freeze these values into Task.ProjectSnapshot at creation;
// legacy Task.Overrides only exist so old rows can still resolve.
//
// DB column names use the current public contract directly. Explicitly marked
// legacy columns remain only where startup migrations still need them.
type Project struct {
	ID         string `gorm:"type:char(36);primaryKey" json:"id"`
	UserID     string `gorm:"type:char(36);index;not null" json:"user_id"`
	Platform   string `gorm:"type:varchar(20);not null" json:"platform"` // article, seednote, moments, ecommerce, montage
	Name       string `gorm:"type:varchar(100);not null" json:"name"`
	AvatarURL  string `gorm:"type:varchar(500)" json:"avatar_url"`
	ProfileURL string `gorm:"type:varchar(500)" json:"profile_url"` // 平台主页链接
	// Positioning is the legacy project-positioning column. New code writes and
	// reads Instructions; this column remains only for migration/backward reads.
	Positioning string `gorm:"type:text" json:"positioning,omitempty"`
	Keywords    string `gorm:"type:text" json:"keywords"` // 关键词
	// Instructions is the canonical project positioning. It is also rendered into
	// the task workspace CLAUDE.md under "## 项目定位" so agents receive the same
	// project context that Studio displays.
	Instructions string `gorm:"type:text" json:"instructions,omitempty"`
	// InstructionsSet marks whether an update request explicitly included
	// instructions, allowing empty string to mean "clear" without treating omitted
	// fields as clears. It is never persisted or serialized.
	InstructionsSet bool `gorm:"-" json:"-"`
	// VisualStyle is the 图片视觉 (image visual style, free text) dimension.
	VisualStyle string `gorm:"column:style;type:text" json:"visual_style"`
	// Writer is the 写作者 YAML resource key (e.g. "dan-koe") for the app/writer
	// styled-writing pipeline. Orthogonal to VisualStyle/Theme.
	Writer string `gorm:"type:varchar(100);default:''" json:"writer"`
	// Theme is the 排版 (layout/typesetting) resource key (e.g. "autumn-warm").
	Theme string `gorm:"type:varchar(50)" json:"theme"`
	// Author is the 作者（署名）— the published author name, passed to create_draft.
	Author                        string        `gorm:"column:author;type:varchar(50)" json:"author"`
	ReferenceImageAssetID         string        `gorm:"type:char(36);index" json:"-"`
	ReferenceImage                *AssetView    `gorm:"-" json:"reference_image,omitempty"`
	ReferenceImageSet             bool          `gorm:"-" json:"-"`
	PortraitReferenceImageAssetID string        `gorm:"type:char(36);index" json:"-"`
	PortraitReferenceImage        *AssetView    `gorm:"-" json:"portrait_reference_image,omitempty"`
	PortraitReferenceImageSet     bool          `gorm:"-" json:"-"`
	ImageRatio                    string        `gorm:"type:varchar(10);default:''" json:"image_ratio"`  // 图片比例: "3:4", "1:1", "4:3", "16:9"
	MaxConcurrentTasks            int           `gorm:"type:int;default:10" json:"max_concurrent_tasks"` // 最大并发任务数
	Config                        ProjectConfig `gorm:"type:json;serializer:json" json:"config"`         // 平台特有配置
	// EcommerceDefaults carries the reusable e-commerce defaults for platform=
	// "ecommerce" projects. Zero value for non-ecommerce projects.
	EcommerceDefaults    datatypes.JSONType[EcommerceProjectDefaults] `gorm:"type:json" json:"ecommerce_defaults"`
	EcommerceDefaultsSet bool                                         `gorm:"-" json:"-"`
	MontageDefaults      datatypes.JSONType[MontageDefaults]          `gorm:"type:json" json:"montage_defaults"`
	AgentConfig          datatypes.JSONType[map[string]any]           `gorm:"type:json" json:"agent_config"`
	AgentConfigSet       bool                                         `gorm:"-" json:"-"`
	MontageDefaultsSet   bool                                         `gorm:"-" json:"-"`
	Status               string                                       `gorm:"type:varchar(20);default:active" json:"status"` // active, archived
	DeletingAt           *time.Time                                   `gorm:"index" json:"-"`
	CreatedAt            time.Time                                    `json:"created_at"`
	UpdatedAt            time.Time                                    `json:"updated_at"`
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

// GetWechatPublishMode returns the selected lifecycle behavior. An omitted
// value is manual so newly created and zero-value projects never auto-publish.
func (p *Project) GetWechatPublishMode() string {
	if p == nil || p.Config.WechatPublishMode == "" {
		return WechatPublishModeManual
	}
	return p.Config.WechatPublishMode
}
