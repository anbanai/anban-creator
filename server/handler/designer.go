package handler

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/gofiber/fiber/v3"
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

const maxDesignerReferenceBytes int64 = 10 * 1024 * 1024

func isValidImageMIME(ct string) bool {
	ct = strings.ToLower(strings.SplitN(ct, ";", 2)[0])
	return validImageMIMETypes[ct]
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

type DesignerHandler struct {
	svc    *service.DesignerService
	repo   repository.Repository
	store  storage.Provider
	logger *zerolog.Logger
}

func NewDesignerHandler(svc *service.DesignerService, logger *zerolog.Logger) *DesignerHandler {
	return &DesignerHandler{svc: svc, logger: logger}
}

func (h *DesignerHandler) SetDirectUploadDependencies(repo repository.Repository, store storage.Provider) {
	h.repo = repo
	h.store = store
}

// GetProviders handles GET /api/v1/designer/providers
func (h *DesignerHandler) GetProviders(c fiber.Ctx) error {
	providers := h.svc.GetProviders(c.Context(), GetUserID(c))
	return Success(c, providers)
}

func (h *DesignerHandler) Quote(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.DesignerGenerateRequest
	if err := c.Bind().JSON(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	quote, err := h.svc.CreateGenerationQuote(c.Context(), userID, req)
	if err != nil {
		if errors.Is(err, service.ErrDesignerCapabilityAccessDenied) {
			return Forbidden(c, "designer capability is not available for your tier")
		}
		return writeBillingServiceError(c, err)
	}
	return Success(c, quote)
}

// Generate handles POST /api/v1/designer/generate
func (h *DesignerHandler) Generate(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req service.DesignerGenerateRequest
	if err := c.Bind().JSON(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.Prompt == "" {
		return Error(c, fiber.StatusBadRequest, "prompt is required")
	}

	created, err := h.svc.CreateGenerationRecord(c.Context(), userID, req)
	if err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			return Error(c, fiber.StatusNotFound, "project not found")
		}
		if errors.Is(err, service.ErrProjectOwnedByUser) {
			return Forbidden(c, "project not owned by user")
		}
		if errors.Is(err, service.ErrDesignerReferenceInvalid) {
			return Error(c, fiber.StatusBadRequest, "designer reference is invalid or unavailable")
		}
		if errors.Is(err, service.ErrDesignerCapabilityAccessDenied) {
			return Forbidden(c, "designer capability is not available for your tier")
		}
		if h.logger != nil {
			h.logger.Error().Err(err).Str("user_id", userID).Msg("designer create generation record failed")
		}
		return writeBillingServiceError(c, err)
	}

	go h.svc.ExecuteGeneration(context.Background(), created.GenerationID)

	h.logger.Info().
		Str("user_id", userID).
		Str("gen_id", created.GenerationID).
		Str("prompt_preview", truncate(req.Prompt, 80)).
		Str("provider", req.Provider).
		Str("model", req.Model).
		Str("size", req.Size).
		Int("n", req.N).
		Msg("designer: generation request accepted")

	return Success(c, created)
}

// UploadReference handles POST /api/v1/designer/upload-reference
func (h *DesignerHandler) UploadReference(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	file, err := c.FormFile("file")
	if err != nil {
		return Error(c, fiber.StatusBadRequest, "file is required")
	}

	// Validate file size (max 10MB)
	if file.Size > 10*1024*1024 {
		return Error(c, fiber.StatusBadRequest, "file too large (max 10MB)")
	}

	// Validate file type
	contentType := file.Header.Get("Content-Type")
	if contentType != "" && !isValidImageMIME(contentType) {
		return Error(c, fiber.StatusBadRequest, "only image files are allowed")
	}

	f, err := file.Open()
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to open file")
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to read file")
	}

	fileID, err := h.svc.UploadReference(c.Context(), userID, file.Filename, contentType, data)
	if err != nil {
		if h.logger != nil {
			h.logger.Error().Err(err).Str("user_id", userID).Msg("designer upload reference failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to upload reference")
	}

	return Success(c, fiber.Map{
		"file_id":  fileID,
		"filename": file.Filename,
		"size":     len(data),
	})
}

type registerDesignerReferenceRequest struct {
	UploadID string `json:"upload_id"`
	Key      string `json:"key"`
}

// RegisterReference handles POST /api/v1/designer/register-reference.
func (h *DesignerHandler) RegisterReference(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req registerDesignerReferenceRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.UploadID = strings.TrimSpace(req.UploadID)
	req.Key = strings.TrimSpace(req.Key)
	if req.UploadID == "" || req.Key == "" {
		return Error(c, fiber.StatusBadRequest, "upload_id and key are required")
	}

	finalStore, _ := h.store.(service.DirectUploadFinalizationStorage)
	verified, err := service.ResolveDirectUploadSessionAttachment(c.Context(), finalStore, h.repo, userID, []string{
		service.DirectUploadPurposeDesignerReference,
	}, req.UploadID, req.Key, time.Now())
	if err != nil {
		return h.respondRegisterReferenceError(c, err)
	}
	if _, err := storage.ReadObject(c.Context(), h.store, verified.Key, maxDesignerReferenceBytes); err != nil {
		return h.respondRegisterReferenceError(c, err)
	}
	fileID, err := h.svc.RegisterStoredReference(c.Context(), userID, verified.Key, verified.FileName, verified.ContentType, verified.Size)
	if err != nil {
		return h.respondRegisterReferenceError(c, err)
	}
	return Success(c, fiber.Map{
		"file_id":  fileID,
		"filename": verified.FileName,
		"size":     verified.Size,
	})
}

func (h *DesignerHandler) respondRegisterReferenceError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, service.ErrUploadSessionAccessDenied):
		return Forbidden(c, "pending upload access denied")
	case errors.Is(err, service.ErrUploadSessionExpired):
		return Error(c, fiber.StatusBadRequest, "pending upload has expired")
	case errors.Is(err, service.ErrUploadSessionStateConflict):
		return Error(c, fiber.StatusBadRequest, "pending upload is not reusable")
	case errors.Is(err, service.ErrUploadSessionObjectInvalid):
		return Error(c, fiber.StatusBadRequest, "pending upload object is invalid")
	case errors.Is(err, storage.ErrObjectExceedsMaxSize):
		return Error(c, fiber.StatusBadRequest, "file too large (max 10MB)")
	default:
		if h.logger != nil {
			h.logger.Error().Err(err).Msg("designer register reference failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to register reference")
	}
}

