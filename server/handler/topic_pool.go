package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
)

// TopicPoolHandler handles topic pool HTTP endpoints.
type TopicPoolHandler struct {
	service *service.TopicPoolService
	logger  *zerolog.Logger
}

// NewTopicPoolHandler creates a new TopicPoolHandler.
func NewTopicPoolHandler(svc *service.TopicPoolService, logger *zerolog.Logger) *TopicPoolHandler {
	return &TopicPoolHandler{service: svc, logger: logger}
}

type createTopicsRequest struct {
	Topics []string `json:"topics"`
}

// List handles GET /api/v1/projects/:project_id/topics.
func (h *TopicPoolHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	projectID := c.Params("project_id")
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}

	status := c.Query("status", "")
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	topics, total, err := h.service.List(c.Context(), userID, projectID, status, offset, limit)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Str("project_id", projectID).Msg("list topics failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list topics")
	}

	return Success(c, fiber.Map{"items": topics, "total": total})
}

// Create handles POST /api/v1/projects/:project_id/topics.
func (h *TopicPoolHandler) Create(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	projectID := c.Params("project_id")
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}

	var req createTopicsRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if len(req.Topics) == 0 {
		return Error(c, fiber.StatusBadRequest, "topics array is required")
	}

	topics, err := h.service.Add(c.Context(), userID, projectID, req.Topics)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Str("project_id", projectID).Msg("add topics failed")
		return Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return Success(c, fiber.Map{"items": topics, "count": len(topics)})
}

// Delete handles DELETE /api/v1/projects/:project_id/topics/:id.
func (h *TopicPoolHandler) Delete(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	projectID := c.Params("project_id")
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return Error(c, fiber.StatusBadRequest, "invalid topic id")
	}

	if err := h.service.Delete(c.Context(), userID, projectID, uint(id)); err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Uint("topic_id", uint(id)).Msg("delete topic failed")
		return Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return Success(c, fiber.Map{"deleted": true})
}

// Reset handles PATCH /api/v1/projects/:project_id/topics/:id/reset.
func (h *TopicPoolHandler) Reset(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	projectID := c.Params("project_id")
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return Error(c, fiber.StatusBadRequest, "invalid topic id")
	}

	if err := h.service.Reset(c.Context(), userID, projectID, uint(id)); err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Uint("topic_id", uint(id)).Msg("reset topic failed")
		return Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return Success(c, fiber.Map{"reset": true})
}
