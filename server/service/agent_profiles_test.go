package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/billing"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func testMatrix(name string) srvconfig.ClaudeModelMatrixConfig {
	return srvconfig.ClaudeModelMatrixConfig{Default: name, Opus: name, Fable: name, Sonnet: name, Haiku: name}
}

func testProviderConfig() map[string]srvconfig.ClaudeProviderConfig {
	return map[string]srvconfig.ClaudeProviderConfig{
		"deepseek": {Protocol: "anthropic", BaseURL: "https://api.deepseek.test/anthropic", AuthToken: "deepseek-secret"},
		"zhipu":    {Protocol: "anthropic", BaseURL: "https://open.bigmodel.test/api/anthropic", AuthToken: "zhipu-secret"},
		"moonshot": {Protocol: "anthropic", BaseURL: "https://api.moonshot.test/anthropic", AuthToken: "moonshot-secret"},
	}
}

func testProfileConfig() map[string]srvconfig.ClaudeExecutionProfileConfig {
	return map[string]srvconfig.ClaudeExecutionProfileConfig{
		"cost_effective": {
			Provider: "deepseek", Description: "low cost", Models: testMatrix("deepseek-v4-flash"),
			ModelUsageAliases: map[string]string{"deepseek-v4-flash": "deepseek-v4-flash"},
		},
		"balanced": {
			Provider: "zhipu", Description: "balanced", Models: testMatrix("glm-5.2"),
			ModelUsageAliases: map[string]string{"glm-5.2": "glm-5.2"},
		},
		"maximum_quality": {
			Provider: "moonshot", Description: "maximum", Models: testMatrix("kimi-k3[1m]"),
			ModelUsageAliases: map[string]string{"kimi-k3[1m]": "kimi-k3", "kimi-k3": "kimi-k3"},
		},
	}
}

func testCostCatalog() billing.CostCatalog {
	return billing.CostCatalog{Models: map[string]billing.ModelCostConfig{
		"deepseek/deepseek-v4-flash": {PricingType: "token"},
		"zhipu/glm-5.2":              {PricingType: "token"},
		"moonshot/kimi-k3":           {PricingType: "token"},
		"moonshot/kimi-k2.7-code":    {PricingType: "token"},
	}}
}

