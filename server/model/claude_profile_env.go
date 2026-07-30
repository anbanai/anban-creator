package model

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const (
	ClaudeEnvAuthToken = "ANTHROPIC_AUTH_TOKEN"
	ClaudeEnvBaseURL   = "ANTHROPIC_BASE_URL"
	ClaudeEnvModel     = "ANTHROPIC_MODEL"

	ClaudeProfileSchemaV3 = 3

	claudeProfileEnvValueMaxBytes = 16 * 1024
	claudeProfileEnvTotalMaxBytes = 32 * 1024
)

const (
	claudeEnvDefaultOpusModel            = "ANTHROPIC_DEFAULT_OPUS_MODEL"
	claudeEnvDefaultFableModel           = "ANTHROPIC_DEFAULT_FABLE_MODEL"
	claudeEnvDefaultSonnetModel          = "ANTHROPIC_DEFAULT_SONNET_MODEL"
	claudeEnvDefaultHaikuModel           = "ANTHROPIC_DEFAULT_HAIKU_MODEL"
	claudeEnvEffortLevel                 = "CLAUDE_CODE_EFFORT_LEVEL"
	claudeEnvAlwaysEnableEffort          = "CLAUDE_CODE_ALWAYS_ENABLE_EFFORT"
	claudeEnvMaxThinkingTokens           = "MAX_THINKING_TOKENS"
	claudeEnvDisableAdaptiveThinking     = "CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING"
	claudeEnvDisableThinking             = "CLAUDE_CODE_DISABLE_THINKING"
	claudeEnvMaxContextTokens            = "CLAUDE_CODE_MAX_CONTEXT_TOKENS"
	claudeEnvMaxOutputTokens             = "CLAUDE_CODE_MAX_OUTPUT_TOKENS"
	claudeEnvAutoCompactWindow           = "CLAUDE_CODE_AUTO_COMPACT_WINDOW"
	claudeEnvAutocompactPctOverride      = "CLAUDE_AUTOCOMPACT_PCT_OVERRIDE"
	claudeEnvDisable1MContext            = "CLAUDE_CODE_DISABLE_1M_CONTEXT"
	claudeEnvSubagentModel               = "CLAUDE_CODE_SUBAGENT_MODEL"
	claudeEnvEnableToolSearch            = "ENABLE_TOOL_SEARCH"
)

var claudeProfileEnvKeys = []string{
	ClaudeEnvBaseURL,
	ClaudeEnvAuthToken,
	ClaudeEnvModel,
	claudeEnvDefaultOpusModel,
	claudeEnvDefaultFableModel,
	claudeEnvDefaultSonnetModel,
	claudeEnvDefaultHaikuModel,
	claudeEnvEffortLevel,
	claudeEnvAlwaysEnableEffort,
	claudeEnvMaxThinkingTokens,
	claudeEnvDisableAdaptiveThinking,
	claudeEnvDisableThinking,
	claudeEnvMaxContextTokens,
	claudeEnvMaxOutputTokens,
	claudeEnvAutoCompactWindow,
	claudeEnvAutocompactPctOverride,
	claudeEnvDisable1MContext,
	claudeEnvSubagentModel,
	claudeEnvEnableToolSearch,
}

var claudeProfileRequiredEnvKeys = []string{
	ClaudeEnvBaseURL,
	ClaudeEnvModel,
	claudeEnvDefaultOpusModel,
	claudeEnvDefaultFableModel,
	claudeEnvDefaultSonnetModel,
	claudeEnvDefaultHaikuModel,
}

var claudeProfileModelEnvKeys = []string{
	ClaudeEnvModel,
	claudeEnvDefaultOpusModel,
	claudeEnvDefaultFableModel,
	claudeEnvDefaultSonnetModel,
	claudeEnvDefaultHaikuModel,
	claudeEnvSubagentModel,
}

func ClaudeProfileEnvKeys() []string {
	return append([]string(nil), claudeProfileEnvKeys...)
}

