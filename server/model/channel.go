package model

import "time"

// ChannelConfig holds platform-specific configuration stored as JSON.
type ChannelConfig struct {
	WechatAppID  string `json:"wechat_app_id,omitempty"`
	WechatSecret string `json:"wechat_secret,omitempty"`
}

// Channel represents a user's platform account (e.g., a WeChat public account or Xiaohongshu account).
type Channel struct {
	ID          string        `gorm:"type:char(36);primaryKey" json:"id"`
	UserID      string        `gorm:"type:char(36);index;not null" json:"user_id"`
	Platform    string        `gorm:"type:varchar(20);not null" json:"platform"` // article, xls, rednote
	Name        string        `gorm:"type:varchar(100);not null" json:"name"`
	AvatarURL   string        `gorm:"type:varchar(500)" json:"avatar_url"`
	ProfileURL  string        `gorm:"type:varchar(500)" json:"profile_url"`    // 平台主页链接
	Positioning string        `gorm:"type:text" json:"positioning"`            // 账号定位
	Keywords    string        `gorm:"type:text" json:"keywords"`               // 关键词
	Style       string        `gorm:"type:text" json:"style"`                    // 写作风格 / 视觉风格（小红书用）
	Theme       string        `gorm:"type:varchar(50)" json:"theme"`           // 主题
	Author             string        `gorm:"type:varchar(50)" json:"author"`               // 作者名
	ReferenceImageURL  string        `gorm:"type:varchar(500)" json:"reference_image_url"` // 品牌视觉参考图 URL
	MaxConcurrentTasks int           `gorm:"type:int;default:10" json:"max_concurrent_tasks"` // 最大并发任务数
	Config             ChannelConfig `gorm:"type:json;serializer:json" json:"config"`       // 平台特有配置
	Status      string        `gorm:"type:varchar(20);default:active" json:"status"` // active, archived
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
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
