package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/anbanai/anban-creator/server/billing"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func resolveAgentProfileForUser(ctx context.Context, repo repository.Repository, registry *AgentProfileRegistry, userID, profileID string) (AgentExecutionProfile, error) {
	if registry == nil || repo == nil {
		return AgentExecutionProfile{}, fmt.Errorf("%w: profile dependencies are unavailable", ErrAgentProfileUnavailable)
	}
	user, err := repo.Users().FindByID(ctx, strings.TrimSpace(userID))
	if err != nil {
		return AgentExecutionProfile{}, fmt.Errorf("find user tier: %w", err)
	}
	return registry.ResolveForTier(profileID, model.ResolveTier(user.Tier))
}

var (
	ErrAgentProfileInvalid          = errors.New("invalid agent execution profile")
	ErrAgentProfileNotFound         = errors.New("agent execution profile not found")
	ErrAgentProfileUnavailable      = errors.New("agent execution profile unavailable")
	ErrAgentProfileAccessDenied     = errors.New("agent execution profile access denied")
	ErrAgentProfileSnapshotInvalid  = errors.New("agent execution profile snapshot invalid")
	ErrAgentProfileSnapshotConflict = errors.New("agent execution profile snapshot conflict")
	ErrAgentProviderUnavailable     = errors.New("agent provider unavailable")
	ErrAgentModelCostUnmapped       = errors.New("agent model cost unmapped")
)

var agentProfileProducts = map[string]struct {
	DisplayName string
	MinTier     model.Tier
}{
	"cost_effective":  {DisplayName: "性价比", MinTier: model.TierFree},
	"balanced":        {DisplayName: "平衡型", MinTier: model.TierPro},
	"maximum_quality": {DisplayName: "极致效果", MinTier: model.TierEnterprise},
}

type AgentProfileCapability struct {
	ID                string                    `json:"id"`
	DisplayName       string                    `json:"display_name"`
	Description       string                    `json:"description"`
	Provider          string                    `json:"provider"`
	Protocol          string                    `json:"protocol"`
	Models            model.AgentModelMatrix    `json:"models"`
	Claude            model.AgentClaudeControls `json:"claude"`
	MinTier           model.Tier                `json:"min_tier"`
	Available         bool                      `json:"available"`
	UnavailableReason string                    `json:"unavailable_reason,omitempty"`
}

type AgentExecutionProfile struct {
	ID                string
	DisplayName       string
	Description       string
	Provider          string
	Protocol          string
	Models            model.AgentModelMatrix
	Claude            model.AgentClaudeControls
	ModelUsageAliases map[string]string
	BaseURL           string
	AuthToken         string
	MinTier           model.Tier
	Available         bool
	UnavailableReason string
}

type AgentProfileRegistry struct {
	profiles  map[string]AgentExecutionProfile
	providers map[string]srvconfig.ClaudeProviderConfig
}

