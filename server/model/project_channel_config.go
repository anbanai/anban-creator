package model

import (
	"time"

	"gorm.io/datatypes"
)

// ProjectChannelConfig stores one project's connection/configuration for one
// output channel. Credentials remain server-owned and are redacted by the
// project handlers before a response is serialized.
type ProjectChannelConfig struct {
	ID        string                             `gorm:"type:char(36);primaryKey" json:"id"`
	ProjectID string                             `gorm:"type:char(36);not null;index;uniqueIndex:idx_project_channel_config" json:"project_id"`
	Channel   string                             `gorm:"type:varchar(100);not null;uniqueIndex:idx_project_channel_config" json:"channel"`
	Config    datatypes.JSONType[map[string]any] `gorm:"type:json;serializer:json" json:"config"`
	CreatedAt time.Time                          `json:"created_at"`
	UpdatedAt time.Time                          `json:"updated_at"`
}

func (ProjectChannelConfig) TableName() string { return "project_channel_configs" }
