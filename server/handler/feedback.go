package handler

import (
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
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
		h.logger.Error().Err(err).Msg("failed to create feedback")
		return Error(c, fiber.StatusInternalServerError, "failed to create feedback")
	}

	return c.Status(fiber.StatusCreated).JSON(Response{Code: 0, Msg: "success", Data: feedback})
}
