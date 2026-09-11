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

func TestMCPLogRedactsGenericURLAndSecretFields(t *testing.T) {
	redacted := redactMCPLogValue(map[string]any{
		"video_url":          "https://media.example/video.mp4?Signature=secret",
		"image_urls":         []any{"https://img.example/a.png?token=secret"},
		"content_source_url": "https://source.example/article?id=secret",
		"provider_token":     "provider-secret",
	})
	values := redacted.(map[string]any)
	if values["video_url"] != "https://media.example/video.mp4?REDACTED" {
		t.Fatalf("video_url = %#v, want query redaction", values["video_url"])
	}
	images := values["image_urls"].([]any)
	if images[0] != "https://img.example/a.png?REDACTED" {
		t.Fatalf("image_urls[0] = %#v, want query redaction", images[0])
	}
	if values["content_source_url"] != "https://source.example/article?REDACTED" {
		t.Fatalf("content_source_url = %#v, want query redaction", values["content_source_url"])
	}
	if values["provider_token"] != "REDACTED" {
		t.Fatalf("provider_token = %#v, want value redaction", values["provider_token"])
	}
}
