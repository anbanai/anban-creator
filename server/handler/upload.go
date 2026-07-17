package handler

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type UploadHandler struct {
	store     storage.Provider
	repo      repository.Repository
	ownerRepo repository.Repository
	cfg       service.DirectUploadConfig
	logger    *zerolog.Logger
}

// SetRepository wires task and plan ownership lookup for resolving stable
// attachment identities after their pending-upload row is no longer available.
func (h *UploadHandler) SetRepository(repo repository.Repository) {
	h.ownerRepo = repo
}

func NewUploadHandler(store storage.Provider, repo repository.Repository, cfg service.DirectUploadConfig, logger *zerolog.Logger) *UploadHandler {
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}
	return &UploadHandler{store: store, repo: repo, cfg: cfg, logger: logger}
}

func (h *UploadHandler) Prepare(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.store == nil {
		return Error(c, fiber.StatusServiceUnavailable, "file storage is not available")
	}
	var req service.DirectUploadPrepareRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.UserID = userID
	var sessions repository.UploadSessionRepository
	if h.repo != nil {
		sessions = h.repo.UploadSessions()
	}
	result, err := service.PrepareDirectUpload(c.Context(), h.store, sessions, h.cfg, req)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "storage provider") || strings.Contains(msg, "credential") || strings.Contains(msg, "OSS storage") {
			h.logger.Warn().Err(err).Str("user_id", userID).Msg("prepare direct upload unavailable")
			return Error(c, fiber.StatusServiceUnavailable, msg)
		}
		return Error(c, fiber.StatusBadRequest, msg)
	}
	return Success(c, result)
}

type resolveDownloadURLRequest struct {
	UploadID  string `json:"upload_id"`
	Key       string `json:"key"`
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
}

type resolveDownloadURLResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ResolveDownloadURL turns a stable, server-verified object identity into a
// fresh download URL without writing the temporary URL back to persisted state.
func (h *UploadHandler) ResolveDownloadURL(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.store == nil {
		return Error(c, fiber.StatusServiceUnavailable, "file storage is not available")
	}
	var req resolveDownloadURLRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.UploadID = strings.TrimSpace(req.UploadID)
	req.Key = strings.TrimSpace(req.Key)
	req.OwnerType = strings.TrimSpace(req.OwnerType)
	req.OwnerID = strings.TrimSpace(req.OwnerID)
	if req.Key == "" {
		return Error(c, fiber.StatusBadRequest, "key is required")
	}

	var status int
	if req.UploadID != "" {
		status = h.authorizeUploadSession(c.Context(), userID, req.UploadID, req.Key)
	} else {
		if req.OwnerType != "task" && req.OwnerType != "plan" {
			return Error(c, fiber.StatusBadRequest, "owner_type must be task or plan")
		}
		if req.OwnerID == "" {
			return Error(c, fiber.StatusBadRequest, "owner_id is required")
		}
		status = h.authorizeOwnerKey(c.Context(), userID, req.OwnerType, req.OwnerID, req.Key)
	}
	if status != fiber.StatusOK {
		switch status {
		case fiber.StatusNotFound:
			return Error(c, status, "attachment owner not found")
		case fiber.StatusInternalServerError:
			return Error(c, status, "failed to resolve download URL")
		default:
			return Error(c, fiber.StatusForbidden, "attachment access denied")
		}
	}

	url, err := h.store.DownloadURL(c.Context(), req.Key, service.DefaultSignedURLTTL)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("resolve attachment download URL failed")
		return Error(c, fiber.StatusInternalServerError, "failed to resolve download URL")
	}
	return Success(c, resolveDownloadURLResponse{
		URL:       url,
		ExpiresAt: time.Now().Add(time.Duration(service.DefaultSignedURLTTL) * time.Second),
	})
}

