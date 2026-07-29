package service

import (
	"errors"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestAgentProfileRegistryCopiesAndValidatesModelUsageAliases(t *testing.T) {
	const rawAlias = "doubao-seed-evolving-latest-version"
	configuredAliases := map[string]string{rawAlias: "doubao-seed-evolving"}
	configured := map[string]srvconfig.ClaudeExecutionProfileConfig{
		"cost_effective": {
			DisplayName: "性价比", ModelName: "DeepSeek 4 Pro", Provider: "deepseek",
			ModelID: "deepseek-v4-pro", Protocol: "anthropic", BaseURL: "https://deepseek.example.com",
			AuthToken: "deepseek-secret", MinTier: model.TierFree,
		},
		"balanced": {
			DisplayName: "平衡型", ModelName: "豆包", Provider: "volcengine_ark",
			ModelID: "doubao-seed-evolving", Protocol: "anthropic", BaseURL: "https://ark.example.com",
			AuthToken: "ark-secret", ModelUsageAliases: configuredAliases, MinTier: model.TierPro,
		},
		"maximum_quality": {
			DisplayName: "极致效果", ModelName: "Kimi K3（1M）", Provider: "kimi", ModelID: "k3",
			Protocol: "anthropic", BaseURL: "https://api.kimi.com/coding/", AuthToken: "kimi-secret",
			MinTier: model.TierEnterprise, ContextWindow: 1048576, ReasoningEffort: "high", ThinkingRequired: true,
		},
	}

	registry, err := NewAgentProfileRegistryFromConfig(configured)
	if err != nil {
		t.Fatalf("NewAgentProfileRegistryFromConfig: %v", err)
	}
	configuredAliases[rawAlias] = "mutated-after-construction"
	profile, err := registry.Resolve("balanced")
	if err != nil {
		t.Fatal(err)
	}
	if got := profile.ModelUsageAliases[rawAlias]; got != "doubao-seed-evolving" {
		t.Fatalf("resolved alias = %q, want frozen canonical model", got)
	}
	profile.ModelUsageAliases[rawAlias] = "mutated-after-resolve"
	profile, err = registry.Resolve("balanced")
	if err != nil {
		t.Fatal(err)
	}
	if got := profile.ModelUsageAliases[rawAlias]; got != "doubao-seed-evolving" {
		t.Fatalf("registry alias was mutated through Resolve: %q", got)
	}

	for _, test := range []struct {
		name    string
		aliases map[string]string
	}{
		{name: "empty alias", aliases: map[string]string{"": "doubao-seed-evolving"}},
		{name: "wrong canonical target", aliases: map[string]string{rawAlias: "other-model"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			profiles := testAgentProfiles()
			profiles[1].ModelUsageAliases = test.aliases
			if _, err := NewAgentProfileRegistry(profiles); !errors.Is(err, ErrAgentProfileInvalid) {
				t.Fatalf("NewAgentProfileRegistry error = %v, want ErrAgentProfileInvalid", err)
			}
		})
	}
}

func TestAgentProfileRegistry_CapabilitiesIncludeLockedAndUnavailableProfiles(t *testing.T) {
	profiles := testAgentProfiles()
	profiles[2].Available = false
	profiles[2].UnavailableReason = "provider_configuration_missing"
	registry, err := NewAgentProfileRegistry(profiles)
	if err != nil {
		t.Fatal(err)
	}

	capabilities := registry.CapabilitiesForTier(model.TierFree)
	if len(capabilities) != 3 {
		t.Fatalf("capabilities = %d, want 3", len(capabilities))
	}
	if !capabilities[0].Available || capabilities[0].ID != "cost_effective" {
		t.Fatalf("free capability = %#v", capabilities[0])
	}
	if capabilities[1].Available || capabilities[1].UnavailableReason != "requires_pro" {
		t.Fatalf("balanced capability = %#v", capabilities[1])
	}
	if capabilities[2].Available || capabilities[2].UnavailableReason != "provider_configuration_missing" {
		t.Fatalf("maximum capability = %#v", capabilities[2])
	}
}

func TestAgentProfileRegistry_ResolveRuntimePreservesFrozenControlsAndRejectsIdentityDrift(t *testing.T) {
	profiles := testAgentProfiles()
	snapshot := profiles[2].Snapshot()
	profiles[2].BaseURL = "https://rotated.kimi.example.com/coding/"
	profiles[2].AuthToken = "rotated-secret"
	profiles[2].DisplayName = "renamed after task creation"
	profiles[2].ContextWindow = 524288
	profiles[2].ReasoningEffort = "medium"
	registry, err := NewAgentProfileRegistry(profiles)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.ResolveRuntime("maximum_quality", snapshot)
	if err != nil || resolved.ModelID != "k3" || resolved.BaseURL != "https://rotated.kimi.example.com/coding/" || resolved.AuthToken != "rotated-secret" {
		t.Fatalf("ResolveRuntime = %#v, %v", resolved, err)
	}
	if resolved.ContextWindow != snapshot.ContextWindow || resolved.ReasoningEffort != snapshot.ReasoningEffort || resolved.ThinkingRequired != snapshot.ThinkingRequired || resolved.DisplayName != snapshot.DisplayName {
		t.Fatalf("runtime controls = %#v, want frozen snapshot %#v", resolved, snapshot)
	}

	drifted := snapshot
	drifted.ModelID = "replacement-model"
	if _, err := registry.ResolveRuntime("maximum_quality", drifted); !errors.Is(err, ErrAgentProfileSnapshotMismatch) {
		t.Fatalf("drift error = %v, want ErrAgentProfileSnapshotMismatch", err)
	}

	profiles[2].Available = false
	profiles[2].UnavailableReason = "provider_configuration_missing"
	unavailable, err := NewAgentProfileRegistry(profiles)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unavailable.ResolveRuntime("maximum_quality", snapshot); !errors.Is(err, ErrAgentProfileUnavailable) {
		t.Fatalf("unavailable error = %v, want ErrAgentProfileUnavailable", err)
	}
}

func TestNewAgentProfileRegistryFromConfigFailsClosed(t *testing.T) {
	configured := map[string]srvconfig.ClaudeExecutionProfileConfig{}
	for _, profile := range testAgentProfiles() {
		baseURL := "https://agent.example.com/"
		if profile.ID == "maximum_quality" {
			baseURL = "https://api.kimi.com/coding/"
		}
		configured[profile.ID] = srvconfig.ClaudeExecutionProfileConfig{
			DisplayName: profile.DisplayName, ModelName: profile.ModelName, Description: profile.Description,
			Provider: profile.Provider, ModelID: profile.ModelID, Protocol: profile.Protocol,
			BaseURL: baseURL, AuthToken: "test-token", MinTier: profile.MinTier,
			ContextWindow: profile.ContextWindow, ReasoningEffort: profile.ReasoningEffort,
			ThinkingRequired: profile.ThinkingRequired,
		}
	}

	missingSecret := cloneProfileConfig(configured)
	item := missingSecret["cost_effective"]
	item.AuthToken = ""
	missingSecret["cost_effective"] = item
	registry, err := NewAgentProfileRegistryFromConfig(missingSecret)
	if err != nil {
		t.Fatalf("missing secret should publish unavailable capability: %v", err)
	}
	profile, err := registry.Resolve("cost_effective")
	if err != nil || profile.Available || profile.UnavailableReason != "provider_configuration_missing" {
		t.Fatalf("missing-secret profile = %#v, %v", profile, err)
	}

	balancedWithoutDedicatedCredential := cloneProfileConfig(configured)
	balanced := balancedWithoutDedicatedCredential["balanced"]
	balanced.AuthToken = ""
	balancedWithoutDedicatedCredential["balanced"] = balanced
	registry, err = NewAgentProfileRegistryFromConfig(balancedWithoutDedicatedCredential)
	if err != nil {
		t.Fatalf("missing balanced credential must not prevent registry startup: %v", err)
	}
	if _, err := registry.ResolveForTier("balanced", model.TierPro); !errors.Is(err, ErrAgentProfileUnavailable) {
		t.Fatalf("balanced without its dedicated credential = %v, want ErrAgentProfileUnavailable", err)
	}

	if values := cloneProfileConfig(configured); true {
		delete(values, "balanced")
		registry, err := NewAgentProfileRegistryFromConfig(values)
		if err != nil {
			t.Fatalf("missing known profile must remain listable: %v", err)
		}
		profile, err := registry.Resolve("balanced")
		if err != nil || profile.Available || profile.UnavailableReason != "provider_configuration_missing" {
			t.Fatalf("synthesized balanced profile = %#v, %v", profile, err)
		}
	}

	if values := cloneProfileConfig(configured); true {
		values["unknown"] = srvconfig.ClaudeExecutionProfileConfig{}
		if _, err := NewAgentProfileRegistryFromConfig(values); !errors.Is(err, ErrAgentProfileInvalid) {
			t.Fatalf("unknown profile error = %v, want ErrAgentProfileInvalid", err)
		}
	}

	tests := []struct {
		name   string
		id     string
		mutate func(map[string]srvconfig.ClaudeExecutionProfileConfig)
	}{
		{name: "missing provider", id: "balanced", mutate: func(values map[string]srvconfig.ClaudeExecutionProfileConfig) {
			value := values["balanced"]
			value.Provider = ""
			values["balanced"] = value
		}},
		{name: "missing model", id: "balanced", mutate: func(values map[string]srvconfig.ClaudeExecutionProfileConfig) {
			value := values["balanced"]
			value.ModelID = ""
			values["balanced"] = value
		}},
		{name: "missing protocol", id: "balanced", mutate: func(values map[string]srvconfig.ClaudeExecutionProfileConfig) {
			value := values["balanced"]
			value.Protocol = ""
			values["balanced"] = value
		}},
		{name: "unsupported provider", id: "balanced", mutate: func(values map[string]srvconfig.ClaudeExecutionProfileConfig) {
			value := values["balanced"]
			value.Provider = "other"
			values["balanced"] = value
		}},
		{name: "non https endpoint", id: "cost_effective", mutate: func(values map[string]srvconfig.ClaudeExecutionProfileConfig) {
			value := values["cost_effective"]
			value.BaseURL = "http://deepseek.example.com"
			values["cost_effective"] = value
		}},
		{name: "wrong kimi endpoint", id: "maximum_quality", mutate: func(values map[string]srvconfig.ClaudeExecutionProfileConfig) {
			value := values["maximum_quality"]
			value.BaseURL = "https://kimi.example.com"
			values["maximum_quality"] = value
		}},
		{name: "wrong kimi context", id: "maximum_quality", mutate: func(values map[string]srvconfig.ClaudeExecutionProfileConfig) {
			value := values["maximum_quality"]
			value.ContextWindow = 0
			values["maximum_quality"] = value
		}},
		{name: "kimi thinking disabled", id: "maximum_quality", mutate: func(values map[string]srvconfig.ClaudeExecutionProfileConfig) {
			value := values["maximum_quality"]
			value.ThinkingRequired = false
			values["maximum_quality"] = value
		}},
		{name: "kimi effort not high", id: "maximum_quality", mutate: func(values map[string]srvconfig.ClaudeExecutionProfileConfig) {
			value := values["maximum_quality"]
			value.ReasoningEffort = "medium"
			values["maximum_quality"] = value
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := cloneProfileConfig(configured)
			tt.mutate(values)
			registry, err := NewAgentProfileRegistryFromConfig(values)
			if err != nil {
				t.Fatalf("profile configuration should remain listable: %v", err)
			}
			profile, err := registry.Resolve(tt.id)
			if err != nil || profile.Available || profile.UnavailableReason == "" {
				t.Fatalf("misconfigured profile = %#v, %v", profile, err)
			}
		})
	}
}

func cloneProfileConfig(source map[string]srvconfig.ClaudeExecutionProfileConfig) map[string]srvconfig.ClaudeExecutionProfileConfig {
	result := make(map[string]srvconfig.ClaudeExecutionProfileConfig, len(source))
	for id, profile := range source {
		result[id] = profile
	}
	return result
}

func testAgentProfiles() []AgentExecutionProfile {
	return []AgentExecutionProfile{
		{
			ID: "cost_effective", DisplayName: "性价比", ModelName: "DeepSeek 4 Pro",
			Provider: "deepseek", ModelID: "deepseek-v4-pro", Protocol: "anthropic",
			MinTier: model.TierFree, Description: "低成本高效率", Available: true,
		},
		{
			ID: "balanced", DisplayName: "平衡型", ModelName: "豆包",
			Provider: "volcengine_ark", ModelID: "doubao-seed-evolving", Protocol: "anthropic",
			MinTier: model.TierPro, Description: "速度与效果平衡", Available: true,
		},
		{
			ID: "maximum_quality", DisplayName: "极致效果", ModelName: "Kimi K3（1M）",
			Provider: "kimi", ModelID: "k3", Protocol: "anthropic",
			MinTier: model.TierEnterprise, ContextWindow: 1048576,
			ReasoningEffort: "high", ThinkingRequired: true, Description: "旗舰质量", Available: true,
		},
	}
}

func TestAgentProfileRegistry_AccessibilityAndSelection(t *testing.T) {
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatalf("NewAgentProfileRegistry: %v", err)
	}

	tests := []struct {
		name        string
		tier        model.Tier
		wantIDs     []string
		wantDefault string
	}{
		{name: "free", tier: model.TierFree, wantIDs: []string{"cost_effective"}, wantDefault: "cost_effective"},
		{name: "pro", tier: model.TierPro, wantIDs: []string{"cost_effective", "balanced"}, wantDefault: "cost_effective"},
		{name: "enterprise", tier: model.TierEnterprise, wantIDs: []string{"cost_effective", "balanced", "maximum_quality"}, wantDefault: "cost_effective"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profiles := registry.ListForTier(tt.tier)
			gotIDs := make([]string, 0, len(profiles))
			for _, profile := range profiles {
				gotIDs = append(gotIDs, profile.ID)
			}
			if len(gotIDs) != len(tt.wantIDs) {
				t.Fatalf("profiles = %#v, want ids %#v", gotIDs, tt.wantIDs)
			}
			for i := range gotIDs {
				if gotIDs[i] != tt.wantIDs[i] {
					t.Fatalf("profile[%d] = %q, want %q", i, gotIDs[i], tt.wantIDs[i])
				}
			}
			if got := registry.DefaultForTier(tt.tier); got.ID != tt.wantDefault {
				t.Fatalf("default profile = %q, want %q", got.ID, tt.wantDefault)
			}
		})
	}

	if _, err := registry.ResolveForTier("maximum_quality", model.TierPro); err == nil {
		t.Fatal("Pro user must not resolve enterprise-only profile")
	}
}

