package model

import "time"

// ChannelConfig holds platform-specific configuration stored as JSON.
type ChannelConfig struct {
	WechatAppID      string `json:"wechat_app_id,omitempty"`
	WechatSecret     string `json:"wechat_secret,omitempty"`
	EnablePublishing bool   `json:"enable_publishing"`
}

// Channel represents a user's platform account (e.g., a WeChat public account or Seednote account).
type Channel struct {
	ID                 string        `gorm:"type:char(36);primaryKey" json:"id"`
	UserID             string        `gorm:"type:char(36);index;not null" json:"user_id"`
	Platform           string        `gorm:"type:varchar(20);not null" json:"platform"` // article, seednote
	Name               string        `gorm:"type:varchar(100);not null" json:"name"`
	AvatarURL          string        `gorm:"type:varchar(500)" json:"avatar_url"`
	ProfileURL         string        `gorm:"type:varchar(500)" json:"profile_url"`            // 平台主页链接
	Positioning        string        `gorm:"type:text" json:"positioning"`                    // 账号定位
	Keywords           string        `gorm:"type:text" json:"keywords"`                       // 关键词
	Style              string        `gorm:"type:text" json:"style"`                            // 图片视觉风格（封面/配图方向，自由文本）
	WritingStyle       string        `gorm:"type:varchar(100);default:''" json:"writing_style"` // 写作风格（writer 资源 key，如 dan-koe）；与 Style/Theme 三维正交
	Theme              string        `gorm:"type:varchar(50)" json:"theme"`                     // 排版样式（theme 资源 key，如 autumn-warm）
	Author             string        `gorm:"type:varchar(50)" json:"author"`                  // 作者名（署名）
	AuthorStyleIntro   string        `gorm:"type:text" json:"author_style_intro"`             // 写作风格（自由文本 框架/写作方式/笔迹，公众号人设维度）
	AuthorAvatarURL    string        `gorm:"type:varchar(500)" json:"author_avatar_url"`      // 人设头像（可选，不入署名）
	TemplateID         string        `gorm:"type:char(36);default:''" json:"template_id"`     // 绑定的公众号模板（人设/排版随模板同步，运行时解析纳入优先级链）
	ReferenceImageURL  string        `gorm:"type:varchar(500)" json:"reference_image_url"`    // 品牌视觉参考图 URL
	ImageRatio         string        `gorm:"type:varchar(10);default:''" json:"image_ratio"`  // 图片比例: "3:4", "1:1", "4:3", "16:9"
	Layout             string        `gorm:"type:varchar(100);default:''" json:"layout"`      // 默认布局模块
	ImagePreset        string        `gorm:"type:varchar(50);default:''" json:"image_preset"` // 默认图片生成预设
	MaxConcurrentTasks int           `gorm:"type:int;default:10" json:"max_concurrent_tasks"` // 最大并发任务数
	Config             ChannelConfig `gorm:"type:json;serializer:json" json:"config"`         // 平台特有配置
	Status             string        `gorm:"type:varchar(20);default:active" json:"status"`   // active, archived
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
}

func (Channel) TableName() string { return "channels" }

// GetWechatAppID returns the WeChat App ID from config.
func (ch *Channel) GetWechatAppID() string {
	return ch.Config.WechatAppID
}

// GetWechatSecret returns the WeChat Secret from config.
func (ch *Channel) GetWechatSecret() string {
	return ch.Config.WechatSecret
}

// GetEnablePublishing returns whether auto-publishing is enabled for this channel.
func (ch *Channel) GetEnablePublishing() bool {
	return ch.Config.EnablePublishing
}
