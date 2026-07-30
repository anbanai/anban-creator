package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

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

// GetEffectiveImageConfig returns the resolved image config as an ImageAPIConfig.
// Returns nil if no user override exists (use server default).
func (s *ModelConfigService) GetEffectiveImageConfig(ctx context.Context, userID string) *config.ImageAPIConfig {
	uc, err := s.loadUserImageConfig(ctx, userID)
	if err != nil || !uc.HasCompleteConfig() {
		return nil
	}

	// Start from server config as base so fields like TimeoutSec, Size,
	// Volcengine, MaxSizeMB etc. are preserved when user only overrides provider/key/model.
	cover := *s.cfg.ImageAPI.Cover
	content := *s.cfg.ImageAPI.Content
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
		Sizes:   s.cfg.ImageAPI.Sizes,
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
//   - any other key: look up in cfg.ImagePresets, re-check tier at runtime
//     (handles tier downgrade after task creation), fall back if missing.
//
// Returns nil cfg with source "system_default" when no override/preset applies
// — callers must then use their own server default (s.imageCfg).
func (s *ModelConfigService) ResolveImageConfigForKey(
	ctx context.Context, userID, imageModelKey string,
) (*config.ImageAPIConfig, string) {
	if imageModelKey == "" || imageModelKey == model.ImageModelKeySystemDefault {
		if cfg := s.GetEffectiveImageConfig(ctx, userID); cfg != nil {
			return cfg, "user_custom"
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

	// Look up preset by key.
	for i := range s.cfg.ImagePresets {
		p := &s.cfg.ImagePresets[i]
		if p.Key != imageModelKey {
			continue
		}
		// Re-check tier at execution time (handles downgrade).
		userTier := s.lookupUserTier(ctx, userID)
		requiredTier := model.NormalizeTier(p.MinTier)
		if !model.TierSatisfies(userTier, requiredTier) {
			s.logger.Warn().
				Str("user_id", userID).
				Str("preset_key", p.Key).
				Str("user_tier", string(userTier)).
				Str("required_tier", string(requiredTier)).
				Msg("user tier no longer satisfies preset, fallback to system default")
			return nil, "system_default"
		}
		return presetToImageAPIConfig(p, s.cfg), "preset:" + p.Key
	}

	s.logger.Warn().
		Str("user_id", userID).
		Str("image_model_key", imageModelKey).
		Msg("image preset not found, fallback to system default")
	return nil, "system_default"
}

// ResolveImageConfigForTaskKey resolves a persisted task/plan image model key.
// Empty keys keep the default behavior (user override, else system default).
// Non-empty keys are strict: unavailable, unauthorized, or incomplete model
// selections are errors instead of silently falling back to another provider.
func (s *ModelConfigService) ResolveImageConfigForTaskKey(
	ctx context.Context, userID, imageModelKey string,
) (*config.ImageAPIConfig, string, error) {
	if imageModelKey == "" || imageModelKey == model.ImageModelKeySystemDefault {
		if cfg := s.GetEffectiveImageConfig(ctx, userID); cfg != nil {
			return cfg, "user_custom", nil
		}
		return nil, "system_default", nil
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

	for i := range s.cfg.ImagePresets {
		p := &s.cfg.ImagePresets[i]
		if p.Key != imageModelKey {
			continue
		}
		userTier := s.lookupUserTier(ctx, userID)
		requiredTier := model.NormalizeTier(p.MinTier)
		if !model.TierSatisfies(userTier, requiredTier) {
			return nil, "", fmt.Errorf("image model %q is not allowed for user tier %s; requires %s", imageModelKey, userTier, requiredTier)
		}
		return presetToImageAPIConfig(p, s.cfg), "preset:" + p.Key, nil
	}

	return nil, "", fmt.Errorf("unknown image model key %q", imageModelKey)
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
	if referenceCount == 0 || (preferred.supportsReference && referenceCount <= preferred.maxReferenceImages) {
		return resolvedImageModelFromCandidate(preferred, "preferred"), nil
	}

	userTier := s.lookupUserTier(ctx, userID)
	maxAccessible := 0
	if preferred.supportsReference && preferred.maxReferenceImages > maxAccessible {
		maxAccessible = preferred.maxReferenceImages
	}

	candidates := make([]imageModelCandidate, 0, len(s.cfg.ImagePresets))
	for i := range s.cfg.ImagePresets {
		preset := &s.cfg.ImagePresets[i]
		if !model.TierSatisfies(userTier, model.NormalizeTier(preset.MinTier)) {
			continue
		}
		if preset.Capabilities.SupportsReference && preset.Capabilities.MaxReferenceImages > maxAccessible {
			maxAccessible = preset.Capabilities.MaxReferenceImages
		}
		if !preset.Capabilities.SupportsReference || preset.Capabilities.MaxReferenceImages < referenceCount {
			continue
		}
		candidate, candidateErr := imageModelCandidateFromPreset(preset, s.cfg, imageType)
		if candidateErr != nil {
			continue
		}
		candidates = append(candidates, candidate)
	}

	if len(candidates) == 0 {
		if maxAccessible > 0 {
			return nil, &ImageReferenceLimitError{
				Requested:          referenceCount,
				MaxReferenceImages: maxAccessible,
			}
		}
		return nil, fmt.Errorf(
			"no accessible image model supports reference images for image type %q",
			imageType,
		)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].qualityRank != candidates[j].qualityRank {
			return candidates[i].qualityRank > candidates[j].qualityRank
		}
		return candidates[i].key < candidates[j].key
	})
	return resolvedImageModelFromCandidate(candidates[0], "reference_compatible_fallback"), nil
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
		resolvedConfig = &s.cfg.ImageAPI
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
	if strings.HasPrefix(source, "preset:") {
		presetKey := strings.TrimPrefix(source, "preset:")
		for i := range s.cfg.ImagePresets {
			preset := &s.cfg.ImagePresets[i]
			if preset.Key != presetKey {
				continue
			}
			candidate.key = preset.Key
			candidate.qualityRank = preset.QualityRank
			candidate.supportsReference = preset.Capabilities.SupportsReference
			candidate.maxReferenceImages = preset.Capabilities.MaxReferenceImages
			break
		}
	} else {
		candidate.supportsReference, candidate.maxReferenceImages = s.capabilitiesForImageConfig(resolvedConfig, imageType)
	}
	return candidate, nil
}

func imageModelCandidateFromPreset(
	preset *config.ImageModelPreset,
	base *config.Config,
	imageType string,
) (imageModelCandidate, error) {
	resolvedConfig := presetToImageAPIConfig(preset, base)
	apiCfg := imageAPIForType(resolvedConfig, imageType)
	if apiCfg == nil || strings.TrimSpace(apiCfg.Provider) == "" || strings.TrimSpace(apiCfg.Model) == "" {
		return imageModelCandidate{}, fmt.Errorf("image preset %q has no %s configuration", preset.Key, imageType)
	}
	return imageModelCandidate{
		config:             resolvedConfig,
		key:                preset.Key,
		provider:           imageProviderKind(apiCfg.Provider),
		model:              strings.TrimSpace(apiCfg.Model),
		source:             "preset:" + preset.Key,
		qualityRank:        preset.QualityRank,
		supportsReference:  preset.Capabilities.SupportsReference,
		maxReferenceImages: preset.Capabilities.MaxReferenceImages,
	}, nil
}

func resolvedImageModelFromCandidate(candidate imageModelCandidate, reason string) *ResolvedImageModel {
	return &ResolvedImageModel{
		Config:             candidate.config,
		Key:                candidate.key,
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
	routeKeys := make([]string, 0, len(s.cfg.ModelRoutes.ImageGeneration.Designer))
	for key := range s.cfg.ModelRoutes.ImageGeneration.Designer {
		routeKeys = append(routeKeys, key)
	}
	sort.Strings(routeKeys)
	for _, key := range routeKeys {
		route := s.cfg.ModelRoutes.ImageGeneration.Designer[key]
		if imageProviderKind(route.Provider) == provider && strings.TrimSpace(route.Model) == modelID {
			return route.Capabilities.SupportsReference, route.Capabilities.MaxReferenceImages
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

// presetToImageAPIConfig converts a single ImageModelPreset into a fully-formed
// ImageAPIConfig where both Cover and Content use the same provider/model/key.
// Other fields (Size, Volcengine tuning, MaxWidth, MaxSizeMB, etc.) are inherited
// from the server's base ImageAPI config so callers don't lose those defaults.
func presetToImageAPIConfig(p *config.ImageModelPreset, base *config.Config) *config.ImageAPIConfig {
	if p == nil || base == nil {
		return nil
	}

	// Start from base cover/content as templates to preserve Size/Volcengine/etc.
	var cover, content appconfig.ImageAPI
	if base.ImageAPI.Cover != nil {
		cover = *base.ImageAPI.Cover
	}
	if base.ImageAPI.Content != nil {
		content = *base.ImageAPI.Content
	}

	// Override provider/model/endpoint/key with preset values.
	for _, dst := range []*appconfig.ImageAPI{&cover, &content} {
		dst.Provider = p.Provider
		dst.Model = p.Model
		dst.BaseURL = p.Endpoint
		dst.Key = p.APIKey
		if p.Timeout > 0 {
			dst.TimeoutSec = int(p.Timeout / time.Second)
		}
	}

	return &config.ImageAPIConfig{
		Cover:    &cover,
		Content:  &content,
		Designer: base.ImageAPI.Designer,
		Sizes:    base.ImageAPI.Sizes,
	}
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
