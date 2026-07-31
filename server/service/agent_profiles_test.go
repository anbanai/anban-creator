package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/billing"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func testProfileEnvs(endpoint, token, modelID string) map[string]string {
	return map[string]string{
		model.ClaudeEnvBaseURL:                  endpoint,
		model.ClaudeEnvAuthToken:                token,
		model.ClaudeEnvModel:                    modelID,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":          modelID,
		"ANTHROPIC_DEFAULT_FABLE_MODEL":         modelID,
		"ANTHROPIC_DEFAULT_SONNET_MODEL":        modelID,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":         modelID,
		"CLAUDE_CODE_EFFORT_LEVEL":              "high",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS":        "262144",
		"MAX_THINKING_TOKENS":                   "32768",
		"CLAUDE_CODE_DISABLE_THINKING":          "false",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":       "200000",
		"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":       "80",
		"CLAUDE_CODE_DISABLE_1M_CONTEXT":        "true",
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":      "true",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS":         "64000",
		"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING": "false",
		"CLAUDE_CODE_SUBAGENT_MODEL":            modelID,
		"ENABLE_TOOL_SEARCH":                    "true",
	}
}

func configuredAgentProfile(provider, description, endpoint, token, modelID, canonical string) srvconfig.ClaudeExecutionProfileConfig {
	return srvconfig.ClaudeExecutionProfileConfig{
		Provider: provider, Description: description,
		Envs:              testProfileEnvs(endpoint, token, modelID),
		ModelUsageAliases: map[string]string{modelID: canonical},
	}
}

func testProfileConfig() map[string]srvconfig.ClaudeExecutionProfileConfig {
	return map[string]srvconfig.ClaudeExecutionProfileConfig{
		"effective": configuredAgentProfile("deepseek", "low cost", "https://api.deepseek.test/anthropic", "deepseek-secret", "deepseek-v4-flash", "deepseek-v4-flash"),
		"balanced":  configuredAgentProfile("zhipu", "balanced", "https://open.bigmodel.test/api/anthropic", "zhipu-secret", "glm-5.2", "glm-5.2"),
		"quality":   configuredAgentProfile("moonshot", "maximum", "https://api.moonshot.test/anthropic", "moonshot-secret", "kimi-k3[1m]", "kimi-k3"),
	}
}

func testAgentProfiles() []AgentExecutionProfile {
	return []AgentExecutionProfile{
		testExecutionProfile("effective", configuredAgentProfile("deepseek", "", "https://api.deepseek.example/anthropic", "deepseek-secret", "deepseek-v4-flash", "deepseek-v4-flash"), model.TierFree),
		testExecutionProfile("balanced", configuredAgentProfile("volcengine_ark", "", "https://ark.example/anthropic", "doubao-secret", "doubao-seed-evolving", "doubao-seed-evolving"), model.TierPro),
		testExecutionProfile("quality", configuredAgentProfile("moonshot", "", "https://api.moonshot.example/anthropic", "moonshot-secret", "kimi-k3[1m]", "kimi-k3"), model.TierEnterprise),
	}
}