func NewAgentProfileRegistryFromConfig(providers map[string]srvconfig.ClaudeProviderConfig, configured map[string]srvconfig.ClaudeExecutionProfileConfig, costs billing.CostCatalog) (*AgentProfileRegistry, error) {
	for id := range configured {
		if _, ok := agentProfileProducts[id]; !ok {
			return nil, fmt.Errorf("%w: unsupported profile %q", ErrAgentProfileInvalid, id)
		}
	}
	providerSnapshot := cloneClaudeProviders(providers)
	for id, provider := range providerSnapshot {
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("%w: provider id is required", ErrAgentProfileInvalid)
		}
		if strings.TrimSpace(provider.Protocol) != "anthropic" {
			return nil, fmt.Errorf("%w: provider %q must use the anthropic protocol", ErrAgentProfileInvalid, id)
		}
	}

	profiles := make([]AgentExecutionProfile, 0, len(agentProfileProducts))
	for _, id := range []string{"cost_effective", "balanced", "maximum_quality"} {
		product := agentProfileProducts[id]
		item, configuredProfile := configured[id]
		profile := AgentExecutionProfile{ID: id, DisplayName: product.DisplayName, MinTier: product.MinTier}
		if !configuredProfile {
			profile.UnavailableReason = "profile_configuration_missing"
			profiles = append(profiles, profile)
			continue
		}
		if err := validateConfiguredAgentProfile(id, item); err != nil {
			return nil, err
		}
		profile.Description = strings.TrimSpace(item.Description)
		profile.Provider = strings.TrimSpace(item.Provider)
		profile.Models = modelMatrixFromConfig(item.Models)
		profile.Claude = claudeControlsFromConfig(item.Claude)
		profile.ModelUsageAliases = cloneModelUsageAliasTargets(item.ModelUsageAliases)

		provider, providerExists := providerSnapshot[profile.Provider]
		if !providerExists || !validAgentProfileEndpoint(provider.BaseURL) || strings.TrimSpace(provider.AuthToken) == "" {
			profile.Protocol = "anthropic"
			profile.UnavailableReason = "agent_provider_unavailable"
			profiles = append(profiles, profile)
			continue
		}
		profile.Protocol = provider.Protocol
		profile.BaseURL = strings.TrimSpace(provider.BaseURL)
		profile.AuthToken = strings.TrimSpace(provider.AuthToken)
		if !profileModelsHaveCosts(profile, costs) {
			profile.UnavailableReason = "agent_model_cost_unmapped"
			profiles = append(profiles, profile)
			continue
		}
		profile.Available = true
		profiles = append(profiles, profile)
	}
	return newAgentProfileRegistry(profiles, providerSnapshot)
}

func validateConfiguredAgentProfile(id string, profile srvconfig.ClaudeExecutionProfileConfig) error {
	models := modelMatrixFromConfig(profile.Models)
	for _, field := range []struct{ role, value string }{
		{role: "default", value: models.Default}, {role: "opus", value: models.Opus},
		{role: "fable", value: models.Fable}, {role: "sonnet", value: models.Sonnet}, {role: "haiku", value: models.Haiku},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: profile %q models.%s is required", ErrAgentProfileInvalid, id, field.role)
		}
	}
	for raw, canonical := range profile.ModelUsageAliases {
		if strings.TrimSpace(raw) == "" || strings.TrimSpace(canonical) == "" || strings.Contains(canonical, "/") {
			return fmt.Errorf("%w: profile %q has an invalid model usage alias", ErrAgentProfileInvalid, id)
		}
	}
	return nil
}

func profileModelsHaveCosts(profile AgentExecutionProfile, costs billing.CostCatalog) bool {
	models := []string{profile.Models.Default, profile.Models.Opus, profile.Models.Fable, profile.Models.Sonnet, profile.Models.Haiku}
	if profile.Claude.SubagentModel != nil {
		models = append(models, *profile.Claude.SubagentModel)
	}
	seen := make(map[string]struct{}, len(models))
	for _, raw := range models {
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		canonical := strings.TrimSpace(profile.ModelUsageAliases[raw])
		if canonical == "" {
			return false
		}
		if _, ok := costs.Models[profile.Provider+"/"+canonical]; !ok {
			return false
		}
	}
	for _, canonical := range profile.ModelUsageAliases {
		if _, ok := costs.Models[profile.Provider+"/"+strings.TrimSpace(canonical)]; !ok {
			return false
		}
	}
	return true
}

func validAgentProfileEndpoint(endpoint string) bool {
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	return err == nil && endpoint != "" && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func NewAgentProfileRegistry(profiles []AgentExecutionProfile) (*AgentProfileRegistry, error) {
	providers := make(map[string]srvconfig.ClaudeProviderConfig)
	for _, profile := range profiles {
		if profile.Provider != "" {
			providers[profile.Provider] = srvconfig.ClaudeProviderConfig{Protocol: profile.Protocol, BaseURL: profile.BaseURL, AuthToken: profile.AuthToken}
		}
	}
	return newAgentProfileRegistry(profiles, providers)
}

func newAgentProfileRegistry(profiles []AgentExecutionProfile, providers map[string]srvconfig.ClaudeProviderConfig) (*AgentProfileRegistry, error) {
	registry := &AgentProfileRegistry{profiles: make(map[string]AgentExecutionProfile, len(profiles)), providers: cloneClaudeProviders(providers)}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.DisplayName) == "" || !model.ValidTiers[profile.MinTier] {
			return nil, fmt.Errorf("%w: profile product identity is invalid", ErrAgentProfileInvalid)
		}
		if profile.Available {
			if _, _, err := profile.Freeze(); err != nil {
				return nil, fmt.Errorf("%w: profile %q: %v", ErrAgentProfileInvalid, profile.ID, err)
			}
		}
		if _, exists := registry.profiles[profile.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate profile %q", ErrAgentProfileInvalid, profile.ID)
		}
		registry.profiles[profile.ID] = cloneAgentExecutionProfile(profile)
	}
	if len(registry.profiles) == 0 {
		return nil, fmt.Errorf("%w: at least one profile is required", ErrAgentProfileInvalid)
	}
	return registry, nil
}

