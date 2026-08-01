package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	appconfig "github.com/anbanai/anban-creator/app/config"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// sentinel for "keep existing key" in update requests.
const keepExistingKey = "****"

var ErrInvalidModelConfig = errors.New("invalid model config")

// ModelConfigService manages per-user AI model configuration.
type ModelConfigService struct {
	repo   repository.Repository
	cfg    *config.Config
	logger *zerolog.Logger
}

// NewModelConfigService creates a new ModelConfigService.
func NewModelConfigService(repo repository.Repository, cfg *config.Config, logger *zerolog.Logger) *ModelConfigService {
	return &ModelConfigService{
		repo:   repo,
		cfg:    cfg,
		logger: logger,
	}
}

// ---------------------------------------------------------------------------
// DTO types
// ---------------------------------------------------------------------------

// ModelConfigResponse is the API response for GET /api/v1/model-config.
type ModelConfigResponse struct {
	Image *ImageConfigDTO `json:"image,omitempty"`
}

// ImageConfigDTO represents image model config for API display.
type ImageConfigDTO struct {
	Provider string `json:"provider,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Model    string `json:"model,omitempty"`
	Proxy    string `json:"proxy,omitempty"`
}

// UpdateModelConfigRequest is the API request for PUT /api/v1/model-config.
type UpdateModelConfigRequest struct {
	Image *ImageConfigDTO `json:"image,omitempty"`
}

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

// Get returns the user's model config with masked API keys.
func (s *ModelConfigService) Get(ctx context.Context, userID string) (*ModelConfigResponse, error) {
	row, err := s.repo.ModelConfigs().FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &ModelConfigResponse{}, nil
	}

	resp := &ModelConfigResponse{}

	if row.ImageConfigJSON != "" {
		var uc model.ImageUserConfig
		if json.Unmarshal([]byte(row.ImageConfigJSON), &uc) == nil {
			resp.Image = &ImageConfigDTO{
				Provider: uc.Provider,
				Endpoint: uc.Endpoint,
				APIKey:   maskKey(uc.APIKey),
				Model:    uc.Model,
				Proxy:    uc.Proxy,
			}
		}
	}

	return resp, nil
}

// Update saves the user's model config. Omitted sections = no change; null = clear.
func (s *ModelConfigService) Update(ctx context.Context, userID string, req *UpdateModelConfigRequest) error {
	row, err := s.repo.ModelConfigs().FindByUserID(ctx, userID)
	if err != nil {
		return err
	}

	if row == nil {
		row = &model.UserModelConfig{
			ID:     uuid.New().String(),
			UserID: userID,
		}
	}

	// Handle image config.
	if req.Image != nil {
		if req.Image.APIKey == "" && req.Image.Endpoint == "" && req.Image.Model == "" && req.Image.Provider == "" && req.Image.Proxy == "" {
			row.ImageConfigJSON = ""
		} else {
			existing := s.loadImageConfig(row.ImageConfigJSON)
			if req.Image.APIKey == keepExistingKey {
				if existing.APIKey == "" {
					return fmt.Errorf("%w: image.api_key cannot keep existing key because no existing key is saved", ErrInvalidModelConfig)
				}
				req.Image.APIKey = existing.APIKey
			}
			uc := model.ImageUserConfig{
				Provider: NormalizeImageProvider(req.Image.Provider),
				Endpoint: req.Image.Endpoint,
				APIKey:   req.Image.APIKey,
				Model:    req.Image.Model,
				Proxy:    req.Image.Proxy,
			}
			if data, err := json.Marshal(&uc); err != nil {
				return err
			} else {
				row.ImageConfigJSON = string(data)
			}
		}
	}

	return s.repo.ModelConfigs().Upsert(ctx, row)
}

// Delete clears all user model overrides.
func (s *ModelConfigService) Delete(ctx context.Context, userID string) error {
	return s.repo.ModelConfigs().Delete(ctx, userID)
}

// HasImageOverride returns true if the user has custom image model config.
func (s *ModelConfigService) HasImageOverride(ctx context.Context, userID string) bool {
	uc, err := s.loadUserImageConfig(ctx, userID)
	if err != nil {
		return false
	}
	return uc.HasConfig()
}

// HasCompleteImageOverride returns true if the user has all required image model fields
// (provider + api_key + model) — used for credit bypass decisions.
func (s *ModelConfigService) HasCompleteImageOverride(ctx context.Context, userID string) bool {
	uc, err := s.loadUserImageConfig(ctx, userID)
	if err != nil {
		return false
	}
	return uc.HasCompleteConfig()
}

func (s *ModelConfigService) IsEnterpriseUser(ctx context.Context, userID string) bool {
	return s.userTierIsEnterprise(ctx, userID)
}

// GetEffectiveImageConfig returns the resolved image config as an ImageAPIConfig.
// Returns nil if no user override exists (use server default).
func (s *ModelConfigService) GetEffectiveImageConfig(ctx context.Context, userID string) *config.ImageAPIConfig {
	uc, err := s.loadUserImageConfig(ctx, userID)
	if err != nil || !uc.HasCompleteConfig() {
		return nil
	}

	base, ok := s.cfg.ImageAPIForCapability("")
	if !ok || base.Cover == nil || base.Content == nil {
		return nil
	}
	cover := *base.Cover
	content := *base.Content
	if uc.Provider != "" {
		cover.Provider = uc.Provider
		content.Provider = uc.Provider
	}
	if uc.APIKey != "" {
		cover.Key = uc.APIKey
		content.Key = uc.APIKey
	}
	if uc.Endpoint != "" {
		cover.BaseURL = uc.Endpoint
		content.BaseURL = uc.Endpoint
	}
	if uc.Model != "" {
		cover.Model = uc.Model
		content.Model = uc.Model
	}
	cfg := &config.ImageAPIConfig{
		Cover:   &cover,
		Content: &content,
		Sizes:   base.Sizes,
	}

	return cfg
}

// GetImageProxy returns the user's configured image proxy, if any.
func (s *ModelConfigService) GetImageProxy(ctx context.Context, userID string) string {
	uc, err := s.loadUserImageConfig(ctx, userID)
	if err != nil {
		return ""
	}
	return uc.Proxy
}

// ResolveImageConfigForKey decides which image configuration to use for a given
// task/plan image_model_key. Returns the resolved config and a source label
// ("system_default" / "user_custom" / "preset:<key>") for logging.
//
// Resolution rules:
//   - key == "" or "system_default": walk GetEffectiveImageConfig (user override
//     or nil), do NOT enforce tier.
//   - key == "custom": require user override; if none exists, fall back to nil
//     (system default) with a warning. Caller is responsible for enforcing that
//     the user's tier is Enterprise at request time; we re-check tier here as a
//     defense-in-depth (Enterprise downgrade scenario).
//   - any other key: look up in the configured capability catalog and re-check tier at runtime
//     (handles tier downgrade after task creation), fall back if missing.
//
// Returns nil cfg with source "system_default" when no override/preset applies
// — callers must then use their own server default.
func (s *ModelConfigService) ResolveImageConfigForKey(
	ctx context.Context, userID, imageModelKey string,
) (*config.ImageAPIConfig, string) {
	imageModelKey = strings.TrimSpace(imageModelKey)
	if imageModelKey == "" || imageModelKey == model.ImageModelKeySystemDefault {
		if s.userTierIsEnterprise(ctx, userID) {
			if cfg := s.GetEffectiveImageConfig(ctx, userID); cfg != nil {
				return cfg, "user_custom"
			}
		}
		return nil, "system_default"
	}

	if imageModelKey == model.ImageModelKeyCustom {
		// Defense-in-depth: re-check tier in case user downgraded after creating the task.
		if !s.userTierIsEnterprise(ctx, userID) {
			s.logger.Warn().
				Str("user_id", userID).
				Str("image_model_key", imageModelKey).
				Msg("custom image model requested but user tier is no longer enterprise, fallback to system default")
			return nil, "system_default"
		}
		cfg := s.GetEffectiveImageConfig(ctx, userID)
		if cfg == nil {
			s.logger.Warn().
				Str("user_id", userID).
				Msg("custom image model requested but user has no override, fallback to system default")
			return nil, "system_default"
		}
		return cfg, "user_custom"
	}

	route, ok := s.cfg.ImageCapability(imageModelKey)
	if !ok || !route.Enabled || !model.TierSatisfies(s.lookupUserTier(ctx, userID), model.NormalizeTier(route.MinTier)) {
		return nil, "system_default"
	}
	runtime, _ := s.cfg.ImageAPIForCapability(imageModelKey)
	return runtime, "capability:" + imageModelKey
}

// ResolveImageConfigForTaskKey resolves a persisted task/plan image model key.
// Empty keys keep the default behavior (user override, else system default).
// Non-empty keys are strict: unavailable, unauthorized, or incomplete model
// selections are errors instead of silently falling back to another provider.
func (s *ModelConfigService) ResolveImageConfigForTaskKey(
	ctx context.Context, userID, imageModelKey string,
) (*config.ImageAPIConfig, string, error) {
	imageModelKey = strings.TrimSpace(imageModelKey)
	if imageModelKey == "" || imageModelKey == model.ImageModelKeySystemDefault {
		if cfg := s.GetEffectiveImageConfig(ctx, userID); cfg != nil {
			return cfg, "user_custom", nil
		}
		cfg, ok := s.cfg.ImageAPIForCapability("")
		if !ok {
			return nil, "", fmt.Errorf("default image capability is unavailable")
		}
		return cfg, "capability:" + s.cfg.ModelRoutes.ImageGeneration.DefaultCapability, nil
	}

	if imageModelKey == model.ImageModelKeyCustom {
		if !s.userTierIsEnterprise(ctx, userID) {
			return nil, "", fmt.Errorf("image model %q is not allowed for user tier", imageModelKey)
		}
		cfg := s.GetEffectiveImageConfig(ctx, userID)
		if cfg == nil {
			return nil, "", fmt.Errorf("custom image model is not configured")
		}
		return cfg, "user_custom", nil
	}

	route, ok := s.cfg.ImageCapability(imageModelKey)
	if !ok || !route.Enabled {
		return nil, "", fmt.Errorf("unknown image model key %q", imageModelKey)
	}
	userTier := s.lookupUserTier(ctx, userID)
	requiredTier := model.NormalizeTier(route.MinTier)
	if !model.TierSatisfies(userTier, requiredTier) {
		return nil, "", fmt.Errorf("image model %q is not allowed for user tier %s; requires %s", imageModelKey, userTier, requiredTier)
	}
	runtime, _ := s.cfg.ImageAPIForCapability(imageModelKey)
	return runtime, "capability:" + imageModelKey, nil
}

// ImageReferenceLimitError reports that reference-capable image models are
// available to the user, but none accepts the requested number of images.
type ImageReferenceLimitError struct {
	Requested          int
	MaxReferenceImages int
}

func (e *ImageReferenceLimitError) Error() string {
	if e == nil {
		return "image reference limit exceeded"
	}
	return fmt.Sprintf(
		"requested %d reference images exceeds the maximum accessible limit of %d",
		e.Requested,
		e.MaxReferenceImages,
	)
}

// ResolvedImageModel is the immutable descriptor consumed by one generation
// request. Config carries credentials and platform slots internally; the public
// metadata explains which concrete provider/model was selected and why.
type ResolvedImageModel struct {
	Config             *config.ImageAPIConfig `json:"-"`
	Key                string                 `json:"key,omitempty"`
	BillingSKU         string                 `json:"-"`
	Provider           string                 `json:"provider"`
	Model              string                 `json:"model"`
	Source             string                 `json:"source,omitempty"`
	SupportsReference  bool                   `json:"supports_reference"`
	MaxReferenceImages int                    `json:"max_reference_images"`
	SelectionReason    string                 `json:"selection_reason,omitempty"`
}

type imageModelCandidate struct {
	config             *config.ImageAPIConfig
	key                string
	billingSKU         string
	provider           string
	model              string
	source             string
	qualityRank        int
	supportsReference  bool
	maxReferenceImages int
}

// ResolveImageModelForGeneration resolves one concrete, image-type-specific
// model descriptor before billing or generation starts. Reference-bearing
// requests may fall back only to an accessible preset that explicitly declares
// sufficient reference capacity; unknown/custom routes are never guessed.
func (s *ModelConfigService) ResolveImageModelForGeneration(
	ctx context.Context,
	userID string,
	imageModelKey string,
	imageType string,
	referenceCount int,
) (*ResolvedImageModel, error) {
	imageType, err := normalizeGenerationImageType(imageType)
	if err != nil {
		return nil, err
	}
	if referenceCount < 0 {
		return nil, fmt.Errorf("reference count must not be negative")
	}

	preferred, err := s.resolvePreferredImageCandidate(ctx, userID, imageModelKey, imageType)
	if err != nil {
		return nil, err
	}
	if referenceCount > 0 && !preferred.supportsReference {
		return nil, fmt.Errorf("selected image capability does not support reference images")
	}
	if referenceCount > preferred.maxReferenceImages {
		return nil, &ImageReferenceLimitError{Requested: referenceCount, MaxReferenceImages: preferred.maxReferenceImages}
	}
	return resolvedImageModelFromCandidate(preferred, "preferred"), nil
}

func (s *ModelConfigService) resolvePreferredImageCandidate(
	ctx context.Context,
	userID string,
	imageModelKey string,
	imageType string,
) (imageModelCandidate, error) {
	resolvedConfig, source, err := s.ResolveImageConfigForTaskKey(ctx, userID, imageModelKey)
	if err != nil {
		return imageModelCandidate{}, err
	}
	if resolvedConfig == nil {
		if s.cfg == nil {
			return imageModelCandidate{}, fmt.Errorf("system image model is not configured")
		}
		resolvedConfig, _ = s.cfg.ImageAPIForCapability("")
	}

	apiCfg := imageAPIForType(resolvedConfig, imageType)
	if apiCfg == nil || strings.TrimSpace(apiCfg.Provider) == "" || strings.TrimSpace(apiCfg.Model) == "" {
		return imageModelCandidate{}, fmt.Errorf("no image API config available for type %q", imageType)
	}

	candidate := imageModelCandidate{
		config:   resolvedConfig,
		key:      strings.TrimSpace(imageModelKey),
		provider: imageProviderKind(apiCfg.Provider),
		model:    strings.TrimSpace(apiCfg.Model),
		source:   source,
	}
	capabilityKey := strings.TrimPrefix(source, "capability:")
	if capabilityKey == source || capabilityKey == "" {
		capabilityKey = strings.TrimSpace(imageModelKey)
		if capabilityKey == "" || capabilityKey == model.ImageModelKeySystemDefault || capabilityKey == model.ImageModelKeyCustom {
			capabilityKey = s.cfg.ModelRoutes.ImageGeneration.DefaultCapability
		}
	}
	if route, ok := s.cfg.ImageCapability(capabilityKey); ok {
		candidate.key = capabilityKey
		candidate.billingSKU = strings.TrimSpace(route.BillingSKU)
		candidate.qualityRank = route.QualityRank
		if !(strings.TrimSpace(imageModelKey) == model.ImageModelKeyCustom && source == "user_custom") {
			candidate.supportsReference = route.Features.SupportsReference
			candidate.maxReferenceImages = route.Features.MaxReferenceImages
		} else {
			candidate.key = model.ImageModelKeyCustom
		}
	} else {
		candidate.supportsReference, candidate.maxReferenceImages = s.capabilitiesForImageConfig(resolvedConfig, imageType)
	}
	return candidate, nil
}

func resolvedImageModelFromCandidate(candidate imageModelCandidate, reason string) *ResolvedImageModel {
	return &ResolvedImageModel{
		Config:             candidate.config,
		Key:                candidate.key,
		BillingSKU:         candidate.billingSKU,
		Provider:           candidate.provider,
		Model:              candidate.model,
		Source:             candidate.source,
		SupportsReference:  candidate.supportsReference,
		MaxReferenceImages: candidate.maxReferenceImages,
		SelectionReason:    reason,
	}
}

func normalizeGenerationImageType(imageType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(imageType))
	if normalized == "" {
		normalized = "content"
	}
	if normalized != "content" && normalized != "cover" {
		return "", fmt.Errorf("unsupported image type %q", imageType)
	}
	return normalized, nil
}

func imageAPIForType(cfg *config.ImageAPIConfig, imageType string) *appconfig.ImageAPI {
	if cfg == nil {
		return nil
	}
	if imageType == "cover" {
		return cfg.Cover
	}
	return cfg.Content
}

func (s *ModelConfigService) capabilitiesForImageConfig(cfg *config.ImageAPIConfig, imageType string) (bool, int) {
	apiCfg := imageAPIForType(cfg, imageType)
	if apiCfg == nil || s.cfg == nil {
		return false, 0
	}
	provider := imageProviderKind(apiCfg.Provider)
	modelID := strings.TrimSpace(apiCfg.Model)
	routeKeys := make([]string, 0, len(s.cfg.ModelRoutes.ImageGeneration.Capabilities))
	for key := range s.cfg.ModelRoutes.ImageGeneration.Capabilities {
		routeKeys = append(routeKeys, key)
	}
	sort.Strings(routeKeys)
	for _, key := range routeKeys {
		route := s.cfg.ModelRoutes.ImageGeneration.Capabilities[key]
		if imageProviderKind(route.Provider) == provider && strings.TrimSpace(route.Model) == modelID {
			return route.Features.SupportsReference, route.Features.MaxReferenceImages
		}
	}
	return false, 0
}

func imageProviderKind(provider string) string {
	key := strings.ToLower(strings.TrimSpace(provider))
	switch {
	case strings.Contains(key, "volc"):
		return "volcengine"
	case strings.Contains(key, "gemini") || strings.Contains(key, "google"):
		return "gemini"
	case strings.Contains(key, "openai"), strings.Contains(key, "moonshot"), strings.Contains(key, "kimi"), strings.Contains(key, "wangcai"):
		return "openai"
	default:
		return key
	}
}

// userTierIsEnterprise returns true if the user's tier resolves to Enterprise.
// Returns false on lookup failure (fail-closed for "custom" access).
func (s *ModelConfigService) userTierIsEnterprise(ctx context.Context, userID string) bool {
	return s.lookupUserTier(ctx, userID) == model.TierEnterprise
}

// lookupUserTier fetches the user's resolved tier. Returns TierFree on error.
func (s *ModelConfigService) lookupUserTier(ctx context.Context, userID string) model.Tier {
	if userID == "" {
		return model.TierFree
	}
	user, err := s.repo.Users().FindByID(ctx, userID)
	if err != nil || user == nil {
		return model.TierFree
	}
	return model.ResolveTier(user.Tier)
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

func (s *ModelConfigService) loadUserImageConfig(ctx context.Context, userID string) (*model.ImageUserConfig, error) {
	row, err := s.repo.ModelConfigs().FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &model.ImageUserConfig{}, nil
	}
	return s.loadImageConfig(row.ImageConfigJSON), nil
}

func (s *ModelConfigService) loadImageConfig(jsonStr string) *model.ImageUserConfig {
	if jsonStr == "" {
		return &model.ImageUserConfig{}
	}
	var uc model.ImageUserConfig
	if json.Unmarshal([]byte(jsonStr), &uc) != nil {
		return &model.ImageUserConfig{}
	}
	return &uc
}

func NormalizeImageProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

// maskKey returns only the last 4 characters of the key, prefixed with "****".
func maskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}