func TestAgentProfileRegistryDoesNotHardCodeProviderIdentity(t *testing.T) {
	costs := billing.CostCatalog{Models: map[string]billing.ModelCostConfig{"zhipu/glm-5.2": {PricingType: "token"}}}
	registry, err := NewAgentProfileRegistryFromConfig(
		map[string]srvconfig.ClaudeProviderConfig{"zhipu": {Protocol: "anthropic", BaseURL: "https://open.bigmodel.cn/api/anthropic", AuthToken: "secret"}},
		map[string]srvconfig.ClaudeExecutionProfileConfig{"cost_effective": {
			Provider: "zhipu", Models: testMatrix("glm-5.2"), ModelUsageAliases: map[string]string{"glm-5.2": "glm-5.2"},
		}}, costs,
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.ResolveForTier("cost_effective", model.TierFree)
	if err != nil || got.Provider != "zhipu" || got.Models.Default != "glm-5.2" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestAgentProfileRegistryMarksOnlyUnmappedProfileUnavailable(t *testing.T) {
	profiles := testProfileConfig()
	costs := testCostCatalog()
	delete(costs.Models, "zhipu/glm-5.2")
	registry, err := NewAgentProfileRegistryFromConfig(testProviderConfig(), profiles, costs)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := registry.Resolve("cost_effective"); !got.Available {
		t.Fatalf("cost_effective = %#v", got)
	}
	if got, _ := registry.Resolve("balanced"); got.Available || got.UnavailableReason != "agent_model_cost_unmapped" {
		t.Fatalf("balanced = %#v", got)
	}
}

func TestAgentProfileRegistryConstructionDoesNotProbeProviderNetwork(t *testing.T) {
	providers := testProviderConfig()
	providers["deepseek"] = srvconfig.ClaudeProviderConfig{Protocol: "anthropic", BaseURL: "https://127.0.0.1:1/anthropic", AuthToken: "secret"}
	if _, err := NewAgentProfileRegistryFromConfig(providers, testProfileConfig(), testCostCatalog()); err != nil {
		t.Fatalf("registry construction must not dial provider: %v", err)
	}
}

func TestResolveRuntimeKeepsFrozenModelsAndUsesCurrentProviderConnection(t *testing.T) {
	providers := testProviderConfig()
	profiles := testProfileConfig()
	registry, err := NewAgentProfileRegistryFromConfig(providers, profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	frozen := model.AgentProfileSnapshot{
		SchemaVersion: 2, ProfileID: "maximum_quality", DisplayName: "极致效果", Provider: "moonshot", Protocol: "anthropic",
		Models: model.AgentModelMatrix{Default: "kimi-k2.7-code", Opus: "kimi-k2.7-code", Fable: "kimi-k2.7-code", Sonnet: "kimi-k2.7-code", Haiku: "kimi-k2.7-code"},
		Claude: model.AgentClaudeControls{}, ModelUsageAliases: map[string]string{"kimi-k2.7-code": "kimi-k2.7-code"},
	}
	fingerprint, err := model.AgentProfileFingerprint(frozen)
	if err != nil {
		t.Fatal(err)
	}
	providers["moonshot"] = srvconfig.ClaudeProviderConfig{Protocol: "anthropic", BaseURL: "https://rotated.moonshot.test/anthropic", AuthToken: "rotated-secret"}
	registry, err = NewAgentProfileRegistryFromConfig(providers, profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.ResolveRuntime("maximum_quality", frozen, fingerprint)
	if err != nil || got.Models.Default != "kimi-k2.7-code" || got.BaseURL != "https://rotated.moonshot.test/anthropic" || got.AuthToken != "rotated-secret" {
		t.Fatalf("ResolveRuntime = %#v, %v", got, err)
	}

	delete(providers, "moonshot")
	registry, err = NewAgentProfileRegistryFromConfig(providers, profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ResolveRuntime("maximum_quality", frozen, fingerprint); !errors.Is(err, ErrAgentProviderUnavailable) {
		t.Fatalf("ResolveRuntime missing provider = %v", err)
	}
}

func TestResolveRuntimeRejectsSnapshotAndFingerprintConflict(t *testing.T) {
	registry, err := NewAgentProfileRegistryFromConfig(testProviderConfig(), testProfileConfig(), testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	profile, err := registry.ResolveForTier("balanced", model.TierPro)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Models.Default = "drifted"
	if _, err := registry.ResolveRuntime("balanced", snapshot, fingerprint); !errors.Is(err, ErrAgentProfileSnapshotMismatch) {
		t.Fatalf("ResolveRuntime conflict = %v", err)
	}
}

func TestAgentProfileRegistryCapabilitiesAndTierAccess(t *testing.T) {
	registry, err := NewAgentProfileRegistryFromConfig(testProviderConfig(), testProfileConfig(), testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	capabilities := registry.CapabilitiesForTier(model.TierFree)
	if len(capabilities) != 3 || capabilities[0].ID != "cost_effective" || !capabilities[0].Available || capabilities[1].UnavailableReason != "requires_pro" || capabilities[2].UnavailableReason != "requires_enterprise" {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	if _, err := registry.ResolveForTier("maximum_quality", model.TierPro); !errors.Is(err, ErrAgentProfileAccessDenied) {
		t.Fatalf("maximum_quality for Pro = %v", err)
	}
	if got := registry.DefaultForTier(model.TierEnterprise); got.ID != "cost_effective" {
		t.Fatalf("default = %#v", got)
	}
}

func TestAgentProfileSnapshotAndRegistryCopiesAreImmutable(t *testing.T) {
	profiles := testProfileConfig()
	registry, err := NewAgentProfileRegistryFromConfig(testProviderConfig(), profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	profiles["balanced"].ModelUsageAliases["glm-5.2"] = "mutated"
	first, err := registry.Resolve("balanced")
	if err != nil {
		t.Fatal(err)
	}
	first.ModelUsageAliases["glm-5.2"] = "mutated-again"
	second, _ := registry.Resolve("balanced")
	if second.ModelUsageAliases["glm-5.2"] != "glm-5.2" {
		t.Fatalf("registry alias mutated: %#v", second.ModelUsageAliases)
	}
	snapshot, fingerprint, err := second.Freeze()
	if err != nil || len(fingerprint) != 64 || strings.Contains(string(mustJSON(t, snapshot)), "secret") {
		t.Fatalf("Freeze = %#v %q %v", snapshot, fingerprint, err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