func (p AgentExecutionProfile) RuntimeEnv() map[string]string {
	env := map[string]string{
		"ANTHROPIC_BASE_URL": p.BaseURL, "ANTHROPIC_AUTH_TOKEN": p.AuthToken,
		"ANTHROPIC_MODEL": p.Models.Default, "ANTHROPIC_DEFAULT_OPUS_MODEL": p.Models.Opus,
		"ANTHROPIC_DEFAULT_FABLE_MODEL": p.Models.Fable, "ANTHROPIC_DEFAULT_SONNET_MODEL": p.Models.Sonnet,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": p.Models.Haiku,
	}
	if p.Claude.EffortLevel != nil {
		env["CLAUDE_CODE_EFFORT_LEVEL"] = *p.Claude.EffortLevel
	}
	if p.Claude.AlwaysEnableEffort != nil {
		env["CLAUDE_CODE_ALWAYS_ENABLE_EFFORT"] = strconv.FormatBool(*p.Claude.AlwaysEnableEffort)
	}
	if p.Claude.MaxContextTokens != nil {
		env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = strconv.Itoa(*p.Claude.MaxContextTokens)
	}
	if p.Claude.MaxOutputTokens != nil {
		env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] = strconv.Itoa(*p.Claude.MaxOutputTokens)
	}
	if p.Claude.MaxThinkingTokens != nil {
		env["MAX_THINKING_TOKENS"] = strconv.Itoa(*p.Claude.MaxThinkingTokens)
	}
	if p.Claude.DisableAdaptiveThinking != nil {
		env["CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING"] = strconv.FormatBool(*p.Claude.DisableAdaptiveThinking)
	}
	if p.Claude.DisableThinking != nil {
		env["CLAUDE_CODE_DISABLE_THINKING"] = strconv.FormatBool(*p.Claude.DisableThinking)
	}
	if p.Claude.AutoCompactWindow != nil {
		env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = strconv.Itoa(*p.Claude.AutoCompactWindow)
	}
	if p.Claude.AutocompactPctOverride != nil {
		env["CLAUDE_AUTOCOMPACT_PCT_OVERRIDE"] = strconv.Itoa(*p.Claude.AutocompactPctOverride)
	}
	if p.Claude.Disable1MContext != nil {
		env["CLAUDE_CODE_DISABLE_1M_CONTEXT"] = strconv.FormatBool(*p.Claude.Disable1MContext)
	}
	if p.Claude.SubagentModel != nil {
		env["CLAUDE_CODE_SUBAGENT_MODEL"] = *p.Claude.SubagentModel
	}
	if p.Claude.EnableToolSearch != nil {
		env["ENABLE_TOOL_SEARCH"] = strconv.FormatBool(*p.Claude.EnableToolSearch)
	}
	return env
}

func (p AgentExecutionProfile) Snapshot() model.AgentProfileSnapshot {
	return model.AgentProfileSnapshot{
		SchemaVersion: 2, ProfileID: p.ID, DisplayName: p.DisplayName,
		Provider: p.Provider, Protocol: p.Protocol, Models: p.Models,
		Claude: cloneAgentClaudeControls(p.Claude), ModelUsageAliases: cloneModelUsageAliasTargets(p.ModelUsageAliases),
	}
}

func (p AgentExecutionProfile) Freeze() (model.AgentProfileSnapshot, string, error) {
	snapshot := p.Snapshot()
	fingerprint, err := model.AgentProfileFingerprint(snapshot)
	return snapshot, fingerprint, err
}

