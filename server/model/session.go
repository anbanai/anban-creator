package model

import "time"

// LoginSession stores an active login session (JWT tokens).
type LoginSession struct {
	ID           string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID       string    `gorm:"type:char(36);index;not null" json:"user_id"`
	Token        string    `gorm:"type:text;not null" json:"-"`
	RefreshToken string    `gorm:"type:text;not null" json:"-"`
	ExpiresAt    time.Time `gorm:"index;not null" json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
	User         User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName returns the database table name for LoginSession.
func (LoginSession) TableName() string { return "login_sessions" }
