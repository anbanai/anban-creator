package handler

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
	"gorm.io/gorm"
)

// FeedbackHandler handles feedback-related HTTP endpoints.
type FeedbackHandler struct {
	service *service.FeedbackService
	logger  *zerolog.Logger
}

// NewFeedbackHandler creates a new FeedbackHandler.
func NewFeedbackHandler(svc *service.FeedbackService, logger *zerolog.Logger) *FeedbackHandler {
	return &FeedbackHandler{service: svc, logger: logger}
}

type createFeedbackRequest struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// Create handles POST /api/v1/feedback.
func (h *FeedbackHandler) Create(c fiber.Ctx) error {
	var req createFeedbackRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.Type == "" {
		return Error(c, fiber.StatusBadRequest, "type is required")
	}
	if req.Content == "" {
		return Error(c, fiber.StatusBadRequest, "content is required")
	}
	if len(req.Content) > 1000 {
		return Error(c, fiber.StatusBadRequest, "content must be 1000 characters or less")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	feedback, err := h.service.Create(c.Context(), userID, req.Type, req.Content)
	if err != nil {
		if h.logger != nil {
			h.logger.Error().Err(err).Msg("failed to create feedback")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to create feedback")
	}

	return c.Status(fiber.StatusCreated).JSON(Response{Code: 0, Msg: "success", Data: feedback})
}

type taskFeedbackRequest struct {
	Rating  int    `json:"rating"`
	Content string `json:"content"`
}

// GetTaskFeedback handles GET /api/v1/tasks/:id/feedback.
func (h *FeedbackHandler) GetTaskFeedback(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	feedback, err := h.service.GetTaskFeedback(c.Context(), userID, id)
	if err != nil {
		return h.respondTaskFeedbackError(c, err)
	}
	return c.JSON(fiber.Map{"code": 0, "msg": "success", "data": feedback})
}

// UpsertTaskFeedback handles PUT /api/v1/tasks/:id/feedback.
func (h *FeedbackHandler) UpsertTaskFeedback(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req taskFeedbackRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Content) != "" && len([]rune(strings.TrimSpace(req.Content))) > 1000 {
		return Error(c, fiber.StatusBadRequest, "content must be 1000 characters or less")
	}
	feedback, err := h.service.UpsertTaskFeedback(c.Context(), userID, id, req.Rating, req.Content)
	if err != nil {
		return h.respondTaskFeedbackError(c, err)
	}
	return Success(c, feedback)
}

func (h *FeedbackHandler) respondTaskFeedbackError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return Error(c, fiber.StatusNotFound, "task not found")
	case errors.Is(err, service.ErrTaskFeedbackForbidden):
		return Forbidden(c, "you do not have access to this task")
	case errors.Is(err, service.ErrTaskFeedbackNotCompleted):
		return Error(c, fiber.StatusBadRequest, "only completed tasks can be evaluated")
	case errors.Is(err, service.ErrTaskFeedbackInvalidRating):
		return Error(c, fiber.StatusBadRequest, "rating must be between 1 and 5")
	case errors.Is(err, service.ErrTaskFeedbackContentTooLong):
		return Error(c, fiber.StatusBadRequest, "content must be 1000 characters or less")
	default:
		if h.logger != nil {
			h.logger.Error().Err(err).Msg("task feedback request failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to process task feedback")
	}
}
