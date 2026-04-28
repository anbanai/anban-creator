package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// sentinel for "keep existing key" in update requests.
const keepExistingKey = "****"

// ModelConfigService manages per-user AI model configuration.
type ModelConfigService struct {
	repo      repository.Repository
	cfg       *config.Config
	jwtSecret string
	encKey    []byte
	logger    *zerolog.Logger
}

// NewModelConfigService creates a new ModelConfigService.
func NewModelConfigService(repo repository.Repository, cfg *config.Config, jwtSecret string, logger *zerolog.Logger) *ModelConfigService {
	key, err := deriveKey(jwtSecret)
	if err != nil {
		logger.Error().Err(err).Msg("failed to derive encryption key, model config will be unavailable")
	}
	return &ModelConfigService{
		repo:      repo,
		cfg:       cfg,
		jwtSecret: jwtSecret,
		encKey:    key,
		logger:    logger,
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
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`
	Proxy   string `json:"proxy,omitempty"`
}

// ImageConfigDTO represents image model config for API display.
type ImageConfigDTO struct {
	Provider string `json:"provider,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
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

	if len(row.TextConfigEncrypted) > 0 {
		plain, err := s.decrypt(row.TextConfigEncrypted)
		if err != nil {
			s.logger.Warn().Err(err).Str("user_id", userID).Msg("failed to decrypt text config, skipping")
		} else {
			var uc model.TextUserConfig
			if json.Unmarshal([]byte(plain), &uc) == nil {
				resp.Text = &TextConfigDTO{
					BaseURL: uc.BaseURL,
					APIKey:  maskKey(uc.APIKey),
					Model:   uc.Model,
					Proxy:   uc.Proxy,
				}
			}
		}
	}

	if len(row.ImageConfigEncrypted) > 0 {
		plain, err := s.decrypt(row.ImageConfigEncrypted)
		if err != nil {
			s.logger.Warn().Err(err).Str("user_id", userID).Msg("failed to decrypt image config, skipping")
		} else {
			var uc model.ImageUserConfig
			if json.Unmarshal([]byte(plain), &uc) == nil {
				resp.Image = &ImageConfigDTO{
					Provider: uc.Provider,
					BaseURL:  uc.BaseURL,
					APIKey:   maskKey(uc.APIKey),
					Model:    uc.Model,
					Proxy:    uc.Proxy,
				}
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
		if req.Text.APIKey == "" && req.Text.BaseURL == "" && req.Text.Model == "" && req.Text.Proxy == "" {
			// All empty = clear.
			row.TextConfigEncrypted = nil
		} else {
			// Merge with existing if sentinel is used.
			existing := s.loadTextConfig(row.TextConfigEncrypted)
			if req.Text.APIKey == keepExistingKey {
				req.Text.APIKey = existing.APIKey
			}
			uc := model.TextUserConfig{
				BaseURL: req.Text.BaseURL,
				APIKey:  req.Text.APIKey,
				Model:   req.Text.Model,
				Proxy:   req.Text.Proxy,
			}
			if encrypted, err := s.encryptTextConfig(&uc); err != nil {
				return err
			} else {
				row.TextConfigEncrypted = encrypted
			}
		}
	}

	// Handle image config.
	if req.Image != nil {
		if req.Image.APIKey == "" && req.Image.BaseURL == "" && req.Image.Model == "" && req.Image.Provider == "" && req.Image.Proxy == "" {
			row.ImageConfigEncrypted = nil
		} else {
			existing := s.loadImageConfig(row.ImageConfigEncrypted)
			if req.Image.APIKey == keepExistingKey {
				req.Image.APIKey = existing.APIKey
			}
			uc := model.ImageUserConfig{
				Provider: req.Image.Provider,
				BaseURL:  req.Image.BaseURL,
				APIKey:   req.Image.APIKey,
				Model:    req.Image.Model,
				Proxy:    req.Image.Proxy,
			}
			if encrypted, err := s.encryptImageConfig(&uc); err != nil {
				return err
			} else {
				row.ImageConfigEncrypted = encrypted
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
	return uc.Provider != "" && uc.APIKey != "" && uc.Model != ""
}

// GetEffectiveWritingConfig returns the resolved writing config (baseURL, key, model).
// Returns (baseURL, key, model, ok). ok=false means use server default.
func (s *ModelConfigService) GetEffectiveWritingConfig(ctx context.Context, userID string) (string, string, string, bool) {
	uc, err := s.loadUserTextConfig(ctx, userID)
	if err != nil || !uc.HasConfig() {
		return "", "", "", false
	}

	// Need at least API key + base URL + model to create a writing client.
	if uc.APIKey == "" || uc.BaseURL == "" || uc.Model == "" {
		return "", "", "", false
	}

	return uc.BaseURL, uc.APIKey, uc.Model, true
}

// GetEffectiveImageConfig returns the resolved image config as an ImageAPIConfig.
// Returns nil if no user override exists (use server default).
func (s *ModelConfigService) GetEffectiveImageConfig(ctx context.Context, userID string) *config.ImageAPIConfig {
	uc, err := s.loadUserImageConfig(ctx, userID)
	if err != nil || !uc.HasConfig() {
		return nil
	}


	// Create separate instances for Cover and Content to avoid shared-pointer mutation.
	cfg := &config.ImageAPIConfig{
		Cover: &appconfig.ImageAPI{
			Provider: uc.Provider,
			Key:      uc.APIKey,
			BaseURL:  uc.BaseURL,
			Model:    uc.Model,
		},
		Content: &appconfig.ImageAPI{
			Provider: uc.Provider,
			Key:      uc.APIKey,
			BaseURL:  uc.BaseURL,
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
	uc := s.loadTextConfig(row.TextConfigEncrypted)
	return uc, nil
}

func (s *ModelConfigService) loadUserImageConfig(ctx context.Context, userID string) (*model.ImageUserConfig, error) {
	row, err := s.repo.ModelConfigs().FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &model.ImageUserConfig{}, nil
	}
	uc := s.loadImageConfig(row.ImageConfigEncrypted)
	return uc, nil
}

func (s *ModelConfigService) loadTextConfig(encrypted []byte) *model.TextUserConfig {
	if len(encrypted) == 0 {
		return &model.TextUserConfig{}
	}
	plain, err := s.decrypt(encrypted)
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to decrypt text config")
		return &model.TextUserConfig{}
	}
	var uc model.TextUserConfig
	if json.Unmarshal([]byte(plain), &uc) != nil {
		return &model.TextUserConfig{}
	}
	return &uc
}

func (s *ModelConfigService) loadImageConfig(encrypted []byte) *model.ImageUserConfig {
	if len(encrypted) == 0 {
		return &model.ImageUserConfig{}
	}
	plain, err := s.decrypt(encrypted)
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to decrypt image config")
		return &model.ImageUserConfig{}
	}
	var uc model.ImageUserConfig
	if json.Unmarshal([]byte(plain), &uc) != nil {
		return &model.ImageUserConfig{}
	}
	return &uc
}

func (s *ModelConfigService) encryptTextConfig(uc *model.TextUserConfig) ([]byte, error) {
	data, err := json.Marshal(uc)
	if err != nil {
		return nil, err
	}
	return s.encrypt(string(data))
}

func (s *ModelConfigService) encryptImageConfig(uc *model.ImageUserConfig) ([]byte, error) {
	data, err := json.Marshal(uc)
	if err != nil {
		return nil, err
	}
	return s.encrypt(string(data))
}

// decrypt decrypts using the cached encryption key.
func (s *ModelConfigService) decrypt(ciphertext []byte) (string, error) {
	if len(s.encKey) == 0 {
		return "", fmt.Errorf("encryption key not available")
	}
	return decryptWithKey(ciphertext, s.encKey)
}

// encrypt encrypts using the cached encryption key.
func (s *ModelConfigService) encrypt(plaintext string) ([]byte, error) {
	if len(s.encKey) == 0 {
		return nil, fmt.Errorf("encryption key not available")
	}
	return encryptWithKey(plaintext, s.encKey)
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
