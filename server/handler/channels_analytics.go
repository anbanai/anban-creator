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

type ChannelsAnalyticsService interface {
	GetTaskAnalytics(ctx context.Context, userID, taskID string) (*service.ChannelsAnalytics, error)
	BindTask(ctx context.Context, userID, taskID, videoURL string) error
}

type ChannelsAnalyticsHandler struct {
	service ChannelsAnalyticsService
	logger  *zerolog.Logger
}

func NewChannelsAnalyticsHandler(svc ChannelsAnalyticsService, logger *zerolog.Logger) *ChannelsAnalyticsHandler {
	return &ChannelsAnalyticsHandler{service: svc, logger: logger}
}

func (h *ChannelsAnalyticsHandler) GetTaskAnalytics(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "WeChat Channels analytics unavailable")
	}
	analytics, err := h.service.GetTaskAnalytics(c.Context(), userID, id)
	if err == nil {
		return Success(c, analytics)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if strings.Contains(err.Error(), "does not belong") {
		return Forbidden(c, "you do not have access to this task")
	}
	if h.logger != nil {
		h.logger.Error().Err(err).Str("task_id", id).Msg("get WeChat Channels analytics failed")
	}
	return Error(c, fiber.StatusInternalServerError, "failed to get WeChat Channels analytics")
}

func (h *ChannelsAnalyticsHandler) BindTask(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "WeChat Channels analytics unavailable")
	}
	var body struct {
		VideoURL string `json:"video_url"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	err = h.service.BindTask(c.Context(), userID, id, body.VideoURL)
	if err == nil {
		return Success(c, fiber.Map{"tracking": true})
	}
	if errors.Is(err, service.ErrChannelsVideoURLInvalid) {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if errors.Is(err, service.ErrChannelsProviderDisabled) {
		return Error(c, fiber.StatusServiceUnavailable, err.Error())
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if strings.Contains(err.Error(), "does not belong") {
		return Forbidden(c, "you do not have access to this task")
	}
	if strings.Contains(err.Error(), "not a video task") {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if h.logger != nil {
		h.logger.Error().Err(err).Str("task_id", id).Msg("bind WeChat Channels analytics failed")
	}
	return Error(c, fiber.StatusBadGateway, "failed to fetch WeChat Channels video data")
}
