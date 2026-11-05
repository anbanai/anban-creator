package model

import "time"

// Read-only projections: usage belongs to a persona; outputs have their own lifecycle.
type TopicProjectUse struct {
	AssetID     string     `json:"-"`
	ProjectID   string     `json:"project_id"`
	ProjectName string     `json:"project_name"`
	Status      string     `json:"status"`
	TaskID      *string    `json:"task_id,omitempty"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
}
type TopicOutput struct {
	JobID     string `json:"job_id,omitempty"`
	Error     string `json:"error,omitempty"`
	AssetID   string `json:"-"`
	TaskID    string `json:"task_id,omitempty"`
	ProjectID string `json:"project_id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Title     string `json:"title"`
}
