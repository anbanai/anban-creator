package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

type WechatPublicationActions interface {
	Get(context.Context, string, string) (*model.WechatPublication, error)
	Publish(context.Context, string, string) (*model.WechatPublication, error)
	RetryPublish(context.Context, string, string) (*model.WechatPublication, error)
	Reconcile(context.Context, string, string) error
	Select(context.Context, string, string, string) (*model.WechatPublication, error)
	Recover(context.Context, string, string) (model.TaskPublicationOutcome, error)
}

type WechatPublicationHandler struct {
	service WechatPublicationActions
	logger  *zerolog.Logger
}

func NewWechatPublicationHandler(actions WechatPublicationActions, logger *zerolog.Logger) *WechatPublicationHandler {
	return &WechatPublicationHandler{service: actions, logger: logger}
}

func (h *WechatPublicationHandler) requestIdentity(c fiber.Ctx) (string, string, error) {
	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return "", "", err
	}
	userID := GetUserID(c)
	if userID == "" {
		return "", "", Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return "", "", Error(c, fiber.StatusServiceUnavailable, "WeChat publication unavailable")
	}
	return userID, taskID, nil
}

func (h *WechatPublicationHandler) Get(c fiber.Ctx) error {
	userID, taskID, err := h.requestIdentity(c)
	if err != nil {
		return err
	}
	publication, err := h.service.Get(c.Context(), userID, taskID)
	if err != nil {
		return h.handleError(c, taskID, err)
	}
	return Success(c, publication)
}

func (h *WechatPublicationHandler) Capabilities(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	provider, ok := h.service.(interface {
		GetCapabilities(context.Context, string, string) ([]*model.WechatAccountCapability, error)
	})
	if !ok {
		return Error(c, fiber.StatusServiceUnavailable, "WeChat capabilities unavailable")
	}
	items, err := provider.GetCapabilities(c.Context(), userID, projectID)
	if err != nil {
		return h.handleError(c, projectID, err)
	}
	return Success(c, fiber.Map{"items": items})
}

func (h *WechatPublicationHandler) Publish(c fiber.Ctx) error {
	userID, taskID, err := h.requestIdentity(c)
	if err != nil {
		return err
	}
	publication, err := h.service.Publish(c.Context(), userID, taskID)
	if err != nil {
		return h.handleError(c, taskID, err)
	}
	return Success(c, publication)
}

func (h *WechatPublicationHandler) RetryPublish(c fiber.Ctx) error {
	userID, taskID, err := h.requestIdentity(c)
	if err != nil {
		return err
	}
	publication, err := h.service.RetryPublish(c.Context(), userID, taskID)
	if err != nil {
		return h.handleError(c, taskID, err)
	}
	return Success(c, publication)
}

func (h *WechatPublicationHandler) Reconcile(c fiber.Ctx) error {
	userID, taskID, err := h.requestIdentity(c)
	if err != nil {
		return err
	}
	if err := h.service.Reconcile(c.Context(), userID, taskID); err != nil {
		return h.handleError(c, taskID, err)
	}
	return Success(c, fiber.Map{"reconciled": true})
}

func (h *WechatPublicationHandler) Recover(c fiber.Ctx) error {
	userID, taskID, err := h.requestIdentity(c)
	if err != nil {
		return err
	}
	outcome, err := h.service.Recover(c.Context(), userID, taskID)
	if err != nil {
		return h.handleError(c, taskID, err)
	}
	return Success(c, outcome)
}

func (h *WechatPublicationHandler) Select(c fiber.Ctx) error {
	userID, taskID, err := h.requestIdentity(c)
	if err != nil {
		return err
	}
	var body struct {
		ArticleID string `json:"article_id"`
	}
	if err := c.Bind().Body(&body); err != nil || strings.TrimSpace(body.ArticleID) == "" {
		return Error(c, fiber.StatusBadRequest, "article_id is required")
	}
	publication, err := h.service.Select(c.Context(), userID, taskID, strings.TrimSpace(body.ArticleID))
	if err != nil {
		return h.handleError(c, taskID, err)
	}
	return Success(c, publication)
}

func (h *WechatPublicationHandler) ManualBind(c fiber.Ctx) error {
	userID, taskID, err := h.requestIdentity(c)
	if err != nil {
		return err
	}
	var body struct {
		ArticleURL         string `json:"article_url"`
		ConfirmedPublished bool   `json:"confirmed_published"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if !body.ConfirmedPublished {
		return Error(c, fiber.StatusBadRequest, "confirmed_published must be true")
	}
	manualBinder, ok := h.service.(interface {
		BindManualPublication(context.Context, string, string, string) (*model.WechatPublication, error)
	})
	if !ok {
		return Error(c, fiber.StatusServiceUnavailable, "manual publication binding unavailable")
	}
	publication, err := manualBinder.BindManualPublication(c.Context(), userID, taskID, body.ArticleURL)
	if err != nil {
		return h.handleError(c, taskID, err)
	}
	return Success(c, publication)
}

func (h *WechatPublicationHandler) handleError(c fiber.Ctx, taskID string, err error) error {
	switch {
	case errors.Is(err, service.ErrWechatPublicationNotFound):
		return Error(c, fiber.StatusNotFound, service.ErrWechatPublicationNotFound.Error())
	case errors.Is(err, service.ErrWechatPublicationForbidden):
		return Forbidden(c, "you do not have access to this task")
	case errors.Is(err, service.ErrWechatPublicationConflict):
		return Error(c, fiber.StatusConflict, service.ErrWechatPublicationConflict.Error())
	case errors.Is(err, service.ErrWechatPublicationModeConflict):
		return Error(c, fiber.StatusConflict, service.ErrWechatPublicationModeConflict.Error())
	case errors.Is(err, service.ErrWechatPublicationPending):
		return Error(c, fiber.StatusConflict, service.ErrWechatPublicationPending.Error())
	case errors.Is(err, service.ErrWechatPublicationRateLimited):
		return Error(c, fiber.StatusTooManyRequests, service.ErrWechatPublicationRateLimited.Error())
	case errors.Is(err, service.ErrWechatPublicationSchedulerUnavailable):
		return Error(c, fiber.StatusServiceUnavailable, service.ErrWechatPublicationSchedulerUnavailable.Error())
	case errors.Is(err, service.ErrWechatPublicationRecoveryUnavailable):
		return Error(c, fiber.StatusConflict, service.ErrWechatPublicationRecoveryUnavailable.Error())
	case errors.Is(err, service.ErrWechatPublicationArticleNotFound):
		return Error(c, fiber.StatusBadRequest, service.ErrWechatPublicationArticleNotFound.Error())
	case errors.Is(err, service.ErrWechatPublicationInvalidPayload):
		return Error(c, fiber.StatusBadRequest, service.ErrWechatPublicationInvalidPayload.Error())
	default:
		if h.logger != nil {
			h.logger.Error().Err(err).Str("task_id", taskID).Msg("WeChat publication action failed")
		}
		return Error(c, fiber.StatusInternalServerError, "WeChat publication action failed")
	}
}
