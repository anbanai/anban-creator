package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	Text  *TextConfigDTO  `json:"text,omitempty"`
	Image *ImageConfigDTO `json:"image,omitempty"`
}

// TextConfigDTO represents text model config for API display.
type TextConfigDTO struct {
	Endpoint string `json:"endpoint,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Model    string `json:"model,omitempty"`
	Proxy    string `json:"proxy,omitempty"`
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
	Text  *TextConfigDTO  `json:"text,omitempty"`
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

	if row.TextConfigJSON != "" {
		var uc model.TextUserConfig
		if json.Unmarshal([]byte(row.TextConfigJSON), &uc) == nil {
			resp.Text = &TextConfigDTO{
				Endpoint: uc.Endpoint,
				APIKey:   maskKey(uc.APIKey),
				Model:    uc.Model,
				Proxy:    uc.Proxy,
			}
		}
	}

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

	// Handle text config.
	if req.Text != nil {
		if req.Text.APIKey == "" && req.Text.Endpoint == "" && req.Text.Model == "" && req.Text.Proxy == "" {
			row.TextConfigJSON = ""
		} else {
			existing := s.loadTextConfig(row.TextConfigJSON)
			if req.Text.APIKey == keepExistingKey {
				if existing.APIKey == "" {
					return fmt.Errorf("%w: text.api_key cannot keep existing key because no existing key is saved", ErrInvalidModelConfig)
				}
				req.Text.APIKey = existing.APIKey
			}
			uc := model.TextUserConfig{
				Endpoint: req.Text.Endpoint,
				APIKey:   req.Text.APIKey,
				Model:    req.Text.Model,
				Proxy:    req.Text.Proxy,
			}
			if data, err := json.Marshal(&uc); err != nil {
				return err
			} else {
				row.TextConfigJSON = string(data)
			}
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

// GetTextConfig returns the user's text model config (structured).
func (s *ModelConfigService) GetTextConfig(ctx context.Context, userID string) (*model.TextUserConfig, error) {
	return s.loadUserTextConfig(ctx, userID)
}

// HasTextOverride returns true if the user has custom text model config.
func (s *ModelConfigService) HasTextOverride(ctx context.Context, userID string) bool {
	uc, err := s.loadUserTextConfig(ctx, userID)
	if err != nil {
		return false
	}
	return uc.HasConfig()
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

// GetEffectiveWritingConfig returns the resolved writing config (endpoint, key, model).
// Returns (endpoint, key, model, ok). ok=false means use server default.
func (s *ModelConfigService) GetEffectiveWritingConfig(ctx context.Context, userID string) (string, string, string, bool) {
	uc, err := s.loadUserTextConfig(ctx, userID)
	if err != nil || !uc.HasConfig() {
		return "", "", "", false
	}

	if uc.APIKey == "" || uc.Endpoint == "" || uc.Model == "" {
		return "", "", "", false
	}

	return uc.Endpoint, uc.APIKey, uc.Model, true
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
	}

	return &config.ImageAPIConfig{
		Cover:    &cover,
		Content:  &content,
		Designer: base.ImageAPI.Designer,
		Sizes:    base.ImageAPI.Sizes,
	}
}

// GetTextProxy returns the user's configured text model proxy, if any.
func (s *ModelConfigService) GetTextProxy(ctx context.Context, userID string) string {
	uc, err := s.loadUserTextConfig(ctx, userID)
	if err != nil {
		return ""
	}
	return uc.Proxy
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

func (s *ModelConfigService) loadUserTextConfig(ctx context.Context, userID string) (*model.TextUserConfig, error) {
	row, err := s.repo.ModelConfigs().FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &model.TextUserConfig{}, nil
	}
	return s.loadTextConfig(row.TextConfigJSON), nil
}

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

func (s *ModelConfigService) loadTextConfig(jsonStr string) *model.TextUserConfig {
	if jsonStr == "" {
		return &model.TextUserConfig{}
	}
	var uc model.TextUserConfig
	if json.Unmarshal([]byte(jsonStr), &uc) != nil {
		return &model.TextUserConfig{}
	}
	return &uc
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
