package model

import "time"

// User represents a registered user.
type User struct {
	ID              string    `gorm:"type:char(36);primaryKey" json:"id"`
	Phone           string    `gorm:"type:varchar(20);uniqueIndex;nullable" json:"phone"`
	Email           string    `gorm:"type:varchar(100);index;nullable" json:"email"`
	Nickname        string    `gorm:"type:varchar(100)" json:"nickname"`
	Avatar          string    `gorm:"type:varchar(500)" json:"avatar"`
	Password        string    `gorm:"type:varchar(255);not null" json:"-"`
	OpenID          string    `gorm:"type:varchar(128);uniqueIndex;nullable" json:"-"`
	UnionID         string    `gorm:"type:varchar(128);index;nullable" json:"-"`
	Tier            Tier      `gorm:"type:varchar(20);default:free" json:"tier"`
	InviteCode      string    `gorm:"type:varchar(16);uniqueIndex" json:"invite_code"`
	InvitedBy       string    `gorm:"type:char(36);index;nullable" json:"-"`
	InviteCount     int       `gorm:"default:0" json:"invite_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	CreditsBalance  int       `gorm:"default:0" json:"credits_balance"`
}

// TableName returns the database table name for User.
func (User) TableName() string { return "users" }
