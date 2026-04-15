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
	MaxRetries         = 3
	DefaultRetries     = 3
	MaxRateLimitRetries = 5
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

// Channel status constants.
const (
	ChannelStatusActive   = "active"
	ChannelStatusArchived = "archived"
)

// Platform constants.
const (
	PlatformArticle = "article"
	PlatformXLS     = "xls"
	PlatformRednote = "rednote"
)

// Credit transaction type constants.
const (
	CreditTypeSignIn     = "sign_in"
	CreditTypeTaskDeduct = "task_deduct"
	CreditTypeTaskRefund = "task_refund"
	CreditTypeAdminGrant = "admin_grant"
)

// Per-operation credit type constants (for MCP tool billing).
const (
	CreditTypeImageGen      = "image_gen"
	CreditTypeImageUpload   = "image_upload"
	CreditTypeArticleWrite  = "article_write"
	CreditTypeConvert       = "convert"
	CreditTypeHumanize      = "humanize"
	CreditTypeTopicResearch = "topic_research"
	CreditTypeSEO           = "seo"
	CreditTypeDraftPublish  = "draft_publish"
	CreditTypeOutline       = "outline"
)
