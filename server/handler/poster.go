package handler

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/service"
)

// PosterHandler handles poster generation HTTP endpoints.
type PosterHandler struct {
	service *service.PosterService
	logger  *zerolog.Logger
}

// NewPosterHandler creates a new PosterHandler.
func NewPosterHandler(svc *service.PosterService, logger *zerolog.Logger) *PosterHandler {
	return &PosterHandler{service: svc, logger: logger}
}

type createPosterRequest struct {
	TemplateID      *string `json:"template_id"`
	InputContent    any     `json:"input_content"`
	StylePreference string  `json:"style_preference"`
}

// Create handles POST /api/v1/posters.
func (h *PosterHandler) Create(c fiber.Ctx) error {
	var req createPosterRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.InputContent == nil {
		return Error(c, fiber.StatusBadRequest, "input_content is required")
	}

	// Check that input_content is not empty (covers empty string, empty object, empty array).
	switch v := req.InputContent.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return Error(c, fiber.StatusBadRequest, "input_content must not be empty")
		}
	case map[string]any:
		if len(v) == 0 {
			return Error(c, fiber.StatusBadRequest, "input_content must not be empty")
		}
	case []any:
		if len(v) == 0 {
			return Error(c, fiber.StatusBadRequest, "input_content must not be empty")
		}
	}

	if req.StylePreference == "" {
		return Error(c, fiber.StatusBadRequest, "style_preference is required")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Marshal input_content (interface{}) to JSON raw bytes.
	inputJSON, err := json.Marshal(req.InputContent)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid input_content format")
	}

	poster, err := h.service.Create(c.Context(), userID, inputJSON, req.StylePreference, req.TemplateID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create poster failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create poster")
	}

	return Success(c, poster)
}

// GetByID handles GET /api/v1/posters/:id.
func (h *PosterHandler) GetByID(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	task, err := h.service.GetByID(c.Context(), id, userID)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "poster task not found")
	}

	return Success(c, task)
}

// List handles GET /api/v1/posters.
func (h *PosterHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	tasks, total, err := h.service.ListByUserID(c.Context(), userID, offset, limit)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("list posters failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list poster tasks")
	}

	return Success(c, fiber.Map{
		"items": tasks,
		"total": total,
	})
}
