package handler

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

// FileHandler handles authenticated file read endpoints.
type FileHandler struct {
	store          storage.Provider
	pendingUploads service.PendingUploadRepository
	logger         *zerolog.Logger
}

// NewFileHandler creates a new FileHandler.
func NewFileHandler(store storage.Provider, logger *zerolog.Logger) *FileHandler {
	return &FileHandler{store: store, logger: logger}
}

// SetPendingUploadRepository allows /files/* to preview freshly direct-uploaded
// pending objects after validating the matching pending upload record.
func (h *FileHandler) SetPendingUploadRepository(repo service.PendingUploadRepository) {
	h.pendingUploads = repo
}

// contentTypes maps common file extensions to MIME types.
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".htm":  "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "application/javascript",
	".json": "application/json",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".pdf":  "application/pdf",
	".mp4":  "video/mp4",
	".mov":  "video/quicktime",
	".webm": "video/webm",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".ogg":  "audio/ogg",
}

// ServeFile handles GET /api/v1/files/* for non-local storage providers.
// It reads the file from the storage backend, verifies ownership, and streams to the client.
func (h *FileHandler) ServeFile(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	key := c.Params("*")
	if key == "" {
		return Error(c, fiber.StatusBadRequest, "file path is required")
	}

	cleanKey := filepath.Clean(key)
	if strings.Contains(cleanKey, "..") {
		return Error(c, fiber.StatusBadRequest, "invalid file path")
	}

	if strings.HasPrefix(cleanKey, "uploads/pending/") {
		if err := h.validatePendingFileAccess(c, userID, cleanKey); err != nil {
			return err
		}
	} else if !isUserOwnedStorageKey(userID, cleanKey, userID+"/designer/") {
		return Forbidden(c, "you do not have access to this file")
	}

	data, err := h.store.Read(c.Context(), cleanKey)
	if err != nil {
		h.logger.Error().Err(err).Str("key", cleanKey).Msg("failed to read file from storage")
		return Error(c, fiber.StatusNotFound, "file not found")
	}

	ext := filepath.Ext(cleanKey)
	if ct, ok := contentTypes[strings.ToLower(ext)]; ok {
		c.Set("Content-Type", ct)
		if ext == ".svg" {
			c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
			c.Set("X-Content-Type-Options", "nosniff")
		}
	}

	return c.Send(data)
}

func (h *FileHandler) validatePendingFileAccess(c fiber.Ctx, userID, cleanKey string) error {
	key, err := service.ValidatePendingUploadURL(c.Context(), h.pendingUploads, userID, []string{
		service.DirectUploadPurposeProjectReference,
		service.DirectUploadPurposeTaskReference,
		service.DirectUploadPurposeEcommercePhoto,
		service.DirectUploadPurposeVideoReference,
		service.DirectUploadPurposeOpenMontageAsset,
		service.DirectUploadPurposeDesignerReference,
		service.DirectUploadPurposeAIEntryAttachment,
	}, cleanKey, time.Now())
	if err != nil {
		if errors.Is(err, service.ErrPendingUploadInvalidURL) {
			return Error(c, fiber.StatusBadRequest, "invalid pending upload URL")
		}
		if errors.Is(err, service.ErrPendingUploadExpired) || errors.Is(err, service.ErrPendingUploadNotPending) || errors.Is(err, service.ErrPendingUploadAccessDenied) {
			return Forbidden(c, "you do not have access to this file")
		}
		h.logger.Error().Err(err).Str("key", cleanKey).Msg("failed to validate pending upload access")
		return Error(c, fiber.StatusInternalServerError, "failed to validate file access")
	}
	if key != cleanKey {
		return Forbidden(c, "you do not have access to this file")
	}
	return nil
}
