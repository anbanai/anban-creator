package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type WechatAccountAnalyticsAPI interface {
	Overview(ctx context.Context, userID, projectID, from, to string) (*service.WechatAccountOverview, error)
	TrafficSources(ctx context.Context, userID, projectID, from, to, metric, source string) (*service.WechatTrafficSourcesResponse, error)
}

type WechatAccountAnalyticsSyncAPI interface {
	Sync(ctx context.Context, userID, projectID, from, to string) (*service.WechatAccountAnalyticsSyncResult, error)
}

type WechatAccountAnalyticsHandler struct {
	service WechatAccountAnalyticsAPI
	sync    WechatAccountAnalyticsSyncAPI
	logger  *zerolog.Logger
}

func NewWechatAccountAnalyticsHandler(svc WechatAccountAnalyticsAPI, logger *zerolog.Logger, syncServices ...WechatAccountAnalyticsSyncAPI) *WechatAccountAnalyticsHandler {
	handler := &WechatAccountAnalyticsHandler{service: svc, logger: logger}
	if len(syncServices) > 0 {
		handler.sync = syncServices[0]
	}
	return handler
}

func (h *WechatAccountAnalyticsHandler) Sync(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.sync == nil {
		return Error(c, fiber.StatusServiceUnavailable, "公众号账号统计同步暂不可用")
	}
	var request struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := c.Bind().Body(&request); err != nil {
		return Error(c, fiber.StatusBadRequest, "请求格式无效")
	}
	if strings.TrimSpace(request.From) == "" || strings.TrimSpace(request.To) == "" {
		return Error(c, fiber.StatusBadRequest, "from 和 to 为必填日期")
	}
	value, err := h.sync.Sync(c.Context(), userID, projectID, request.From, request.To)
	return h.respond(c, value, err)
}

func (h *WechatAccountAnalyticsHandler) Overview(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	from, to := strings.TrimSpace(c.Query("from")), strings.TrimSpace(c.Query("to"))
	if from == "" || to == "" {
		return Error(c, fiber.StatusBadRequest, "from 和 to 为必填日期")
	}
	value, err := h.service.Overview(c.Context(), userID, projectID, from, to)
	return h.respond(c, value, err)
}

func (h *WechatAccountAnalyticsHandler) TrafficSources(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	from, to := strings.TrimSpace(c.Query("from")), strings.TrimSpace(c.Query("to"))
	if from == "" || to == "" {
		return Error(c, fiber.StatusBadRequest, "from 和 to 为必填日期")
	}
	value, err := h.service.TrafficSources(c.Context(), userID, projectID, from, to, c.Query("metric"), c.Query("source"))
	return h.respond(c, value, err)
}

func (h *WechatAccountAnalyticsHandler) respond(c fiber.Ctx, value any, err error) error {
	if err == nil {
		return Success(c, value)
	}
	switch {
	case errors.Is(err, service.ErrAnalyticsForbidden):
		return Forbidden(c, "you do not have access to this project")
	case errors.Is(err, service.ErrAnalyticsInvalidQuery):
		return Error(c, fiber.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrWechatAccountAnalyticsUnavailable):
		return Error(c, fiber.StatusServiceUnavailable, "账号统计暂时无法读取")
	case errors.Is(err, service.ErrWechatAccountAnalyticsSyncRange):
		return Error(c, fiber.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrWechatAccountAnalyticsAuthorizationRequired):
		return Error(c, fiber.StatusConflict, "请先绑定有效的公众号授权")
	case errors.Is(err, service.ErrWechatAccountAnalyticsCapabilityMissing):
		return Error(c, fiber.StatusConflict, "当前授权未包含公众号用户统计权限")
	case errors.Is(err, gorm.ErrRecordNotFound):
		return Error(c, fiber.StatusNotFound, "project not found")
	default:
		if h.logger != nil {
			h.logger.Error().Err(err).Msg("WeChat account analytics request failed")
		}
		return Error(c, fiber.StatusInternalServerError, "账号统计暂时无法读取")
	}
}
