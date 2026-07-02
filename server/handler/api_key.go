package handler

import (
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
)

// APIKeyHandler handles API key management endpoints.
type APIKeyHandler struct {
	service *service.APIKeyService
	logger  *zerolog.Logger
}

// NewAPIKeyHandler creates a new APIKeyHandler.
func NewAPIKeyHandler(svc *service.APIKeyService, logger *zerolog.Logger) *APIKeyHandler {
	return &APIKeyHandler{service: svc, logger: logger}
}

// Create handles POST /api/v1/api-keys.
func (h *APIKeyHandler) Create(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		req.Name = "Default"
	}

	apiKey, rawKey, err := h.service.Create(c.Context(), userID, req.Name)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create api key failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create api key")
	}

	return Success(c, fiber.Map{
		"id":         apiKey.ID,
		"name":       apiKey.Name,
		"key_prefix": apiKey.KeyPrefix,
		"key":        rawKey, // Only shown once
		"created_at": apiKey.CreatedAt,
	})
}

// List handles GET /api/v1/api-keys.
func (h *APIKeyHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	keys, err := h.service.List(c.Context(), userID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("list api keys failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list api keys")
	}

	return Success(c, fiber.Map{"items": keys})
}

// Revoke handles DELETE /api/v1/api-keys/:id.
func (h *APIKeyHandler) Revoke(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	keyID := c.Params("id")
	if keyID == "" {
		return Error(c, fiber.StatusBadRequest, "missing key id")
	}

	if err := h.service.Revoke(c.Context(), userID, keyID); err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Str("key_id", keyID).Msg("revoke api key failed")
		return Error(c, fiber.StatusNotFound, "api key not found")
	}

	return Success(c, fiber.Map{"revoked": true})
}
