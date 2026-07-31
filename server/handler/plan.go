package handler

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

// PlanHandler handles plan-related HTTP endpoints.
type PlanHandler struct {
	service                 *service.PlanService
	logger                  *zerolog.Logger
	imagePresets            []config.ImageModelPreset
	repo                    repository.Repository
	store                   storage.Provider
	referenceAssets         *service.ReferenceAssetService
	scheduleRecommendations *service.ScheduleRecommendationService
}

func (h *PlanHandler) SetReferenceAssetService(referenceAssets *service.ReferenceAssetService) {
	h.referenceAssets = referenceAssets
}

func (h *PlanHandler) SetScheduleRecommendationService(recommendations *service.ScheduleRecommendationService) {
	h.scheduleRecommendations = recommendations
}

func (h *PlanHandler) ScheduleRecommendation(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.scheduleRecommendations == nil {
		return Error(c, fiber.StatusServiceUnavailable, "schedule recommendation unavailable")
	}
	return Success(c, h.scheduleRecommendations.Recommend(c.Context(), userID, time.Now()))
}

func (h *PlanHandler) presentPlanReference(ctx context.Context, userID string, plan *model.Plan) error {
	if plan == nil || plan.ReferenceImageAssetID == "" {
		if plan != nil {
			plan.ReferenceImage = nil
		}
		return nil
	}
	view, err := h.planReferenceView(ctx, userID, plan.ReferenceImageAssetID)
	if err != nil {
		return err
	}
	plan.ReferenceImage = view
	return nil
}

func (h *PlanHandler) planReferenceView(ctx context.Context, userID, assetID string) (*model.AssetView, error) {
	if assetID == "" {
		return nil, nil
	}
	if h.referenceAssets == nil {
		return nil, service.ErrReferenceAssetUnavailable
	}
	return h.referenceAssets.Present(ctx, userID, assetID, []string{service.DirectUploadPurposeTaskReference})
}

func (h *PlanHandler) presentPlanReferences(ctx context.Context, userID string, plans []*model.Plan) error {
	for _, plan := range plans {
		if err := h.presentPlanReference(ctx, userID, plan); err != nil {
			return err
		}
	}
	return nil
}

// NewPlanHandler creates a new PlanHandler.
func NewPlanHandler(svc *service.PlanService, logger *zerolog.Logger) *PlanHandler {
	return &PlanHandler{service: svc, logger: logger}
}

// SetStore injects a storage provider for response-only owned-object key
// serialization and attachment validation.
func (h *PlanHandler) SetStore(s storage.Provider) {
	h.store = s
}

// SetImagePresets wires the system-managed image model presets for tier-gated
// validation of createPlanRequest/updatePlanRequest.ImageModelKey.
func (h *PlanHandler) SetImagePresets(presets []config.ImageModelPreset) {
	h.imagePresets = presets
}

// SetRepository wires the user repository so the handler can resolve the caller's
// tier for image-model validation.
func (h *PlanHandler) SetRepository(repo repository.Repository) {
	h.repo = repo
}

// validAttachmentURL checks that a generic attachment URL is empty, an internal
// /api/v1/files/ path, or an absolute http(s) URL.
var attachmentURLPattern = regexp.MustCompile(`^https?://`)

func validAttachmentURL(url string) bool {
	if url == "" {
		return true
	}
	if len(url) > 500 {
		return false
	}
	return strings.HasPrefix(url, "/api/v1/files/") || attachmentURLPattern.MatchString(url)
}

// Request types.

type createPlanRequest struct {
	ProjectID          string                           `json:"project_id"`
	ExecutionProfile   string                           `json:"execution_profile"`
	CronExpr           string                           `json:"cron_expr"`
	Prompt             string                           `json:"prompt"`
	ImageModelKey      string                           `json:"image_model_key"`
	SkipReferenceImage *bool                            `json:"skip_reference_image"`
	ReferenceImage     *service.ReferenceImageSelection `json:"reference_image"`
	Watermark          *bool                            `json:"watermark"`
	Goal               string                           `json:"goal"`
	GoalMode           bool                             `json:"goal_mode"`
	// HasContentImage / HasTailImage: seednote image composition (cover always
	// generated). nil → fall back to plan model defaults (content on, tail off).
	HasContentImage *bool `json:"has_content_image,omitempty"`
	HasTailImage    *bool `json:"has_tail_image,omitempty"`
	// ArticleWithCover / ArticleWithContentImages: 公众号 article image toggles
	// (cover NOT mandatory). nil → fall back to plan model defaults (both on).
	ArticleWithCover         *bool                   `json:"article_with_cover,omitempty"`
	ArticleWithContentImages *bool                   `json:"article_with_content_images,omitempty"`
	MontageInput             *model.MontageInput     `json:"montage_input,omitempty"`
	InputAttachments         []model.EntryAttachment `json:"input_attachments,omitempty"`
}

