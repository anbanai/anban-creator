package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type WechatAnalyticsImportAPI interface {
	Revoke(context.Context, string, string, string) (*service.WechatAnalyticsImportSummary, error)
	Preview(context.Context, service.WechatAnalyticsImportRequest) (*service.WechatAnalyticsImportPreview, error)
	Import(context.Context, service.WechatAnalyticsImportRequest) (*service.WechatAnalyticsImportSummary, error)
	ListBatches(context.Context, string, string, int, int) ([]*model.WechatAnalyticsImportBatch, int64, error)
	GetBatch(context.Context, string, string, string) (*service.WechatAnalyticsImportSummary, error)
	GetBatchByID(context.Context, string, string) (*service.WechatAnalyticsImportSummary, error)
	Overview(context.Context, string, string) (*service.WechatAnalyticsOverview, error)
	ListArticles(context.Context, string, string) ([]service.WechatAnalyticsArticleView, error)
	Snapshots(context.Context, string, string, string) ([]*model.WechatAnalyticsSnapshot, error)
	SnapshotsByPublicationID(context.Context, string, string) ([]*model.WechatAnalyticsSnapshot, error)
}

func (h *WechatAnalyticsImportHandler) GlobalDetail(c fiber.Ctx) error {
	batchID, err := validateUUIDParam(c, "batchId")
	if err != nil {
		return err
	}
	result, err := h.service.GetBatchByID(c.Context(), GetUserID(c), batchID)
	return h.respond(c, result, err)
}

func (h *WechatAnalyticsImportHandler) GlobalArticle(c fiber.Ctx) error {
	articleID, err := validateUUIDParam(c, "articleId")
	if err != nil {
		return err
	}
	items, err := h.service.SnapshotsByPublicationID(c.Context(), GetUserID(c), articleID)
	if err != nil {
		return h.respond(c, nil, err)
	}
	return Success(c, fiber.Map{"items": items})
}

func (h *WechatAnalyticsImportHandler) Preview(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.WechatAnalyticsImportRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.UserID, req.ProjectID = userID, projectID
	result, err := h.service.Preview(c.Context(), req)
	return h.respond(c, result, err)
}

type WechatAnalyticsImportHandler struct {
	service WechatAnalyticsImportAPI
	logger  *zerolog.Logger
}

func NewWechatAnalyticsImportHandler(svc WechatAnalyticsImportAPI, logger *zerolog.Logger) *WechatAnalyticsImportHandler {
	return &WechatAnalyticsImportHandler{service: svc, logger: logger}
}

func (h *WechatAnalyticsImportHandler) Import(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.WechatAnalyticsImportRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.UserID, req.ProjectID = userID, projectID
	result, err := h.service.Import(c.Context(), req)
	if err != nil {
		return h.respond(c, nil, err)
	}
	return Success(c, result.Receipt())
}

func (h *WechatAnalyticsImportHandler) List(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	batches, total, err := h.service.ListBatches(c.Context(), GetUserID(c), projectID, offset, limit)
	if err != nil {
		return h.respond(c, nil, err)
	}
	return Success(c, fiber.Map{"items": batches, "total": total, "offset": offset, "limit": limit})
}

func (h *WechatAnalyticsImportHandler) Detail(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	result, err := h.service.GetBatch(c.Context(), GetUserID(c), projectID, strings.TrimSpace(c.Params("batchId")))
	return h.respond(c, result, err)
}

func (h *WechatAnalyticsImportHandler) Overview(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	result, err := h.service.Overview(c.Context(), GetUserID(c), projectID)
	return h.respond(c, result, err)
}

func (h *WechatAnalyticsImportHandler) Articles(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	items, err := h.service.ListArticles(c.Context(), GetUserID(c), projectID)
	if err != nil {
		return h.respond(c, nil, err)
	}
	return Success(c, fiber.Map{"items": items})
}

func (h *WechatAnalyticsImportHandler) Article(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	items, err := h.service.Snapshots(c.Context(), GetUserID(c), projectID, strings.TrimSpace(c.Params("articleId")))
	if err != nil {
		return h.respond(c, nil, err)
	}
	return Success(c, fiber.Map{"items": items})
}

func (h *WechatAnalyticsImportHandler) respond(c fiber.Ctx, value any, err error) error {
	if err == nil {
		return Success(c, value)
	}
	if errors.Is(err, service.ErrAnalyticsImportRevoked) {
		return Error(c, fiber.StatusConflict, err.Error())
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Error(c, fiber.StatusNotFound, "not found")
	}
	if strings.Contains(err.Error(), "does not belong") {
		return Forbidden(c, "you do not have access to this project")
	}
	if h.logger != nil {
		h.logger.Error().Err(err).Msg("WeChat analytics import request failed")
	}
	return Error(c, fiber.StatusBadRequest, err.Error())
}

func (h *WechatAnalyticsImportHandler) Revoke(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil || projectID == "" {
		return err
	}
	batchID, err := validateUUIDParam(c, "batchId")
	if err != nil || batchID == "" {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	result, err := h.service.Revoke(c.Context(), userID, projectID, batchID)
	return h.respond(c, result, err)
}
