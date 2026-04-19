package handler

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/storage"
)

// FileHandler handles file upload endpoints.
type FileHandler struct {
	store  storage.Provider
	logger *zerolog.Logger
}

// NewFileHandler creates a new FileHandler.
func NewFileHandler(store storage.Provider, logger *zerolog.Logger) *FileHandler {
	return &FileHandler{store: store, logger: logger}
}

// allowedImageExtensions defines acceptable image file extensions.
var allowedImageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
	".gif":  true,
}

const maxUploadSize = 10 << 20 // 10 MB

// Upload handles POST /api/v1/files/upload.
func (h *FileHandler) Upload(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	if h.store == nil {
		return Error(c, fiber.StatusServiceUnavailable, "file storage is not available")
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return Error(c, fiber.StatusBadRequest, "file is required")
	}

	if fileHeader.Size > maxUploadSize {
		return Error(c, fiber.StatusBadRequest, fmt.Sprintf("file size exceeds the %d MB limit", maxUploadSize/(1<<20)))
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if !allowedImageExtensions[ext] {
		return Error(c, fiber.StatusBadRequest, "only image files (JPG, PNG, WebP, GIF) are allowed")
	}

	src, err := fileHeader.Open()
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to open uploaded file")
		return Error(c, fiber.StatusBadRequest, "failed to read uploaded file")
	}
	defer src.Close()

	// Detect actual content type from file content (first 512 bytes), not client header.
	sniffBuf := make([]byte, 512)
	n, err := io.ReadFull(src, sniffBuf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		h.logger.Error().Err(err).Msg("failed to read file for content type detection")
		return Error(c, fiber.StatusBadRequest, "failed to read uploaded file")
	}
	mimeType := http.DetectContentType(sniffBuf[:n])
	if !strings.HasPrefix(mimeType, "image/") {
		return Error(c, fiber.StatusBadRequest, "uploaded file is not an image")
	}

	// Rewind reader for upload.
	src.Seek(0, 0)

	key := fmt.Sprintf("uploads/channels/%s/%s%s", userID, uuid.New().String(), ext)

	result, err := h.store.Upload(c.Context(), key, src, mimeType)
	if err != nil {
		h.logger.Error().Err(err).Str("key", key).Msg("file upload failed")
		return Error(c, fiber.StatusInternalServerError, "failed to store file")
	}

	return Success(c, fiber.Map{
		"url":  result.URL,
		"key":  result.Key,
		"size": result.Size,
		"type": result.MimeType,
	})
}
