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

// agentProfilePublicEnvKeys are the only Claude profile env vars the Studio API
// may surface. Provider routing, the base URL, the model matrix, the subagent
// model, and the usage aliases stay internal execution facts.
var agentProfilePublicEnvKeys = []string{
	claudeEnvEffortLevel,
	claudeEnvMaxContextTokens,
	claudeEnvDisableThinking,
}

// AgentProfileAPISnapshot is the lean public projection of a frozen agent
// profile. It carries only the facts the Studio renders (schema, profile id,
// display name, and display-scoped env values such as effort/context/thinking)
// and never the provider base URL, model matrix, or model usage aliases.
type AgentProfileAPISnapshot struct {
	SchemaVersion int               `json:"schema_version"`
	ProfileID     string            `json:"profile_id,omitempty"`
	DisplayName   string            `json:"display_name,omitempty"`
	Envs          map[string]string `json:"envs,omitempty"`
}

// APISnapshot returns the public-safe projection of the frozen profile for the
// Studio task API. The returned value never carries routing or model facts.
func (s AgentProfileSnapshot) APISnapshot() AgentProfileAPISnapshot {
	public := AgentProfileAPISnapshot{
		SchemaVersion: s.SchemaVersion,
		ProfileID:     s.ProfileID,
		DisplayName:   s.DisplayName,
	}
	for _, key := range agentProfilePublicEnvKeys {
		if value, ok := s.Envs[key]; ok {
			if public.Envs == nil {
				public.Envs = make(map[string]string, len(agentProfilePublicEnvKeys))
			}
			public.Envs[key] = value
		}
	}
	return public
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
	if err := ValidateClaudeProfileModelUsageAliases(snapshot.Provider, snapshot.Envs, snapshot.ModelUsageAliases); err != nil {
		return fmt.Errorf("agent profile snapshot model_usage_aliases are invalid: %w", err)
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
