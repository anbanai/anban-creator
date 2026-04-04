package model

// UserConfig is a legacy model retained only for data migration.
// New code should use Channel instead.
type UserConfig struct {
	ID             string `gorm:"primaryKey;size:64"`
	UserID         string `gorm:"size:64;index"`
	Scope          string `gorm:"size:32"`
	Name           string `gorm:"size:128"`
	WechatAppID    string `gorm:"column:wechat_app_id;size:64"`
	WechatSecret   string `gorm:"column:wechat_secret;size:128"`
	Keywords       string `gorm:"type:text"`
	Positioning    string `gorm:"type:text"`
	Style          string `gorm:"size:64"`
	Theme          string `gorm:"size:64"`
	Author         string `gorm:"size:128"`
}

func (UserConfig) TableName() string {
	return "user_configs"
}
