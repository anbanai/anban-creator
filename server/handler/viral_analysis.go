package handler

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/service"
)

// ViralAnalysisHandler handles viral content analysis HTTP endpoints.
type ViralAnalysisHandler struct {
	service *service.ViralAnalysisService
	logger  *zerolog.Logger
}

// NewViralAnalysisHandler creates a new ViralAnalysisHandler.
func NewViralAnalysisHandler(svc *service.ViralAnalysisService, logger *zerolog.Logger) *ViralAnalysisHandler {
	return &ViralAnalysisHandler{service: svc, logger: logger}
}

type createViralAnalysisRequest struct {
	SourceType string `json:"source_type"`
	SourceURL  string `json:"source_url"`
}

// Create handles POST /api/v1/viral-analyses.
func (h *ViralAnalysisHandler) Create(c fiber.Ctx) error {
	var req createViralAnalysisRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.SourceType != "note" && req.SourceType != "profile" {
		return Error(c, fiber.StatusBadRequest, "source_type must be 'note' or 'profile'")
	}

	if req.SourceURL == "" {
		return Error(c, fiber.StatusBadRequest, "source_url is required")
	}

	if len(req.SourceURL) > 500 {
		return Error(c, fiber.StatusBadRequest, "source_url must be 500 characters or fewer")
	}

	if !strings.HasPrefix(req.SourceURL, "http://") && !strings.HasPrefix(req.SourceURL, "https://") {
		return Error(c, fiber.StatusBadRequest, "source_url must start with http:// or https://")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	analysis, err := h.service.Create(c.Context(), userID, req.SourceType, req.SourceURL)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create viral analysis failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create viral analysis")
	}

	return Success(c, analysis)
}

// GetByID handles GET /api/v1/viral-analyses/:id.
func (h *ViralAnalysisHandler) GetByID(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	analysis, err := h.service.GetByID(c.Context(), id, userID)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "viral analysis not found")
	}

	return Success(c, analysis)
}

// List handles GET /api/v1/viral-analyses.
func (h *ViralAnalysisHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	analyses, total, err := h.service.ListByUserID(c.Context(), userID, offset, limit)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("list viral analyses failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list viral analyses")
	}

	return Success(c, fiber.Map{
		"items": analyses,
		"total": total,
	})
}
