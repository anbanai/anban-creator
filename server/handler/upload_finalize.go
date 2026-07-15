package handler

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

func finalizePendingURLs(ctx context.Context, store storage.Provider, repo service.PendingUploadRepository, userID, purpose string, urls []string) error {
	if repo == nil || len(urls) == 0 {
		return nil
	}
	statStore, _ := store.(storage.ObjectStatProvider)
	return service.FinalizePendingUploadURLs(ctx, statStore, repo, userID, purpose, urls, time.Now())
}

func respondPendingUploadFinalizeError(c fiber.Ctx, logger *zerolog.Logger, err error) error {
	switch {
	case errors.Is(err, service.ErrPendingUploadAccessDenied):
		return Error(c, fiber.StatusBadRequest, "pending upload access denied")
	case errors.Is(err, service.ErrPendingUploadExpired):
		return Error(c, fiber.StatusBadRequest, "pending upload has expired")
	case errors.Is(err, service.ErrPendingUploadNotPending):
		return Error(c, fiber.StatusBadRequest, "pending upload is not pending")
	case errors.Is(err, service.ErrPendingUploadObjectInvalid):
		return Error(c, fiber.StatusBadRequest, "pending upload object is invalid")
	default:
		if logger != nil {
			logger.Error().Err(err).Msg("pending upload finalization failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to finalize pending upload")
	}
}

func videoReferenceURLs(cfg *model.VideoTaskConfig) []string {
	if cfg == nil || len(cfg.References) == 0 {
		return nil
	}
	urls := make([]string, 0, len(cfg.References))
	for _, ref := range cfg.References {
		if ref.URL != "" {
			urls = append(urls, ref.URL)
		}
	}
	return urls
}

func videoConfigForReferenceURLs(cfg *model.VideoTaskConfig, input *model.VideoInput) *model.VideoTaskConfig {
	if input == nil {
		return cfg
	}
	merged := model.VideoTaskConfig{}
	if cfg != nil {
		merged = *cfg
	}
	merged.References = append(merged.References, input.References...)
	return &merged
}
