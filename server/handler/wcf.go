package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/service"
)

// WCFBindingService is the handler-side interface for the WeChat-binding
// service, extracted so the handler can be unit-tested with a fake.
type WCFBindingService interface {
	StartBind(ctx context.Context, userID string) (service.StartBindResult, error)
	PollBindStatus(ctx context.Context, userID, sessionID string) (service.BindStatusResult, error)
	Unbind(ctx context.Context, userID string) error
	GetStatus(ctx context.Context, userID string) (service.BindStatusResult, error)
	SetDefaultProject(ctx context.Context, userID, projectID string) error
}

// WCFHandler exposes the WeChat-binding lifecycle over HTTP. Mirrors the
// SeednoteAnalyticsHandler shape (interface dep, NewXHandler, GetUserID +
// Success/Error/Forbidden helpers).
type WCFHandler struct {
	service WCFBindingService
	logger  *zerolog.Logger
}

// NewWCFHandler constructs the handler. svc may be nil when wcf is disabled;
// every method then reports the binding as unavailable rather than 404'ing, so
// Studio can render a coherent "微信通知未启用" state.
func NewWCFHandler(svc WCFBindingService, logger *zerolog.Logger) *WCFHandler {
	return &WCFHandler{service: svc, logger: logger}
}

// StartBind handles POST /api/v1/wechat/bind/start.
// Body: none. Returns the QR PNG as a data URI plus the login session id.
func (h *WCFHandler) StartBind(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "wechat bot is not enabled")
	}

	result, err := h.service.StartBind(c.Context(), userID)
	if err != nil {
		return wcfBindError(c, h, err)
	}
	return Success(c, result)
}

// PollBindStatus handles GET /api/v1/wechat/bind/status?session_id=.
// Studio polls this every ~2s until status flips to "active".
func (h *WCFHandler) PollBindStatus(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "wechat bot is not enabled")
	}
	sessionID := strings.TrimSpace(c.Query("session_id"))

	result, err := h.service.PollBindStatus(c.Context(), userID, sessionID)
	if err != nil {
		// Missing/expired session is a client error, not a 500.
		if errors.Is(err, service.ErrLoginNotPending) || errors.Is(err, service.ErrWCFUnavailable) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		return wcfBindError(c, h, err)
	}
	return Success(c, result)
}

// Unbind handles POST /api/v1/wechat/unbind.
func (h *WCFHandler) Unbind(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "wechat bot is not enabled")
	}

	if err := h.service.Unbind(c.Context(), userID); err != nil {
		if errors.Is(err, service.ErrNoBinding) {
			return Error(c, fiber.StatusNotFound, "no wechat binding")
		}
		return wcfBindError(c, h, err)
	}
	return Success(c, fiber.Map{"unbound": true})
}

// GetStatus handles GET /api/v1/wechat/status.
func (h *WCFHandler) GetStatus(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		// Still respond so Studio can show the disabled state cleanly.
		return Success(c, service.BindStatusResult{Available: false, Status: "unbound"})
	}

	result, err := h.service.GetStatus(c.Context(), userID)
	if err != nil {
		return wcfBindError(c, h, err)
	}
	return Success(c, result)
}

// SetDefaultProject handles PUT /api/v1/wechat/default-project.
// Body: {"project_id": "..."}.
func (h *WCFHandler) SetDefaultProject(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "wechat bot is not enabled")
	}

	var req struct {
		ProjectID string `json:"project_id"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if err := h.service.SetDefaultProject(c.Context(), userID, req.ProjectID); err != nil {
		if errors.Is(err, service.ErrNoBinding) {
			return Error(c, fiber.StatusBadRequest, "bind wechat first")
		}
		if strings.Contains(err.Error(), "does not belong") {
			return Forbidden(c, "you do not have access to this project")
		}
		return wcfBindError(c, h, err)
	}
	return Success(c, fiber.Map{"default_project_id": req.ProjectID})
}

// wcfBindError maps binding-service sentinel errors to HTTP statuses and logs
// unexpected errors. Centralized so each handler method stays readable.
func wcfBindError(c fiber.Ctx, h *WCFHandler, err error) error {
	switch {
	case errors.Is(err, service.ErrWCFUnavailable):
		return Error(c, fiber.StatusServiceUnavailable, "wechat bot is not enabled")
	case errors.Is(err, service.ErrAlreadyBound):
		return Error(c, fiber.StatusConflict, "wechat account already bound")
	case errors.Is(err, service.ErrAccountConflict):
		return Error(c, fiber.StatusConflict, "wechat account already bound by another user")
	case errors.Is(err, service.ErrNoBinding):
		return Error(c, fiber.StatusNotFound, "no wechat binding")
	default:
		if h.logger != nil {
			h.logger.Error().Err(err).Msg("wechat binding operation failed")
		}
		return Error(c, fiber.StatusInternalServerError, "wechat binding operation failed")
	}
}
