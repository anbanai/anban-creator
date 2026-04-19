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
	Fields             []PlatformFieldConfig `json:"fields"`
}

// PlatformConfigs defines all supported platforms and their form configurations.
var PlatformConfigs = map[string]*PlatformConfig{
	PlatformArticle: {
		ID:                 PlatformArticle,
		Label:              "公众号",
		BadgeVariant:       "success",
		SupportsPublishing: true,
		SupportsAutoFetch:  true,
		ProfileURLPattern:  `^https?://mp\.weixin\.qq\.com`,
		Fields: []PlatformFieldConfig{
			{Key: "profile_url", Label: "公众号主页", Placeholder: "https://mp.weixin.qq.com/...", Type: "url", Group: "basic", AutoFetched: true},
			{Key: "name", Label: "频道名称", Placeholder: "e.g. 我的科技博客", Required: true, Type: "text", Group: "basic"},
			{Key: "avatar_url", Label: "头像", Placeholder: "自动获取或手动填写", Type: "url", Group: "basic", AutoFetched: true},
			{Key: "positioning", Label: "账号定位", Placeholder: "e.g. 面向开发者的实用 AI 教程", Type: "textarea", Group: "basic", AutoFetched: true},
			{Key: "wechat_app_id", Label: "WeChat App ID", Placeholder: "wx...", Required: true, Type: "text", Group: "credentials"},
			{Key: "wechat_secret", Label: "WeChat App Secret", Placeholder: "创建后不可查看", Required: true, Type: "password", Group: "credentials"},
			{Key: "keywords", Label: "关键词", Placeholder: "e.g. 科技, AI, 软件工程", Type: "textarea", Group: "advanced"},
			{Key: "style", Label: "写作风格", Placeholder: "e.g. casual-science, dan-koe", Type: "text", Group: "advanced"},
			{Key: "theme", Label: "主题", Placeholder: "e.g. autumn-warm, spring-fresh", Type: "text", Group: "advanced"},
			{Key: "author", Label: "作者名", Placeholder: "e.g. 张三", Type: "text", Group: "advanced"},
			{Key: "reference_image_url", Label: "品牌视觉参考图", Placeholder: "粘贴图片 URL（支持 JPG, PNG）", Type: "url", Group: "advanced"},
			{Key: "max_concurrent_tasks", Label: "最大并发任务数", Placeholder: "默认 10", Type: "number", Group: "advanced"},
		},
	},
	PlatformXLS: {
		ID:                 PlatformXLS,
		Label:              "小绿书",
		BadgeVariant:       "info",
		SupportsPublishing: true,
		SupportsAutoFetch:  true,
		ProfileURLPattern:  `^https?://mp\.weixin\.qq\.com`,
		Fields: []PlatformFieldConfig{
			{Key: "profile_url", Label: "小绿书主页", Placeholder: "https://mp.weixin.qq.com/...", Type: "url", Group: "basic", AutoFetched: true},
			{Key: "name", Label: "频道名称", Placeholder: "e.g. 我的小绿书", Required: true, Type: "text", Group: "basic"},
			{Key: "avatar_url", Label: "头像", Placeholder: "自动获取或手动填写", Type: "url", Group: "basic", AutoFetched: true},
			{Key: "positioning", Label: "账号定位", Placeholder: "e.g. 生活方式分享", Type: "textarea", Group: "basic", AutoFetched: true},
			{Key: "wechat_app_id", Label: "WeChat App ID", Placeholder: "wx...", Required: true, Type: "text", Group: "credentials"},
			{Key: "wechat_secret", Label: "WeChat App Secret", Placeholder: "创建后不可查看", Required: true, Type: "password", Group: "credentials"},
			{Key: "keywords", Label: "关键词", Placeholder: "e.g. 生活方式, 美食, 旅行", Type: "textarea", Group: "advanced"},
			{Key: "style", Label: "写作风格", Placeholder: "e.g. casual-science, dan-koe", Type: "text", Group: "advanced"},
			{Key: "theme", Label: "主题", Placeholder: "e.g. autumn-warm, spring-fresh", Type: "text", Group: "advanced"},
			{Key: "author", Label: "作者名", Placeholder: "e.g. 张三", Type: "text", Group: "advanced"},
			{Key: "reference_image_url", Label: "品牌视觉参考图", Placeholder: "粘贴图片 URL（支持 JPG, PNG）", Type: "url", Group: "advanced"},
			{Key: "max_concurrent_tasks", Label: "最大并发任务数", Placeholder: "默认 10", Type: "number", Group: "advanced"},
		},
	},
	PlatformRednote: {
		ID:                 PlatformRednote,
		Label:              "小红书",
		BadgeVariant:       "danger",
		SupportsPublishing: false,
		SupportsAutoFetch:  true,
		ProfileURLPattern:  `^https?://(www\.)?xiaohongshu\.com/user/profile/`,
		Fields: []PlatformFieldConfig{
			{Key: "profile_url", Label: "小红书主页", Placeholder: "粘贴小红书主页链接...", Type: "url", Group: "basic"},
			{Key: "name", Label: "频道名称", Placeholder: "自动获取", Type: "text", Group: "basic", AutoFetched: true},
			{Key: "avatar_url", Label: "头像", Placeholder: "自动获取", Type: "url", Group: "basic", AutoFetched: true},
			{Key: "positioning", Label: "账号定位", Placeholder: "自动获取", Type: "textarea", Group: "basic", AutoFetched: true},
			{Key: "keywords", Label: "关键词", Placeholder: "e.g. 美妆, 时尚, 生活方式", Type: "textarea", Group: "advanced"},
			{Key: "style", Label: "视觉风格", Placeholder: "e.g. 手绘感，暖色调，小清新，治愈系水彩插画风格", Type: "textarea", Group: "advanced"},
			{Key: "theme", Label: "主题", Placeholder: "e.g. autumn-warm, spring-fresh", Type: "text", Group: "advanced"},
			{Key: "author", Label: "作者名", Placeholder: "e.g. 张三", Type: "text", Group: "advanced"},
			{Key: "reference_image_url", Label: "品牌视觉参考图", Placeholder: "粘贴图片 URL（支持 JPG, PNG）", Type: "url", Group: "advanced"},
			{Key: "max_concurrent_tasks", Label: "最大并发任务数", Placeholder: "默认 10", Type: "number", Group: "advanced"},
		},
	},
}

// GetPlatformConfig returns the config for a given platform, or nil if not found.
func GetPlatformConfig(platform string) *PlatformConfig {
	return PlatformConfigs[platform]
}

// GetAllPlatformConfigs returns a slice of all platform configs in deterministic order.
func GetAllPlatformConfigs() []*PlatformConfig {
	order := []string{PlatformRednote, PlatformArticle, PlatformXLS}
	configs := make([]*PlatformConfig, 0, len(order))
	for _, key := range order {
		if pc, ok := PlatformConfigs[key]; ok {
			configs = append(configs, pc)
		}
	}
	return configs
}
