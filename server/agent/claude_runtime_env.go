package agent

import "github.com/anbanai/anban-creator/server/model"

var claudeConflictingInheritedEnvKeys = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_CUSTOM_HEADERS",
	"ANTHROPIC_UNIX_SOCKET",
	"ANTHROPIC_BEDROCK_BASE_URL",
	"ANTHROPIC_BEDROCK_MANTLE_BASE_URL",
	"ANTHROPIC_AWS_BASE_URL",
	"ANTHROPIC_AWS_WORKSPACE_ID",
	"ANTHROPIC_GOOGLE_CLOUD_BASE_URL",
	"ANTHROPIC_GOOGLE_CLOUD_LOCATION",
	"ANTHROPIC_GOOGLE_CLOUD_PROJECT",
	"ANTHROPIC_GOOGLE_CLOUD_WORKSPACE_ID",
	"ANTHROPIC_VERTEX_BASE_URL",
	"ANTHROPIC_VERTEX_PROJECT_ID",
	"ANTHROPIC_FOUNDRY_BASE_URL",
	"ANTHROPIC_FOUNDRY_RESOURCE",
	"CLAUDE_CODE_API_BASE_URL",
	"CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST",
	"CLAUDE_CODE_USE_ANTHROPIC_AWS",
	"CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_USE_GATEWAY",
	"CLAUDE_CODE_USE_MANTLE",
	"CLAUDE_CODE_USE_VERTEX",
	"_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL",
}

// ClaudeRuntimeEnv returns an allowlisted copy of the Server-owned Claude
// configuration for delivery to a single authenticated Agent execution.
func ClaudeRuntimeEnv(configured map[string]string) map[string]string {
	keys := model.ClaudeProfileEnvKeys()
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, exists := configured[key]; exists {
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
	return model.ClaudeProfileEnvKeys()
}

// ClaudeEnvironmentKeysToUnset includes supported Profile keys and inherited
// authentication or provider-routing keys that must not influence a managed run.
func ClaudeEnvironmentKeysToUnset() []string {
	profileKeys := model.ClaudeProfileEnvKeys()
	result := make([]string, 0, len(profileKeys)+len(claudeConflictingInheritedEnvKeys))
	result = append(result, profileKeys...)
	result = append(result, claudeConflictingInheritedEnvKeys...)
	return result
}

func ValidateClaudeRuntimeEnv(runtimeEnv map[string]string) error {
	return model.ValidateClaudeProfileEnvs(runtimeEnv, true)
}
