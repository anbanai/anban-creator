package model

import "time"

// Plan represents a scheduled content generation plan.
type Plan struct {
	ID          string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID      string     `gorm:"type:char(36);index;not null;index:idx_plan_user_channel,priority:1" json:"user_id"`
	ChannelID   string     `gorm:"type:char(36);index;index:idx_plan_user_channel,priority:2" json:"channel_id"`
	Type        string     `gorm:"type:varchar(20);not null" json:"type"` // rednote, article, xls
	Title       string     `gorm:"type:varchar(200)" json:"title"`
	Description string     `gorm:"type:text" json:"description"`
	CronExpr    string     `gorm:"type:varchar(100)" json:"cron_expr"`
	TopicHint   string     `gorm:"type:text" json:"topic_hint"`
	Status      string     `gorm:"type:varchar(20);default:active" json:"status"` // active, paused, completed
	NextRunAt   *time.Time `gorm:"index" json:"next_run_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TableName returns the database table name for Plan.
func (Plan) TableName() string { return "plans" }
