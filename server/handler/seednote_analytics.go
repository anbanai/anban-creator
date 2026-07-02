package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/service"
)

type SeednoteAnalyticsService interface {
	GetTaskAnalytics(ctx context.Context, userID, taskID string) (*service.SeednoteAnalytics, error)
}

type SeednoteAnalyticsHandler struct {
	service SeednoteAnalyticsService
	logger  *zerolog.Logger
}

func NewSeednoteAnalyticsHandler(svc SeednoteAnalyticsService, logger *zerolog.Logger) *SeednoteAnalyticsHandler {
	return &SeednoteAnalyticsHandler{service: svc, logger: logger}
}

func (h *SeednoteAnalyticsHandler) GetTaskAnalytics(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "seednote analytics unavailable")
	}

	analytics, err := h.service.GetTaskAnalytics(c.Context(), userID, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Error(c, fiber.StatusNotFound, "task not found")
		}
		if strings.Contains(err.Error(), "does not belong") {
			return Forbidden(c, "you do not have access to this task")
		}
		if h.logger != nil {
			h.logger.Error().Err(err).Str("task_id", id).Msg("get seednote analytics failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to get seednote analytics")
	}
	return Success(c, analytics)
}
