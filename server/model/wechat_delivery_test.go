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
		{name: "automatic", appID: "wx-app", secret: true, automatic: boolPtr(true), wantMode: WechatDeliveryModeAutomatic},
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

func boolPtr(value bool) *bool { return &value }
