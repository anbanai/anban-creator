package model

import "slices"

// Task status constants.
const (
	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	TaskStatusCancelled = "cancelled"
)

// TerminalTaskStatuses is the set of task statuses that signal the end of
// execution. UIs, SSE handlers, and cleanup queries should treat all of these
// as final.
var TerminalTaskStatuses = []string{
	TaskStatusCompleted,
	TaskStatusFailed,
	TaskStatusCancelled,
}

// IsTerminalTaskStatus reports whether s is a terminal task status.
func IsTerminalTaskStatus(s string) bool {
	return slices.Contains(TerminalTaskStatuses, s)
}

// Publish-approval state constants (Batch 4A). PublishApprovalStateEmpty means
// the task never entered the gate (the common case: project does not require
// approval, or the task is not an auto-publishable article).
const (
	PublishApprovalStateEmpty    = ""
	PublishApprovalStatePending  = "pending"
	PublishApprovalStateApproved = "approved"
	PublishApprovalStateRejected = "rejected"
)

// Plan status constants.
const (
	PlanStatusActive    = "active"
	PlanStatusPaused    = "paused"
	PlanStatusCompleted = "completed"
)

// Retry constants.
const (
	MaxRetries          = 3
	DefaultRetries      = 3
	MaxRateLimitRetries = 5
)

// Config scope constants.
const (
	ScopeArticle   = "article"
	ScopeSeednote  = "seednote"
	ScopeEcommerce = "ecommerce"
)

// File role constants.
const (
	FileRoleImage         = "image"
	FileRoleCover         = "cover"
	FileRoleHTML          = "html"
	FileRoleMarkdown      = "markdown"
	FileRoleOther         = "other"
	FileRoleTopic         = "topic"
	FileRoleOutline       = "outline"
	FileRoleDraft         = "draft"
	FileRoleFinalMarkdown = "final_markdown"
	FileRoleImageManifest = "image_manifest"
	FileRoleDraftPackage  = "draft_package"
	FileRoleReview        = "review"
)

// Project status constants.
const (
	ProjectStatusActive   = "active"
	ProjectStatusArchived = "archived"
)

// Platform constants.
const (
	PlatformArticle   = "article"
	PlatformSeednote  = "seednote"
	PlatformEcommerce = "ecommerce"
)

// ValidImageRatios is the set of allowed image aspect ratios.
var ValidImageRatios = map[string]bool{
	"3:4":  true,
	"1:1":  true,
	"4:3":  true,
	"16:9": true,
}

// DefaultImageRatio returns the default image ratio for a platform.
func DefaultImageRatio(platform string) string {
	if platform == PlatformArticle {
		return "16:9"
	}
	return "3:4"
}

// Credit transaction type constants.
const (
	CreditTypeSignIn        = "sign_in"
	CreditTypeTaskDeduct    = "task_deduct"
	CreditTypeTaskRefund    = "task_refund"
	CreditTypeAdminGrant    = "admin_grant"
	CreditTypeRegisterBonus = "register_bonus"
	CreditTypeInviteReward  = "invite_reward"
)

// User tier type.
type Tier string

const (
	TierFree       Tier = "free"
	TierPro        Tier = "pro"
	TierEnterprise Tier = "enterprise"
)

// ValidTiers is the set of all valid tier values.
var ValidTiers = map[Tier]bool{
	TierFree:       true,
	TierPro:        true,
	TierEnterprise: true,
}

// TierMaxConcurrentTasks maps each tier to its maximum concurrent tasks limit.
var TierMaxConcurrentTasks = map[Tier]int{
	TierFree:       2,
	TierPro:        5,
	TierEnterprise: 10,
}

// GetTierMaxConcurrentTasks returns the max concurrent tasks for a given tier.
// Returns the free tier default if the tier is unknown.
func GetTierMaxConcurrentTasks(tier Tier) int {
	if v, ok := TierMaxConcurrentTasks[tier]; ok {
		return v
	}
	return TierMaxConcurrentTasks[TierFree]
}

// ResolveTier normalizes an empty or unknown tier to TierFree.
func ResolveTier(tier Tier) Tier {
	if tier == "" || !ValidTiers[tier] {
		return TierFree
	}
	return tier
}

// Image model key constants stored on Task/Plan.ImageModelKey.
//
// ImageModelKeySystemDefault ("") means: use the server default image provider/model.
// ImageModelKeyCustom ("custom") means: use the user's per-account model-config
// override (only allowed for Enterprise tier).
// Any other value must match an ImageModelPreset.Key configured on the server.
const (
	ImageModelKeySystemDefault = ""
	ImageModelKeyCustom        = "custom"
)

// TierRank returns the ordinal rank of a tier for privilege comparison
// (higher = more privileged). Unknown tiers map to free.
func TierRank(t Tier) int {
	switch t {
	case TierFree:
		return 0
	case TierPro:
		return 1
	case TierEnterprise:
		return 2
	}
	return 0
}

// TierSatisfies reports whether userTier meets or exceeds requiredTier.
// Used to gate per-tier image model selection.
func TierSatisfies(userTier, requiredTier Tier) bool {
	return TierRank(userTier) >= TierRank(requiredTier)
}

// NormalizeTier converts a raw string (e.g. from config or JSON) into a valid
// Tier. Invalid values default to TierFree.
func NormalizeTier(s string) Tier {
	t := Tier(s)
	if !ValidTiers[t] {
		return TierFree
	}
	return t
}

// Per-operation credit type constants (for MCP tool billing).
const (
	CreditTypeImageGen         = "image_gen"
	CreditTypeArticleWrite     = "article_write"
	CreditTypeConvert          = "convert"
	CreditTypeHumanize         = "humanize"
	CreditTypeTopicResearch    = "topic_research"
	CreditTypeSEO              = "seo"
	CreditTypeOutline          = "outline"
	CreditTypePosterGeneration = "poster_generation"
	CreditTypeViralAnalysis    = "viral_analysis"
)

// Viral analysis status constants.
const (
	ViralAnalysisStatusPending   = "pending"
	ViralAnalysisStatusAnalyzing = "analyzing"
	ViralAnalysisStatusCompleted = "completed"
	ViralAnalysisStatusFailed    = "failed"
)

// Poster task status constants.
const (
	PosterTaskStatusDrafting   = "drafting"
	PosterTaskStatusGenerating = "generating"
	PosterTaskStatusCompleted  = "completed"
	PosterTaskStatusFailed     = "failed"
)

// Template type constants.
const (
	TemplateTypePoster   = "poster"
	TemplateTypeSeednote = "seednote"
	TemplateTypeArticle  = "article"
)

// Viral analysis source type constants.
const (
	ViralAnalysisSourceNote    = "note"
	ViralAnalysisSourceProfile = "profile"
)
