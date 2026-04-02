package model

import "time"

// UserConfig stores per-scope configuration for a user (e.g. WeChat credentials per content type).
type UserConfig struct {
	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID         string    `gorm:"type:char(36);index;not null" json:"user_id"`
	Scope          string    `gorm:"type:varchar(20);not null" json:"scope"` // article, xls, rednote
	WechatAppID    string    `gorm:"type:varchar(100)" json:"wechat_app_id"`
	WechatSecret   string    `gorm:"type:varchar(200)" json:"wechat_secret"`
	Name           string    `gorm:"type:varchar(100)" json:"name"`
	Keywords       string    `gorm:"type:text" json:"keywords"`
	Positioning    string    `gorm:"type:text" json:"positioning"`
	Style          string    `gorm:"type:varchar(50)" json:"style"`
	Theme          string    `gorm:"type:varchar(50)" json:"theme"`
	Author         string    `gorm:"type:varchar(50)" json:"author"`
	ImageAPIConfig string    `gorm:"type:json" json:"image_api_config"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName returns the database table name for UserConfig.
func (UserConfig) TableName() string { return "user_configs" }