func testExecutionProfile(id string, configured srvconfig.ClaudeExecutionProfileConfig, minTier model.Tier) AgentExecutionProfile {
	displayName := map[string]string{"effective": "性价比", "balanced": "平衡型", "quality": "极致效果"}[id]
	return AgentExecutionProfile{
		ID: id, DisplayName: displayName, Description: configured.Description,
		Provider: configured.Provider, Protocol: "anthropic", Envs: model.CloneClaudeProfileEnvs(configured.Envs),
		ModelUsageAliases: cloneModelUsageAliasTargets(configured.ModelUsageAliases), MinTier: minTier, Available: true,
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

func TestAgentProfileRegistryUsesExactProductsAndTierAccess(t *testing.T) {
	registry, err := NewAgentProfileRegistryFromConfig(testProfileConfig(), testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}

	capabilities := registry.CapabilitiesForTier(model.TierFree)
	if len(capabilities) != 3 || capabilities[0].ID != "effective" || capabilities[0].DisplayName != "性价比" || !capabilities[0].Available || capabilities[0].ModelName != "deepseek-v4-flash" {
		t.Fatalf("effective capability = %#v", capabilities)
	}
	if capabilities[1].ID != "balanced" || capabilities[1].Available || capabilities[1].UnavailableReason != "requires_pro" {
		t.Fatalf("balanced capability = %#v", capabilities[1])
	}
	if capabilities[2].ID != "quality" || capabilities[2].Available || capabilities[2].UnavailableReason != "requires_enterprise" {
		t.Fatalf("quality capability = %#v", capabilities[2])
	}
	if _, err := registry.ResolveForTier("quality", model.TierPro); !errors.Is(err, ErrAgentProfileAccessDenied) {
		t.Fatalf("quality for Pro = %v", err)
	}
	if got := registry.DefaultForTier(model.TierEnterprise); got.ID != "effective" {
		t.Fatalf("default = %#v", got)
	}
}

func TestAgentProfileRegistryRejectsObsoleteProductIDs(t *testing.T) {
	registry, err := NewAgentProfileRegistryFromConfig(testProfileConfig(), testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	for _, obsolete := range []string{"cost_effective", "maximum_quality"} {
		if _, err := registry.ResolveForTier(obsolete, model.TierEnterprise); !errors.Is(err, ErrAgentProfileNotFound) {
			t.Fatalf("resolve obsolete profile %q error = %v", obsolete, err)
		}
		profiles := testProfileConfig()
		profiles[obsolete] = profiles["effective"]
		if _, err := NewAgentProfileRegistryFromConfig(profiles, testCostCatalog()); !errors.Is(err, ErrAgentProfileInvalid) {
			t.Fatalf("profile %q error = %v", obsolete, err)
		}
	}
}

func TestAgentProfileRegistryDoesNotHardCodeProviderIdentity(t *testing.T) {
	costs := billing.CostCatalog{Models: map[string]billing.ModelCostConfig{"zhipu/glm-5.2": {PricingType: "token"}}}
	configured := configuredAgentProfile("zhipu", "economical", "https://open.bigmodel.cn/api/anthropic", "secret", "glm-5.2", "glm-5.2")
	registry, err := NewAgentProfileRegistryFromConfig(map[string]srvconfig.ClaudeExecutionProfileConfig{"effective": configured}, costs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.ResolveForTier("effective", model.TierFree)
	if err != nil || got.Provider != "zhipu" || got.Envs[model.ClaudeEnvModel] != "glm-5.2" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestAgentProfileRegistryUsesModelIdentityWhenAliasIsOmitted(t *testing.T) {
	costs := billing.CostCatalog{Models: map[string]billing.ModelCostConfig{"zhipu/glm-5.2": {PricingType: "token"}}}
	configured := configuredAgentProfile("zhipu", "balanced", "https://open.bigmodel.cn/api/anthropic", "secret", "glm-5.2", "glm-5.2")
	configured.ModelUsageAliases = nil
	registry, err := NewAgentProfileRegistryFromConfig(map[string]srvconfig.ClaudeExecutionProfileConfig{"effective": configured}, costs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.ResolveForTier("effective", model.TierFree)
	if err != nil || !got.Available {
		t.Fatalf("identity-mapped profile = %#v, %v", got, err)
	}
}

func TestAgentProfileRegistryMarksModelAliasAndCostMismatchUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]srvconfig.ClaudeExecutionProfileConfig, *billing.CostCatalog)
	}{
		{name: "referenced model cost missing", mutate: func(_ map[string]srvconfig.ClaudeExecutionProfileConfig, costs *billing.CostCatalog) {
			delete(costs.Models, "zhipu/glm-5.2")
		}},
		{name: "extra alias target cost missing", mutate: func(profiles map[string]srvconfig.ClaudeExecutionProfileConfig, _ *billing.CostCatalog) {
			profile := profiles["balanced"]
			profile.ModelUsageAliases["provider-side-name"] = "unpriced-model"
			profiles["balanced"] = profile
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profiles, costs := testProfileConfig(), testCostCatalog()
			tc.mutate(profiles, &costs)
			registry, err := NewAgentProfileRegistryFromConfig(profiles, costs)
			if err != nil {
				t.Fatal(err)
			}
			got, err := registry.Resolve("balanced")
			if err != nil {
				t.Fatal(err)
			}
			if got.Available || got.UnavailableReason != "agent_model_cost_unmapped" {
				t.Fatalf("balanced = %#v", got)
			}
			if _, err := registry.ResolveForTier("balanced", model.TierEnterprise); !errors.Is(err, ErrAgentModelCostUnmapped) {
				t.Fatalf("ResolveForTier = %v", err)
			}
		})
	}
}

func TestAgentProfileRegistryMarksBootstrapInvalidAliasesUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*srvconfig.ClaudeExecutionProfileConfig)
	}{
		{name: "raw alias has whitespace", mutate: func(profile *srvconfig.ClaudeExecutionProfileConfig) {
			profile.ModelUsageAliases[" glm-5.2"] = "glm-5.2"
		}},
		{name: "canonical alias has whitespace", mutate: func(profile *srvconfig.ClaudeExecutionProfileConfig) {
			profile.ModelUsageAliases["glm-5.2"] = "glm-5.2 "
		}},
		{name: "alias count exceeds bootstrap limit", mutate: func(profile *srvconfig.ClaudeExecutionProfileConfig) {
			for i := 0; i < 128; i++ {
				profile.ModelUsageAliases[fmt.Sprintf("extra-%03d", i)] = "glm-5.2"
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profiles := testProfileConfig()
			profile := profiles["balanced"]
			tc.mutate(&profile)
			profiles["balanced"] = profile
			registry, err := NewAgentProfileRegistryFromConfig(profiles, testCostCatalog())
			if err != nil {
				t.Fatal(err)
			}
			got, err := registry.Resolve("balanced")
			if err != nil {
				t.Fatal(err)
			}
			if got.Available || got.UnavailableReason != "agent_provider_unavailable" {
				t.Fatalf("balanced = %#v", got)
			}
		})
	}
}

