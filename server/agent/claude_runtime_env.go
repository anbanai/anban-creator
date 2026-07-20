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
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_MODEL",
	"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
	"CLAUDE_CODE_DISABLE_AUTO_MEMORY",
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
	return nil
}