type updatePlanRequest struct {
	ExecutionProfile         string                           `json:"execution_profile"`
	CronExpr                 string                           `json:"cron_expr"`
	Prompt                   string                           `json:"prompt"`
	ImageModelKey            *string                          `json:"image_model_key"`
	SkipReferenceImage       *bool                            `json:"skip_reference_image"`
	ReferenceImage           *service.ReferenceImageSelection `json:"reference_image"`
	ReferenceImageSet        bool                             `json:"-"`
	Watermark                *bool                            `json:"watermark"`
	Goal                     string                           `json:"goal"`
	GoalMode                 *bool                            `json:"goal_mode"`
	HasContentImage          *bool                            `json:"has_content_image,omitempty"`
	HasTailImage             *bool                            `json:"has_tail_image,omitempty"`
	ArticleWithCover         *bool                            `json:"article_with_cover,omitempty"`
	ArticleWithContentImages *bool                            `json:"article_with_content_images,omitempty"`
	MontageInput             *model.MontageInput              `json:"montage_input,omitempty"`
	InputAttachments         *[]model.EntryAttachment         `json:"input_attachments,omitempty"`
}

// Create handles POST /api/v1/plans.
func (h *PlanHandler) Create(c fiber.Ctx) error {
	if err := rejectRemovedReferenceImageField(c.Body()); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	var req createPlanRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.ProjectID == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}
	if strings.TrimSpace(req.ExecutionProfile) == "" {
		return Error(c, fiber.StatusBadRequest, "execution_profile is required")
	}
	if req.MontageInput != nil && strings.TrimSpace(req.MontageInput.Brief) == "" {
		return Error(c, fiber.StatusBadRequest, "montage task requires brief")
	}

	if err := validateMontageSourceAssetURLs(req.MontageInput); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var referenceAssetID string
	var referenceView *model.AssetView
	if req.ReferenceImage != nil {
		if h.referenceAssets == nil {
			return respondReferenceAssetError(c, h.logger, service.ErrReferenceAssetUnavailable)
		}
		resolved, err := h.referenceAssets.ResolveSelection(c.Context(), userID, *req.ReferenceImage, []string{service.DirectUploadPurposeTaskReference})
		if err != nil {
			return respondReferenceAssetError(c, h.logger, err)
		}
		referenceAssetID = resolved
		referenceView, err = h.referenceAssets.Present(c.Context(), userID, resolved, []string{service.DirectUploadPurposeTaskReference})
		if err != nil {
			return respondReferenceAssetError(c, h.logger, err)
		}
	}

	// Validate image_model_key against the caller's tier.
	if err := h.validateImageModelKeyForUser(c, userID, req.ImageModelKey); err != nil {
		return Error(c, fiber.StatusForbidden, err.Error())
	}

	if req.GoalMode && strings.TrimSpace(req.Goal) == "" {
		return Error(c, fiber.StatusBadRequest, "goal must not be empty when goal_mode is true")
	}
	var pending repository.Repository
	if h.repo != nil {
		pending = h.repo
	}
	validatedAttachments, err := validateInputAttachments(c.Context(), h.store, pending, userID, req.InputAttachments, InputAttachmentValidationOptions{
		MaxCount:     maxAgentInputAttachments,
		AllowedTypes: allAgentAttachmentTypes,
	})
	if err != nil {
		return respondInputAttachmentError(c, h.logger, err)
	}
	req.InputAttachments = validatedAttachments
	if h.repo != nil {
		if isMontageProjectForUser(c.Context(), h.repo, userID, req.ProjectID) {
			rewrites, err := finalizeUploadSessionURLs(c.Context(), h.store, h.repo, userID, service.DirectUploadPurposeMontageAsset, montageSourceAssetURLs(req.MontageInput))
			if err != nil {
				return respondUploadSessionFinalizeError(c, h.logger, err)
			}
			rewriteFinalizedMontageAssetURLs(req.MontageInput, rewrites)
		}
	}

	plan, err := h.service.Create(c.Context(), service.CreatePlanParams{
		UserID:                   userID,
		ProjectID:                req.ProjectID,
		ExecutionProfile:         strings.TrimSpace(req.ExecutionProfile),
		CronExpr:                 req.CronExpr,
		Prompt:                   req.Prompt,
		ImageModelKey:            req.ImageModelKey,
		SkipReferenceImage:       req.SkipReferenceImage,
		ReferenceImageAssetID:    referenceAssetID,
		Watermark:                req.Watermark,
		Goal:                     req.Goal,
		GoalMode:                 req.GoalMode,
		HasContentImage:          req.HasContentImage,
		HasTailImage:             req.HasTailImage,
		ArticleWithCover:         req.ArticleWithCover,
		ArticleWithContentImages: req.ArticleWithContentImages,
		MontageInput:             req.MontageInput,
		InputAttachments:         req.InputAttachments,
	})
	if err != nil {
		if handled, response := respondAgentProfileError(c, err); handled {
			return response
		}
		if isReferenceAssetError(err) {
			return respondReferenceAssetError(c, h.logger, err)
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create plan failed")
		if errors.Is(err, service.ErrMontageInput) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if errors.Is(err, service.ErrUnsupportedPlanPlatform) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if errors.Is(err, service.ErrBillingInsufficientForTask) || errors.Is(err, service.ErrBillingDebtOutstanding) {
			return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
				"code": 40202,
				"msg":  "billing_task_admission_rejected",
			})
		}
		return Error(c, fiber.StatusInternalServerError, "failed to create plan")
	}
	plan.ReferenceImage = referenceView
	return Success(c, planAPIResponse(plan, h.store))
}

