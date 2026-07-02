package main

import "testing"

func TestToolBaseNameHandlesPluginMCPNames(t *testing.T) {
	cases := map[string]string{
		"generate_image": "generate_image",
		"mcp__plugin_anban_creator__generate_image":        "generate_image",
		"mcp__plugin_anban_creator__create_video_asr_task": "create_video_asr_task",
	}

	for name, want := range cases {
		if got := toolBaseName(name); got != want {
			t.Fatalf("toolBaseName(%q) = %q, want %q", name, got, want)
		}
	}
}
