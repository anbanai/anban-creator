package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
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

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(c.Body(), &fields); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	for field := range fields {
		if field != "image" {
			return Error(c, fiber.StatusBadRequest, "unsupported model config field: "+field)
		}
	}

	var req service.UpdateModelConfigRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.Image == nil {
		return Error(c, fiber.StatusBadRequest, "no image config provided")
	}

	const maxFieldLen = 2048
	const sentinel = "****"

	if req.Image != nil {
		if err := validateConfigField("image.endpoint", req.Image.Endpoint, maxFieldLen, true); err != nil {
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
		if err := validateImageConfig(req.Image, sentinel); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
	}

	if err := h.service.Update(c.Context(), userID, &req); err != nil {
		if errors.Is(err, service.ErrInvalidModelConfig) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("update model config failed")
		return Error(c, fiber.StatusInternalServerError, "failed to update model config")
	}

	return Success(c, fiber.Map{"updated": true})
}

func validateImageConfig(img *service.ImageConfigDTO, sentinel string) error {
	if img == nil {
		return nil
	}
	if img.APIKey == "" && img.Endpoint == "" && img.Model == "" && img.Provider == "" && img.Proxy == "" {
		return nil
	}

	provider := service.NormalizeImageProvider(img.Provider)
	switch provider {
	case "openai", "gemini", "volcengine":
	case "":
		return fmt.Errorf("image.provider is required when configuring image model")
	default:
		return fmt.Errorf("image.provider must be one of: openai, gemini, volcengine")
	}

	if img.Endpoint == "" {
		return fmt.Errorf("image.endpoint is required when configuring image model")
	}
	if img.Model == "" {
		return fmt.Errorf("image.model is required when configuring image model")
	}
	if img.APIKey == "" {
		return fmt.Errorf("image.api_key is required when configuring image model")
	}
	if img.APIKey != sentinel && len(strings.TrimSpace(img.APIKey)) != len(img.APIKey) {
		return fmt.Errorf("image.api_key must not be blank or whitespace-only")
	}
	return nil
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
