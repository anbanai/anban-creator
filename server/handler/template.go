package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

// TemplateHandler handles template-related HTTP endpoints.
type TemplateHandler struct {
	service         *service.TemplateService
	logger          *zerolog.Logger
	referenceAssets *service.ReferenceAssetService
}

// NewTemplateHandler creates a new TemplateHandler.
func NewTemplateHandler(svc *service.TemplateService, logger *zerolog.Logger) *TemplateHandler {
	return &TemplateHandler{service: svc, logger: logger}
}

func (h *TemplateHandler) SetReferenceAssetService(assets *service.ReferenceAssetService) {
	h.referenceAssets = assets
}

type templateResponse struct {
	ID                string                   `json:"id"`
	Type              string                   `json:"type"`
	Name              string                   `json:"name"`
	Category          string                   `json:"category"`
	Thumbnail         *model.AssetView         `json:"thumbnail,omitempty"`
	Prompt            string                   `json:"prompt"`
	PromptSource      string                   `json:"prompt_source"`
	ReadinessStatus   string                   `json:"readiness_status"`
	ActivateWhenReady bool                     `json:"activate_when_ready"`
	ImageAnalysis     *model.ImageAnalysisView `json:"image_analysis,omitempty"`
	Visibility        string                   `json:"visibility"`
	SortOrder         int                      `json:"sort_order"`
	IsActive          bool                     `json:"is_active"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

func canonicalTemplateResponse(tmpl *model.Template) templateResponse {
	return templateResponse{
		ID: tmpl.ID, Type: tmpl.Type, Name: tmpl.Name, Category: tmpl.Category,
		Thumbnail: tmpl.Thumbnail, Prompt: tmpl.Prompt, PromptSource: tmpl.PromptSource,
		ReadinessStatus: tmpl.ReadinessStatus, ActivateWhenReady: tmpl.ActivateWhenReady,
		ImageAnalysis: tmpl.ImageAnalysis, Visibility: tmpl.Visibility,
		SortOrder: tmpl.SortOrder, IsActive: tmpl.IsActive,
		CreatedAt: tmpl.CreatedAt, UpdatedAt: tmpl.UpdatedAt,
	}
}

func (h *TemplateHandler) presentThumbnail(ctx fiber.Ctx, tmpl *model.Template) error {
	if tmpl == nil || tmpl.ThumbnailAssetID == "" {
		return nil
	}
	if h.referenceAssets == nil {
		return service.ErrReferenceAssetUnavailable
	}
	view, err := h.referenceAssets.Present(ctx.Context(), tmpl.UserID, tmpl.ThumbnailAssetID, []string{service.DirectUploadPurposeTemplateThumbnail})
	if err != nil {
		return err
	}
	tmpl.Thumbnail = view
	return nil
}

// List handles GET /api/v1/templates.
//
// Query params:
//   - type: filter by template type (seednote)
//   - category, tag: additional filters
//   - scope: admin scoping — "all" (default) | "public" | "private" | "inactive"
//   - pagination via offset/limit
//
// Ordinary users always see active public templates. Admin scopes are resolved
// from users.is_admin by TemplateService.
func (h *TemplateHandler) List(c fiber.Ctx) error {
	templateType := c.Query("type", "")
	category := c.Query("category", "")
	tag := c.Query("tag", "")
	scope := c.Query("scope", "all")
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Validate scope; reject unknown values rather than silently falling back.
	switch scope {
	case "all", "public", "private", "inactive":
	default:
		return Error(c, fiber.StatusBadRequest, "scope must be one of: all, public, private, inactive")
	}

	userID := GetUserID(c)

	templates, total, err := h.service.List(c.Context(), templateType, category, tag, userID, scope, offset, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list templates failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list templates")
	}

	items := make([]templateResponse, 0, len(templates))
	for _, t := range templates {
		if err := h.presentThumbnail(c, t); err != nil {
			return respondReferenceAssetError(c, h.logger, err)
		}
		items = append(items, canonicalTemplateResponse(t))
	}

	return Success(c, fiber.Map{
		"items": items,
		"total": total,
	})
}

// GetByID handles GET /api/v1/templates/:id.
//
// Private or inactive templates are visible only to administrators.
func (h *TemplateHandler) GetByID(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "template id is required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid template id format")
	}

	tmpl, err := h.service.GetByID(c.Context(), id, GetUserID(c))
	if err != nil {
		return Error(c, fiber.StatusNotFound, "template not found")
	}

	if err := h.presentThumbnail(c, tmpl); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	return Success(c, canonicalTemplateResponse(tmpl))
}

// createTemplateRequest is the body for POST /api/v1/templates (Create).
// Templates are visual-only: request fields import image prompt metadata only.
type createTemplateRequest struct {
	Name           string                           `json:"name"`
	Type           string                           `json:"type"`
	ThumbnailImage *service.ReferenceImageSelection `json:"thumbnail_image"`
	Prompt         string                           `json:"prompt"`
	Visibility     string                           `json:"visibility"`
	Category       string                           `json:"category"`
	SortOrder      int                              `json:"sort_order"`
	IsActive       *bool                            `json:"is_active"`
}

type updateTemplateRequest struct {
	Name           *string                          `json:"name"`
	Type           *string                          `json:"type"`
	ThumbnailImage *service.ReferenceImageSelection `json:"thumbnail_image"`
	Prompt         *string                          `json:"prompt"`
	Visibility     *string                          `json:"visibility"`
	Category       *string                          `json:"category"`
	SortOrder      *int                             `json:"sort_order"`
	IsActive       *bool                            `json:"is_active"`
}

func rejectLegacyTemplatePromptFields(body []byte) error {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	for _, field := range []string{"style_prompt", "visual_style", "tags", "thumbnail_url"} {
		if _, exists := payload[field]; exists {
			if field == "tags" {
				return errors.New("tags is no longer supported")
			}
			if field == "thumbnail_url" {
				return errors.New("thumbnail_url is no longer supported; use thumbnail_image")
			}
			return fmt.Errorf("%s is no longer supported; use prompt", field)
		}
	}
	return nil
}

func (h *TemplateHandler) authorizeAdmin(c fiber.Ctx, userID string) error {
	if err := h.service.AuthorizeAdmin(c.Context(), userID); err != nil {
		if errors.Is(err, service.ErrTemplateForbidden) {
			return Error(c, fiber.StatusForbidden, "forbidden")
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("template admin authorization failed")
		return Error(c, fiber.StatusInternalServerError, "failed to authorize template administration")
	}
	return nil
}

// Create handles POST /api/v1/templates.
func (h *TemplateHandler) Create(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := h.authorizeAdmin(c, userID); err != nil {
		return err
	}
	if err := rejectLegacyTemplatePromptFields(c.Body()); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if err := rejectRemovedRequestFields(c.Body()); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}

	var req createTemplateRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Type == "" {
		return Error(c, fiber.StatusBadRequest, "type is required")
	}
	if req.Type != model.TemplateTypeSeednote {
		return Error(c, fiber.StatusBadRequest, "type must be seednote")
	}
	if !model.IsSeednoteTemplateCategory(req.Category) {
		return Error(c, fiber.StatusBadRequest, "invalid seednote template category")
	}
	if req.ThumbnailImage == nil {
		return Error(c, fiber.StatusBadRequest, "thumbnail_image is required")
	}
	req.Name = service.ResolveTemplateName(req.Name, req.Prompt)
	if req.Name == "" {
		return Error(c, fiber.StatusBadRequest, service.ErrTemplateNameMissing.Error())
	}
	if req.Visibility != "public" && req.Visibility != "private" {
		req.Visibility = "public"
	}
	if h.referenceAssets == nil {
		return respondReferenceAssetError(c, h.logger, service.ErrReferenceAssetUnavailable)
	}
	thumbnailAssetID, err := h.referenceAssets.ResolveSelection(c.Context(), userID, *req.ThumbnailImage, []string{service.DirectUploadPurposeTemplateThumbnail})
	if err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}

	tmpl := &model.Template{
		Name: req.Name, Type: req.Type, ThumbnailAssetID: thumbnailAssetID,
		Prompt: req.Prompt, Visibility: req.Visibility, Category: req.Category,
		SortOrder: req.SortOrder, IsActive: true,
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	created, err := h.service.CreateWithActive(c.Context(), tmpl, userID, isActive)
	if err != nil {
		if errors.Is(err, service.ErrTemplateNameMissing) || errors.Is(err, service.ErrTemplatePromptMissing) || errors.Is(err, service.ErrTemplateTypeInvalid) || errors.Is(err, service.ErrTemplateCategoryInvalid) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if errors.Is(err, service.ErrTemplateForbidden) {
			return Error(c, fiber.StatusForbidden, "forbidden")
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create template failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create template")
	}

	if err := h.presentThumbnail(c, created); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	return Success(c, canonicalTemplateResponse(created))
}

// Update handles PUT /api/v1/templates/:id for administrators.
func (h *TemplateHandler) Update(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := h.authorizeAdmin(c, userID); err != nil {
		return err
	}

	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "template id is required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid template id format")
	}
	if err := rejectLegacyTemplatePromptFields(c.Body()); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if err := rejectRemovedRequestFields(c.Body()); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}

	var req updateTemplateRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	// Empty type = leave unchanged (matches service PATCH semantics). Non-empty
	// type must still be a known value.
	if req.Type != nil && *req.Type != "" {
		if *req.Type != model.TemplateTypeSeednote {
			return Error(c, fiber.StatusBadRequest, "type must be seednote")
		}
	}
	if req.Category != nil && !model.IsSeednoteTemplateCategory(*req.Category) {
		return Error(c, fiber.StatusBadRequest, "invalid seednote template category")
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return Error(c, fiber.StatusBadRequest, service.ErrTemplateNameMissing.Error())
	}
	var thumbnailAssetID *string
	if req.ThumbnailImage != nil {
		if h.referenceAssets == nil {
			return respondReferenceAssetError(c, h.logger, service.ErrReferenceAssetUnavailable)
		}
		resolved, err := h.referenceAssets.ResolveSelection(c.Context(), userID, *req.ThumbnailImage, []string{service.DirectUploadPurposeTemplateThumbnail})
		if err != nil {
			return respondReferenceAssetError(c, h.logger, err)
		}
		thumbnailAssetID = &resolved
	}

	patch := service.TemplatePatch{
		Name: req.Name, Type: req.Type, ThumbnailAssetID: thumbnailAssetID,
		Prompt: req.Prompt, Visibility: req.Visibility, Category: req.Category,
		SortOrder: req.SortOrder, IsActive: req.IsActive,
	}
	if req.Visibility != nil && *req.Visibility != "" && *req.Visibility != "public" && *req.Visibility != "private" {
		return Error(c, fiber.StatusBadRequest, "visibility must be public or private")
	}
	if req.Type != nil && *req.Type == "" {
		req.Type = nil
		patch.Type = nil
	}
	if req.Visibility != nil && *req.Visibility == "" {
		req.Visibility = nil
		patch.Visibility = nil
	}
	updated, err := h.service.UpdatePatch(c.Context(), id, userID, patch)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTemplateNotFound):
			return Error(c, fiber.StatusNotFound, "template not found")
		case errors.Is(err, service.ErrTemplateForbidden):
			return Error(c, fiber.StatusForbidden, "forbidden")
		case errors.Is(err, service.ErrTemplateNameMissing), errors.Is(err, service.ErrTemplatePromptMissing), errors.Is(err, service.ErrTemplateTypeInvalid), errors.Is(err, service.ErrTemplateCategoryInvalid):
			return Error(c, fiber.StatusBadRequest, err.Error())
		default:
			h.logger.Error().Err(err).Str("template_id", id).Str("user_id", userID).Msg("update template failed")
			return Error(c, fiber.StatusInternalServerError, "failed to update template")
		}
	}

	if err := h.presentThumbnail(c, updated); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	return Success(c, canonicalTemplateResponse(updated))
}

// Delete handles DELETE /api/v1/templates/:id for administrators.
func (h *TemplateHandler) Delete(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := h.authorizeAdmin(c, userID); err != nil {
		return err
	}

	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "template id is required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid template id format")
	}

	if err := h.service.Delete(c.Context(), id, userID); err != nil {
		switch {
		case errors.Is(err, service.ErrTemplateNotFound):
			return Error(c, fiber.StatusNotFound, "template not found")
		case errors.Is(err, service.ErrTemplateForbidden):
			return Error(c, fiber.StatusForbidden, "forbidden")
		default:
			h.logger.Error().Err(err).Str("template_id", id).Str("user_id", userID).Msg("delete template failed")
			return Error(c, fiber.StatusInternalServerError, "failed to delete template")
		}
	}

	return Success(c, fiber.Map{"deleted": id})
}
