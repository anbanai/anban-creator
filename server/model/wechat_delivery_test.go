package model

import "testing"

func TestResolveWechatDeliveryDecision(t *testing.T) {
	tests := []struct {
		name        string
		appID       string
		secret      bool
		automatic   *bool
		wantMode    string
		wantReasons []string
	}{
		{name: "missing app id", secret: true, automatic: boolPtr(true), wantMode: WechatDeliveryModeUserAssisted, wantReasons: []string{WechatDeliveryReasonMissingAppID}},
		{name: "missing secret", appID: "wx-app", automatic: boolPtr(true), wantMode: WechatDeliveryModeUserAssisted, wantReasons: []string{WechatDeliveryReasonMissingAppSecret}},
		{name: "disabled", appID: "wx-app", secret: true, automatic: boolPtr(false), wantMode: WechatDeliveryModeUserAssisted, wantReasons: []string{WechatDeliveryReasonAutomaticDisabled}},
		{name: "missing switch", appID: "wx-app", secret: true, wantMode: WechatDeliveryModeUserAssisted, wantReasons: []string{WechatDeliveryReasonAutomaticDisabled}},
		{name: "automatic migrates to draft", appID: "wx-app", secret: true, automatic: boolPtr(true), wantMode: WechatDeliveryModeDraft},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveWechatDeliveryDecision(tt.appID, tt.secret, tt.automatic)
			if got.Mode != tt.wantMode || len(got.ReasonCodes) != len(tt.wantReasons) {
				t.Fatalf("decision = %+v, want mode %q reasons %#v", got, tt.wantMode, tt.wantReasons)
			}
			for i := range tt.wantReasons {
				if got.ReasonCodes[i] != tt.wantReasons[i] {
					t.Fatalf("reasons = %#v, want %#v", got.ReasonCodes, tt.wantReasons)
				}
			}
		})
	}
}

func TestWechatDeliveryDecisionFailsClosedForUnknownOrIncompleteModes(t *testing.T) {
	decisions := []WechatDeliveryDecision{
		{},
		{Mode: "automatic-ish", AutomaticEnabled: true, AppIDConfigured: true, SecretConfigured: true},
		{Mode: WechatDeliveryModeAutomatic, AppIDConfigured: true, SecretConfigured: true},
		{Mode: WechatDeliveryModeAutomatic, AutomaticEnabled: true, SecretConfigured: true},
		{Mode: WechatDeliveryModeAutomatic, AutomaticEnabled: true, AppIDConfigured: true},
	}
	for _, decision := range decisions {
		if !decision.IsUserAssisted() {
			t.Fatalf("decision = %+v, want fail-closed user-assisted", decision)
		}
	}
}

func TestWechatOpenPlatformConnectionEnablesAutomaticDraftDeliveryWithoutLegacySecret(t *testing.T) {
	enabled := true
	capabilities := WechatDeliveryCapabilities{CanCreateDraft: true, CanFormallyPublish: true}
	decision := ResolveWechatDeliveryDecisionWithOpenPlatform("wx-authorizer", true, capabilities, &enabled)
	if decision.IsUserAssisted() || !decision.AutomaticEnabled || !decision.OpenPlatformConnected || decision.SecretConfigured {
		t.Fatalf("connected Open Platform decision = %#v", decision)
	}
	if !decision.MayUploadMaterial {
		t.Fatal("connected Open Platform account should allow Server-owned material delivery")
	}

	revoked := ResolveWechatDeliveryDecisionWithOpenPlatform("wx-authorizer", false, WechatDeliveryCapabilities{}, &enabled)
	if !revoked.IsUserAssisted() || revoked.Mode != WechatDeliveryModeUserAssisted {
		t.Fatalf("revoked account decision = %#v, want user-assisted", revoked)
	}
	missingScope := ResolveWechatDeliveryDecisionWithOpenPlatform("wx-authorizer", true, WechatDeliveryCapabilities{}, &enabled)
	if !missingScope.IsUserAssisted() || missingScope.MayUploadMaterial || len(missingScope.ReasonCodes) != 1 || missingScope.ReasonCodes[0] != WechatDeliveryReasonCapabilityMissing {
		t.Fatalf("authorization without draft scope should fail closed: %#v", missingScope)
	}
}

func TestWechatDeliveryModeNormalizesLegacyAutomaticSetting(t *testing.T) {
	enabled := true
	if got := NormalizeWechatDeliveryMode("", &enabled); got != WechatDeliveryModeDraft {
		t.Fatalf("enabled legacy mode = %q, want %q", got, WechatDeliveryModeDraft)
	}
	enabled = false
	if got := NormalizeWechatDeliveryMode("", &enabled); got != WechatDeliveryModeUserAssisted {
		t.Fatalf("disabled legacy mode = %q, want %q", got, WechatDeliveryModeUserAssisted)
	}
	if got := NormalizeWechatDeliveryMode(WechatDeliveryModeDirectPublish, &enabled); got != WechatDeliveryModeDirectPublish {
		t.Fatalf("explicit mode = %q, want %q", got, WechatDeliveryModeDirectPublish)
	}
}

func TestResolveWechatDeliveryDecisionWithOpenPlatformPreservesExplicitMode(t *testing.T) {
	enabled := true
	capabilities := WechatDeliveryCapabilities{CanCreateDraft: true, CanFormallyPublish: true}
	decision := ResolveWechatDeliveryDecisionWithOpenPlatform("wx-authorizer", true, capabilities, &enabled, WechatDeliveryModeDirectPublish)
	if decision.Mode != WechatDeliveryModeDirectPublish || !decision.MayUploadMaterial || !decision.CanFormallyPublish {
		t.Fatalf("direct publish decision = %+v", decision)
	}
	decision = ResolveWechatDeliveryDecisionWithOpenPlatform("wx-authorizer", true, WechatDeliveryCapabilities{}, &enabled, WechatDeliveryModeDraft)
	if decision.Mode != WechatDeliveryModeUserAssisted || decision.MayUploadMaterial {
		t.Fatalf("missing draft capability decision = %+v", decision)
	}
	decision = ResolveWechatDeliveryDecisionWithOpenPlatform("wx-authorizer", true, WechatDeliveryCapabilities{CanFormallyPublish: true}, &enabled, WechatDeliveryModeDirectPublish)
	if decision.CanFormallyPublish {
		t.Fatalf("formal publishing should require draft capability too: %+v", decision)
	}
}

func boolPtr(value bool) *bool { return &value }
