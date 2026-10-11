package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type WechatAccountAnalyticsImportAPI interface {
	Preview(context.Context, service.WechatAccountAnalyticsImportRequest) (*service.WechatAccountAnalyticsImportPreview, error)
	Import(context.Context, service.WechatAccountAnalyticsImportRequest) (*service.WechatAccountAnalyticsImportSummary, error)
	ListBatches(context.Context, string, string, int, int) ([]*model.WechatAccountAnalyticsBatch, int64, error)
	GetBatch(context.Context, string, string, string) (*service.WechatAccountAnalyticsImportSummary, error)
	Revoke(context.Context, string, string, string) (*service.WechatAccountAnalyticsImportSummary, error)
}

type WechatAccountAnalyticsImportHandler struct {
	service WechatAccountAnalyticsImportAPI
	logger  *zerolog.Logger
}

func NewWechatAccountAnalyticsImportHandler(svc WechatAccountAnalyticsImportAPI, logger *zerolog.Logger) *WechatAccountAnalyticsImportHandler {
	return &WechatAccountAnalyticsImportHandler{service: svc, logger: logger}
}

func (h *WechatAccountAnalyticsImportHandler) Preview(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.WechatAccountAnalyticsImportRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.UserID, req.ProjectID = userID, projectID
	result, err := h.service.Preview(c.Context(), req)
	return h.respond(c, result, err)
}

func (h *WechatAccountAnalyticsImportHandler) Import(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.WechatAccountAnalyticsImportRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.UserID, req.ProjectID = userID, projectID
	if strings.TrimSpace(req.IdempotencyKey) == "" || req.DataAsOfAt == nil {
		return Error(c, fiber.StatusBadRequest, "data_as_of_at 和 idempotency_key 为必填")
	}
	result, err := h.service.Import(c.Context(), req)
	return h.respond(c, result, err)
}

func (h *WechatAccountAnalyticsImportHandler) List(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	items, total, err := h.service.ListBatches(c.Context(), userID, projectID, offset, limit)
	if err != nil {
		return h.respond(c, nil, err)
	}
	return Success(c, fiber.Map{"items": items, "total": total, "offset": offset, "limit": limit})
}

func (h *WechatAccountAnalyticsImportHandler) Detail(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	batchID, err := validateUUIDParam(c, "batchId")
	if err != nil {
		return err
	}
	result, err := h.service.GetBatch(c.Context(), userID, projectID, batchID)
	return h.respond(c, result, err)
}

func (h *WechatAccountAnalyticsImportHandler) Revoke(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	batchID, err := validateUUIDParam(c, "batchId")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	result, err := h.service.Revoke(c.Context(), userID, projectID, batchID)
	return h.respond(c, result, err)
}

func (h *WechatAccountAnalyticsImportHandler) respond(c fiber.Ctx, value any, err error) error {
	if errors.Is(err, repository.ErrAnalyticsIdempotencyConflict) || errors.Is(err, repository.ErrAnalyticsRebuilding) {
		return analyticsResponse(c, value, err)
	}
	if err == nil {
		return Success(c, value)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Error(c, fiber.StatusNotFound, "not found")
	}
	if errors.Is(err, service.ErrWechatAccountAnalyticsUnavailable) {
		return Error(c, fiber.StatusServiceUnavailable, "账号统计导入暂时不可用")
	}
	if strings.Contains(err.Error(), "does not belong") || strings.Contains(err.Error(), "requires a WeChat project") {
		return Forbidden(c, "you do not have access to this project")
	}
	if h.logger != nil {
		h.logger.Error().Err(err).Msg("WeChat account analytics import request failed")
	}
	return Error(c, fiber.StatusBadRequest, err.Error())
}
