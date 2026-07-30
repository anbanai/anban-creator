package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
)

// ViralAnalysisHandler handles read-only legacy viral analysis endpoints.
type ViralAnalysisHandler struct {
	service *service.ViralAnalysisHistoryService
	logger  *zerolog.Logger
}

// NewViralAnalysisHandler creates a new ViralAnalysisHandler.
func NewViralAnalysisHandler(svc *service.ViralAnalysisHistoryService, logger *zerolog.Logger) *ViralAnalysisHandler {
	return &ViralAnalysisHandler{service: svc, logger: logger}
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
	if offset < 0 {
		offset = 0
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