// List handles GET /api/v1/plans.
func (h *PlanHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	projectID := c.Query("project_id", "")

	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	plans, total, err := h.service.List(c.Context(), userID, offset, limit, projectID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list plans failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list plans")
	}
	if err := h.presentPlanReferences(c.Context(), userID, plans); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}

	return Success(c, fiber.Map{
		"items": planAPIResponses(plans, h.store),
		"total": total,
	})
}

// GetByID handles GET /api/v1/plans/:id.
func (h *PlanHandler) GetByID(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "plan id is required")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	plan, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "plan not found")
	}

	if plan.UserID != userID {
		return Forbidden(c, "you do not have access to this plan")
	}
	if err := h.presentPlanReference(c.Context(), userID, plan); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}

	return Success(c, planAPIResponse(plan, h.store))
}

// Update handles PUT /api/v1/plans/:id.
func (h *PlanHandler) Update(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "plan id is required")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	if err := rejectRemovedReferenceImageField(c.Body()); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	var req updatePlanRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.ExecutionProfile) == "" {
		return Error(c, fiber.StatusBadRequest, "execution_profile is required")
	}
	req.ReferenceImageSet = hasJSONField(c.Body(), "reference_image")
	if err := validateMontageSourceAssetURLs(req.MontageInput); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if req.MontageInput != nil && strings.TrimSpace(req.MontageInput.Brief) == "" {
		return Error(c, fiber.StatusBadRequest, "montage task requires brief")
	}

	// Verify ownership before update.
	existing, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "plan not found")
	}
	if existing.UserID != userID {
		return Forbidden(c, "you do not have access to this plan")
	}
	var referenceAssetID *string
	desiredReferenceID := existing.ReferenceImageAssetID
	if req.ReferenceImageSet {
		desiredReferenceID = ""
		if req.ReferenceImage != nil {
			if h.referenceAssets == nil {
				return respondReferenceAssetError(c, h.logger, service.ErrReferenceAssetUnavailable)
			}
			resolved, err := h.referenceAssets.ResolveSelection(c.Context(), userID, *req.ReferenceImage, []string{service.DirectUploadPurposeTaskReference})
			if err != nil {
				return respondReferenceAssetError(c, h.logger, err)
			}
			desiredReferenceID = resolved
		}
		referenceAssetID = &desiredReferenceID
	}
	referenceView, err := h.planReferenceView(c.Context(), userID, desiredReferenceID)
	if err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}

	// Validate image_model_key against the caller's tier.
	// nil/unset ImageModelKey in the request body means "leave unchanged" — no validation needed.
	if req.ImageModelKey != nil {
		if err := h.validateImageModelKeyForUser(c, userID, *req.ImageModelKey); err != nil {
			return Error(c, fiber.StatusForbidden, err.Error())
		}
	}

	if req.GoalMode != nil && *req.GoalMode && strings.TrimSpace(req.Goal) == "" {
		return Error(c, fiber.StatusBadRequest, "goal must not be empty when goal_mode is true")
	}
	var pending repository.Repository
	if h.repo != nil {
		pending = h.repo
	}
	if req.InputAttachments != nil {
		validatedAttachments, err := validateInputAttachments(c.Context(), h.store, pending, userID, *req.InputAttachments, InputAttachmentValidationOptions{
			MaxCount:     maxAgentInputAttachments,
			AllowedTypes: allAgentAttachmentTypes,
		})
		if err != nil {
			return respondInputAttachmentError(c, h.logger, err)
		}
		req.InputAttachments = &validatedAttachments
	}
	if h.repo != nil {
		if model.IsMontagePlatform(existing.Type) {
			rewrites, err := finalizeUploadSessionURLs(c.Context(), h.store, h.repo, userID, service.DirectUploadPurposeMontageAsset, montageSourceAssetURLs(req.MontageInput))
			if err != nil {
				return respondUploadSessionFinalizeError(c, h.logger, err)
			}
			rewriteFinalizedMontageAssetURLs(req.MontageInput, rewrites)
		}
	}

	updateParams := service.UpdatePlanParams{
		ID:                       id,
		ExecutionProfile:         strings.TrimSpace(req.ExecutionProfile),
		CronExpr:                 req.CronExpr,
		Prompt:                   req.Prompt,
		ImageModelKey:            req.ImageModelKey,
		SkipReferenceImage:       req.SkipReferenceImage,
		ReferenceImageAssetID:    referenceAssetID,
		Watermark:                req.Watermark,
		Goal:                     req.Goal,
		GoalMode:                 req.GoalMode,
		HasContentImage:          req.HasContentImage,
		HasTailImage:             req.HasTailImage,
		ArticleWithCover:         req.ArticleWithCover,
		ArticleWithContentImages: req.ArticleWithContentImages,
		MontageInput:             req.MontageInput,
		InputAttachments:         req.InputAttachments,
	}
	var plan *model.Plan
	if req.ReferenceImageSet {
		plan, err = h.service.Update(c.Context(), updateParams)
	} else {
		const maxReferenceCASAttempts = 3
		for attempt := 0; attempt < maxReferenceCASAttempts; attempt++ {
			plan, err = h.service.UpdateIfReferenceImageAssetID(c.Context(), updateParams, desiredReferenceID)
			if !errors.Is(err, service.ErrPlanUpdateConflict) {
				break
			}
			if attempt == maxReferenceCASAttempts-1 {
				break
			}
			existing, err = h.service.GetByID(c.Context(), id)
			if err != nil {
				return Error(c, fiber.StatusNotFound, "plan not found")
			}
			if existing.UserID != userID {
				return Forbidden(c, "you do not have access to this plan")
			}
			desiredReferenceID = existing.ReferenceImageAssetID
			referenceView, err = h.planReferenceView(c.Context(), userID, desiredReferenceID)
			if err != nil {
				return respondReferenceAssetError(c, h.logger, err)
			}
		}
	}
	if err != nil {
		if handled, response := respondAgentProfileError(c, err); handled {
			return response
		}
		if isReferenceAssetError(err) {
			return respondReferenceAssetError(c, h.logger, err)
		}
		if errors.Is(err, service.ErrPlanUpdateConflict) {
			return Error(c, fiber.StatusConflict, "plan changed concurrently; please retry")
		}
		h.logger.Error().Err(err).Str("plan_id", id).Msg("update plan failed")
		if errors.Is(err, service.ErrMontageInput) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		return Error(c, fiber.StatusInternalServerError, "failed to update plan")
	}
	plan.ReferenceImage = referenceView
	return Success(c, planAPIResponse(plan, h.store))
}

