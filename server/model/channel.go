package model

import "time"

// Channel represents a user's platform account (e.g., a WeChat public account or Xiaohongshu account).
type Channel struct {
	ID             string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID         string    `gorm:"type:char(36);index;not null" json:"user_id"`
	Platform       string    `gorm:"type:varchar(20);not null" json:"platform"` // article, xls, rednote
	Name           string    `gorm:"type:varchar(100);not null" json:"name"`
	AvatarURL      string    `gorm:"type:varchar(500)" json:"avatar_url"`
	Description    string    `gorm:"type:text" json:"description"`
	WechatAppID    string    `gorm:"type:varchar(100)" json:"wechat_app_id"`
	WechatSecret   string    `gorm:"type:varchar(200)" json:"-"`
	Keywords       string    `gorm:"type:text" json:"keywords"`
	Positioning    string    `gorm:"type:text" json:"positioning"`
	Style          string    `gorm:"type:varchar(50)" json:"style"`
	Theme          string    `gorm:"type:varchar(50)" json:"theme"`
	Author         string    `gorm:"type:varchar(50)" json:"author"`
	Status         string    `gorm:"type:varchar(20);default:active" json:"status"` // active, archived
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (Channel) TableName() string { return "channels" }
