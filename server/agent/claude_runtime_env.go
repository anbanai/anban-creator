package agent

import (
	"fmt"
	"strings"
)

const (
	maxClaudeRuntimeEnvValueBytes = 16 << 10
	maxClaudeRuntimeEnvTotalBytes = 32 << 10
)

var claudeRuntimeEnvKeys = [...]string{
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL",
	"ANTHROPIC_DEFAULT_FABLE_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL",
	"CLAUDE_CODE_EFFORT_LEVEL",
	"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT",
	"CLAUDE_CODE_MAX_CONTEXT_TOKENS",
	"CLAUDE_CODE_MAX_OUTPUT_TOKENS",
	"MAX_THINKING_TOKENS",
	"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING",
	"CLAUDE_CODE_DISABLE_THINKING",
	"CLAUDE_CODE_AUTO_COMPACT_WINDOW",
	"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE",
	"CLAUDE_CODE_DISABLE_1M_CONTEXT",
	"CLAUDE_CODE_SUBAGENT_MODEL",
	"ENABLE_TOOL_SEARCH",
}

var claudeProviderRuntimeEnvKeys = [...]string{
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL",
	"ANTHROPIC_DEFAULT_FABLE_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL",
}

// ClaudeRuntimeEnv returns an allowlisted copy of the Server-owned Claude
// configuration for delivery to a single authenticated Agent execution.
func ClaudeRuntimeEnv(configured map[string]string) map[string]string {
	result := make(map[string]string, len(claudeRuntimeEnvKeys))
	for _, key := range claudeRuntimeEnvKeys {
		if value := strings.TrimSpace(configured[key]); value != "" {
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ClaudeRuntimeEnvKeys returns the stable injection order for the shared
// Server-to-Agent runtime contract.
func ClaudeRuntimeEnvKeys() []string {
	return append([]string(nil), claudeRuntimeEnvKeys[:]...)
}

func ValidateClaudeRuntimeEnv(runtimeEnv map[string]string) error {
	allowed := make(map[string]struct{}, len(claudeRuntimeEnvKeys))
	for _, key := range claudeRuntimeEnvKeys {
		allowed[key] = struct{}{}
	}
	total := 0
	for key, value := range runtimeEnv {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("Claude runtime environment key %q is not allowed", key)
		}
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("Claude runtime environment value for %q is invalid", key)
		}
		if len(value) > maxClaudeRuntimeEnvValueBytes {
			return fmt.Errorf("Claude runtime environment value for %q exceeds size limit", key)
		}
		total += len(key) + len(value)
		if total > maxClaudeRuntimeEnvTotalBytes {
			return fmt.Errorf("Claude runtime environment exceeds total size limit")
		}
	}
	for _, key := range claudeProviderRuntimeEnvKeys {
		if strings.TrimSpace(runtimeEnv[key]) == "" {
			return fmt.Errorf("Claude runtime environment key %q is required", key)
		}
	}
	return nil
}
