package mcp

import (
	"testing"
)

func TestSeednoteAgentToolsExcludeLoginAdministration(t *testing.T) {
	names := listToolNames(t, registerSeednoteTools)
	for _, want := range []string{"search_seednote_feeds", "get_seednote_feed_detail", "get_seednote_user_profile"} {
		if !names[want] {
			t.Errorf("missing Seednote research tool %q", want)
		}
	}
	for _, forbidden := range []string{"check_seednote_login_status", "get_seednote_login_qrcode"} {
		if names[forbidden] {
			t.Errorf("Agent MCP exposes administrator login tool %q", forbidden)
		}
	}
}

func TestSeednoteXsecTokenIsRedactedFromMCPLogs(t *testing.T) {
	redacted := redactMCPLogValue(map[string]any{
		"feed_id":    "feed-1",
		"xsec_token": "secret-token",
		"nested": map[string]any{
			"xsec_token": "nested-secret",
		},
	}).(map[string]any)

	if redacted["xsec_token"] != "REDACTED" {
		t.Fatalf("xsec_token = %v, want REDACTED", redacted["xsec_token"])
	}
	nested := redacted["nested"].(map[string]any)
	if nested["xsec_token"] != "REDACTED" {
		t.Fatalf("nested xsec_token = %v, want REDACTED", nested["xsec_token"])
	}
}
