package agent

import "github.com/anbanai/anban-creator/server/model"

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

func ValidateClaudeRuntimeEnv(runtimeEnv map[string]string) error {
	return model.ValidateClaudeProfileEnvs(runtimeEnv, true)
}
