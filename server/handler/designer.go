package handler

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/rs/zerolog"
)

var validImageMIMETypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/jpg":  true,
	"image/gif":  true,
	"image/webp": true,
	"image/bmp":  true,
}

func isValidImageMIME(ct string) bool {
	ct = strings.ToLower(strings.SplitN(ct, ";", 2)[0])
	return validImageMIMETypes[ct]
}

type DesignerHandler struct {
	svc    *service.DesignerService
	logger *zerolog.Logger
}

func NewDesignerHandler(svc *service.DesignerService, logger *zerolog.Logger) *DesignerHandler {
	return &DesignerHandler{svc: svc, logger: logger}
}

// GetProviders handles GET /api/v1/designer/providers
func (h *DesignerHandler) GetProviders(c fiber.Ctx) error {
	providers := h.svc.GetProviders()
	return c.JSON(fiber.Map{"data": providers})
}

// Generate handles POST /api/v1/designer/generate
func (h *DesignerHandler) Generate(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req service.DesignerGenerateRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	if req.Prompt == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "prompt is required"})
	}

	genID, err := h.svc.CreateGenerationRecord(c.Context(), userID, req)
	if err != nil {
		if errors.Is(err, service.ErrInsufficientCredits) {
			return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{"error": "积分不足，请充值后重试"})
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("designer create generation record failed")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	go h.svc.ExecuteGeneration(context.Background(), genID)

	return c.JSON(fiber.Map{
		"data": fiber.Map{
			"generation_id": genID,
			"status":        "generating",
		},
	})
}

// UploadReference handles POST /api/v1/designer/upload-reference
func (h *DesignerHandler) UploadReference(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "file is required"})
	}

	// Validate file size (max 10MB)
	if file.Size > 10*1024*1024 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "file too large (max 10MB)"})
	}

	// Validate file type
	contentType := file.Header.Get("Content-Type")
	if contentType != "" && !isValidImageMIME(contentType) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "only image files are allowed"})
	}

	f, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to open file"})
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to read file"})
	}

	fileID, err := h.svc.UploadReference(c.Context(), userID, file.Filename, data)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"data": fiber.Map{
			"file_id":  fileID,
			"filename": file.Filename,
			"size":     len(data),
		},
	})
}

// GetHistory handles GET /api/v1/designer/history
func (h *DesignerHandler) GetHistory(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	channelID := c.Query("channel_id", "")
	page, _ := strconv.Atoi(c.Query("page", "1"))
	pageSize, _ := strconv.Atoi(c.Query("page_size", "20"))

	generations, total, err := h.svc.GetHistory(c.Context(), userID, channelID, page, pageSize)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"data": fiber.Map{
			"items": generations,
			"total": total,
			"page":  page,
			"page_size": pageSize,
		},
	})
}

// GetGeneration handles GET /api/v1/designer/generations/:id
func (h *DesignerHandler) GetGeneration(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	genID := c.Params("id")
	if genID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "generation id is required"})
	}

	gen, err := h.svc.GetGeneration(c.Context(), userID, genID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "generation not found"})
	}

	return c.JSON(fiber.Map{"data": gen})
}
