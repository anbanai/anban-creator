package handler

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type UploadHandler struct {
	store  storage.Provider
	repo   service.PendingUploadRepository
	cfg    service.DirectUploadConfig
	logger *zerolog.Logger
}

func NewUploadHandler(store storage.Provider, repo service.PendingUploadRepository, cfg service.DirectUploadConfig, logger *zerolog.Logger) *UploadHandler {
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}
	return &UploadHandler{store: store, repo: repo, cfg: cfg, logger: logger}
}

func (h *UploadHandler) Prepare(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.store == nil {
		return Error(c, fiber.StatusServiceUnavailable, "file storage is not available")
	}
	var req service.DirectUploadPrepareRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.UserID = userID
	result, err := service.PrepareDirectUpload(c.Context(), h.store, h.repo, h.cfg, req)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "storage provider") || strings.Contains(msg, "credential") || strings.Contains(msg, "OSS storage") {
			h.logger.Warn().Err(err).Str("user_id", userID).Msg("prepare direct upload unavailable")
			return Error(c, fiber.StatusServiceUnavailable, msg)
		}
		return Error(c, fiber.StatusBadRequest, msg)
	}
	return Success(c, result)
}
