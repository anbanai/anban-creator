package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

var (
	errReferenceAssetRequestInvalid = errors.New("reference image request is invalid")
	errLegacyReferenceImageURL      = fmt.Errorf("%w: reference_image_url is no longer supported; use reference_image", errReferenceAssetRequestInvalid)
)

func rejectLegacyReferenceImageURL(body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return fmt.Errorf("%w: request body must be a JSON object", errReferenceAssetRequestInvalid)
	}
	var value json.RawMessage
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return fmt.Errorf("%w: decode request body: %w", errReferenceAssetRequestInvalid, err)
	}
	if len(value) == 0 || value[0] != '{' {
		return fmt.Errorf("%w: request body must be a non-null JSON object", errReferenceAssetRequestInvalid)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(value, &raw); err != nil {
		return fmt.Errorf("%w: decode request object: %w", errReferenceAssetRequestInvalid, err)
	}
	for key := range raw {
		if strings.EqualFold(key, "reference_image_url") {
			return errLegacyReferenceImageURL
		}
	}
	return nil
}

func respondReferenceAssetError(c fiber.Ctx, logger *zerolog.Logger, err error) error {
	switch {
	case errors.Is(err, errLegacyReferenceImageURL):
		return Error(c, fiber.StatusBadRequest, errLegacyReferenceImageURL.Error())
	case errors.Is(err, errReferenceAssetRequestInvalid):
		return Error(c, fiber.StatusBadRequest, errReferenceAssetRequestInvalid.Error())
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

func isReferenceAssetError(err error) bool {
	return errors.Is(err, service.ErrReferenceImageSelectionInvalid) ||
		errors.Is(err, service.ErrReferenceAssetPurposeMismatch) ||
		errors.Is(err, service.ErrReferenceAssetInvalidMetadata) ||
		errors.Is(err, service.ErrReferenceAssetForbidden) ||
		errors.Is(err, service.ErrReferenceAssetConcurrentFinalization) ||
		errors.Is(err, service.ErrReferenceAssetExpired) ||
		errors.Is(err, service.ErrReferenceAssetUnavailable)
}
