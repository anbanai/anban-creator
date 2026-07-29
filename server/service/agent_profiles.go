package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func resolveAgentProfileForUser(ctx context.Context, repo repository.Repository, registry *AgentProfileRegistry, userID, profileID string) (AgentExecutionProfile, error) {
	if registry == nil {
		return AgentExecutionProfile{}, fmt.Errorf("%w: registry is unavailable", ErrAgentProfileUnavailable)
	}
	if repo == nil {
		return AgentExecutionProfile{}, fmt.Errorf("%w: user repository is unavailable", ErrAgentProfileUnavailable)
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
	ErrAgentProfileSnapshotMismatch = errors.New("agent execution profile snapshot mismatch")
)

// AgentProfileCapability is the public, non-sensitive capability catalog row.
type AgentProfileCapability struct {
	ID                string     `json:"id"`
	DisplayName       string     `json:"display_name"`
	ModelName         string     `json:"model_name"`
	ModelID           string     `json:"model_id"`
	Description       string     `json:"description"`
	MinTier           model.Tier `json:"min_tier"`
	Available         bool       `json:"available"`
	UnavailableReason string     `json:"unavailable_reason,omitempty"`
}

// AgentExecutionProfile is the server-owned public and runtime identity of one
// selectable Agent model. BaseURL/AuthToken are internal-only construction data
// and are omitted from profile snapshots and API responses.
type AgentExecutionProfile struct {
	ID                string
	DisplayName       string
	ModelName         string
	Description       string
	Provider          string
	ModelID           string
	Protocol          string
	BaseURL           string
	AuthToken         string
	ModelUsageAliases map[string]string
	MinTier           model.Tier
	ContextWindow     int
	ReasoningEffort   string
	ThinkingRequired  bool
	Available         bool
	UnavailableReason string
}

func NewAgentProfileRegistryFromConfig(configured map[string]srvconfig.ClaudeExecutionProfileConfig) (*AgentProfileRegistry, error) {
	required := map[string]struct {
		displayName string
		modelName   string
		provider    string
		modelID     string
		minTier     model.Tier
	}{
		"cost_effective":  {displayName: "性价比", modelName: "DeepSeek 4 Pro", provider: "deepseek", modelID: "deepseek-v4-pro", minTier: model.TierFree},
		"balanced":        {displayName: "平衡型", modelName: "豆包 Seed Evolving", provider: "volcengine_ark", modelID: "doubao-seed-evolving", minTier: model.TierPro},
		"maximum_quality": {displayName: "极致效果", modelName: "Kimi K3（1M）", provider: "kimi", modelID: "k3", minTier: model.TierEnterprise},
	}
	for id := range configured {
		if _, ok := required[id]; !ok {
			return nil, fmt.Errorf("%w: unsupported profile %q", ErrAgentProfileInvalid, id)
		}
	}
	profiles := make([]AgentExecutionProfile, 0, len(required))
	for id, expected := range required {
		item := configured[id]
		displayName := strings.TrimSpace(item.DisplayName)
		if displayName == "" {
			displayName = expected.displayName
		}
		modelName := strings.TrimSpace(item.ModelName)
		if modelName == "" {
			modelName = expected.modelName
		}
		missing := strings.TrimSpace(item.Provider) == "" || strings.TrimSpace(item.ModelID) == "" ||
			strings.TrimSpace(item.Protocol) == "" || strings.TrimSpace(item.BaseURL) == "" || strings.TrimSpace(item.AuthToken) == ""
		valid := !missing && item.Provider == expected.provider && item.ModelID == expected.modelID && item.Protocol == "anthropic" &&
			item.MinTier == expected.minTier && validAgentProfileEndpoint(id, item.BaseURL) && validConfiguredProfileControls(id, item)
		reason := "provider_configuration_invalid"
		if missing {
			reason = "provider_configuration_missing"
		}
		if valid {
			reason = ""
		}
		contextWindow, reasoningEffort, thinkingRequired := item.ContextWindow, item.ReasoningEffort, item.ThinkingRequired
		if id == "maximum_quality" {
			contextWindow, reasoningEffort, thinkingRequired = 1048576, "high", true
		} else if !validConfiguredControls(item) {
			contextWindow, reasoningEffort, thinkingRequired = 0, "", false
		}
		profiles = append(profiles, AgentExecutionProfile{
			ID: id, DisplayName: displayName, ModelName: modelName, Description: item.Description,
			Provider: expected.provider, ModelID: expected.modelID, Protocol: "anthropic",
			BaseURL: strings.TrimSpace(item.BaseURL), AuthToken: strings.TrimSpace(item.AuthToken),
			ModelUsageAliases: cloneModelUsageAliasTargets(item.ModelUsageAliases), MinTier: expected.minTier,
			ContextWindow: contextWindow, ReasoningEffort: reasoningEffort,
			ThinkingRequired: thinkingRequired, Available: valid, UnavailableReason: reason,
		})
	}
	return NewAgentProfileRegistry(profiles)
}

func validAgentProfileEndpoint(id, endpoint string) bool {
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || endpoint == "" {
		return false
	}
	return id != "maximum_quality" || endpoint == "https://api.kimi.com/coding/"
}

func validConfiguredProfileControls(id string, item srvconfig.ClaudeExecutionProfileConfig) bool {
	if !validConfiguredControls(item) {
		return false
	}
	if id == "maximum_quality" {
		return item.ContextWindow == 1048576 && item.ReasoningEffort == "high" && item.ThinkingRequired
	}
	return true
}

func validConfiguredControls(item srvconfig.ClaudeExecutionProfileConfig) bool {
	if item.ContextWindow < 0 || item.ContextWindow > 1048576 {
		return false
	}
	if item.ReasoningEffort != "" && item.ReasoningEffort != "low" && item.ReasoningEffort != "medium" && item.ReasoningEffort != "high" {
		return false
	}
	return !item.ThinkingRequired || item.ReasoningEffort != ""
}

func (p AgentExecutionProfile) RuntimeEnv() map[string]string {
	return map[string]string{
		"ANTHROPIC_BASE_URL":             p.BaseURL,
		"ANTHROPIC_AUTH_TOKEN":           p.AuthToken,
		"ANTHROPIC_MODEL":                p.ModelID,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   p.ModelID,
		"ANTHROPIC_DEFAULT_FABLE_MODEL":  p.ModelID,
		"ANTHROPIC_DEFAULT_SONNET_MODEL": p.ModelID,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  p.ModelID,
	}
}

type AgentProfileRegistry struct {
	profiles map[string]AgentExecutionProfile
}

func NewAgentProfileRegistry(profiles []AgentExecutionProfile) (*AgentProfileRegistry, error) {
	registry := &AgentProfileRegistry{profiles: make(map[string]AgentExecutionProfile, len(profiles))}
	for _, profile := range profiles {
		if err := validateAgentExecutionProfile(profile); err != nil {
			return nil, err
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

func validateAgentExecutionProfile(profile AgentExecutionProfile) error {
	if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.DisplayName) == "" || strings.TrimSpace(profile.ModelID) == "" || strings.TrimSpace(profile.Provider) == "" {
		return fmt.Errorf("%w: id, display name, provider and model are required", ErrAgentProfileInvalid)
	}
	if profile.Protocol != "anthropic" {
		return fmt.Errorf("%w: profile %q must use the anthropic protocol", ErrAgentProfileInvalid, profile.ID)
	}
	if profile.Provider != "deepseek" && profile.Provider != "volcengine_ark" && profile.Provider != "kimi" {
		return fmt.Errorf("%w: profile %q has unsupported provider", ErrAgentProfileInvalid, profile.ID)
	}
	if !model.ValidTiers[profile.MinTier] {
		return fmt.Errorf("%w: profile %q has invalid minimum tier", ErrAgentProfileInvalid, profile.ID)
	}
	if profile.ThinkingRequired && strings.TrimSpace(profile.ReasoningEffort) == "" {
		return fmt.Errorf("%w: profile %q requires reasoning_effort", ErrAgentProfileInvalid, profile.ID)
	}
	if profile.ContextWindow < 0 || profile.ContextWindow > 1048576 {
		return fmt.Errorf("%w: profile %q has invalid context window", ErrAgentProfileInvalid, profile.ID)
	}
	if profile.ReasoningEffort != "" && profile.ReasoningEffort != "low" && profile.ReasoningEffort != "medium" && profile.ReasoningEffort != "high" {
		return fmt.Errorf("%w: profile %q has invalid reasoning effort", ErrAgentProfileInvalid, profile.ID)
	}
	aliases := make(map[string]model.ModelUsageIdentity, len(profile.ModelUsageAliases))
	for raw, target := range profile.ModelUsageAliases {
		if target != profile.ModelID {
			return fmt.Errorf("%w: profile %q model usage alias %q must target canonical model %q", ErrAgentProfileInvalid, profile.ID, raw, profile.ModelID)
		}
		aliases[raw] = model.ModelUsageIdentity{Provider: profile.Provider, Model: target}
	}
	if err := model.ValidateModelUsageAliases(aliases); err != nil {
		return fmt.Errorf("%w: profile %q model usage aliases: %v", ErrAgentProfileInvalid, profile.ID, err)
	}
	return nil
}

func cloneModelUsageAliasTargets(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for raw, target := range source {
		result[raw] = target
	}
	return result
}

func cloneAgentExecutionProfile(profile AgentExecutionProfile) AgentExecutionProfile {
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
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProfileUnavailable, profile.ID)
	}
	if !tierCanUseProfile(tier, profile.MinTier) {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s requires %s", ErrAgentProfileAccessDenied, profile.ID, profile.MinTier)
	}
	return profile, nil
}

// ResolveRuntime combines current credentials with the task's frozen execution
// controls. Provider or model substitution is not allowed.
func (r *AgentProfileRegistry) ResolveRuntime(id string, snapshot model.AgentProfileSnapshot) (AgentExecutionProfile, error) {
	profile, err := r.Resolve(id)
	if err != nil {
		return AgentExecutionProfile{}, err
	}
	if !profile.Available {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProfileUnavailable, profile.ID)
	}
	want := profile.Snapshot()
	if snapshot.ProfileID != want.ProfileID || snapshot.Provider != want.Provider ||
		snapshot.ModelID != want.ModelID || snapshot.Protocol != want.Protocol {
		return AgentExecutionProfile{}, fmt.Errorf("%w: %s", ErrAgentProfileSnapshotMismatch, profile.ID)
	}
	profile.ContextWindow = snapshot.ContextWindow
	profile.ReasoningEffort = snapshot.ReasoningEffort
	profile.ThinkingRequired = snapshot.ThinkingRequired
	profile.DisplayName = snapshot.DisplayName
	return profile, nil
}

func (r *AgentProfileRegistry) CapabilitiesForTier(tier model.Tier) []AgentProfileCapability {
	profiles := r.ListAll()
	result := make([]AgentProfileCapability, 0, len(profiles))
	for _, profile := range profiles {
		capability := AgentProfileCapability{
			ID: profile.ID, DisplayName: profile.DisplayName, ModelName: profile.ModelName,
			ModelID: profile.ModelID, Description: profile.Description, MinTier: profile.MinTier,
			Available: profile.Available, UnavailableReason: profile.UnavailableReason,
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

func (p AgentExecutionProfile) Snapshot() model.AgentProfileSnapshot {
	return model.AgentProfileSnapshot{
		ProfileID: p.ID, Provider: p.Provider, ModelID: p.ModelID, Protocol: p.Protocol,
		ContextWindow: p.ContextWindow, ReasoningEffort: p.ReasoningEffort,
		ThinkingRequired: p.ThinkingRequired, DisplayName: p.DisplayName,
	}
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
