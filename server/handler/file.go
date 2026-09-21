package handler

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

// FileHandler handles authenticated file read endpoints.
type FileHandler struct {
	store          storage.Provider
	uploadSessions repository.UploadSessionRepository
	logger         *zerolog.Logger
}

// NewFileHandler creates a new FileHandler.
func NewFileHandler(store storage.Provider, logger *zerolog.Logger) *FileHandler {
	return &FileHandler{store: store, logger: logger}
}

// SetUploadSessionRepository allows /files/* to preview freshly direct-uploaded
// staging objects after validating the matching upload session.
func (h *FileHandler) SetUploadSessionRepository(repo repository.UploadSessionRepository) {
	h.uploadSessions = repo
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
	".zip":  "application/zip",
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
	} else if !isUserOwnedStorageKey(userID, cleanKey) {
		return Forbidden(c, "you do not have access to this file")
	}

	var body io.ReadCloser
	var err error
	if streamer, ok := h.store.(storage.ObjectStreamProvider); ok {
		body, err = streamer.OpenObject(c.Context(), cleanKey)
	} else {
		// Legacy providers may serve small previews through an explicitly bounded
		// reader; never fall back to an unbounded whole-object read.
		var data []byte
		data, err = storage.ReadObject(c.Context(), h.store, cleanKey, 8<<20)
		if err == nil {
			body = io.NopCloser(bytes.NewReader(data))
		}
	}
	if err != nil {
		h.logger.Error().Err(err).Str("key", cleanKey).Msg("failed to read file from storage")
		return Error(c, fiber.StatusNotFound, "file not found")
	}

	ext := strings.ToLower(filepath.Ext(cleanKey))
	c.Set("X-Content-Type-Options", "nosniff")
	if ct, ok := contentTypes[ext]; ok {
		c.Set("Content-Type", ct)
		if ext == ".svg" {
			c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		}
	}

	// Fiber owns and closes the stream after writing the response.
	return c.SendStream(body)
}

func (h *FileHandler) validatePendingFileAccess(c fiber.Ctx, userID, cleanKey string) error {
	key, err := service.ValidateUploadSessionURL(c.Context(), h.uploadSessions, userID, []string{
		service.DirectUploadPurposeProjectReference,
		service.DirectUploadPurposeProjectPortraitReference,
		service.DirectUploadPurposeTaskReference,
		service.DirectUploadPurposeEcommercePhoto,
		service.DirectUploadPurposeMontageAsset,
		service.DirectUploadPurposeHypitAsset,
		service.DirectUploadPurposeAIEntryAttachment,
	}, cleanKey, time.Now())
	if err != nil {
		if errors.Is(err, service.ErrUploadSessionInvalidURL) {
			return Error(c, fiber.StatusBadRequest, "invalid pending upload URL")
		}
		if errors.Is(err, service.ErrUploadSessionExpired) || errors.Is(err, service.ErrUploadSessionStateConflict) || errors.Is(err, service.ErrUploadSessionAccessDenied) {
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
