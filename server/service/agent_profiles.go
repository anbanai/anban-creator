package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	"effective": {DisplayName: "性价比", MinTier: model.TierFree},
	"balanced":  {DisplayName: "平衡型", MinTier: model.TierPro},
	"quality":   {DisplayName: "极致效果", MinTier: model.TierEnterprise},
}

var agentProfileOrder = []string{"effective", "balanced", "quality"}

type AgentProfileCapability struct {
	ID                string     `json:"id"`
	DisplayName       string     `json:"display_name"`
	Description       string     `json:"description"`
	Provider          string     `json:"provider"`
	ModelName         string     `json:"model_name"`
	MinTier           model.Tier `json:"min_tier"`
	Available         bool       `json:"available"`
	UnavailableReason string     `json:"unavailable_reason,omitempty"`
}

type AgentExecutionProfile struct {
	ID                string
	DisplayName       string
	Description       string
	Provider          string
	Protocol          string
	Envs              map[string]string
	ModelUsageAliases map[string]string
	MinTier           model.Tier
	Available         bool
	UnavailableReason string
}

type AgentProfileRegistry struct {
	profiles map[string]AgentExecutionProfile
}

func NewAgentProfileRegistryFromConfig(configured map[string]srvconfig.ClaudeExecutionProfileConfig, costs billing.CostCatalog) (*AgentProfileRegistry, error) {
	for id := range configured {
		if _, ok := agentProfileProducts[id]; !ok {
			return nil, fmt.Errorf("%w: unsupported profile %q", ErrAgentProfileInvalid, id)
		}
	}

	profiles := make([]AgentExecutionProfile, 0, len(agentProfileProducts))
	for _, id := range agentProfileOrder {
		product := agentProfileProducts[id]
		item, exists := configured[id]
		profile := AgentExecutionProfile{
			ID: id, DisplayName: product.DisplayName, MinTier: product.MinTier, Protocol: "anthropic",
		}
		if !exists {
			profile.UnavailableReason = "profile_configuration_missing"
			profiles = append(profiles, profile)
			continue
		}
		profile.Description = strings.TrimSpace(item.Description)
		profile.Provider = strings.TrimSpace(item.Provider)
		profile.Envs = model.CloneClaudeProfileEnvs(item.Envs)
		profile.ModelUsageAliases = cloneModelUsageAliasTargets(item.ModelUsageAliases)

		if profile.Provider == "" || model.ValidateClaudeProfileEnvs(profile.Envs, true) != nil ||
			model.ValidateClaudeProfileModelUsageAliasMappings(profile.Provider, profile.ModelUsageAliases) != nil {
			profile.UnavailableReason = "agent_provider_unavailable"
			profiles = append(profiles, profile)
			continue
		}
		if !profileModelsHaveCosts(profile, costs) {
			profile.UnavailableReason = "agent_model_cost_unmapped"
			profiles = append(profiles, profile)
			continue
		}
		profile.Available = true
		profiles = append(profiles, profile)
	}
	return newAgentProfileRegistry(profiles)
}

func profileModelsHaveCosts(profile AgentExecutionProfile, costs billing.CostCatalog) bool {
	for _, raw := range model.ClaudeProfileReferencedModels(profile.Envs) {
		canonical := profile.ModelUsageAliases[raw]
		if _, ok := costs.Models[profile.Provider+"/"+canonical]; !ok {
			return false
		}
	}
	for _, canonical := range profile.ModelUsageAliases {
		if _, ok := costs.Models[profile.Provider+"/"+canonical]; !ok {
			return false
		}
	}
	return true
}

func NewAgentProfileRegistry(profiles []AgentExecutionProfile) (*AgentProfileRegistry, error) {
	return newAgentProfileRegistry(profiles)
}

func newAgentProfileRegistry(profiles []AgentExecutionProfile) (*AgentProfileRegistry, error) {
	registry := &AgentProfileRegistry{profiles: make(map[string]AgentExecutionProfile, len(profiles))}
	for _, profile := range profiles {
		product, validProduct := agentProfileProducts[profile.ID]
		if !validProduct || strings.TrimSpace(profile.DisplayName) == "" || profile.MinTier != product.MinTier {
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
	return model.CloneClaudeProfileEnvs(p.Envs)
}

func (p AgentExecutionProfile) Snapshot() model.AgentProfileSnapshot {
	return model.AgentProfileSnapshot{
		SchemaVersion: model.ClaudeProfileSchemaV3,
		ProfileID:     p.ID, DisplayName: p.DisplayName, Provider: p.Provider, Protocol: p.Protocol,
		Envs: model.RedactClaudeProfileEnvs(p.Envs), ModelUsageAliases: cloneModelUsageAliasTargets(p.ModelUsageAliases),
	}
}

func (p AgentExecutionProfile) Freeze() (model.AgentProfileSnapshot, string, error) {
	if err := model.ValidateClaudeProfileEnvs(p.Envs, true); err != nil {
		return model.AgentProfileSnapshot{}, "", fmt.Errorf("profile envs are invalid: %w", err)
	}
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

func cloneAgentExecutionProfile(profile AgentExecutionProfile) AgentExecutionProfile {
	profile.Envs = model.CloneClaudeProfileEnvs(profile.Envs)
	profile.ModelUsageAliases = cloneModelUsageAliasTargets(profile.ModelUsageAliases)
	return profile
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
	actualFingerprint, err := model.AgentProfileFingerprint(snapshot)
	if err != nil {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProfileSnapshotInvalid, id)
	}
	if strings.TrimSpace(id) != snapshot.ProfileID || actualFingerprint != fingerprint {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProfileSnapshotConflict, id)
	}
	current, err := r.Resolve(id)
	if err != nil {
		return AgentExecutionProfile{}, err
	}
	if current.Provider != snapshot.Provider || current.Protocol != snapshot.Protocol {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProviderUnavailable, snapshot.Provider)
	}
	token := current.Envs[model.ClaudeEnvAuthToken]
	runtimeEnvs := model.CloneClaudeProfileEnvs(snapshot.Envs)
	runtimeEnvs[model.ClaudeEnvAuthToken] = token
	if err := model.ValidateClaudeProfileEnvs(runtimeEnvs, true); err != nil {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProviderUnavailable, snapshot.Provider)
	}
	return AgentExecutionProfile{
		ID: snapshot.ProfileID, DisplayName: snapshot.DisplayName, Description: current.Description,
		Provider: snapshot.Provider, Protocol: snapshot.Protocol, Envs: runtimeEnvs,
		ModelUsageAliases: cloneModelUsageAliasTargets(snapshot.ModelUsageAliases),
		MinTier:           current.MinTier, Available: true,
	}, nil
}

func (r *AgentProfileRegistry) CapabilitiesForTier(tier model.Tier) []AgentProfileCapability {
	profiles := r.ListAll()
	result := make([]AgentProfileCapability, 0, len(profiles))
	for _, profile := range profiles {
		capability := AgentProfileCapability{
			ID: profile.ID, DisplayName: profile.DisplayName, Description: profile.Description,
			Provider: profile.Provider, ModelName: strings.TrimSpace(profile.Envs[model.ClaudeEnvModel]),
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
	case "effective":
		return 1
	case "balanced":
		return 2
	case "quality":
		return 3
	default:
		return 100
	}
}
