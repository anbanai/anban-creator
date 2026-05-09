package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/service"
)

// TemplateHandler handles template-related HTTP endpoints.
type TemplateHandler struct {
	service *service.TemplateService
	logger  *zerolog.Logger
}

// NewTemplateHandler creates a new TemplateHandler.
func NewTemplateHandler(svc *service.TemplateService, logger *zerolog.Logger) *TemplateHandler {
	return &TemplateHandler{service: svc, logger: logger}
}

// List handles GET /api/v1/templates.
func (h *TemplateHandler) List(c fiber.Ctx) error {
	templateType := c.Query("type", "")
	category := c.Query("category", "")
	tag := c.Query("tag", "")
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	templates, total, err := h.service.List(c.Context(), templateType, category, tag, offset, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list templates failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list templates")
	}

	return Success(c, fiber.Map{
		"items": templates,
		"total": total,
	})
}

// GetByID handles GET /api/v1/templates/:id.
func (h *TemplateHandler) GetByID(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "template id is required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid template id format")
	}

	tmpl, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "template not found")
	}

	return Success(c, tmpl)
}
