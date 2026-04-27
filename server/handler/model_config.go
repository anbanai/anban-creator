package handler

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/service"
)

// ModelConfigHandler handles per-user AI model configuration endpoints.
type ModelConfigHandler struct {
	service *service.ModelConfigService
	logger  *zerolog.Logger
}

// NewModelConfigHandler creates a new ModelConfigHandler.
func NewModelConfigHandler(svc *service.ModelConfigService, logger *zerolog.Logger) *ModelConfigHandler {
	return &ModelConfigHandler{service: svc, logger: logger}
}

// Get handles GET /api/v1/model-config.
func (h *ModelConfigHandler) Get(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	resp, err := h.service.Get(c.Context(), userID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("get model config failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get model config")
	}

	return Success(c, resp)
}

// Update handles PUT /api/v1/model-config.
func (h *ModelConfigHandler) Update(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req service.UpdateModelConfigRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.Text == nil && req.Image == nil {
		return Error(c, fiber.StatusBadRequest, "no config provided")
	}

	const maxFieldLen = 2048
	const sentinel = "****"

	if req.Text != nil {
		if err := validateConfigField("text.base_url", req.Text.BaseURL, maxFieldLen, true); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if err := validateConfigField("text.model", req.Text.Model, maxFieldLen, false); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if err := validateConfigField("text.api_key", req.Text.APIKey, maxFieldLen, false); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if err := validateConfigField("text.proxy", req.Text.Proxy, maxFieldLen, true); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		// Reject sentinel as a real API key value (must be used only to keep existing).
		if req.Text.APIKey != sentinel && (req.Text.APIKey == "" || len(strings.TrimSpace(req.Text.APIKey)) != len(req.Text.APIKey)) {
			return Error(c, fiber.StatusBadRequest, "text.api_key must not be blank or whitespace-only")
		}
	}

	if req.Image != nil {
		if err := validateConfigField("image.base_url", req.Image.BaseURL, maxFieldLen, true); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if err := validateConfigField("image.model", req.Image.Model, maxFieldLen, false); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if err := validateConfigField("image.api_key", req.Image.APIKey, maxFieldLen, false); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if err := validateConfigField("image.proxy", req.Image.Proxy, maxFieldLen, true); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if req.Image.Provider != "" && len(req.Image.Provider) > 64 {
			return Error(c, fiber.StatusBadRequest, "image.provider too long (max 64 chars)")
		}
	}

	if err := h.service.Update(c.Context(), userID, &req); err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("update model config failed")
		return Error(c, fiber.StatusInternalServerError, "failed to update model config")
	}

	return Success(c, fiber.Map{"updated": true})
}

// validateConfigField checks length and URL format for a config field.
func validateConfigField(field, value string, maxLen int, isURL bool) error {
	if value == "" || value == "****" {
		return nil
	}
	if len(value) > maxLen {
		return fmt.Errorf("%s too long (max %d chars)", field, maxLen)
	}
	if isURL {
		if _, err := url.ParseRequestURI(value); err != nil {
			return fmt.Errorf("%s is not a valid URL", field)
		}
	}
	return nil
}

// Delete handles DELETE /api/v1/model-config.
func (h *ModelConfigHandler) Delete(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	if err := h.service.Delete(c.Context(), userID); err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("delete model config failed")
		return Error(c, fiber.StatusInternalServerError, "failed to delete model config")
	}

	return Success(c, fiber.Map{"deleted": true})
}
