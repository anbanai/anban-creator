package handler

import (
	"encoding/json"
	"errors"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

var errLegacyReferenceImageURL = errors.New("reference_image_url is no longer supported; use reference_image")

func rejectLegacyReferenceImageURL(body []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	if _, exists := raw["reference_image_url"]; exists {
		return errLegacyReferenceImageURL
	}
	return nil
}

func respondReferenceAssetError(c fiber.Ctx, logger *zerolog.Logger, err error) error {
	switch {
	case errors.Is(err, errLegacyReferenceImageURL):
		return Error(c, fiber.StatusBadRequest, errLegacyReferenceImageURL.Error())
	case errors.Is(err, service.ErrReferenceImageSelectionInvalid):
		return Error(c, fiber.StatusBadRequest, "reference image selection is invalid")
	case errors.Is(err, service.ErrReferenceAssetPurposeMismatch):
		return Error(c, fiber.StatusBadRequest, "reference image purpose is not allowed")
	case errors.Is(err, service.ErrReferenceAssetInvalidMetadata):
		return Error(c, fiber.StatusBadRequest, "reference image metadata is invalid")
	case errors.Is(err, service.ErrReferenceAssetForbidden):
		return Error(c, fiber.StatusForbidden, "reference image is not accessible")
	case errors.Is(err, service.ErrReferenceAssetConcurrentFinalization):
		return Error(c, fiber.StatusConflict, "reference image upload is being finalized")
	case errors.Is(err, service.ErrReferenceAssetExpired):
		return Error(c, fiber.StatusGone, "reference image upload has expired")
	case errors.Is(err, service.ErrReferenceAssetUnavailable):
		if logger != nil {
			logger.Error().Err(err).Msg("reference asset dependency unavailable")
		}
		return Error(c, fiber.StatusServiceUnavailable, "reference image storage is temporarily unavailable")
	default:
		if logger != nil {
			logger.Error().Err(err).Msg("reference asset resolution failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to resolve reference image")
	}
}
