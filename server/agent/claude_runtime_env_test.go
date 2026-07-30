package agent

import (
	"strings"
	"testing"
)

func TestClaudeRuntimeEnvCopiesOnlyExplicitClaudeContractWithoutRewritingValues(t *testing.T) {
	source := map[string]string{
		"ANTHROPIC_AUTH_TOKEN":                     " token ",
		"ANTHROPIC_API_KEY":                        "api-key",
		"ANTHROPIC_BASE_URL":                       "https://anthropic.example.com",
		"ANTHROPIC_MODEL":                          "model",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":             "opus",
		"ANTHROPIC_DEFAULT_FABLE_MODEL":            "fable",
		"ANTHROPIC_DEFAULT_SONNET_MODEL":           "sonnet",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":            "haiku",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          "0",
		"CLAUDE_CODE_EFFORT_LEVEL":                 "max",
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":         "false",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "1048576",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS":            "131072",
		"MAX_THINKING_TOKENS":                      "0",
		"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING":    "false",
		"CLAUDE_CODE_DISABLE_THINKING":             "false",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "1000000",
		"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":          "95",
		"CLAUDE_CODE_DISABLE_1M_CONTEXT":           "false",
		"CLAUDE_CODE_SUBAGENT_MODEL":               "subagent-model",
		"ENABLE_TOOL_SEARCH":                       "false",
		"ANBAN_API_KEY":                            "server-secret",
		"PATH":                                     "/untrusted/bin",
		"HOME":                                     "/untrusted/home",
	}
	got := ClaudeRuntimeEnv(source)
	if len(got) != len(ClaudeRuntimeEnvKeys()) || got["ANTHROPIC_AUTH_TOKEN"] != " token " {
		t.Fatalf("runtime environment = %#v", got)
	}
	for _, forbidden := range []string{"ANTHROPIC_API_KEY", "ANBAN_API_KEY", "PATH", "HOME", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "CLAUDE_CODE_DISABLE_AUTO_MEMORY"} {
		if _, ok := got[forbidden]; ok {
			t.Fatalf("protected environment %q was copied", forbidden)
		}
	}
	got["ANTHROPIC_MODEL"] = "changed"
	if source["ANTHROPIC_MODEL"] == "changed" {
		t.Fatal("runtime environment aliases Server config")
	}
}

func TestValidateClaudeRuntimeEnvAcceptsAllFrozenClaudeControls(t *testing.T) {
	runtimeEnv := map[string]string{
		"ANTHROPIC_AUTH_TOKEN":                  "token",
		"ANTHROPIC_BASE_URL":                    "https://anthropic.example.com",
		"ANTHROPIC_MODEL":                       "default-model",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":          "opus-model",
		"ANTHROPIC_DEFAULT_FABLE_MODEL":         "fable-model",
		"ANTHROPIC_DEFAULT_SONNET_MODEL":        "sonnet-model",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":         "haiku-model",
		"CLAUDE_CODE_EFFORT_LEVEL":              "max",
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":      "false",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS":        "1048576",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS":         "131072",
		"MAX_THINKING_TOKENS":                   "0",
		"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING": "false",
		"CLAUDE_CODE_DISABLE_THINKING":          "false",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":       "1000000",
		"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":       "95",
		"CLAUDE_CODE_DISABLE_1M_CONTEXT":        "false",
		"CLAUDE_CODE_SUBAGENT_MODEL":            "subagent-model",
		"ENABLE_TOOL_SEARCH":                    "false",
	}
	if err := ValidateClaudeRuntimeEnv(runtimeEnv); err != nil {
		t.Fatalf("all frozen Claude controls rejected: %v", err)
	}
	got := ClaudeRuntimeEnv(runtimeEnv)
	for key, want := range runtimeEnv {
		if got[key] != want {
			t.Fatalf("runtime environment %q = %q, want %q", key, got[key], want)
		}
	}
}

func TestValidateClaudeRuntimeEnvRejectsUnknownAndOversizedValues(t *testing.T) {
	for _, runtimeEnv := range []map[string]string{
		{"ANTHROPIC_AUTH_TOKEN": "token"},
		{"PATH": "/tmp/bin"},
		{"ANTHROPIC_AUTH_TOKEN": ""},
		{"ANTHROPIC_AUTH_TOKEN": " token"},
		{"ANTHROPIC_AUTH_TOKEN": "token\x00suffix"},
		{"ANTHROPIC_AUTH_TOKEN": strings.Repeat("x", 16<<10+1)},
		{
			"ANTHROPIC_AUTH_TOKEN":         strings.Repeat("x", 16<<10),
			"ANTHROPIC_DEFAULT_OPUS_MODEL": strings.Repeat("y", 16<<10),
		},
	} {
		if err := ValidateClaudeRuntimeEnv(runtimeEnv); err == nil {
			t.Fatalf("unsafe runtime environment accepted: keys=%v", runtimeEnv)
		}
	}
}
