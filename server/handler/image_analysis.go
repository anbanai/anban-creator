package handler

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
)

type ImageAnalysisHandler struct {
	service *service.ImageAnalysisService
	logger  *zerolog.Logger
}

func NewImageAnalysisHandler(svc *service.ImageAnalysisService, logger *zerolog.Logger) *ImageAnalysisHandler {
	return &ImageAnalysisHandler{service: svc, logger: logger}
}

func (h *ImageAnalysisHandler) Retry(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	job, err := h.service.Retry(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.respondError(c, err)
	}
	return Success(c, job.View())
}

func (h *ImageAnalysisHandler) Cancel(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := h.service.CancelForManualEdit(c.Context(), userID, c.Params("id")); err != nil {
		return h.respondError(c, err)
	}
	return Success(c, fiber.Map{"cancelled": true})
}

func (h *ImageAnalysisHandler) respondError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, service.ErrImageAnalysisNotFound):
		return Error(c, fiber.StatusNotFound, "image analysis not found")
	case errors.Is(err, service.ErrImageAnalysisForbidden):
		return Error(c, fiber.StatusForbidden, "forbidden")
	case errors.Is(err, service.ErrImageAnalysisNotRetryable), errors.Is(err, service.ErrImageAnalysisNotCancellable):
		return Error(c, fiber.StatusConflict, err.Error())
	default:
		h.logger.Error().Err(err).Msg("image analysis action failed")
		return Error(c, fiber.StatusInternalServerError, "image analysis action failed")
	}
}