func ValidateClaudeProfileEnvs(envs map[string]string, requireAuthToken bool) error {
	allowed := make(map[string]struct{}, len(claudeProfileEnvKeys))
	for _, key := range claudeProfileEnvKeys {
		allowed[key] = struct{}{}
	}

	totalBytes := 0
	for key, value := range envs {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("claude profile env %q is not allowed", key)
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("claude profile env %s contains a forbidden control character", key)
		}
		if len(value) > claudeProfileEnvValueMaxBytes {
			return fmt.Errorf("claude profile env %s exceeds %d bytes", key, claudeProfileEnvValueMaxBytes)
		}
		totalBytes += len(value)
		if totalBytes > claudeProfileEnvTotalMaxBytes {
			return fmt.Errorf("claude profile env values exceed %d bytes", claudeProfileEnvTotalMaxBytes)
		}
	}

	for _, key := range claudeProfileRequiredEnvKeys {
		if strings.TrimSpace(envs[key]) == "" {
			return fmt.Errorf("claude profile env %s is required", key)
		}
	}
	if requireAuthToken && strings.TrimSpace(envs[ClaudeEnvAuthToken]) == "" {
		return fmt.Errorf("claude profile env %s is required", ClaudeEnvAuthToken)
	}
	if value, exists := envs[claudeEnvSubagentModel]; exists && strings.TrimSpace(value) == "" {
		return fmt.Errorf("claude profile env %s must not be empty", claudeEnvSubagentModel)
	}

	if err := validateClaudeBaseURL(envs[ClaudeEnvBaseURL]); err != nil {
		return err
	}
	if value, exists := envs[claudeEnvEffortLevel]; exists {
		switch value {
		case "low", "medium", "high", "max":
		default:
			return fmt.Errorf("claude profile env %s must be low, medium, high, or max", claudeEnvEffortLevel)
		}
	}
	for _, key := range []string{
		claudeEnvAlwaysEnableEffort,
		claudeEnvDisableAdaptiveThinking,
		claudeEnvDisableThinking,
		claudeEnvDisable1MContext,
		claudeEnvEnableToolSearch,
	} {
		if value, exists := envs[key]; exists && value != "true" && value != "false" {
			return fmt.Errorf("claude profile env %s must be true or false", key)
		}
	}
	for _, key := range []string{
		claudeEnvMaxContextTokens,
		claudeEnvMaxOutputTokens,
		claudeEnvAutoCompactWindow,
	} {
		if value, exists := envs[key]; exists {
			parsed, err := parseClaudeDecimal(value)
			if err != nil || parsed == 0 {
				return fmt.Errorf("claude profile env %s must be a positive decimal integer", key)
			}
		}
	}
	if value, exists := envs[claudeEnvMaxThinkingTokens]; exists {
		if _, err := parseClaudeDecimal(value); err != nil {
			return fmt.Errorf("claude profile env %s must be a non-negative decimal integer", claudeEnvMaxThinkingTokens)
		}
	}
	if value, exists := envs[claudeEnvAutocompactPctOverride]; exists {
		parsed, err := parseClaudeDecimal(value)
		if err != nil || parsed < 1 || parsed > 100 {
			return fmt.Errorf("claude profile env %s must be a decimal integer from 1 through 100", claudeEnvAutocompactPctOverride)
		}
	}
	return nil
}

func validateClaudeBaseURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return fmt.Errorf("claude profile env %s must be an HTTPS URL without credentials, query, or fragment", ClaudeEnvBaseURL)
	}
	return nil
}

func parseClaudeDecimal(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("empty decimal integer")
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return 0, fmt.Errorf("invalid decimal integer")
		}
	}
	return strconv.Atoi(value)
}

func RedactClaudeProfileEnvs(envs map[string]string) map[string]string {
	if envs == nil {
		return nil
	}
	redacted := make(map[string]string, len(envs))
	for key, value := range envs {
		if key != ClaudeEnvAuthToken {
			redacted[key] = value
		}
	}
	return redacted
}

func CloneClaudeProfileEnvs(envs map[string]string) map[string]string {
	if envs == nil {
		return nil
	}
	cloned := make(map[string]string, len(envs))
	for key, value := range envs {
		cloned[key] = value
	}
	return cloned
}

func ClaudeProfileReferencedModels(envs map[string]string) []string {
	seen := make(map[string]struct{}, len(claudeProfileModelEnvKeys))
	for _, key := range claudeProfileModelEnvKeys {
		if model := strings.TrimSpace(envs[key]); model != "" {
			seen[model] = struct{}{}
		}
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}
