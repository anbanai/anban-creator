package handler

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

func finalizeUploadSessionURLs(ctx context.Context, store storage.Provider, repo repository.Repository, userID, purpose string, urls []string) (map[string]string, error) {
	if repo == nil || len(urls) == 0 {
		return map[string]string{}, nil
	}
	finalStore, _ := store.(service.DirectUploadFinalizationStorage)
	var ownedURLChecks []func(string) bool
	if store != nil {
		ownedURLChecks = append(ownedURLChecks, store.IsOwnedURL)
	}
	return service.FinalizeUploadSessionURLs(ctx, finalStore, repo, userID, purpose, urls, time.Now(), ownedURLChecks...)
}

func rewriteFinalizedUploadURL(value string, rewrites map[string]string) string {
	if final := rewrites[value]; final != "" {
		return final
	}
	return value
}

func rewriteFinalizedUploadURLSlice(values []string, rewrites map[string]string) {
	for i := range values {
		values[i] = rewriteFinalizedUploadURL(values[i], rewrites)
	}
}

func respondUploadSessionFinalizeError(c fiber.Ctx, logger *zerolog.Logger, err error) error {
	switch {
	case errors.Is(err, service.ErrUploadSessionAccessDenied):
		return Error(c, fiber.StatusBadRequest, "pending upload access denied")
	case errors.Is(err, service.ErrUploadSessionExpired):
		return Error(c, fiber.StatusBadRequest, "pending upload has expired")
	case errors.Is(err, service.ErrUploadSessionStateConflict):
		return Error(c, fiber.StatusBadRequest, "pending upload is not pending")
	case errors.Is(err, service.ErrUploadSessionObjectInvalid):
		return Error(c, fiber.StatusBadRequest, "pending upload object is invalid")
	case errors.Is(err, service.ErrUploadSessionInvalidURL):
		return Error(c, fiber.StatusBadRequest, "pending upload URL is invalid")
	default:
		if logger != nil {
			logger.Error().Err(err).Msg("pending upload finalization failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to finalize pending upload")
	}
}