func (h *UploadHandler) authorizeUploadSession(ctx context.Context, userID, uploadID, key string) int {
	if h.repo == nil {
		return fiber.StatusInternalServerError
	}
	session, err := h.repo.UploadSessions().FindByID(ctx, uploadID)
	if errors.Is(err, model.ErrUploadSessionNotFound) {
		return fiber.StatusNotFound
	}
	if err != nil {
		h.logger.Error().Err(err).Str("upload_id", uploadID).Msg("resolve pending upload lookup failed")
		return fiber.StatusInternalServerError
	}
	if session.UserID != userID {
		return fiber.StatusForbidden
	}
	switch session.Status {
	case model.UploadSessionPending:
		if !session.ExpiresAt.After(time.Now()) || session.StagingKey != key {
			return fiber.StatusForbidden
		}
	case model.UploadSessionFinalized:
		asset, err := h.repo.Assets().FindByID(ctx, session.AssetID)
		if err != nil || asset.StorageKey != key || asset.UserID != userID {
			return fiber.StatusForbidden
		}
	default:
		return fiber.StatusForbidden
	}
	return fiber.StatusOK
}

func (h *UploadHandler) authorizeOwnerKey(ctx context.Context, userID, ownerType, ownerID, key string) int {
	if h.ownerRepo == nil {
		return fiber.StatusInternalServerError
	}
	switch ownerType {
	case "task":
		task, err := h.ownerRepo.Tasks().FindByID(ctx, ownerID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.StatusNotFound
		}
		if err != nil {
			h.logger.Error().Err(err).Str("task_id", ownerID).Msg("resolve attachment task lookup failed")
			return fiber.StatusInternalServerError
		}
		if task.UserID != userID || !taskOwnsStorageKey(task, h.store, key) {
			return fiber.StatusForbidden
		}
	case "plan":
		plan, err := h.ownerRepo.Plans().FindByID(ctx, ownerID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.StatusNotFound
		}
		if err != nil {
			h.logger.Error().Err(err).Str("plan_id", ownerID).Msg("resolve attachment plan lookup failed")
			return fiber.StatusInternalServerError
		}
		if plan.UserID != userID || !planOwnsStorageKey(plan, h.store, key) {
			return fiber.StatusForbidden
		}
	}
	return fiber.StatusOK
}

func taskOwnsStorageKey(task *model.Task, store storage.Provider, key string) bool {
	if task == nil {
		return false
	}
	if attachmentsOwnStorageKey(task.InputAttachments.Data(), store, key) || ownedURLHasKey(store, task.ReferenceImageURL, key) || ownedURLHasKey(store, task.ProjectSnapshot.Data().ReferenceImageURL, key) {
		return true
	}
	for _, rawURL := range task.Ecommerce.Data().ProductPhotos {
		if ownedURLHasKey(store, rawURL, key) {
			return true
		}
	}
	return referencesOwnStorageKey(task.VideoInput.Data(), task.VideoConfig.Data(), task.MontageInput.Data(), store, key)
}

func planOwnsStorageKey(plan *model.Plan, store storage.Provider, key string) bool {
	if plan == nil {
		return false
	}
	return attachmentsOwnStorageKey(plan.InputAttachments.Data(), store, key) ||
		ownedURLHasKey(store, plan.ReferenceImageURL, key) ||
		referencesOwnStorageKey(plan.VideoInput.Data(), plan.VideoConfig.Data(), plan.MontageInput.Data(), store, key)
}

func attachmentsOwnStorageKey(attachments []model.EntryAttachment, store storage.Provider, key string) bool {
	for _, attachment := range attachments {
		if (attachment.Key != "" && attachment.Key == key) || ownedURLHasKey(store, attachment.URL, key) {
			return true
		}
	}
	return false
}

func referencesOwnStorageKey(video model.VideoInput, config model.VideoTaskConfig, montage model.MontageInput, store storage.Provider, key string) bool {
	for _, reference := range video.References {
		if ownedURLHasKey(store, reference.URL, key) {
			return true
		}
	}
	for _, reference := range config.References {
		if ownedURLHasKey(store, reference.URL, key) {
			return true
		}
	}
	for _, asset := range montage.SourceAssets {
		if ownedURLHasKey(store, asset.URL, key) {
			return true
		}
	}
	return false
}

func ownedURLHasKey(store storage.Provider, rawURL, key string) bool {
	ownedKey, ok := ownedStorageKey(store, rawURL)
	return ok && ownedKey == key
}
