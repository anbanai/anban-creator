package handler

import (
	"context"
	"errors"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

type wechatOpenPlatformHandlerService interface {
	StartAuthorization(context.Context, string, string) (*service.WechatAuthorizationStart, error)
	Disconnect(context.Context, string, string) error
	CompleteAuthorization(context.Context, string, string) (string, error)
	AuthorizationReturnURL(string, string) string
	VerifyCallbackURL(string, string, string) bool
	HandleEvent(context.Context, string, string, string, string, []byte) error
}

type WechatOpenPlatformHandler struct {
	service wechatOpenPlatformHandlerService
	logger  *zerolog.Logger
}

func NewWechatOpenPlatformHandler(svc wechatOpenPlatformHandlerService, logger *zerolog.Logger) *WechatOpenPlatformHandler {
	return &WechatOpenPlatformHandler{service: svc, logger: logger}
}

func (h *WechatOpenPlatformHandler) StartAuthorization(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	started, err := h.service.StartAuthorization(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.respondProjectError(c, err)
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return Success(c, started)
}

func (h *WechatOpenPlatformHandler) Disconnect(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := h.service.Disconnect(c.Context(), userID, c.Params("id")); err != nil {
		return h.respondProjectError(c, err)
	}
	return Success(c, fiber.Map{"disconnected": true})
}

func (h *WechatOpenPlatformHandler) AuthorizationCallback(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	projectID, err := h.service.CompleteAuthorization(c.Context(), c.Query("state"), c.Query("auth_code"))
	if err != nil {
		return c.Redirect().To(h.service.AuthorizationReturnURL("", "failed"))
	}
	return c.Redirect().To(h.service.AuthorizationReturnURL(projectID, "connected"))
}

func (h *WechatOpenPlatformHandler) VerifyEvents(c fiber.Ctx) error {
	if !h.service.VerifyCallbackURL(c.Query("signature"), c.Query("timestamp"), c.Query("nonce")) {
		return Error(c, fiber.StatusForbidden, "invalid WeChat callback signature")
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.SendString(c.Query("echostr"))
}

func (h *WechatOpenPlatformHandler) ReceiveEvents(c fiber.Ctx) error {
	body := c.Body()
	if len(body) == 0 || len(body) > 1<<20 {
		return Error(c, fiber.StatusBadRequest, "invalid WeChat event body")
	}
	err := h.service.HandleEvent(
		c.Context(), c.Query("signature"), c.Query("msg_signature"), c.Query("timestamp"), c.Query("nonce"), body,
	)
	if err != nil {
		if errors.Is(err, service.ErrWechatOpenPlatformEventInvalid) {
			return Error(c, fiber.StatusForbidden, "invalid WeChat event signature or payload")
		}
		if errors.Is(err, service.ErrWechatOpenPlatformDisabled) {
			return Error(c, fiber.StatusServiceUnavailable, "WeChat Open Platform is not configured")
		}
		if h.logger != nil {
			h.logger.Error().Msg("process WeChat Open Platform event")
		}
		return Error(c, fiber.StatusInternalServerError, "WeChat event processing failed")
	}
	c.Set(fiber.HeaderContentType, "text/plain; charset=utf-8")
	return c.SendString("success")
}

func (h *WechatOpenPlatformHandler) respondProjectError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, service.ErrWechatOpenPlatformForbidden), errors.Is(err, service.ErrProjectOwnedByUser):
		return Forbidden(c, "you do not have access to this project")
	case errors.Is(err, service.ErrProjectNotFound):
		return Error(c, fiber.StatusNotFound, "project not found")
	case errors.Is(err, service.ErrWechatOpenPlatformDisabled):
		return Error(c, fiber.StatusServiceUnavailable, "WeChat Open Platform is not configured")
	case errors.Is(err, service.ErrWechatOpenPlatformProjectUnsupported):
		return Error(c, fiber.StatusBadRequest, "this project does not support WeChat authorization")
	case errors.Is(err, service.ErrWechatOpenPlatformStateInvalid):
		return Error(c, fiber.StatusBadRequest, "WeChat authorization state expired; start again")
	default:
		if h.logger != nil {
			h.logger.Error().Msg("WeChat Open Platform project operation failed")
		}
		return Error(c, fiber.StatusInternalServerError, "WeChat authorization operation failed")
	}
}
