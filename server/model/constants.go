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
	MaxRetries          = 3
	DefaultRetries      = 3
	MaxRateLimitRetries = 5
)

// Config scope constants.
const (
	ScopeArticle = "article"
	ScopeSeednote = "seednote"
)

// File role constants.
const (
	FileRoleImage         = "image"
	FileRoleCover         = "cover"
	FileRoleHTML          = "html"
	FileRoleMarkdown      = "markdown"
	FileRoleVideo         = "video"
	FileRoleOther         = "other"
	FileRoleTopic         = "topic"
	FileRoleOutline       = "outline"
	FileRoleDraft         = "draft"
	FileRoleFinalMarkdown = "final_markdown"
	FileRoleImageManifest = "image_manifest"
	FileRoleDraftPackage  = "draft_package"
	FileRoleReview        = "review"
)

// Channel status constants.
const (
	ChannelStatusActive   = "active"
	ChannelStatusArchived = "archived"
)

// Platform constants.
const (
	PlatformArticle = "article"
	PlatformSeednote = "seednote"
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

// Per-operation credit type constants (for MCP tool billing).
const (
	CreditTypeImageGen      = "image_gen"
	CreditTypeArticleWrite  = "article_write"
	CreditTypeConvert       = "convert"
	CreditTypeHumanize      = "humanize"
	CreditTypeTopicResearch = "topic_research"
	CreditTypeSEO           = "seo"
	CreditTypeOutline       = "outline"
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
	TemplateTypePoster  = "poster"
	TemplateTypeSeednote = "seednote"
	TemplateTypeArticle = "article"
)

// Viral analysis source type constants.
const (
	ViralAnalysisSourceNote    = "note"
	ViralAnalysisSourceProfile = "profile"
)
