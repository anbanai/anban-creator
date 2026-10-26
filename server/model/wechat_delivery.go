package model

import "strings"

const (
	WechatDeliveryModeAutomatic    = "automatic"
	WechatDeliveryModeUserAssisted = "user_assisted"

	WechatDeliveryReasonMissingAppID         = "missing_app_id"
	WechatDeliveryReasonMissingAppSecret     = "missing_app_secret"
	WechatDeliveryReasonAutomaticDisabled    = "automatic_delivery_disabled"
	WechatDeliveryReasonCapabilityMissing    = "wechat_capability_unavailable"
	WechatDeliveryReasonPictureDraftDisabled = "picture_draft_not_requested"
)

// WechatDeliveryDecision freezes the credential-free WeChat delivery policy
// onto a task. Only credential presence is retained, never credential values.
type WechatDeliveryDecision struct {
	Mode               string   `json:"mode"`
	AutomaticEnabled   bool     `json:"automatic_enabled"`
	AppIDConfigured    bool     `json:"app_id_configured"`
	SecretConfigured   bool     `json:"secret_configured"`
	MayUploadMaterial  bool     `json:"may_upload_material"`
	MayCreateDraft     bool     `json:"may_create_draft"`
	MayFormallyPublish bool     `json:"may_formally_publish"`
	ReasonCodes        []string `json:"reason_codes,omitempty"`
}

func (d WechatDeliveryDecision) IsUserAssisted() bool {
	return d.Mode != WechatDeliveryModeAutomatic || !d.AutomaticEnabled || !d.AppIDConfigured || !d.SecretConfigured
}

// ResolveWechatDeliveryDecision only enables automatic side effects when all
// credential requirements and the explicit project switch are present.
func ResolveWechatDeliveryDecision(appID string, secretConfigured bool, automaticEnabled *bool) WechatDeliveryDecision {
	appIDConfigured := strings.TrimSpace(appID) != ""
	enabled := automaticEnabled != nil && *automaticEnabled
	decision := WechatDeliveryDecision{
		Mode: WechatDeliveryModeAutomatic, AutomaticEnabled: enabled,
		AppIDConfigured: appIDConfigured, SecretConfigured: secretConfigured,
		MayUploadMaterial: true,
	}
	if !appIDConfigured {
		decision.Mode = WechatDeliveryModeUserAssisted
		decision.ReasonCodes = append(decision.ReasonCodes, WechatDeliveryReasonMissingAppID)
	}
	if !secretConfigured {
		decision.Mode = WechatDeliveryModeUserAssisted
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

// Managed Agents may upload media; draft creation and formal publication stay
// owned by the Server finalizer.
func (d WechatDeliveryDecision) WithServerPublicationCapabilities() WechatDeliveryDecision {
	d.MayCreateDraft = false
	d.MayFormallyPublish = false
	return d
}
