package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type AgentModelMatrix struct {
	Default string `json:"default"`
	Opus    string `json:"opus"`
	Fable   string `json:"fable"`
	Sonnet  string `json:"sonnet"`
	Haiku   string `json:"haiku"`
}

type AgentClaudeControls struct {
	EffortLevel             *string `json:"effort_level,omitempty"`
	AlwaysEnableEffort      *bool   `json:"always_enable_effort,omitempty"`
	MaxContextTokens        *int    `json:"max_context_tokens,omitempty"`
	MaxOutputTokens         *int    `json:"max_output_tokens,omitempty"`
	MaxThinkingTokens       *int    `json:"max_thinking_tokens,omitempty"`
	DisableAdaptiveThinking *bool   `json:"disable_adaptive_thinking,omitempty"`
	DisableThinking         *bool   `json:"disable_thinking,omitempty"`
	AutoCompactWindow       *int    `json:"auto_compact_window,omitempty"`
	AutocompactPctOverride  *int    `json:"autocompact_pct_override,omitempty"`
	Disable1MContext        *bool   `json:"disable_1m_context,omitempty"`
	SubagentModel           *string `json:"subagent_model,omitempty"`
	EnableToolSearch        *bool   `json:"enable_tool_search,omitempty"`
}

// AgentProfileSnapshot is the non-sensitive model configuration frozen on a task.
type AgentProfileSnapshot struct {
	SchemaVersion     int               `json:"schema_version"`
	ProfileID         string            `json:"profile_id"`
	DisplayName       string            `json:"display_name"`
	Provider          string            `json:"provider"`
	Protocol          string            `json:"protocol"`
	Envs              map[string]string `json:"envs"`
	ModelUsageAliases map[string]string `json:"model_usage_aliases"`
}

func ValidateAgentProfileSnapshot(snapshot AgentProfileSnapshot) error {
	if snapshot.SchemaVersion != ClaudeProfileSchemaV3 {
		return fmt.Errorf("agent profile snapshot schema_version must be %d", ClaudeProfileSchemaV3)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "profile_id", value: snapshot.ProfileID},
		{name: "display_name", value: snapshot.DisplayName},
		{name: "provider", value: snapshot.Provider},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("agent profile snapshot %s is required", field.name)
		}
	}
	if snapshot.Protocol != "anthropic" {
		return fmt.Errorf("agent profile snapshot protocol must be anthropic")
	}
	if _, exists := snapshot.Envs[ClaudeEnvAuthToken]; exists {
		return fmt.Errorf("agent profile snapshot envs must not contain %s", ClaudeEnvAuthToken)
	}
	if err := ValidateClaudeProfileEnvs(snapshot.Envs, false); err != nil {
		return fmt.Errorf("agent profile snapshot envs are invalid: %w", err)
	}
	for _, model := range ClaudeProfileReferencedModels(snapshot.Envs) {
		if strings.TrimSpace(snapshot.ModelUsageAliases[model]) == "" {
			return fmt.Errorf("agent profile snapshot model_usage_aliases.%s is required", model)
		}
	}
	for raw, canonical := range snapshot.ModelUsageAliases {
		if strings.TrimSpace(raw) == "" || strings.TrimSpace(canonical) == "" || strings.Contains(canonical, "/") {
			return fmt.Errorf("agent profile snapshot model_usage_aliases contains an invalid mapping")
		}
	}
	return nil
}

func AgentProfileFingerprint(snapshot AgentProfileSnapshot) (string, error) {
	if err := ValidateAgentProfileSnapshot(snapshot); err != nil {
		return "", err
	}
	type alias struct {
		Raw       string `json:"raw"`
		Canonical string `json:"canonical"`
	}
	type env struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	envs := make([]env, 0, len(snapshot.Envs))
	for key, value := range RedactClaudeProfileEnvs(snapshot.Envs) {
		envs = append(envs, env{Key: key, Value: value})
	}
	sort.Slice(envs, func(i, j int) bool {
		if envs[i].Key == envs[j].Key {
			return envs[i].Value < envs[j].Value
		}
		return envs[i].Key < envs[j].Key
	})
	aliases := make([]alias, 0, len(snapshot.ModelUsageAliases))
	for raw, canonical := range snapshot.ModelUsageAliases {
		aliases = append(aliases, alias{Raw: raw, Canonical: canonical})
	}
	sort.Slice(aliases, func(i, j int) bool {
		if aliases[i].Raw == aliases[j].Raw {
			return aliases[i].Canonical < aliases[j].Canonical
		}
		return aliases[i].Raw < aliases[j].Raw
	})
	canonical := struct {
		SchemaVersion int     `json:"schema_version"`
		ProfileID     string  `json:"profile_id"`
		DisplayName   string  `json:"display_name"`
		Provider      string  `json:"provider"`
		Protocol      string  `json:"protocol"`
		Envs          []env   `json:"envs"`
		Aliases       []alias `json:"model_usage_aliases"`
	}{
		SchemaVersion: snapshot.SchemaVersion,
		ProfileID:     snapshot.ProfileID,
		DisplayName:   snapshot.DisplayName,
		Provider:      snapshot.Provider,
		Protocol:      snapshot.Protocol,
		Envs:          envs,
		Aliases:       aliases,
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal canonical agent profile snapshot: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