func TestAgentProfileRegistryInvalidConnectionIsUnavailableWithoutSecretLeak(t *testing.T) {
	profiles := testProfileConfig()
	profiles["effective"].Envs[model.ClaudeEnvAuthToken] = ""
	profiles["quality"].Envs[model.ClaudeEnvAuthToken] = "do-not-leak\ninvalid"
	registry, err := NewAgentProfileRegistryFromConfig(profiles, testCostCatalog())
	if err != nil {
		t.Fatalf("invalid connection should fail closed per profile: %v", err)
	}
	for _, id := range []string{"effective", "quality"} {
		got, resolveErr := registry.Resolve(id)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if got.Available || got.UnavailableReason == "" || strings.Contains(got.UnavailableReason, "do-not-leak") {
			t.Fatalf("%s = %#v", id, got)
		}
	}
}

func TestAgentProfileRegistryConstructionDoesNotProbeProviderNetwork(t *testing.T) {
	profiles := testProfileConfig()
	profiles["effective"].Envs[model.ClaudeEnvBaseURL] = "https://127.0.0.1:1/anthropic"
	if _, err := NewAgentProfileRegistryFromConfig(profiles, testCostCatalog()); err != nil {
		t.Fatalf("registry construction must not dial provider: %v", err)
	}
}