// Delete handles DELETE /api/v1/plans/:id.
func (h *PlanHandler) Delete(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "plan id is required")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before delete.
	existing, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "plan not found")
	}
	if existing.UserID != userID {
		return Forbidden(c, "you do not have access to this plan")
	}

	if err := h.service.Delete(c.Context(), id); err != nil {
		h.logger.Error().Err(err).Msg("delete plan failed")
		return Error(c, fiber.StatusInternalServerError, "failed to delete plan")
	}

	return Success(c, fiber.Map{"message": "plan deleted"})
}

// Pause handles POST /api/v1/plans/:id/pause.
func (h *PlanHandler) Pause(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "plan id is required")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before pause.
	existing, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "plan not found")
	}
	if existing.UserID != userID {
		return Forbidden(c, "you do not have access to this plan")
	}

	if err := h.service.Pause(c.Context(), id); err != nil {
		h.logger.Error().Err(err).Str("plan_id", id).Msg("pause plan failed")
		return Error(c, fiber.StatusInternalServerError, "failed to pause plan")
	}

	return Success(c, fiber.Map{"message": "plan paused"})
}

// validateImageModelKeyForUser delegates to the package-level helper, binding
// this handler's repository and image presets. See validateImageModelKeyForUser
// in image_model.go for the fail-closed tier-resolution rules.
func (h *PlanHandler) validateImageModelKeyForUser(c fiber.Ctx, userID, key string) error {
	return validateImageModelKeyForUser(c.Context(), h.repo, userID, key, h.imagePresets)
}

// Resume handles POST /api/v1/plans/:id/resume.
func (h *PlanHandler) Resume(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "plan id is required")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before resume.
	existing, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "plan not found")
	}
	if existing.UserID != userID {
		return Forbidden(c, "you do not have access to this plan")
	}

	if err := h.service.Resume(c.Context(), id); err != nil {
		h.logger.Error().Err(err).Str("plan_id", id).Msg("resume plan failed")
		return Error(c, fiber.StatusInternalServerError, "failed to resume plan")
	}

	return Success(c, fiber.Map{"message": "plan resumed"})
}