// UploadReferenceFromURL handles POST /api/v1/designer/upload-reference-from-url
//
// Downloads an image from a storage URL owned by this backend and registers it
// as a reference file. Used by the designer edit flow to avoid CORS errors when
// the client would otherwise need to fetch() a signed OSS URL in order to
// re-upload it.
func (h *DesignerHandler) UploadReferenceFromURL(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req struct {
		URL string `json:"url"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.URL == "" {
		return Error(c, fiber.StatusBadRequest, "url is required")
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") && !strings.HasPrefix(req.URL, "/api/v1/files/") {
		return Error(c, fiber.StatusBadRequest, "invalid url")
	}

	fileID, err := h.svc.UploadReferenceFromURL(c.Context(), userID, req.URL)
	if err != nil {
		if errors.Is(err, service.ErrURLNotOwned) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if h.logger != nil {
			h.logger.Error().Err(err).Str("user_id", userID).Msg("designer upload reference from url failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to upload reference")
	}

	return Success(c, fiber.Map{
		"file_id":  fileID,
		"filename": "source",
		"size":     0,
	})
}

// GetHistory handles GET /api/v1/designer/history
func (h *DesignerHandler) GetHistory(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	projectID := c.Query("project_id", "")
	page, _ := strconv.Atoi(c.Query("page", "1"))
	pageSize, _ := strconv.Atoi(c.Query("page_size", "20"))

	generations, total, err := h.svc.GetHistory(c.Context(), userID, projectID, page, pageSize)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return Success(c, fiber.Map{
		"items":     generations,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetGeneration handles GET /api/v1/designer/generations/:id
func (h *DesignerHandler) GetGeneration(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	genID := c.Params("id")
	if genID == "" {
		return Error(c, fiber.StatusBadRequest, "generation id is required")
	}

	gen, err := h.svc.GetGeneration(c.Context(), userID, genID)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "generation not found")
	}

	return Success(c, gen)
}
