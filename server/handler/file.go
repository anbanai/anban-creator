package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/storage"
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

var allowedVideoReferenceExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
	".gif":  true,
	".mp3":  true,
	".wav":  true,
	".m4a":  true,
	".aac":  true,
	".ogg":  true,
	".mp4":  true,
	".mov":  true,
	".webm": true,
}

const maxUploadSize = 10 << 20               // 10 MB
const maxVideoReferenceUploadSize = 50 << 20 // 50 MB

var execVideoReferenceDurationProbe = func(ctx context.Context, path string) ([]byte, error) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path).Output()
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

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	purpose := c.FormValue("purpose")
	maxSize := int64(maxUploadSize)
	if purpose == "video_reference" {
		maxSize = maxVideoReferenceUploadSize
	}
	if fileHeader.Size > maxSize {
		return Error(c, fiber.StatusBadRequest, fmt.Sprintf("file size exceeds the %d MB limit", maxSize/(1<<20)))
	}
	if purpose == "video_reference" {
		if !allowedVideoReferenceExtensions[ext] {
			return Error(c, fiber.StatusBadRequest, "only image, audio, or video reference files are allowed")
		}
	} else if !allowedImageExtensions[ext] {
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
	if purpose == "video_reference" && mimeType == "application/octet-stream" {
		if ct, ok := contentTypes[ext]; ok {
			mimeType = strings.Split(ct, ";")[0]
		}
	}
	if purpose == "video_reference" {
		if !strings.HasPrefix(mimeType, "image/") && !strings.HasPrefix(mimeType, "audio/") && !strings.HasPrefix(mimeType, "video/") {
			return Error(c, fiber.StatusBadRequest, "uploaded file is not an image, audio, or video reference")
		}
	} else if !strings.HasPrefix(mimeType, "image/") {
		return Error(c, fiber.StatusBadRequest, "uploaded file is not an image")
	}

	var inputDurationSeconds float64
	if purpose == "video_reference" && strings.HasPrefix(mimeType, "video/") {
		if _, err := src.Seek(0, 0); err != nil {
			return Error(c, fiber.StatusBadRequest, "failed to read uploaded file")
		}
		duration, err := probeUploadedVideoDuration(c.Context(), src, ext)
		if err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		inputDurationSeconds = duration
	}

	// Rewind reader for upload.
	if _, err := src.Seek(0, 0); err != nil {
		return Error(c, fiber.StatusBadRequest, "failed to read uploaded file")
	}

	prefix := "uploads/projects"
	switch purpose {
	case "reference":
		prefix = "uploads/references"
	case "video_reference":
		prefix = "uploads/video-references"
	case "project", "":
	default:
		return Error(c, fiber.StatusBadRequest, "invalid purpose value")
	}

	key := fmt.Sprintf("%s/%s/%s%s", prefix, userID, uuid.New().String(), ext)

	result, err := h.store.Upload(c.Context(), key, src, mimeType)
	if err != nil {
		h.logger.Error().Err(err).Str("key", key).Msg("file upload failed")
		return Error(c, fiber.StatusInternalServerError, "failed to store file")
	}

	resp := fiber.Map{
		"url":  result.URL,
		"key":  result.Key,
		"size": result.Size,
		"type": result.MimeType,
	}
	if inputDurationSeconds > 0 {
		resp["input_duration_seconds"] = inputDurationSeconds
	}
	return Success(c, resp)
}

func probeUploadedVideoDuration(ctx context.Context, reader io.Reader, ext string) (float64, error) {
	tmp, err := os.CreateTemp("", "anban-upload-video-reference-*"+ext)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare video duration probe")
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, reader); err != nil {
		tmp.Close()
		return 0, fmt.Errorf("failed to read video reference for duration probe")
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("failed to finalize video duration probe")
	}
	out, err := execVideoReferenceDurationProbe(ctx, tmpPath)
	if err != nil {
		return 0, fmt.Errorf("failed to measure video reference duration")
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid video reference duration")
	}
	return duration, nil
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

	// Verify ownership: allow user's uploads AND user's designer-generated images
	// (OSS object keys for designer results are shaped "{userID}/designer/{genID}/{index}{ext}").
	if !isUserOwnedStorageKey(userID, cleanKey, userID+"/designer/") {
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
