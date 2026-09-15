package model

import (
	"reflect"
	"strings"
	"testing"
)

func validClaudeProfileEnvs() map[string]string {
	return map[string]string{
		ClaudeEnvBaseURL:                 "https://api.example.com/anthropic",
		ClaudeEnvModel:                   "default-model",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "opus-model",
		"ANTHROPIC_DEFAULT_FABLE_MODEL":  "fable-model",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "sonnet-model",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "haiku-model",
	}
}

func TestValidateClaudeProfileEnvs(t *testing.T) {
	wantKeys := []string{
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_FABLE_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"CLAUDE_CODE_EFFORT_LEVEL",
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT",
		"MAX_THINKING_TOKENS",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY",
		"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING",
		"CLAUDE_CODE_DISABLE_THINKING",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW",
		"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE",
		"CLAUDE_CODE_DISABLE_1M_CONTEXT",
		"CLAUDE_CODE_SUBAGENT_MODEL",
		"ENABLE_TOOL_SEARCH",
	}
	if got := ClaudeProfileEnvKeys(); !reflect.DeepEqual(got, wantKeys) {
		t.Fatalf("ClaudeProfileEnvKeys() = %#v, want %#v", got, wantKeys)
	}
	gotKeys := ClaudeProfileEnvKeys()
	gotKeys[0] = "mutated"
	if got := ClaudeProfileEnvKeys()[0]; got != ClaudeEnvBaseURL {
		t.Fatalf("ClaudeProfileEnvKeys returned shared storage: first key = %q", got)
	}

	all := validClaudeProfileEnvs()
	for _, key := range wantKeys {
		if _, exists := all[key]; !exists {
			all[key] = "true"
		}
	}
	all[ClaudeEnvAuthToken] = "token"
	all["CLAUDE_CODE_EFFORT_LEVEL"] = "max"
	all["MAX_THINKING_TOKENS"] = "0"
	all["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
	all["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] = "0"
	all["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = "200000"
	all["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] = "64000"
	all["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = "120000"
	all["CLAUDE_AUTOCOMPACT_PCT_OVERRIDE"] = "80"
	all["CLAUDE_CODE_SUBAGENT_MODEL"] = "subagent-model"

	tests := []struct {
		name        string
		envs        map[string]string
		requireAuth bool
		wantErr     bool
	}{
		{name: "all allowed keys", envs: all, requireAuth: true},
		{name: "optional values omitted", envs: validClaudeProfileEnvs()},
		{name: "auth token required and present", envs: withClaudeEnv(validClaudeProfileEnvs(), ClaudeEnvAuthToken, "token"), requireAuth: true},
		{name: "auth token required and missing", envs: validClaudeProfileEnvs(), requireAuth: true, wantErr: true},
		{name: "auth token required and blank", envs: withClaudeEnv(validClaudeProfileEnvs(), ClaudeEnvAuthToken, " "), requireAuth: true, wantErr: true},
		{name: "unknown key", envs: withClaudeEnv(validClaudeProfileEnvs(), "UNKNOWN", "value"), wantErr: true},
		{name: "empty key", envs: withClaudeEnv(validClaudeProfileEnvs(), "", "value"), wantErr: true},
		{name: "value contains nul", envs: withClaudeEnv(validClaudeProfileEnvs(), ClaudeEnvModel, "bad\x00model"), wantErr: true},
		{name: "value contains line feed", envs: withClaudeEnv(validClaudeProfileEnvs(), ClaudeEnvModel, "bad\nmodel"), wantErr: true},
		{name: "value contains carriage return", envs: withClaudeEnv(validClaudeProfileEnvs(), ClaudeEnvModel, "bad\rmodel"), wantErr: true},
		{name: "non token value has leading whitespace", envs: withClaudeEnv(validClaudeProfileEnvs(), ClaudeEnvModel, " model"), wantErr: true},
		{name: "non token value has trailing whitespace", envs: withClaudeEnv(validClaudeProfileEnvs(), ClaudeEnvModel, "model "), wantErr: true},
		{name: "auth token preserves surrounding whitespace", envs: withClaudeEnv(validClaudeProfileEnvs(), ClaudeEnvAuthToken, " token "), requireAuth: true},
		{name: "value at sixteen kibibytes", envs: withClaudeEnv(validClaudeProfileEnvs(), "CLAUDE_CODE_SUBAGENT_MODEL", strings.Repeat("a", 16*1024))},
		{name: "value over sixteen kibibytes", envs: withClaudeEnv(validClaudeProfileEnvs(), "CLAUDE_CODE_SUBAGENT_MODEL", strings.Repeat("a", 16*1024+1)), wantErr: true},
		{name: "aggregate values over thirty two kibibytes", envs: withClaudeEnvs(validClaudeProfileEnvs(), map[string]string{
			"CLAUDE_CODE_SUBAGENT_MODEL": strings.Repeat("a", 16*1024),
			ClaudeEnvAuthToken:           strings.Repeat("b", 16*1024),
		}), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateClaudeProfileEnvs(tt.envs, tt.requireAuth); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateClaudeProfileEnvs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	for _, required := range []string{
		ClaudeEnvBaseURL,
		ClaudeEnvModel,
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_FABLE_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
	} {
		t.Run("required "+required, func(t *testing.T) {
			envs := validClaudeProfileEnvs()
			delete(envs, required)
			if err := ValidateClaudeProfileEnvs(envs, false); err == nil {
				t.Fatal("ValidateClaudeProfileEnvs() error = nil, want required-key error")
			}
			envs[required] = " "
			if err := ValidateClaudeProfileEnvs(envs, false); err == nil {
				t.Fatal("ValidateClaudeProfileEnvs() error = nil, want blank-value error")
			}
		})
	}
}

func TestValidateClaudeProfileEnvsStrictTypedValues(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		values  []string
		wantErr bool
	}{
		{name: "effort valid", key: "CLAUDE_CODE_EFFORT_LEVEL", values: []string{"low", "medium", "high", "max"}},
		{name: "effort invalid", key: "CLAUDE_CODE_EFFORT_LEVEL", values: []string{"", "LOW", "maximum", " high"}, wantErr: true},
		{name: "booleans valid", key: "CLAUDE_CODE_ALWAYS_ENABLE_EFFORT", values: []string{"true", "false"}},
		{name: "booleans invalid", key: "CLAUDE_CODE_ALWAYS_ENABLE_EFFORT", values: []string{"TRUE", "False", "1", "yes", ""}, wantErr: true},
		{name: "positive integer valid", key: "CLAUDE_CODE_MAX_CONTEXT_TOKENS", values: []string{"1", "200000"}},
		{name: "positive integer invalid", key: "CLAUDE_CODE_MAX_CONTEXT_TOKENS", values: []string{"0", "00", "0001", "-1", "+1", "1.0", " 1", "", "9223372036854775808"}, wantErr: true},
		{name: "thinking integer valid", key: "MAX_THINKING_TOKENS", values: []string{"0", "1", "32000"}},
		{name: "thinking integer invalid", key: "MAX_THINKING_TOKENS", values: []string{"00", "0001", "-1", "+1", "1.0", " 1", "", "9223372036854775808"}, wantErr: true},
		{name: "percentage valid", key: "CLAUDE_AUTOCOMPACT_PCT_OVERRIDE", values: []string{"1", "50", "100"}},
		{name: "percentage invalid", key: "CLAUDE_AUTOCOMPACT_PCT_OVERRIDE", values: []string{"0", "01", "101", "-1", "+1", "1.0", ""}, wantErr: true},
	}
	booleanKeys := []string{
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT",
		"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING",
		"CLAUDE_CODE_DISABLE_THINKING",
		"CLAUDE_CODE_DISABLE_1M_CONTEXT",
		"ENABLE_TOOL_SEARCH",
	}
	positiveIntegerKeys := []string{
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW",
	}
	for _, tt := range tests {
		keys := []string{tt.key}
		if strings.Contains(tt.name, "booleans") {
			keys = booleanKeys
		}
		if strings.Contains(tt.name, "positive integer") {
			keys = positiveIntegerKeys
		}
		for _, key := range keys {
			for _, value := range tt.values {
				t.Run(tt.name+"/"+key+"/"+value, func(t *testing.T) {
					envs := withClaudeEnv(validClaudeProfileEnvs(), key, value)
					if err := ValidateClaudeProfileEnvs(envs, false); (err != nil) != tt.wantErr {
						t.Fatalf("ValidateClaudeProfileEnvs(%s=%q) error = %v, wantErr %v", key, value, err, tt.wantErr)
					}
				})
			}
		}
	}
	envs := validClaudeProfileEnvs()
	envs["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "true"
	if err := ValidateClaudeProfileEnvs(envs, false); err == nil || !strings.Contains(err.Error(), "must be 0 or 1") {
		t.Fatalf("accepted invalid Claude traffic switch value: %v", err)
	}
}

func TestValidateClaudeProfileEnvsBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "https origin", value: "https://api.example.com"},
		{name: "https path", value: "https://api.example.com/anthropic/v1"},
		{name: "http", value: "http://api.example.com", wantErr: true},
		{name: "missing host", value: "https:///v1", wantErr: true},
		{name: "credentials", value: "https://user:pass@api.example.com", wantErr: true},
		{name: "query", value: "https://api.example.com?v=1", wantErr: true},
		{name: "empty query", value: "https://api.example.com?", wantErr: true},
		{name: "fragment", value: "https://api.example.com#fragment", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envs := withClaudeEnv(validClaudeProfileEnvs(), ClaudeEnvBaseURL, tt.value)
			if err := ValidateClaudeProfileEnvs(envs, false); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateClaudeProfileEnvs(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidateClaudeProfileEnvsRejectsDisabledManagedAutoMemory(t *testing.T) {
	for _, value := range []string{"1", "true"} {
		envs := validClaudeProfileEnvs()
		envs["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] = value
		if err := ValidateClaudeProfileEnvs(envs, false); err == nil || !strings.Contains(err.Error(), "must be 0") {
			t.Fatalf("disabled managed auto memory value %q error = %v, want must be 0", value, err)
		}
	}
}

func TestClaudeProfileEnvHelpers(t *testing.T) {
	source := withClaudeEnvs(validClaudeProfileEnvs(), map[string]string{
		ClaudeEnvAuthToken:           "secret-token",
		"CLAUDE_CODE_SUBAGENT_MODEL": "opus-model",
	})
	redacted := RedactClaudeProfileEnvs(source)
	if _, exists := redacted[ClaudeEnvAuthToken]; exists {
		t.Fatalf("RedactClaudeProfileEnvs() retained %s", ClaudeEnvAuthToken)
	}
	if redacted[ClaudeEnvModel] != source[ClaudeEnvModel] || source[ClaudeEnvAuthToken] != "secret-token" {
		t.Fatalf("redaction changed non-token values or input: source=%#v redacted=%#v", source, redacted)
	}
	redacted[ClaudeEnvModel] = "mutated"
	if source[ClaudeEnvModel] == "mutated" {
		t.Fatal("RedactClaudeProfileEnvs returned shared storage")
	}

	cloned := CloneClaudeProfileEnvs(source)
	if !reflect.DeepEqual(cloned, source) {
		t.Fatalf("CloneClaudeProfileEnvs() = %#v, want %#v", cloned, source)
	}
	cloned[ClaudeEnvModel] = "mutated"
	if source[ClaudeEnvModel] == "mutated" {
		t.Fatal("CloneClaudeProfileEnvs returned shared storage")
	}
	if CloneClaudeProfileEnvs(nil) != nil || RedactClaudeProfileEnvs(nil) != nil {
		t.Fatal("nil env maps must remain nil")
	}

	wantModels := []string{"default-model", "fable-model", "haiku-model", "opus-model", "sonnet-model"}
	if got := ClaudeProfileReferencedModels(source); !reflect.DeepEqual(got, wantModels) {
		t.Fatalf("ClaudeProfileReferencedModels() = %#v, want %#v", got, wantModels)
	}
}

func withClaudeEnv(source map[string]string, key, value string) map[string]string {
	return withClaudeEnvs(source, map[string]string{key: value})
}

func withClaudeEnvs(source, additions map[string]string) map[string]string {
	result := make(map[string]string, len(source)+len(additions))
	for key, value := range source {
		result[key] = value
	}
	for key, value := range additions {
		result[key] = value
	}
	return result
}
