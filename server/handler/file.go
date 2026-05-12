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
	".mp4":  "video/mp4",
	".pdf":  "application/pdf",
}

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
		"url":  "/api/v1/files/" + result.Key,
		"key":  result.Key,
		"size": result.Size,
		"type": result.MimeType,
	})
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

	// Verify ownership: key format is uploads/channels/{userID}/...
	if !strings.HasPrefix(cleanKey, "uploads/channels/"+userID+"/") {
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
