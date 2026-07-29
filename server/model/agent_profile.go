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
	SchemaVersion     int                 `json:"schema_version"`
	ProfileID         string              `json:"profile_id"`
	DisplayName       string              `json:"display_name"`
	Provider          string              `json:"provider"`
	Protocol          string              `json:"protocol"`
	Models            AgentModelMatrix    `json:"models"`
	Claude            AgentClaudeControls `json:"claude"`
	ModelUsageAliases map[string]string   `json:"model_usage_aliases"`
}

func ValidateAgentProfileSnapshot(snapshot AgentProfileSnapshot) error {
	if snapshot.SchemaVersion != 2 {
		return fmt.Errorf("agent profile snapshot schema_version must be 2")
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
	models := []struct {
		role  string
		value string
	}{
		{role: "default", value: snapshot.Models.Default},
		{role: "opus", value: snapshot.Models.Opus},
		{role: "fable", value: snapshot.Models.Fable},
		{role: "sonnet", value: snapshot.Models.Sonnet},
		{role: "haiku", value: snapshot.Models.Haiku},
	}
	for _, model := range models {
		if strings.TrimSpace(model.value) == "" {
			return fmt.Errorf("agent profile snapshot models.%s is required", model.role)
		}
		if strings.TrimSpace(snapshot.ModelUsageAliases[model.value]) == "" {
			return fmt.Errorf("agent profile snapshot model_usage_aliases.%s is required", model.value)
		}
	}
	if snapshot.Claude.SubagentModel != nil {
		model := strings.TrimSpace(*snapshot.Claude.SubagentModel)
		if model == "" {
			return fmt.Errorf("agent profile snapshot claude.subagent_model must not be empty")
		}
		if strings.TrimSpace(snapshot.ModelUsageAliases[model]) == "" {
			return fmt.Errorf("agent profile snapshot model_usage_aliases.%s is required", model)
		}
	}
	for raw, canonical := range snapshot.ModelUsageAliases {
		if strings.TrimSpace(raw) == "" || strings.TrimSpace(canonical) == "" || strings.Contains(canonical, "/") {
			return fmt.Errorf("agent profile snapshot model_usage_aliases contains an invalid mapping")
		}
	}
	if snapshot.Claude.EffortLevel != nil {
		switch *snapshot.Claude.EffortLevel {
		case "low", "medium", "high", "max":
		default:
			return fmt.Errorf("agent profile snapshot claude.effort_level is invalid")
		}
	}
	for _, field := range []struct {
		name  string
		value *int
	}{
		{name: "max_context_tokens", value: snapshot.Claude.MaxContextTokens},
		{name: "max_output_tokens", value: snapshot.Claude.MaxOutputTokens},
		{name: "auto_compact_window", value: snapshot.Claude.AutoCompactWindow},
	} {
		if field.value != nil && *field.value <= 0 {
			return fmt.Errorf("agent profile snapshot claude.%s must be positive", field.name)
		}
	}
	if snapshot.Claude.MaxThinkingTokens != nil && *snapshot.Claude.MaxThinkingTokens < 0 {
		return fmt.Errorf("agent profile snapshot claude.max_thinking_tokens must not be negative")
	}
	if value := snapshot.Claude.AutocompactPctOverride; value != nil && (*value < 1 || *value > 100) {
		return fmt.Errorf("agent profile snapshot claude.autocompact_pct_override must be between 1 and 100")
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
		SchemaVersion int                 `json:"schema_version"`
		ProfileID     string              `json:"profile_id"`
		DisplayName   string              `json:"display_name"`
		Provider      string              `json:"provider"`
		Protocol      string              `json:"protocol"`
		Models        AgentModelMatrix    `json:"models"`
		Claude        AgentClaudeControls `json:"claude"`
		Aliases       []alias             `json:"model_usage_aliases"`
	}{
		SchemaVersion: snapshot.SchemaVersion,
		ProfileID:     snapshot.ProfileID,
		DisplayName:   snapshot.DisplayName,
		Provider:      snapshot.Provider,
		Protocol:      snapshot.Protocol,
		Models:        snapshot.Models,
		Claude:        snapshot.Claude,
		Aliases:       aliases,
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal canonical agent profile snapshot: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
