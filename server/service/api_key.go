package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// APIKeyService manages per-user API keys.
type APIKeyService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewAPIKeyService creates a new APIKeyService.
func NewAPIKeyService(repo repository.Repository, logger *zerolog.Logger) *APIKeyService {
	return &APIKeyService{repo: repo, logger: logger}
}

// Create generates a new API key for a user. Returns the model and the raw key
// (which is only shown once at creation time).
func (s *APIKeyService) Create(ctx context.Context, userID, name string) (*model.APIKey, string, error) {
	rawKey, keyHash, keyPrefix, err := generateAPIKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate key: %w", err)
	}

	apiKey := &model.APIKey{
		ID:        strings.ReplaceAll(uuid.New().String(), "-", ""),
		UserID:    userID,
		Name:      name,
		KeyHash:   keyHash,
		KeyPrefix: keyPrefix,
	}

	if err := s.repo.APIKeys().Create(ctx, apiKey); err != nil {
		return nil, "", fmt.Errorf("create api key: %w", err)
	}

	s.logger.Info().
		Str("user_id", userID).
		Str("key_prefix", keyPrefix).
		Msg("api key created")

	return apiKey, rawKey, nil
}

// List returns all user-created API keys for a user (without the hash).
// Managed keys (auto-created for Docker MCP auth) are excluded.
func (s *APIKeyService) List(ctx context.Context, userID string) ([]*model.APIKey, error) {
	return s.repo.APIKeys().FindByUserID(ctx, userID)
}

// EnsureUserKey returns a raw API key for the given user.
// If the user has a managed key, it returns the existing raw key.
// Otherwise, it creates a new managed key and returns the raw key.
func (s *APIKeyService) EnsureUserKey(ctx context.Context, userID string) (string, error) {
	// Check for existing managed key.
	existing, err := s.repo.APIKeys().FindManagedByUserID(ctx, userID)
	if err == nil && existing != nil && existing.RawKey != "" {
		return existing.RawKey, nil
	}

	// Create a new managed key.
	rawKey, keyHash, keyPrefix, err := generateAPIKey()
	if err != nil {
		return "", fmt.Errorf("generate managed key: %w", err)
	}

	apiKey := &model.APIKey{
		ID:        strings.ReplaceAll(uuid.New().String(), "-", ""),
		UserID:    userID,
		Name:      "system-managed",
		KeyHash:   keyHash,
		KeyPrefix: keyPrefix,
		IsManaged: true,
		RawKey:    rawKey,
	}

	if err := s.repo.APIKeys().Create(ctx, apiKey); err != nil {
		return "", fmt.Errorf("create managed api key: %w", err)
	}

	s.logger.Info().
		Str("user_id", userID).
		Str("key_prefix", keyPrefix).
		Msg("managed API key created")

	return rawKey, nil
}

// Revoke deletes an API key. Verifies ownership before deleting.
func (s *APIKeyService) Revoke(ctx context.Context, userID, keyID string) error {
	key, err := s.repo.APIKeys().FindByID(ctx, keyID)
	if err != nil {
		return fmt.Errorf("api key not found")
	}
	if key.UserID != userID {
		return fmt.Errorf("api key not found")
	}

	if err := s.repo.APIKeys().Delete(ctx, keyID); err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}

	s.logger.Info().
		Str("user_id", userID).
		Str("key_id", keyID).
		Msg("api key revoked")

	return nil
}

// Validate checks a raw API key and returns the associated APIKey record.
// Updates LastUsedAt on success.
func (s *APIKeyService) Validate(ctx context.Context, rawKey string) (*model.APIKey, error) {
	hash := hashAPIKey(rawKey)
	key, err := s.repo.APIKeys().FindByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("invalid api key")
	}

	// Update last used timestamp (best effort).
	_ = s.repo.APIKeys().UpdateLastUsed(ctx, key.ID)

	return key, nil
}

// generateAPIKey creates a raw key, its SHA256 hash, and display prefix.
func generateAPIKey() (rawKey, keyHash, keyPrefix string, err error) {
	bytes := make([]byte, 32)
	if _, err = rand.Read(bytes); err != nil {
		return "", "", "", err
	}
	rawKey = "anb_" + hex.EncodeToString(bytes)
	keyHash = hashAPIKey(rawKey)
	keyPrefix = rawKey[:12] // "anb_" + first 8 hex chars
	return rawKey, keyHash, keyPrefix, nil
}

// hashAPIKey returns the SHA256 hex of a raw key.
func hashAPIKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}
