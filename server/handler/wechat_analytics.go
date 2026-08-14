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

type WechatAnalyticsService interface {
	GetTaskAnalytics(ctx context.Context, userID, taskID string) (*service.WechatAnalytics, error)
	BindTask(ctx context.Context, userID, taskID, articleURL string) error
}

type WechatAnalyticsHandler struct {
	service WechatAnalyticsService
	logger  *zerolog.Logger
}

func NewWechatAnalyticsHandler(svc WechatAnalyticsService, logger *zerolog.Logger) *WechatAnalyticsHandler {
	return &WechatAnalyticsHandler{service: svc, logger: logger}
}

func (h *WechatAnalyticsHandler) GetTaskAnalytics(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "WeChat analytics unavailable")
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
		h.logger.Error().Err(err).Str("task_id", id).Msg("get WeChat analytics failed")
	}
	return Error(c, fiber.StatusInternalServerError, "failed to get WeChat analytics")
}

func (h *WechatAnalyticsHandler) BindTask(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "WeChat analytics unavailable")
	}
	var body struct {
		ArticleURL string `json:"article_url"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	err = h.service.BindTask(c.Context(), userID, id, body.ArticleURL)
	if err == nil {
		return Success(c, fiber.Map{"tracking": true})
	}
	if errors.Is(err, service.ErrWechatArticleURLInvalid) || errors.Is(err, service.ErrWechatArticleNotFound) || errors.Is(err, service.ErrWechatArticleOutsideDataWindow) {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if strings.Contains(err.Error(), "does not belong") {
		return Forbidden(c, "you do not have access to this task")
	}
	if strings.Contains(err.Error(), "not an article task") || strings.Contains(err.Error(), "missing WeChat credentials") {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if h.logger != nil {
		h.logger.Error().Err(err).Str("task_id", id).Msg("bind WeChat analytics failed")
	}
	return Error(c, fiber.StatusInternalServerError, "failed to bind WeChat analytics")
}
