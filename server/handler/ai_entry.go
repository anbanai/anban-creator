package handler

import (
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type aiEntrySubmitBody struct {
	Channel            string                  `json:"channel"`
	ProjectID          string                  `json:"project_id"`
	ExecutionProfile   string                  `json:"execution_profile"`
	Text               string                  `json:"text"`
	Attachments        []model.EntryAttachment `json:"attachments,omitempty"`
	Quantity           *int                    `json:"quantity,omitempty"`
	ImageRatio         string                  `json:"image_ratio,omitempty"`
	ImageCapabilityKey string                  `json:"image_capability_key,omitempty"`
	ExecutionTarget    string                  `json:"execution_target,omitempty"`
}

type AIEntryHandler struct {
	submitter service.AIEntrySubmitter
	repo      repository.Repository
	store     storage.Provider
	logger    *zerolog.Logger
}

func NewAIEntryHandler(submitter service.AIEntrySubmitter, repo repository.Repository, store storage.Provider, logger *zerolog.Logger) *AIEntryHandler {
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}
	return &AIEntryHandler{submitter: submitter, repo: repo, store: store, logger: logger}
}

func (h *AIEntryHandler) Submit(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var body aiEntrySubmitBody
	if err := c.Bind().Body(&body); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	quantity := 1
	if body.Quantity != nil {
		if *body.Quantity < 1 || *body.Quantity > 5 {
			return Error(c, fiber.StatusBadRequest, "quantity must be between 1 and 5")
		}
		quantity = *body.Quantity
	}
	req := service.AIEntrySubmitRequest{
		UserID:             userID,
		Channel:            strings.TrimSpace(body.Channel),
		ProjectID:          strings.TrimSpace(body.ProjectID),
		ExecutionProfile:   strings.TrimSpace(body.ExecutionProfile),
		Text:               strings.TrimSpace(body.Text),
		Attachments:        body.Attachments,
		Quantity:           quantity,
		ImageRatio:         strings.TrimSpace(body.ImageRatio),
		ImageCapabilityKey: strings.TrimSpace(body.ImageCapabilityKey),
		ExecutionTarget:    body.ExecutionTarget,
	}
	if req.Channel == "" {
		req.Channel = "studio"
	}
	if req.ProjectID == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}
	if req.ExecutionProfile == "" {
		return Error(c, fiber.StatusBadRequest, "execution_profile is required")
	}
	if utf8.RuneCountInString(req.Text) > maxTaskPromptCharacters {
		return Error(c, fiber.StatusBadRequest, "text must not exceed 5120 characters")
	}
	validatedAttachments, err := validateInputAttachments(c.Context(), h.store, h.repo, userID, req.Attachments, InputAttachmentValidationOptions{
		MaxCount:             maxAgentInputAttachments,
		AllowedTypes:         allAgentAttachmentTypes,
		AllowedAssetPurposes: taskInputAttachmentAssetPurposes,
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
		if handled, response := respondAgentProfileError(c, err); handled {
			return response
		}
		if isReferenceAssetError(err) {
			return respondReferenceAssetError(c, h.logger, err)
		}
		if h.logger != nil {
			h.logger.Error().Err(err).Str("user_id", userID).Msg("ai entry submit failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to submit AI entry")
	}
	return Success(c, result)
}
