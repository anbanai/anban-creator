package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
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

	cfg := &config.ImageAPIConfig{
		Cover: &appconfig.ImageAPI{
			Provider: uc.Provider,
			Key:      uc.APIKey,
			BaseURL:  uc.Endpoint,
			Model:    uc.Model,
		},
		Content: &appconfig.ImageAPI{
			Provider: uc.Provider,
			Key:      uc.APIKey,
			BaseURL:  uc.Endpoint,
			Model:    uc.Model,
		},
		Sizes: s.cfg.ImageAPI.Sizes,
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