func cloneModelUsageAliasTargets(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for raw, target := range source {
		result[raw] = target
	}
	return result
}

func cloneClaudeProviders(source map[string]srvconfig.ClaudeProviderConfig) map[string]srvconfig.ClaudeProviderConfig {
	result := make(map[string]srvconfig.ClaudeProviderConfig, len(source))
	for id, provider := range source {
		result[id] = provider
	}
	return result
}

func cloneAgentClaudeControls(source model.AgentClaudeControls) model.AgentClaudeControls {
	return model.AgentClaudeControls{
		EffortLevel: clonePointer(source.EffortLevel), AlwaysEnableEffort: clonePointer(source.AlwaysEnableEffort),
		MaxContextTokens: clonePointer(source.MaxContextTokens), MaxOutputTokens: clonePointer(source.MaxOutputTokens),
		MaxThinkingTokens: clonePointer(source.MaxThinkingTokens), DisableAdaptiveThinking: clonePointer(source.DisableAdaptiveThinking),
		DisableThinking: clonePointer(source.DisableThinking), AutoCompactWindow: clonePointer(source.AutoCompactWindow),
		AutocompactPctOverride: clonePointer(source.AutocompactPctOverride), Disable1MContext: clonePointer(source.Disable1MContext),
		SubagentModel: clonePointer(source.SubagentModel), EnableToolSearch: clonePointer(source.EnableToolSearch),
	}
}

func cloneAgentExecutionProfile(profile AgentExecutionProfile) AgentExecutionProfile {
	profile.ModelUsageAliases = cloneModelUsageAliasTargets(profile.ModelUsageAliases)
	profile.Claude = cloneAgentClaudeControls(profile.Claude)
	return profile
}

func modelMatrixFromConfig(source srvconfig.ClaudeModelMatrixConfig) model.AgentModelMatrix {
	return model.AgentModelMatrix{Default: source.Default, Opus: source.Opus, Fable: source.Fable, Sonnet: source.Sonnet, Haiku: source.Haiku}
}

func claudeControlsFromConfig(source srvconfig.ClaudeControlsConfig) model.AgentClaudeControls {
	return model.AgentClaudeControls{
		EffortLevel: clonePointer(source.EffortLevel), AlwaysEnableEffort: clonePointer(source.AlwaysEnableEffort),
		MaxContextTokens: clonePointer(source.MaxContextTokens), MaxOutputTokens: clonePointer(source.MaxOutputTokens),
		MaxThinkingTokens: clonePointer(source.MaxThinkingTokens), DisableAdaptiveThinking: clonePointer(source.DisableAdaptiveThinking),
		DisableThinking: clonePointer(source.DisableThinking), AutoCompactWindow: clonePointer(source.AutoCompactWindow),
		AutocompactPctOverride: clonePointer(source.AutocompactPctOverride), Disable1MContext: clonePointer(source.Disable1MContext),
		SubagentModel: clonePointer(source.SubagentModel), EnableToolSearch: clonePointer(source.EnableToolSearch),
	}
}

func clonePointer[T any](source *T) *T {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}

func (r *AgentProfileRegistry) Resolve(id string) (AgentExecutionProfile, error) {
	if r == nil {
		return AgentExecutionProfile{}, ErrAgentProfileNotFound
	}
	profile, ok := r.profiles[strings.TrimSpace(id)]
	if !ok {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProfileNotFound, id)
	}
	return cloneAgentExecutionProfile(profile), nil
}

func (r *AgentProfileRegistry) ResolveForTier(id string, tier model.Tier) (AgentExecutionProfile, error) {
	profile, err := r.Resolve(id)
	if err != nil {
		return AgentExecutionProfile{}, err
	}
	if !profile.Available {
		switch profile.UnavailableReason {
		case "agent_provider_unavailable":
			return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProviderUnavailable, profile.ID)
		case "agent_model_cost_unmapped":
			return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentModelCostUnmapped, profile.ID)
		}
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProfileUnavailable, profile.ID)
	}
	if !tierCanUseProfile(tier, profile.MinTier) {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s requires %s", ErrAgentProfileAccessDenied, profile.ID, profile.MinTier)
	}
	return profile, nil
}

