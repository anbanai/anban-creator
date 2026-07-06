package model

import "time"

// User represents a registered user.
type User struct {
	ID             string    `gorm:"type:char(36);primaryKey" json:"id"`
	Email          string    `gorm:"type:varchar(100);uniqueIndex" json:"email"`
	Nickname       string    `gorm:"type:varchar(100)" json:"nickname"`
	Avatar         string    `gorm:"type:varchar(500)" json:"avatar"`
	Password       string    `gorm:"type:varchar(255);not null" json:"-"`
	OpenID         string    `gorm:"type:varchar(128);index" json:"-"`
	UnionID        string    `gorm:"type:varchar(128);index" json:"-"`
	Tier           Tier      `gorm:"type:varchar(20);default:free" json:"tier"`
	InviteCode     string    `gorm:"type:varchar(16);uniqueIndex" json:"invite_code"`
	InvitedBy      string    `gorm:"type:char(36);index;nullable" json:"-"`
	InviteCount    int       `gorm:"default:0" json:"invite_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	CreditsBalance int       `gorm:"default:0" json:"credits_balance"`
	// BillingMultiplier adjusts model-usage billing for account-specific margin.
	// Nil or non-positive values default to 1.0 in the credit service.
	BillingMultiplier *float64 `gorm:"type:decimal(8,4)" json:"billing_multiplier,omitempty"`
}

// TableName returns the database table name for User.
func (User) TableName() string { return "users" }
