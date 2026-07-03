package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
)

type IlinkBindingService interface {
	CreateBindCode(ctx context.Context, userID string) (service.IlinkBindCodeResult, error)
	Unbind(ctx context.Context, userID string) error
	GetStatus(ctx context.Context, userID string) (service.IlinkStatusResult, error)
	SetDefaultProject(ctx context.Context, userID, projectID string) error
}

type IlinkHandler struct {
	service IlinkBindingService
	logger  *zerolog.Logger
}

func NewIlinkHandler(svc IlinkBindingService, logger *zerolog.Logger) *IlinkHandler {
	return &IlinkHandler{service: svc, logger: logger}
}

func (h *IlinkHandler) CreateBindCode(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "ilink is not enabled")
	}
	result, err := h.service.CreateBindCode(c.Context(), userID)
	if err != nil {
		return ilinkError(c, h, err)
	}
	return Success(c, result)
}

func (h *IlinkHandler) Unbind(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "ilink is not enabled")
	}
	if err := h.service.Unbind(c.Context(), userID); err != nil {
		return ilinkError(c, h, err)
	}
	return Success(c, fiber.Map{"unbound": true})
}

func (h *IlinkHandler) GetStatus(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Success(c, service.IlinkStatusResult{Available: false, Status: "unbound"})
	}
	result, err := h.service.GetStatus(c.Context(), userID)
	if err != nil {
		return ilinkError(c, h, err)
	}
	return Success(c, result)
}

func (h *IlinkHandler) SetDefaultProject(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "ilink is not enabled")
	}
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.service.SetDefaultProject(c.Context(), userID, req.ProjectID); err != nil {
		if errors.Is(err, service.ErrIlinkNoBinding) {
			return Error(c, fiber.StatusBadRequest, "bind ilink first")
		}
		if strings.Contains(err.Error(), "does not belong") {
			return Forbidden(c, "you do not have access to this project")
		}
		return ilinkError(c, h, err)
	}
	return Success(c, fiber.Map{"default_project_id": req.ProjectID})
}

func ilinkError(c fiber.Ctx, h *IlinkHandler, err error) error {
	switch {
	case errors.Is(err, service.ErrIlinkUnavailable):
		return Error(c, fiber.StatusServiceUnavailable, "ilink is not enabled")
	case errors.Is(err, service.ErrIlinkNoBinding):
		return Error(c, fiber.StatusNotFound, "no ilink binding")
	case errors.Is(err, service.ErrIlinkBindInvalid):
		return Error(c, fiber.StatusBadRequest, "invalid or expired bind code")
	default:
		if h.logger != nil {
			h.logger.Error().Err(err).Msg("ilink operation failed")
		}
		return Error(c, fiber.StatusInternalServerError, "ilink operation failed")
	}
}
