package model

import (
	"time"

	"gorm.io/datatypes"
)

// ProjectAgentConfig stores the project-specific settings for one Agent Pack.
// AgentID is deliberately part of the row identity so a project can configure
// several independent Agents without coupling the project to one channel.
type ProjectAgentConfig struct {
	ID        string                             `gorm:"type:char(36);primaryKey" json:"id"`
	ProjectID string                             `gorm:"type:char(36);not null;index;uniqueIndex:idx_project_agent_config" json:"project_id"`
	AgentID   string                             `gorm:"type:varchar(100);not null;uniqueIndex:idx_project_agent_config" json:"agent_id"`
	Config    datatypes.JSONType[map[string]any] `gorm:"type:json;serializer:json" json:"config"`
	CreatedAt time.Time                          `json:"created_at"`
	UpdatedAt time.Time                          `json:"updated_at"`
}

func (ProjectAgentConfig) TableName() string { return "project_agent_configs" }
