package handler

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/royalrick/anbanwriter/server/storage"
)

// PlanHandler handles plan-related HTTP endpoints.
type PlanHandler struct {
	service      *service.PlanService
	logger       *zerolog.Logger
	imagePresets []config.ImageModelPreset
	repo         repository.Repository
	store        storage.Provider
}

// NewPlanHandler creates a new PlanHandler.
func NewPlanHandler(svc *service.PlanService, logger *zerolog.Logger) *PlanHandler {
	return &PlanHandler{service: svc, logger: logger}
}

// SetStore injects a storage provider so the reference image URL can be resolved
// to a signed, directly-fetchable URL in responses.
func (h *PlanHandler) SetStore(s storage.Provider) {
	h.store = s
}

// signPlanURLs resolves the stored reference-image URL to a directly-fetchable
// signed URL. No-op when no store is wired (e.g. unit tests) or the URL is
// external/empty.
func (h *PlanHandler) signPlanURLs(ctx context.Context, p *model.Plan) {
	if p == nil {
		return
	}
	p.ReferenceImageURL = service.SignURL(ctx, h.store, h.logger, p.ReferenceImageURL, service.DefaultSignedURLTTL)
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

// validReferenceImageURL checks that a reference image URL is empty, an internal
// /api/v1/files/ path, or an absolute http(s) URL.
var referenceURLPattern = regexp.MustCompile(`^https?://`)

func validReferenceImageURL(url string) bool {
	if url == "" {
		return true
	}
	if len(url) > 500 {
		return false
	}
	return strings.HasPrefix(url, "/api/v1/files/") || referenceURLPattern.MatchString(url)
}

// Request types.

type createPlanRequest struct {
	ProjectID          string `json:"project_id"`
	CronExpr           string `json:"cron_expr"`
	Prompt             string `json:"prompt"`
	ImageModelKey      string `json:"image_model_key"`
	SkipReferenceImage *bool  `json:"skip_reference_image"`
	ReferenceImageURL  string `json:"reference_image_url"`
	VisualStyle        string `json:"visual_style"`
	WriterKey          string `json:"writer_key"`
	Theme              string `json:"theme"`
	// Byline / WritingVoice / PersonaAvatar: 公众号 作者（署名）+ 写作风格（模仿）
	// + 可选人设头像 overrides, orthogonal to VisualStyle/WriterKey/Theme.
	Byline        string `json:"byline"`
	WritingVoice  string `json:"writing_voice"`
	PersonaAvatar string `json:"persona_avatar"`
	Watermark     *bool  `json:"watermark"`
	Goal          string `json:"goal"`
	GoalMode      bool   `json:"goal_mode"`
	// TemplateID records the template selected during plan creation. nil/empty = no template.
	TemplateID *string `json:"template_id,omitempty"`
	// HasContentImage / HasTailImage: seednote image composition (cover always
	// generated). nil → fall back to plan model defaults (content on, tail off).
	HasContentImage *bool `json:"has_content_image,omitempty"`
	HasTailImage    *bool `json:"has_tail_image,omitempty"`
}

type updatePlanRequest struct {
	CronExpr           string  `json:"cron_expr"`
	Prompt             string  `json:"prompt"`
	ImageModelKey      *string `json:"image_model_key"`
	SkipReferenceImage *bool   `json:"skip_reference_image"`
	ReferenceImageURL  *string `json:"reference_image_url"`
	VisualStyle        *string `json:"visual_style"`
	WriterKey          *string `json:"writer_key"`
	Theme              *string `json:"theme"`
	// Byline / WritingVoice / PersonaAvatar: leave-unchanged semantics
	// (omitted = unchanged), same as VisualStyle/WriterKey/Theme.
	Byline          *string `json:"byline"`
	WritingVoice    *string `json:"writing_voice"`
	PersonaAvatar   *string `json:"persona_avatar"`
	Watermark       *bool   `json:"watermark"`
	Goal            string  `json:"goal"`
	GoalMode        *bool   `json:"goal_mode"`
	TemplateID      *string `json:"template_id,omitempty"`
	HasContentImage *bool   `json:"has_content_image,omitempty"`
	HasTailImage    *bool   `json:"has_tail_image,omitempty"`
}

// Create handles POST /api/v1/plans.
func (h *PlanHandler) Create(c fiber.Ctx) error {
	var req createPlanRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.ProjectID == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}

	if !validReferenceImageURL(req.ReferenceImageURL) {
		return Error(c, fiber.StatusBadRequest, "reference_image_url must be an internal file path or an http(s) URL")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Validate image_model_key against the caller's tier.
	if err := h.validateImageModelKeyForUser(c, userID, req.ImageModelKey); err != nil {
		return Error(c, fiber.StatusForbidden, err.Error())
	}

	if req.GoalMode && strings.TrimSpace(req.Goal) == "" {
		return Error(c, fiber.StatusBadRequest, "goal must not be empty when goal_mode is true")
	}

	// Byline must not be a writer-persona name (same defense-in-depth guard as
	// task/project/template create). A plan byline flows to spawned tasks.
	if err := service.RejectWriterNameAsByline(req.Byline); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}

	plan, err := h.service.Create(c.Context(), service.CreatePlanParams{
		UserID:             userID,
		ProjectID:          req.ProjectID,
		CronExpr:           req.CronExpr,
		Prompt:             req.Prompt,
		ImageModelKey:      req.ImageModelKey,
		SkipReferenceImage: req.SkipReferenceImage,
		ReferenceImageURL:  req.ReferenceImageURL,
		Watermark:          req.Watermark,
		Goal:               req.Goal,
		GoalMode:           req.GoalMode,
		HasContentImage:    req.HasContentImage,
		HasTailImage:       req.HasTailImage,
		VisualStyle:        req.VisualStyle,
		WriterKey:          req.WriterKey,
		WritingVoice:       req.WritingVoice,
		Byline:             req.Byline,
		PersonaAvatar:      req.PersonaAvatar,
		Theme:              req.Theme,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create plan failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create plan")
	}

	h.signPlanURLs(c.Context(), plan)
	return Success(c, plan)
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

	plans, total, err := h.service.List(c.Context(), userID, offset, limit, projectID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list plans failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list plans")
	}

	for _, p := range plans {
		h.signPlanURLs(c.Context(), p)
	}

	return Success(c, fiber.Map{
		"items": plans,
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

	h.signPlanURLs(c.Context(), plan)
	return Success(c, plan)
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

	var req updatePlanRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.ReferenceImageURL != nil && !validReferenceImageURL(*req.ReferenceImageURL) {
		return Error(c, fiber.StatusBadRequest, "reference_image_url must be an internal file path or an http(s) URL")
	}

	// Verify ownership before update.
	existing, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "plan not found")
	}
	if existing.UserID != userID {
		return Forbidden(c, "you do not have access to this plan")
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

	// Byline must not be a writer-persona name. req.Byline is nil for "leave
	// unchanged"; only validate when the caller is setting/clearing it.
	if req.Byline != nil {
		if err := service.RejectWriterNameAsByline(*req.Byline); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
	}

	plan, err := h.service.Update(c.Context(), service.UpdatePlanParams{
		ID:                 id,
		CronExpr:           req.CronExpr,
		Prompt:             req.Prompt,
		ImageModelKey:      req.ImageModelKey,
		SkipReferenceImage: req.SkipReferenceImage,
		ReferenceImageURL:  req.ReferenceImageURL,
		Watermark:          req.Watermark,
		Goal:               req.Goal,
		GoalMode:           req.GoalMode,
		HasContentImage:    req.HasContentImage,
		HasTailImage:       req.HasTailImage,
		VisualStyle:        req.VisualStyle,
		WriterKey:          req.WriterKey,
		WritingVoice:       req.WritingVoice,
		Byline:             req.Byline,
		PersonaAvatar:      req.PersonaAvatar,
		Theme:              req.Theme,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("plan_id", id).Msg("update plan failed")
		return Error(c, fiber.StatusInternalServerError, "failed to update plan")
	}

	h.signPlanURLs(c.Context(), plan)
	return Success(c, plan)
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

// validateImageModelKeyForUser resolves the user's tier and validates image_model_key.
// Returns nil if the key is acceptable for this user, an error otherwise.
// Fail-closed: if the user's tier cannot be determined (repo unavailable or
// lookup error), default to Free so a DB hiccup cannot accidentally widen
// access to Pro/Enterprise-only models.
func (h *PlanHandler) validateImageModelKeyForUser(c fiber.Ctx, userID, key string) error {
	if key == "" {
		return nil
	}
	tier := model.TierFree
	if h.repo != nil {
		if user, err := h.repo.Users().FindByID(c.Context(), userID); err == nil && user != nil {
			tier = model.ResolveTier(user.Tier)
		}
	}
	return ValidateImageModelKey(key, tier, h.imagePresets)
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
