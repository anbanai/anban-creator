package handler

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/service"
)

// PlanHandler handles plan-related HTTP endpoints.
type PlanHandler struct {
	service *service.PlanService
	logger  *zerolog.Logger
}

// NewPlanHandler creates a new PlanHandler.
func NewPlanHandler(svc *service.PlanService, logger *zerolog.Logger) *PlanHandler {
	return &PlanHandler{service: svc, logger: logger}
}

// validReferenceImageURL checks that a reference image URL is empty, an internal
// /api/v1/files/ path, or an absolute http(s) URL.
var referenceURLPattern = regexp.MustCompile(`^https?://`)

func validReferenceImageURL(url string) bool {
	if url == "" {
		return true
	}
	return strings.HasPrefix(url, "/api/v1/files/") || referenceURLPattern.MatchString(url)
}

// Request types.

type createPlanRequest struct {
	ChannelID          string `json:"channel_id"`
	CronExpr           string `json:"cron_expr"`
	Prompt             string `json:"prompt"`
	SkipReferenceImage *bool  `json:"skip_reference_image"`
	ReferenceImageURL  string `json:"reference_image_url"`
}

type updatePlanRequest struct {
	CronExpr           string `json:"cron_expr"`
	Prompt             string `json:"prompt"`
	SkipReferenceImage *bool  `json:"skip_reference_image"`
	ReferenceImageURL  string `json:"reference_image_url"`
}

// Create handles POST /api/v1/plans.
func (h *PlanHandler) Create(c fiber.Ctx) error {
	var req createPlanRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.ChannelID == "" {
		return Error(c, fiber.StatusBadRequest, "channel_id is required")
	}

	if !validReferenceImageURL(req.ReferenceImageURL) {
		return Error(c, fiber.StatusBadRequest, "reference_image_url must be an internal file path or an http(s) URL")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	plan, err := h.service.Create(c.Context(), userID, req.ChannelID, req.CronExpr, req.Prompt, req.SkipReferenceImage, req.ReferenceImageURL)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create plan failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create plan")
	}

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
	channelID := c.Query("channel_id", "")

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	plans, total, err := h.service.List(c.Context(), userID, offset, limit, channelID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list plans failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list plans")
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

	if !validReferenceImageURL(req.ReferenceImageURL) {
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

	plan, err := h.service.Update(c.Context(), id, req.CronExpr, req.Prompt, req.SkipReferenceImage, req.ReferenceImageURL)
	if err != nil {
		h.logger.Error().Err(err).Str("plan_id", id).Msg("update plan failed")
		return Error(c, fiber.StatusInternalServerError, "failed to update plan")
	}

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
