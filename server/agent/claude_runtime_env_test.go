package agent

import (
	"reflect"
	"strings"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

func TestClaudeRuntimeEnvAndAliasesAreIdenticalForManagedRuntimeAndBootstrap(t *testing.T) {
	claude := srvconfig.ClaudeConfig{
		Provider:  srvconfig.ClaudeProviderVolcengineArk,
		BaseURL:   srvconfig.ClaudeArkCompatibleBaseURL,
		AuthToken: "runtime-secret",
		Models: srvconfig.ClaudeModelsConfig{
			Default: "doubao-seed-evolving", Opus: "doubao-seed-evolving", Fable: "doubao-seed-evolving",
			Sonnet: "doubao-seed-2-1-pro-260628", Haiku: "doubao-seed-2-1-turbo-260628",
		},
		UsageAliases: map[string]string{"doubao-seed-evolving-latest-version": "doubao-seed-evolving"},
		Env: map[string]string{
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          "0",
			"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "1000000",
		},
	}
	runtimeEnv := claude.RuntimeEnv()
	aliases := claude.RuntimeModelUsageAliases()
	managedEnv := ClaudeRuntimeEnv(runtimeEnv)
	bootstrapEnv := ClaudeRuntimeEnv(runtimeEnv)
	if !reflect.DeepEqual(managedEnv, bootstrapEnv) {
		t.Fatalf("runtime env mismatch: managed=%#v bootstrap=%#v", managedEnv, bootstrapEnv)
	}
	managedAliases := cloneModelUsageAliases(aliases)
	bootstrapAliases := cloneModelUsageAliases(aliases)
	if !reflect.DeepEqual(managedAliases, bootstrapAliases) {
		t.Fatalf("model usage aliases mismatch: managed=%#v bootstrap=%#v", managedAliases, bootstrapAliases)
	}
}

func TestClaudeRuntimeEnvCopiesOnlyExplicitClaudeContract(t *testing.T) {
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
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "1000000",
		"ANBAN_API_KEY":                            "server-secret",
		"PATH":                                     "/untrusted/bin",
		"HOME":                                     "/untrusted/home",
	}
	got := ClaudeRuntimeEnv(source)
	if len(got) != len(claudeRuntimeEnvKeys) || got["ANTHROPIC_AUTH_TOKEN"] != "token" {
		t.Fatalf("runtime environment = %#v", got)
	}
	for _, forbidden := range []string{"ANTHROPIC_API_KEY", "ANBAN_API_KEY", "PATH", "HOME"} {
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
		{"ANTHROPIC_AUTH_TOKEN": "token"},
		{"PATH": "/tmp/bin"},
		{"ANTHROPIC_AUTH_TOKEN": ""},
		{"ANTHROPIC_AUTH_TOKEN": " token"},
		{"ANTHROPIC_AUTH_TOKEN": "token\x00suffix"},
		{"ANTHROPIC_AUTH_TOKEN": strings.Repeat("x", maxClaudeRuntimeEnvValueBytes+1)},
		{
			"ANTHROPIC_AUTH_TOKEN":         strings.Repeat("x", maxClaudeRuntimeEnvValueBytes),
			"ANTHROPIC_DEFAULT_OPUS_MODEL": strings.Repeat("y", maxClaudeRuntimeEnvValueBytes),
		},
	} {
		if err := ValidateClaudeRuntimeEnv(runtimeEnv); err == nil {
			t.Fatalf("unsafe runtime environment accepted: keys=%v", runtimeEnv)
		}
	}
}