func TestResolveRuntimeMergesOnlyCurrentToken(t *testing.T) {
	profiles := testProfileConfig()
	registry, err := NewAgentProfileRegistryFromConfig(profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	profile, err := registry.ResolveForTier("quality", model.TierEnterprise)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != model.ClaudeProfileSchemaV3 || snapshot.Envs[model.ClaudeEnvAuthToken] != "" || strings.Contains(string(mustJSON(t, snapshot)), "moonshot-secret") {
		t.Fatalf("snapshot leaked token: %#v", snapshot)
	}

	profiles["quality"].Envs[model.ClaudeEnvAuthToken] = "rotated-secret"
	profiles["quality"].Envs[model.ClaudeEnvBaseURL] = "https://rotated.example/anthropic"
	profiles["quality"].Envs[model.ClaudeEnvModel] = "new-model"
	profiles["quality"].Envs["CLAUDE_CODE_EFFORT_LEVEL"] = "low"
	profiles["quality"].Envs["MAX_THINKING_TOKENS"] = "1"
	profiles["quality"].ModelUsageAliases["new-model"] = "kimi-k3"
	registry, err = NewAgentProfileRegistryFromConfig(profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := registry.ResolveRuntime("quality", snapshot, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Envs[model.ClaudeEnvAuthToken] != "rotated-secret" {
		t.Fatalf("token = %q", runtime.Envs[model.ClaudeEnvAuthToken])
	}
	for key, want := range map[string]string{
		model.ClaudeEnvBaseURL:     "https://api.moonshot.test/anthropic",
		model.ClaudeEnvModel:       "kimi-k3[1m]",
		"CLAUDE_CODE_EFFORT_LEVEL": "high",
		"MAX_THINKING_TOKENS":      "32768",
	} {
		if got := runtime.Envs[key]; got != want {
			t.Fatalf("runtime env %s = %q, want frozen %q", key, got, want)
		}
	}
}

func TestResolveRuntimePreservesCurrentTokenBytes(t *testing.T) {
	profiles := testProfileConfig()
	registry, err := NewAgentProfileRegistryFromConfig(profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := registry.ResolveForTier("effective", model.TierFree)
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatal(err)
	}

	const rotated = " rotated-secret "
	profiles["effective"].Envs[model.ClaudeEnvAuthToken] = rotated
	registry, err = NewAgentProfileRegistryFromConfig(profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := registry.ResolveRuntime("effective", snapshot, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime.Envs[model.ClaudeEnvAuthToken]; got != rotated {
		t.Fatalf("runtime token bytes = %q, want %q", got, rotated)
	}
}

func TestAgentProfileFreezeRejectsMissingAuthToken(t *testing.T) {
	profile := testAgentProfiles()[0]
	delete(profile.Envs, model.ClaudeEnvAuthToken)
	if _, _, err := profile.Freeze(); err == nil {
		t.Fatal("Freeze accepted an available profile without ANTHROPIC_AUTH_TOKEN")
	}
}

func TestResolveRuntimeRejectsSnapshotFingerprintAndCurrentProviderConflict(t *testing.T) {
	registry, err := NewAgentProfileRegistryFromConfig(testProfileConfig(), testCostCatalog())
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

	invalid := snapshot
	invalid.Envs = model.CloneClaudeProfileEnvs(snapshot.Envs)
	invalid.Envs["CLAUDE_CODE_EFFORT_LEVEL"] = "low"
	if _, fingerprintErr := model.AgentProfileFingerprint(invalid); fingerprintErr != nil {
		t.Fatalf("drifted snapshot fixture is invalid: %v", fingerprintErr)
	}
	if _, err := registry.ResolveRuntime("balanced", invalid, fingerprint); !errors.Is(err, ErrAgentProfileSnapshotConflict) {
		t.Fatalf("drifted snapshot error = %v", err)
	}
	invalid = snapshot
	invalid.SchemaVersion = 2
	if _, err := registry.ResolveRuntime("balanced", invalid, fingerprint); !errors.Is(err, ErrAgentProfileSnapshotInvalid) {
		t.Fatalf("invalid snapshot error = %v", err)
	}
	if _, err := registry.ResolveRuntime("effective", snapshot, fingerprint); !errors.Is(err, ErrAgentProfileSnapshotConflict) {
		t.Fatalf("profile id conflict error = %v", err)
	}

	profiles := testProfileConfig()
	balanced := profiles["balanced"]
	balanced.Provider = "other-provider"
	profiles["balanced"] = balanced
	registry, err = NewAgentProfileRegistryFromConfig(profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ResolveRuntime("balanced", snapshot, fingerprint); !errors.Is(err, ErrAgentProviderUnavailable) {
		t.Fatalf("provider conflict error = %v", err)
	}
}

func TestResolveRuntimeRejectsMissingOrInvalidCurrentTokenWithoutLeak(t *testing.T) {
	baseProfiles := testProfileConfig()
	baseRegistry, err := NewAgentProfileRegistryFromConfig(baseProfiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := baseRegistry.ResolveForTier("effective", model.TierFree)
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatal(err)
	}

	for _, token := range []string{"", "sensitive-value\ninvalid"} {
		profiles := testProfileConfig()
		profiles["effective"].Envs[model.ClaudeEnvAuthToken] = token
		registry, err := NewAgentProfileRegistryFromConfig(profiles, testCostCatalog())
		if err != nil {
			t.Fatal(err)
		}
		_, resolveErr := registry.ResolveRuntime("effective", snapshot, fingerprint)
		if !errors.Is(resolveErr, ErrAgentProviderUnavailable) || strings.Contains(resolveErr.Error(), "sensitive-value") {
			t.Fatalf("token %q error = %v", token, resolveErr)
		}
	}
}

func TestResolveRuntimeIgnoresCurrentCostAvailabilityForFrozenTask(t *testing.T) {
	profiles := testProfileConfig()
	registry, err := NewAgentProfileRegistryFromConfig(profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := registry.ResolveForTier("balanced", model.TierPro)
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatal(err)
	}

	costs := testCostCatalog()
	delete(costs.Models, "zhipu/glm-5.2")
	registry, err = NewAgentProfileRegistryFromConfig(profiles, costs)
	if err != nil {
		t.Fatal(err)
	}
	if current, _ := registry.Resolve("balanced"); current.Available || current.UnavailableReason != "agent_model_cost_unmapped" {
		t.Fatalf("current profile = %#v", current)
	}
	if _, err := registry.ResolveRuntime("balanced", snapshot, fingerprint); err != nil {
		t.Fatalf("frozen runtime depended on current cost catalog: %v", err)
	}
}

func TestAgentProfileSnapshotAndRegistryCopiesAreImmutable(t *testing.T) {
	profiles := testProfileConfig()
	registry, err := NewAgentProfileRegistryFromConfig(profiles, testCostCatalog())
	if err != nil {
		t.Fatal(err)
	}
	profiles["balanced"].Envs[model.ClaudeEnvModel] = "mutated"
	profiles["balanced"].ModelUsageAliases["glm-5.2"] = "mutated"
	first, err := registry.Resolve("balanced")
	if err != nil {
		t.Fatal(err)
	}
	first.Envs[model.ClaudeEnvModel] = "mutated-again"
	first.ModelUsageAliases["glm-5.2"] = "mutated-again"
	second, _ := registry.Resolve("balanced")
	if second.Envs[model.ClaudeEnvModel] != "glm-5.2" || second.ModelUsageAliases["glm-5.2"] != "glm-5.2" {
		t.Fatalf("registry profile mutated: %#v", second)
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
