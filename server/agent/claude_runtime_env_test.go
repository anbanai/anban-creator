package agent

import (
	"strings"
	"testing"
)

func TestClaudeRuntimeEnvCopiesOnlyExplicitClaudeContract(t *testing.T) {
	source := map[string]string{
		"ANTHROPIC_AUTH_TOKEN":                     " token ",
		"ANTHROPIC_API_KEY":                        "api-key",
		"ANTHROPIC_BASE_URL":                       "https://anthropic.example.com",
		"ANTHROPIC_MODEL":                          "model",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          "0",
		"ANBAN_API_KEY":                            "server-secret",
		"PATH":                                     "/untrusted/bin",
		"HOME":                                     "/untrusted/home",
	}
	got := ClaudeRuntimeEnv(source)
	if len(got) != len(claudeRuntimeEnvKeys) || got["ANTHROPIC_AUTH_TOKEN"] != "token" {
		t.Fatalf("runtime environment = %#v", got)
	}
	for _, forbidden := range []string{"ANBAN_API_KEY", "PATH", "HOME"} {
		if _, ok := got[forbidden]; ok {
			t.Fatalf("protected environment %q was copied", forbidden)
		}
	}
	got["ANTHROPIC_MODEL"] = "changed"
	if source["ANTHROPIC_MODEL"] == "changed" {
		t.Fatal("runtime environment aliases Server config")
	}
}

func TestValidateClaudeRuntimeEnvRejectsUnknownAndOversizedValues(t *testing.T) {
	for _, runtimeEnv := range []map[string]string{
		{"PATH": "/tmp/bin"},
		{"ANTHROPIC_AUTH_TOKEN": ""},
		{"ANTHROPIC_AUTH_TOKEN": " token"},
		{"ANTHROPIC_AUTH_TOKEN": "token\x00suffix"},
		{"ANTHROPIC_AUTH_TOKEN": strings.Repeat("x", maxClaudeRuntimeEnvValueBytes+1)},
		{
			"ANTHROPIC_AUTH_TOKEN": strings.Repeat("x", maxClaudeRuntimeEnvValueBytes),
			"ANTHROPIC_API_KEY":    strings.Repeat("y", maxClaudeRuntimeEnvValueBytes),
		},
	} {
		if err := ValidateClaudeRuntimeEnv(runtimeEnv); err == nil {
			t.Fatalf("unsafe runtime environment accepted: keys=%v", runtimeEnv)
		}
	}
}
