package model

import "strings"

const (
	WechatDeliveryModeAutomatic     = "automatic" // legacy alias; normalized to draft
	WechatDeliveryModeDraft         = "draft"
	WechatDeliveryModeDirectPublish = "direct_publish"
	WechatDeliveryModeUserAssisted  = "user_assisted"

	WechatDeliveryReasonMissingAppID         = "missing_app_id"
	WechatDeliveryReasonMissingAppSecret     = "missing_app_secret"
	WechatDeliveryReasonAutomaticDisabled    = "automatic_delivery_disabled"
	WechatDeliveryReasonCapabilityMissing    = "wechat_capability_unavailable"
	WechatDeliveryReasonPictureDraftDisabled = "picture_draft_not_requested"
)

// WechatDeliveryDecision freezes the credential-free WeChat delivery policy
// onto a task. Only credential presence is retained, never credential values.
type WechatDeliveryDecision struct {
	Mode                    string   `json:"mode"`
	AutomaticEnabled        bool     `json:"automatic_enabled"`
	OpenPlatformConnected   bool     `json:"open_platform_connected"`
	AppIDConfigured         bool     `json:"app_id_configured"`
	SecretConfigured        bool     `json:"secret_configured"`
	MayUploadMaterial       bool     `json:"may_upload_material"`
	MayCreateDraft          bool     `json:"may_create_draft"`
	MayFormallyPublish      bool     `json:"may_formally_publish"`
	CanCreateDraft          bool     `json:"can_create_draft"`
	CanFormallyPublish      bool     `json:"can_formally_publish"`
	CanReadArticleAnalytics bool     `json:"can_read_article_analytics"`
	CanReadAccountAnalytics bool     `json:"can_read_account_analytics"`
	ReasonCodes             []string `json:"reason_codes,omitempty"`
}

type WechatDeliveryCapabilities struct {
	CanCreateDraft          bool
	CanFormallyPublish      bool
	CanReadArticleAnalytics bool
	CanReadAccountAnalytics bool
}

// NormalizeWechatDeliveryMode translates the pre-Open-Platform boolean into
// the explicit delivery contract. Empty/legacy automatic settings remain
// backward compatible while direct_publish is opt-in.
func NormalizeWechatDeliveryMode(mode string, automaticEnabled *bool) string {
	switch strings.TrimSpace(mode) {
	case WechatDeliveryModeDraft, WechatDeliveryModeDirectPublish, WechatDeliveryModeUserAssisted:
		return strings.TrimSpace(mode)
	case WechatDeliveryModeAutomatic:
		// Fall through to the legacy boolean below.
	}
	if automaticEnabled != nil && *automaticEnabled {
		return WechatDeliveryModeDraft
	}
	return WechatDeliveryModeUserAssisted
}

func (d WechatDeliveryDecision) IsUserAssisted() bool {
	if d.OpenPlatformConnected {
		return (d.Mode != WechatDeliveryModeDraft && d.Mode != WechatDeliveryModeDirectPublish) || !d.AutomaticEnabled
	}
	return (d.Mode != WechatDeliveryModeAutomatic && d.Mode != WechatDeliveryModeDraft && d.Mode != WechatDeliveryModeDirectPublish) || !d.AutomaticEnabled || !d.AppIDConfigured || !d.SecretConfigured
}

// ResolveWechatDeliveryDecision only enables automatic side effects when all
// credential requirements and the explicit project switch are present.
func ResolveWechatDeliveryDecision(appID string, secretConfigured bool, automaticEnabled *bool) WechatDeliveryDecision {
	appIDConfigured := strings.TrimSpace(appID) != ""
	enabled := automaticEnabled != nil && *automaticEnabled
	mode := WechatDeliveryModeUserAssisted
	if enabled {
		mode = WechatDeliveryModeDraft
	}
	decision := WechatDeliveryDecision{
		Mode: mode, AutomaticEnabled: enabled,
		AppIDConfigured: appIDConfigured, SecretConfigured: secretConfigured,
		MayUploadMaterial: true, CanCreateDraft: appIDConfigured && secretConfigured,
		CanFormallyPublish: appIDConfigured && secretConfigured,
	}
	if !appIDConfigured {
		decision.Mode = WechatDeliveryModeUserAssisted
		decision.CanCreateDraft = false
		decision.CanFormallyPublish = false
		decision.ReasonCodes = append(decision.ReasonCodes, WechatDeliveryReasonMissingAppID)
	}
	if !secretConfigured {
		decision.Mode = WechatDeliveryModeUserAssisted
		decision.CanCreateDraft = false
		decision.CanFormallyPublish = false
		decision.ReasonCodes = append(decision.ReasonCodes, WechatDeliveryReasonMissingAppSecret)
	}
	if !enabled {
		decision.Mode = WechatDeliveryModeUserAssisted
		decision.ReasonCodes = append(decision.ReasonCodes, WechatDeliveryReasonAutomaticDisabled)
	}
	if decision.IsUserAssisted() {
		decision.MayUploadMaterial = false
	}
	return decision
}

// ResolveWechatDeliveryDecisionWithOpenPlatform selects the authorized
// authorizer account as the credential source while keeping legacy credentials
// as a separate fallback for projects that have never linked Open Platform.
func ResolveWechatDeliveryDecisionWithOpenPlatform(appID string, connected bool, capabilities WechatDeliveryCapabilities, automaticEnabled *bool, requestedMode ...string) WechatDeliveryDecision {
	if !connected {
		return ResolveWechatDeliveryDecision(appID, false, automaticEnabled)
	}
	enabled := automaticEnabled != nil && *automaticEnabled
	mode := WechatDeliveryModeDraft
	if len(requestedMode) > 0 {
		mode = NormalizeWechatDeliveryMode(requestedMode[0], automaticEnabled)
	} else if !enabled {
		mode = WechatDeliveryModeUserAssisted
	}
	decision := WechatDeliveryDecision{
		Mode: mode, AutomaticEnabled: enabled,
		OpenPlatformConnected: true, AppIDConfigured: strings.TrimSpace(appID) != "",
		MayUploadMaterial:       enabled && capabilities.CanCreateDraft,
		CanCreateDraft:          capabilities.CanCreateDraft,
		CanFormallyPublish:      capabilities.CanCreateDraft && capabilities.CanFormallyPublish,
		CanReadArticleAnalytics: capabilities.CanReadArticleAnalytics,
		CanReadAccountAnalytics: capabilities.CanReadAccountAnalytics,
	}
	if !capabilities.CanCreateDraft {
		decision.Mode = WechatDeliveryModeUserAssisted
		decision.ReasonCodes = append(decision.ReasonCodes, WechatDeliveryReasonCapabilityMissing)
	} else if !enabled || mode == WechatDeliveryModeUserAssisted {
		decision.Mode = WechatDeliveryModeUserAssisted
		decision.ReasonCodes = append(decision.ReasonCodes, WechatDeliveryReasonAutomaticDisabled)
	}
	if decision.IsUserAssisted() {
		decision.MayUploadMaterial = false
	}
	return decision
}

// Managed Agents may upload media; draft creation and formal publication stay
// owned by the Server finalizer.
func (d WechatDeliveryDecision) WithServerPublicationCapabilities() WechatDeliveryDecision {
	d.MayCreateDraft = false
	d.MayFormallyPublish = false
	return d
}
