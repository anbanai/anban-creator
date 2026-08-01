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
	removedReferenceImageField      = "reference_image_url"
	errRemovedReferenceImageField   = fmt.Errorf("%w: %s is no longer supported; use reference_image", errReferenceAssetRequestInvalid, removedReferenceImageField)
	removedImageModelField          = "image_model_key"
	errRemovedImageModelField       = fmt.Errorf("%w: %s is no longer supported; use image_capability_key", errReferenceAssetRequestInvalid, removedImageModelField)
)

func rejectRemovedRequestFields(body []byte) error {
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
		if strings.EqualFold(key, removedReferenceImageField) {
			return errRemovedReferenceImageField
		}
	}
	if containsJSONField(value, removedImageModelField) {
		return errRemovedImageModelField
	}
	return nil
}

func containsJSONField(value json.RawMessage, field string) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err == nil && object != nil {
		for key, child := range object {
			if strings.EqualFold(key, field) || containsJSONField(child, field) {
				return true
			}
		}
		return false
	}
	var array []json.RawMessage
	if err := json.Unmarshal(value, &array); err == nil {
		for _, child := range array {
			if containsJSONField(child, field) {
				return true
			}
		}
	}
	return false
}

func respondReferenceAssetError(c fiber.Ctx, logger *zerolog.Logger, err error) error {
	switch {
	case errors.Is(err, errRemovedReferenceImageField):
		return Error(c, fiber.StatusBadRequest, errRemovedReferenceImageField.Error())
	case errors.Is(err, errRemovedImageModelField):
		return Error(c, fiber.StatusBadRequest, errRemovedImageModelField.Error())
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
	return service.IsReferenceAssetError(err)
}
