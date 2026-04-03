package model

// Task status constants.
const (
	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	TaskStatusCancelled = "cancelled"
)

// Plan status constants.
const (
	PlanStatusActive    = "active"
	PlanStatusPaused    = "paused"
	PlanStatusCompleted = "completed"
)

// Retry constants.
const (
	MaxRetries     = 3
	DefaultRetries = 3
)

// Config scope constants.
const (
	ScopeArticle = "article"
	ScopeXls     = "xls"
	ScopeRednote = "rednote"
)

// File role constants.
const (
	FileRoleImage    = "image"
	FileRoleCover    = "cover"
	FileRoleHTML     = "html"
	FileRoleMarkdown = "markdown"
	FileRoleOther    = "other"
)
