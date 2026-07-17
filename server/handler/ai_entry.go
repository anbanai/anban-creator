package handler

import (
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type AIEntryHandler struct {
	submitter service.AIEntrySubmitter
	pending   service.PendingUploadRepository
	store     storage.Provider
	logger    *zerolog.Logger
}

func NewAIEntryHandler(submitter service.AIEntrySubmitter, pending service.PendingUploadRepository, store storage.Provider, logger *zerolog.Logger) *AIEntryHandler {
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}
	return &AIEntryHandler{submitter: submitter, pending: pending, store: store, logger: logger}
}

func (h *AIEntryHandler) Submit(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.AIEntrySubmitRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.UserID = userID
	req.Channel = strings.TrimSpace(req.Channel)
	if req.Channel == "" {
		req.Channel = "studio"
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.Text = strings.TrimSpace(req.Text)
	if req.ProjectID == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}
	if utf8.RuneCountInString(req.Text) > maxTaskPromptCharacters {
		return Error(c, fiber.StatusBadRequest, "text must not exceed 5120 characters")
	}
	validatedAttachments, err := validateInputAttachments(c.Context(), h.store, h.pending, userID, req.Attachments, InputAttachmentValidationOptions{
		MaxCount:     maxAgentInputAttachments,
		AllowedTypes: allAgentAttachmentTypes,
	})
	if err != nil {
		return respondInputAttachmentError(c, h.logger, err)
	}
	req.Attachments = validatedAttachments
	if h.submitter == nil {
		return Error(c, fiber.StatusServiceUnavailable, "AI entry service is not available")
	}
	result, err := h.submitter.Submit(c.Context(), req)
	if err != nil {
		if h.logger != nil {
			h.logger.Error().Err(err).Str("user_id", userID).Msg("ai entry submit failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to submit AI entry")
	}
	return Success(c, result)
}
