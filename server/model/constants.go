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
	ScopeWechat    = "wechat"
	ScopeSeednote  = "seednote"
	ScopeMoments   = "moments"
	ScopeEcommerce = "ecommerce"
	ScopeMontage   = "montage"
)

// Agent IDs are stable execution identities. An Agent owns exactly one output
// channel; task kind selects the workflow exposed by that Agent.
const (
	AgentIDArticle       = "wechat-article"
	AgentIDSeednote      = "seednote"
	AgentIDWechatPicture = "wechat-picture"
	AgentIDChannelsVideo = "channels-video"
	AgentIDHypit         = "hypit"
	AgentIDFeedback      = "feedback"
	AgentIDProfile       = "profile-analysis"
	AgentIDWhiteboard    = "whiteboard-animation"
)

// Channel constants. Keep these values provider-neutral; labels belong to the
// Studio and connector implementations.
const (
	ChannelArticle       = "wechat-article"
	ChannelSeednote      = "seednote"
	ChannelWechatPicture = "wechat-picture"
	ChannelChannelsVideo = "channels-video"
	ChannelHypit         = "hypit"
	ChannelMoments       = "moments"
	ChannelEcommerce     = "ecommerce"
	ChannelLiveSlicer    = "live-slicer"
	ChannelProfile       = "profile-analysis"
	ChannelMontage       = "montage"
	ChannelFeedback      = "feedback"
	ChannelWhiteboard    = "whiteboard-animation"
)

// Task kinds are workflow identities within one Agent/channel.
const (
	TaskKindContentGeneration = "content_generation"
	TaskKindViralAnalysis     = "viral_analysis"
	TaskKindProfileAnalysis   = "profile_analysis"
	TaskKindLiveSlicer        = "live_slicer"
	TaskKindFeedbackAnalysis  = "feedback_analysis"
)

// Managed task types that are not project platforms (legacy aliases retained
// for historical rows and migration readers).
const (
	TaskTypeLiveSlicer      = "live-slicer"
	TaskTypeViralAnalysis   = "viral_analysis"
	TaskTypeProfileAnalysis = "profile_analysis"
)

func IsChannel(value string) bool {
	switch value {
	case ChannelArticle, ChannelSeednote, ChannelWechatPicture, ChannelFeedback, ChannelProfile, ChannelWhiteboard:
		return true
	default:
		return false
	}
}

func IsAgentID(value string) bool {
	switch value {
	case AgentIDArticle, AgentIDSeednote, AgentIDWechatPicture, AgentIDFeedback, AgentIDProfile, AgentIDWhiteboard:
		return true
	default:
		return false
	}
}

func AgentChannel(agentID string) (string, bool) {
	switch agentID {
	case AgentIDArticle:
		return ChannelArticle, true
	case AgentIDSeednote:
		return ChannelSeednote, true
	case AgentIDWechatPicture:
		return ChannelWechatPicture, true
	case AgentIDFeedback:
		return ChannelFeedback, true
	case AgentIDWhiteboard:
		return ChannelWhiteboard, true
	default:
		return "", false
	}
}

// PlatformForChannel is a compatibility helper for image/connector settings
// that still use the legacy platform configuration registry internally. New
// product identity and routing must use Channel directly.
func PlatformForChannel(channel string) string {
	switch channel {
	case ChannelArticle, ChannelWechatPicture:
		return PlatformWechat
	case ChannelSeednote:
		return PlatformSeednote
	case ChannelWhiteboard:
		return PlatformWhiteboardAnimation
	default:
		return ""
	}
}

func SupportedImageRatiosForChannel(channel string) []string {
	platform := PlatformForChannel(channel)
	if platform == "" {
		return nil
	}
	return SupportedImageRatios(platform)
}

func IsBusinessImageRatioAllowedForChannel(channel, ratio string) bool {
	platform := PlatformForChannel(channel)
	if platform == "" {
		return ratio == ImageRatioAuto
	}
	return IsBusinessImageRatioAllowed(platform, ratio)
}

func DefaultImageRatioForChannel(channel string) string {
	switch channel {
	case ChannelWechatPicture:
		return "3:4"
	case ChannelArticle:
		return "16:9"
	case ChannelSeednote:
		return "3:4"
	default:
		return "3:4"
	}
}

func IsTaskKind(value string) bool {
	switch value {
	case TaskKindContentGeneration, TaskKindViralAnalysis, TaskKindProfileAnalysis, TaskKindLiveSlicer,
		TaskTypeLiveSlicer, TaskKindFeedbackAnalysis, PlatformMoments, PlatformEcommerce, PlatformMontage, PlatformWhiteboardAnimation, PlatformHypit,
		TaskTypeWechatArticle, TaskTypeWechatPicture:
		return true
	default:
		return false
	}
}

