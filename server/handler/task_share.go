package handler

import (
	"encoding/json"
	"errors"
	"math"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

type TaskShareHandler struct{ service *service.TaskShareService }

func NewTaskShareHandler(svc *service.TaskShareService) *TaskShareHandler {
	return &TaskShareHandler{service: svc}
}

// TaskShareHeaders also covers responses returned by authentication middleware.
func TaskShareHeaders(c fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	c.Set("X-Content-Type-Options", "nosniff")
	return c.Next()
}

func shareHeaders(c fiber.Ctx, public bool) {
	c.Set("Cache-Control", "no-store")
	c.Set("X-Content-Type-Options", "nosniff")
	if public {
		c.Set("X-Robots-Tag", "noindex, nofollow")
		c.Set("Referrer-Policy", "no-referrer")
	}
}

func taskShareError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, service.ErrTaskShareNotFound):
		return Error(c, 404, "share not found")
	case errors.Is(err, service.ErrTaskShareGone):
		return Error(c, 410, "share expired or revoked")
	case errors.Is(err, service.ErrTaskShareInvalidExpiry):
		return Error(c, 400, err.Error())
	case errors.Is(err, service.ErrTaskShareUnavailable):
		return ErrorWithCode(c, 409, "task_share_unavailable", "shared content is unavailable")
	default:
		return Error(c, 500, "failed to read share")
	}
}

func (h *TaskShareHandler) Get(c fiber.Ctx) error {
	shareHeaders(c, false)
	if GetUserID(c) == "" {
		return Error(c, 401, "unauthorized")
	}
	result, err := h.service.Get(c.Context(), GetUserID(c), c.Params("id"))
	if err != nil {
		return taskShareError(c, err)
	}
	return Success(c, result)
}

func (h *TaskShareHandler) Put(c fiber.Ctx) error {
	shareHeaders(c, false)
	if GetUserID(c) == "" {
		return Error(c, 401, "unauthorized")
	}
	var request struct {
		Days *float64 `json:"expires_in_days"`
	}
	if err := json.Unmarshal(c.Body(), &request); err != nil || request.Days == nil || *request.Days < 1 || *request.Days > 365 || math.Trunc(*request.Days) != *request.Days {
		return taskShareError(c, service.ErrTaskShareInvalidExpiry)
	}
	result, err := h.service.Put(c.Context(), GetUserID(c), c.Params("id"), int(*request.Days))
	if err != nil {
		return taskShareError(c, err)
	}
	return Success(c, result)
}

func (h *TaskShareHandler) Revoke(c fiber.Ctx) error {
	shareHeaders(c, false)
	if GetUserID(c) == "" {
		return Error(c, 401, "unauthorized")
	}
	if err := h.service.Revoke(c.Context(), GetUserID(c), c.Params("id")); err != nil {
		return taskShareError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *TaskShareHandler) Public(c fiber.Ctx) error {
	shareHeaders(c, true)
	result, err := h.service.Public(c.Context(), c.Params("token"))
	if err != nil {
		return taskShareError(c, err)
	}
	return Success(c, result)
}

func (h *TaskShareHandler) Image(c fiber.Ctx) error {
	shareHeaders(c, true)
	result, err := h.service.Image(c.Context(), c.Params("token"), c.Params("fileId"))
	if err != nil {
		return taskShareError(c, err)
	}
	c.Set("Content-Type", result.MIMEType)
	return c.SendStream(result.Body, int(result.Size))
}
