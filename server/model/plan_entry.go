package model

import (
	"time"

	"gorm.io/datatypes"
)

// PlanEntry is one independently executable Agent declaration inside a Plan.
// A Plan owns the shared schedule and prompt; entries freeze the execution
// identity and per-Agent inputs used to create tasks.
type PlanEntry struct {
	ID               string                             `gorm:"type:char(36);primaryKey" json:"id"`
	PlanID           string                             `gorm:"type:char(36);not null;index:idx_plan_entry_plan;uniqueIndex:idx_plan_entry_plan_agent,priority:1" json:"plan_id"`
	AgentID          string                             `gorm:"type:varchar(100);not null;index:idx_plan_entry_agent;uniqueIndex:idx_plan_entry_plan_agent,priority:2" json:"agent_id"`
	Channel          string                             `gorm:"type:varchar(50);not null" json:"channel"`
	TaskKind         string                             `gorm:"type:varchar(80);not null" json:"task_kind"`
	ExecutionProfile string                             `gorm:"type:varchar(40);not null" json:"execution_profile"`
	AgentInput       datatypes.JSONType[map[string]any] `gorm:"type:json" json:"agent_input"`
	ImageDefaults    datatypes.JSONType[map[string]any] `gorm:"type:json" json:"image_defaults"`
	Status           string                             `gorm:"type:varchar(20);default:active;not null" json:"status"`
	CreatedAt        time.Time                          `json:"created_at"`
	UpdatedAt        time.Time                          `json:"updated_at"`
}

func (PlanEntry) TableName() string { return "plan_entries" }

const (
	PlanEntryStatusActive = "active"
	PlanEntryStatusPaused = "paused"
	PlanEntryStatusFailed = "failed"
)

func (e *PlanEntry) SetAgentInput(input map[string]any) {
	e.AgentInput = datatypes.NewJSONType(input)
}

func (e *PlanEntry) SetImageDefaults(input map[string]any) {
	e.ImageDefaults = datatypes.NewJSONType(input)
}