func (r *AgentProfileRegistry) ResolveRuntime(id string, snapshot model.AgentProfileSnapshot, fingerprint string) (AgentExecutionProfile, error) {
	product, err := r.Resolve(id)
	if err != nil {
		return AgentExecutionProfile{}, err
	}
	actualFingerprint, err := model.AgentProfileFingerprint(snapshot)
	if err != nil {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProfileSnapshotInvalid, id)
	}
	if snapshot.ProfileID != product.ID || actualFingerprint != fingerprint {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProfileSnapshotConflict, id)
	}
	provider, ok := r.providers[snapshot.Provider]
	if !ok || provider.Protocol != snapshot.Protocol || !validAgentProfileEndpoint(provider.BaseURL) || strings.TrimSpace(provider.AuthToken) == "" {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProviderUnavailable, snapshot.Provider)
	}
	return AgentExecutionProfile{
		ID: snapshot.ProfileID, DisplayName: snapshot.DisplayName, Provider: snapshot.Provider, Protocol: snapshot.Protocol,
		Models: snapshot.Models, Claude: cloneAgentClaudeControls(snapshot.Claude), ModelUsageAliases: cloneModelUsageAliasTargets(snapshot.ModelUsageAliases),
		BaseURL: strings.TrimSpace(provider.BaseURL), AuthToken: strings.TrimSpace(provider.AuthToken), MinTier: product.MinTier, Available: true,
	}, nil
}

func (r *AgentProfileRegistry) CapabilitiesForTier(tier model.Tier) []AgentProfileCapability {
	profiles := r.ListAll()
	result := make([]AgentProfileCapability, 0, len(profiles))
	for _, profile := range profiles {
		capability := AgentProfileCapability{
			ID: profile.ID, DisplayName: profile.DisplayName, Description: profile.Description,
			Provider: profile.Provider, Protocol: profile.Protocol, Models: profile.Models, Claude: cloneAgentClaudeControls(profile.Claude),
			MinTier: profile.MinTier, Available: profile.Available, UnavailableReason: profile.UnavailableReason,
		}
		if capability.Available && !tierCanUseProfile(tier, profile.MinTier) {
			capability.Available = false
			capability.UnavailableReason = "requires_" + string(profile.MinTier)
		}
		result = append(result, capability)
	}
	return result
}

func (r *AgentProfileRegistry) ListForTier(tier model.Tier) []AgentExecutionProfile {
	if r == nil {
		return nil
	}
	profiles := make([]AgentExecutionProfile, 0, len(r.profiles))
	for _, profile := range r.profiles {
		if profile.Available && tierCanUseProfile(tier, profile.MinTier) {
			profiles = append(profiles, cloneAgentExecutionProfile(profile))
		}
	}
	sort.Slice(profiles, func(i, j int) bool { return profileOrder(profiles[i].ID) < profileOrder(profiles[j].ID) })
	return profiles
}

func (r *AgentProfileRegistry) ListAll() []AgentExecutionProfile {
	if r == nil {
		return nil
	}
	profiles := make([]AgentExecutionProfile, 0, len(r.profiles))
	for _, profile := range r.profiles {
		profiles = append(profiles, cloneAgentExecutionProfile(profile))
	}
	sort.Slice(profiles, func(i, j int) bool { return profileOrder(profiles[i].ID) < profileOrder(profiles[j].ID) })
	return profiles
}

func (r *AgentProfileRegistry) DefaultForTier(tier model.Tier) AgentExecutionProfile {
	profiles := r.ListForTier(tier)
	if len(profiles) == 0 {
		return AgentExecutionProfile{}
	}
	return profiles[0]
}

func tierCanUseProfile(userTier, minimum model.Tier) bool {
	return tierRank(userTier) >= tierRank(minimum)
}

func tierRank(tier model.Tier) int {
	switch tier {
	case model.TierEnterprise:
		return 3
	case model.TierPro:
		return 2
	case model.TierFree:
		return 1
	default:
		return 0
	}
}

func profileOrder(id string) int {
	switch id {
	case "cost_effective":
		return 1
	case "balanced":
		return 2
	case "maximum_quality":
		return 3
	default:
		return 100
	}
}