func TestAgentProfileRegistry_RejectsInvalidProfiles(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]AgentExecutionProfile)
	}{
		{name: "duplicate id", mutate: func(profiles []AgentExecutionProfile) { profiles[1].ID = profiles[0].ID }},
		{name: "missing model", mutate: func(profiles []AgentExecutionProfile) { profiles[0].ModelID = "" }},
		{name: "invalid protocol", mutate: func(profiles []AgentExecutionProfile) { profiles[0].Protocol = "openai" }},
		{name: "required thinking without effort", mutate: func(profiles []AgentExecutionProfile) { profiles[2].ReasoningEffort = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profiles := testAgentProfiles()
			tc.mutate(profiles)
			if _, err := NewAgentProfileRegistry(profiles); err == nil {
				t.Fatal("expected invalid profile error")
			}
		})
	}
}

func TestAgentProfileRegistry_SnapshotIsNonSensitiveAndStable(t *testing.T) {
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatalf("NewAgentProfileRegistry: %v", err)
	}
	profile, err := registry.ResolveForTier("maximum_quality", model.TierEnterprise)
	if err != nil {
		t.Fatalf("ResolveForTier: %v", err)
	}
	snapshot := profile.Snapshot()
	if snapshot.ProfileID != "maximum_quality" || snapshot.ModelID != "k3" || snapshot.ContextWindow != 1048576 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.AuthToken != "" || snapshot.BaseURL != "" {
		t.Fatalf("snapshot contains sensitive/provider routing data: %#v", snapshot)
	}
}
