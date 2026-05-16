package model

// PlatformFieldConfig describes a single form field for a platform.
type PlatformFieldConfig struct {
	Key         string `json:"key"`          // field name (matches Channel struct / request JSON)
	Label       string `json:"label"`        // Chinese label
	Placeholder string `json:"placeholder"`  // placeholder text
	Required    bool   `json:"required"`     // whether the field is required
	Type        string `json:"type"`         // "text", "password", "url", "textarea"
	Group       string `json:"group"`        // "basic", "credentials", "content", "advanced"
	AutoFetched bool   `json:"auto_fetched"` // can be auto-fetched from platform
}

// PlatformConfig describes a platform's form and behavior configuration.
type PlatformConfig struct {
	ID                 string                `json:"id"`
	Label              string                `json:"label"`
	BadgeVariant       string                `json:"badge_variant"`
	SupportsPublishing bool                  `json:"supports_publishing"`
	SupportsAutoFetch  bool                  `json:"supports_auto_fetch"`
	ProfileURLPattern  string                `json:"profile_url_pattern"`
	DefaultImageRatio  string                `json:"default_image_ratio"`
	Fields             []PlatformFieldConfig `json:"fields"`
}

// PlatformConfigs defines all supported platforms and their form configurations.
var PlatformConfigs = map[string]*PlatformConfig{
	PlatformArticle: {
		ID:                 PlatformArticle,
		Label:              "公众号",
		BadgeVariant:       "success",
		SupportsPublishing: true,
		SupportsAutoFetch:  false,
		ProfileURLPattern:  `^https?://mp\.weixin\.qq\.com`,
		DefaultImageRatio:  "16:9",
		Fields: []PlatformFieldConfig{
			{Key: "name", Label: "频道名称", Placeholder: "例如 我的科技博客", Required: true, Type: "text", Group: "basic"},
			{Key: "avatar_url", Label: "头像", Placeholder: "自动获取或手动填写", Type: "url", Group: "basic", AutoFetched: true},
			{Key: "positioning", Label: "账号定位", Placeholder: "例如 面向开发者的实用 AI 教程", Type: "textarea", Group: "basic", AutoFetched: true},
			{Key: "wechat_app_id", Label: "微信 AppID", Placeholder: "wx...", Type: "text", Group: "credentials"},
			{Key: "wechat_secret", Label: "微信 AppSecret", Placeholder: "创建后不可查看", Type: "password", Group: "credentials"},
			{Key: "keywords", Label: "关键词", Placeholder: "例如 科技, AI, 软件工程", Type: "textarea", Group: "advanced"},
			{Key: "style", Label: "写作风格", Placeholder: "例如 casual-science, dan-koe", Type: "text", Group: "advanced"},
			{Key: "theme", Label: "主题", Placeholder: "例如 autumn-warm, spring-fresh", Type: "text", Group: "advanced"},
			{Key: "author", Label: "作者名", Placeholder: "例如 张三", Type: "text", Group: "advanced"},
			{Key: "reference_image_url", Label: "品牌视觉参考图", Placeholder: "粘贴图片 URL（支持 JPG, PNG）", Type: "url", Group: "advanced"},
			{Key: "image_ratio", Label: "图片比例", Placeholder: "16:9（公众号默认）", Type: "select", Group: "advanced"},
		},
	},
	PlatformSeednote: {
		ID:                 PlatformSeednote,
		Label:              "种草笔记",
		BadgeVariant:       "danger",
		SupportsPublishing: false,
		SupportsAutoFetch:  true,
		ProfileURLPattern:  `^https?://((m\.|www\.)?xiaohongshu\.com|xhslink\.com)/`,
		DefaultImageRatio:  "3:4",
		Fields: []PlatformFieldConfig{
			{Key: "profile_url", Label: "种草笔记主页", Placeholder: "粘贴种草笔记主页链接或分享文本...", Type: "textarea", Group: "basic"},
			{Key: "name", Label: "频道名称", Placeholder: "自动获取", Type: "text", Group: "basic", AutoFetched: true},
			{Key: "avatar_url", Label: "头像", Placeholder: "自动获取", Type: "url", Group: "basic", AutoFetched: true},
			{Key: "positioning", Label: "账号定位", Placeholder: "自动获取", Type: "textarea", Group: "basic", AutoFetched: true},
			{Key: "keywords", Label: "关键词", Placeholder: "例如 美妆, 时尚, 生活方式", Type: "textarea", Group: "advanced"},
			{Key: "style", Label: "视觉风格", Placeholder: "例如 手绘感，暖色调，小清新，治愈系水彩插画风格", Type: "textarea", Group: "advanced"},
			{Key: "theme", Label: "主题", Placeholder: "例如 autumn-warm, spring-fresh", Type: "text", Group: "advanced"},
			{Key: "author", Label: "作者名", Placeholder: "例如 张三", Type: "text", Group: "advanced"},
			{Key: "reference_image_url", Label: "品牌视觉参考图", Placeholder: "粘贴图片 URL（支持 JPG, PNG）", Type: "url", Group: "advanced"},
			{Key: "image_ratio", Label: "图片比例", Placeholder: "3:4（种草笔记默认）", Type: "select", Group: "advanced"},
		},
	},
}

// GetPlatformConfig returns the config for a given platform, or nil if not found.
func GetPlatformConfig(platform string) *PlatformConfig {
	return PlatformConfigs[platform]
}

// GetAllPlatformConfigs returns a slice of all platform configs in deterministic order.
func GetAllPlatformConfigs() []*PlatformConfig {
	order := []string{PlatformSeednote, PlatformArticle}
	configs := make([]*PlatformConfig, 0, len(order))
	for _, key := range order {
		if pc, ok := PlatformConfigs[key]; ok {
			configs = append(configs, pc)
		}
	}
	return configs
}