// File role constants.
const (
	FileRoleImage         = "image"
	FileRoleVideo         = "video"
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
<<<<<<< HEAD
	PlatformArticle             = "article"
=======
	PlatformWechat              = "wechat"
	TaskTypeWechatArticle       = "wechat-article"
	TaskTypeWechatPicture       = "wechat-picture"
>>>>>>> codex/tasks-infinite-scroll-review
	PlatformSeednote            = "seednote"
	PlatformMoments             = "moments"
	PlatformEcommerce           = "ecommerce"
	PlatformMontage             = "montage"
	PlatformWhiteboardAnimation = "whiteboard-animation"
	PlatformHypit               = "hypit"
)

// IsProjectPlatform reports whether value is an explicitly implemented
// first-class project platform. Agent Pack bindings may reference these
// identities, but must not create new business identities by themselves.
func IsProjectPlatform(value string) bool {
	switch value {
<<<<<<< HEAD
	case PlatformArticle, PlatformSeednote, PlatformMoments, PlatformEcommerce, PlatformMontage, PlatformWhiteboardAnimation, PlatformHypit:
=======
	case PlatformWechat, PlatformSeednote, PlatformMoments, PlatformEcommerce, PlatformMontage, PlatformWhiteboardAnimation, PlatformHypit:
>>>>>>> codex/tasks-infinite-scroll-review
		return true
	default:
		return false
	}
}

// IsAdminOnlyProjectPlatform reports project types that are still in internal validation.
func IsAdminOnlyProjectPlatform(value string) bool {
	switch value {
	case PlatformMoments, PlatformEcommerce, PlatformHypit:
		return true
	default:
		return false
	}
}

// IsTaskType reports whether value is an explicitly implemented task type.
func IsTaskType(value string) bool {
	return value == TaskTypeWechatArticle || value == TaskTypeWechatPicture || IsProjectPlatform(value) || value == TaskTypeLiveSlicer || value == TaskTypeViralAnalysis || value == TaskTypeProfileAnalysis
}

func IsMontagePlatform(platform string) bool {
	return platform == PlatformMontage
}

// SupportsPortraitCover identifies workflows with an independently selected project portrait cover.
func SupportsPortraitCover(taskType string) bool {
	switch taskType {
	case TaskTypeWechatArticle, TaskTypeWechatPicture, PlatformSeednote, PlatformMontage, PlatformHypit:
		return true
	default:
		return false
	}
}

const ImageRatioAuto = "auto"

// ValidImageRatios is the union of business image ratios, including explicit auto selection.
var ValidImageRatios = map[string]bool{
	ImageRatioAuto: true,
	"9:16":         true,
	"3:4":          true,
	"1:1":          true,
	"4:3":          true,
	"16:9":         true,
}

const ValidImageRatioHint = "image_ratio must be auto or one of the platform supported_image_ratios"

// IsBusinessImageRatioAllowed validates a persisted task/project ratio against its business platform.
func IsBusinessImageRatioAllowed(platform, ratio string) bool {
	if ratio == ImageRatioAuto {
		return true
	}
	cfg := GetPlatformConfig(platform)
	if cfg == nil {
		return false
	}
	for _, allowed := range cfg.SupportedImageRatios {
		if ratio == allowed {
			return true
		}
	}
	return false
}

func SupportedImageRatios(platform string) []string {
	cfg := GetPlatformConfig(platform)
	if cfg == nil {
		return nil
	}
	return append([]string(nil), cfg.SupportedImageRatios...)
}

// DefaultImageRatio returns the default image ratio for a platform.
func DefaultImageRatio(platform string) string {
	if platform == TaskTypeWechatPicture {
		return "3:4"
	}
	if cfg := GetPlatformConfig(platform); cfg != nil && cfg.DefaultImageRatio != "" {
		return cfg.DefaultImageRatio
	}
	return "3:4"
}

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

// ImageCapabilityKeySystemDefault selects the configured default capability.
const ImageCapabilityKeySystemDefault = ""

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

// Provider-cost operation identities for understanding tools.
const (
	OperationImageUnderstanding = "image_understanding"
	OperationVideoUnderstanding = "video_understanding"
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

const (
	SeednoteTemplateCategoryProduct   = "好物种草"
	SeednoteTemplateCategoryBeauty    = "美妆护肤"
	SeednoteTemplateCategoryHealth    = "健康养生"
	SeednoteTemplateCategoryFood      = "美食生活"
	SeednoteTemplateCategoryHome      = "家居家装"
	SeednoteTemplateCategoryKnowledge = "知识科普"
)

var SeednoteTemplateCategories = [...]string{
	SeednoteTemplateCategoryProduct,
	SeednoteTemplateCategoryBeauty,
	SeednoteTemplateCategoryHealth,
	SeednoteTemplateCategoryFood,
	SeednoteTemplateCategoryHome,
	SeednoteTemplateCategoryKnowledge,
}

func IsSeednoteTemplateCategory(category string) bool {
	for _, allowed := range SeednoteTemplateCategories {
		if category == allowed {
			return true
		}
	}
	return false
}

// Viral analysis source type constants.
const (
	ViralAnalysisSourceNote    = "note"
	ViralAnalysisSourceProfile = "profile"
)
