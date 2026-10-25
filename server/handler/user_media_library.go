package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/service"
)

type UserMediaLibraryHandler struct {
	service *service.UserMediaLibraryService
	logger  *zerolog.Logger
}

func NewUserMediaLibraryHandler(svc *service.UserMediaLibraryService, logger *zerolog.Logger) *UserMediaLibraryHandler {
	return &UserMediaLibraryHandler{service: svc, logger: logger}
}

type createUserMediaRequest struct {
	UploadSessionID string `json:"upload_session_id"`
	ProjectID       string `json:"project_id,omitempty"`
	UsageNotes      string `json:"usage_notes,omitempty"`
}

// nullableString preserves the distinction between an omitted PATCH field and
// an explicit JSON null. The service uses an empty string to clear the
// nullable project tag/usage note stored by the current model.
type nullableString struct {
	value *string
}

func (s *nullableString) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		value := ""
		s.value = &value
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	s.value = &value
	return nil
}

type updateUserMediaRequest struct {
	ProjectID  nullableString `json:"project_id"`
	UsageNotes nullableString `json:"usage_notes"`
}

func (h *UserMediaLibraryHandler) Create(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req createUserMediaRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	view, err := h.service.Create(c.Context(), service.UserMediaCreateRequest{UserID: userID, UploadSessionID: req.UploadSessionID, ProjectID: req.ProjectID, UsageNotes: req.UsageNotes})
	if err != nil {
		return h.respondError(c, err)
	}
	return Success(c, view)
}

func (h *UserMediaLibraryHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	result, err := h.service.List(c.Context(), service.UserMediaListRequest{UserID: userID, Query: c.Query("q"), MediaType: c.Query("media_type"), ProjectID: c.Query("project_id"), Status: c.Query("status"), Offset: offset, Limit: limit})
	if err != nil {
		return h.respondError(c, err)
	}
	return Success(c, result)
}

func (h *UserMediaLibraryHandler) Get(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	view, err := h.service.Get(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.respondError(c, err)
	}
	return Success(c, view)
}

func (h *UserMediaLibraryHandler) Update(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req updateUserMediaRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	view, err := h.service.Update(c.Context(), service.UserMediaUpdateRequest{UserID: userID, ProjectID: req.ProjectID.value, UsageNotes: req.UsageNotes.value}, c.Params("id"))
	if err != nil {
		return h.respondError(c, err)
	}
	return Success(c, view)
}

func (h *UserMediaLibraryHandler) Reanalyze(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	view, err := h.service.Retry(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.respondError(c, err)
	}
	return Success(c, view)
}

func (h *UserMediaLibraryHandler) Remove(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := h.service.Remove(c.Context(), userID, c.Params("id")); err != nil {
		return h.respondError(c, err)
	}
	return Success(c, fiber.Map{"removed": true})
}

func (h *UserMediaLibraryHandler) respondError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, service.ErrUserMediaNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return Error(c, fiber.StatusNotFound, "media asset not found")
	case errors.Is(err, service.ErrUserMediaProjectDenied):
		return Error(c, fiber.StatusForbidden, "project access denied")
	case errors.Is(err, service.ErrUserMediaAlreadyRemoved):
		return Error(c, fiber.StatusConflict, "media asset has been removed")
	case errors.Is(err, service.ErrUserMediaNotRetryable):
		return Error(c, fiber.StatusConflict, "media asset is not ready for reanalysis")
	case errors.Is(err, service.ErrUploadSessionAccessDenied), errors.Is(err, service.ErrUploadSessionExpired), errors.Is(err, service.ErrUploadSessionStateConflict), errors.Is(err, service.ErrUploadSessionObjectInvalid), errors.Is(err, service.ErrUserMediaInvalidUpload):
		return Error(c, fiber.StatusBadRequest, "invalid media upload")
	case errors.Is(err, service.ErrUploadSessionUnavailable):
		return Error(c, fiber.StatusServiceUnavailable, "upload finalization is unavailable")
	default:
		if h.logger != nil {
			h.logger.Error().Err(err).Msg("user media library request failed")
		}
		return Error(c, fiber.StatusInternalServerError, "media library request failed")
	}
}
