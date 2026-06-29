package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/royalrick/anbanwriter/server/storage"
)

// TemplateHandler handles template-related HTTP endpoints.
type TemplateHandler struct {
	service *service.TemplateService
	logger  *zerolog.Logger
	store   storage.Provider
}

// NewTemplateHandler creates a new TemplateHandler.
func NewTemplateHandler(svc *service.TemplateService, logger *zerolog.Logger) *TemplateHandler {
	return &TemplateHandler{service: svc, logger: logger}
}

// SetStore injects a storage provider so image URLs can be resolved to signed,
// directly-fetchable URLs in responses.
func (h *TemplateHandler) SetStore(s storage.Provider) {
	h.store = s
}

// signTemplateURLs resolves stored runtime image URLs to directly-fetchable
// signed URLs so any viewer who can see the template can load its images
// regardless of which user originally uploaded them. No-op when no store is
// wired (e.g. unit tests) or the URL is external/empty. Delegates the field list
// to the shared service.SignTemplateURLs so it stays in sync with the
// recommended-templates path in project create.
func (h *TemplateHandler) signTemplateURLs(ctx context.Context, t *model.Template) {
	service.SignTemplateURLs(ctx, h.store, h.logger, t)
}

// List handles GET /api/v1/templates.
//
// Query params:
//   - type: filter by template type (poster|seednote|article)
//   - category, tag: additional filters
//   - scope: visibility scoping — "all" (default) | "mine" | "public"
//   - pagination via offset/limit
//
// Unauthenticated callers only see public templates. Authenticated callers see
// public templates plus their own private ones when scope=all or scope=mine.
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
	case "all", "mine", "public":
	default:
		return Error(c, fiber.StatusBadRequest, "scope must be one of: all, mine, public")
	}

	userID := GetUserID(c)

	templates, total, err := h.service.List(c.Context(), templateType, category, tag, userID, scope, offset, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list templates failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list templates")
	}

	for _, t := range templates {
		h.signTemplateURLs(c.Context(), t)
	}

	return Success(c, fiber.Map{
		"items": templates,
		"total": total,
	})
}

// GetByID handles GET /api/v1/templates/:id.
//
// Private templates are only visible to their owner.
func (h *TemplateHandler) GetByID(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "template id is required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid template id format")
	}

	tmpl, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "template not found")
	}

	// Enforce visibility for private templates.
	if tmpl.Visibility == "private" {
		userID := GetUserID(c)
		if tmpl.UserID != userID {
			return Error(c, fiber.StatusNotFound, "template not found")
		}
	}

	h.signTemplateURLs(c.Context(), tmpl)
	return Success(c, tmpl)
}

// createTemplateRequest is the body for POST /api/v1/templates (Create) and
// PUT /api/v1/templates/:id (Update). Structure/ExampleContent are sent as plain
// markdown text from the Studio form; the handler wraps them into {"text": ...}
// for the model's JSON columns (the MCP save_template path keeps passing raw JSON).
type createTemplateRequest struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	ThumbnailURL   string   `json:"thumbnail_url"`
	StylePrompt    string   `json:"style_prompt"`
	Visibility     string   `json:"visibility"`
	Writer         string   `json:"writer"`
	Theme          string   `json:"theme"`
	Structure      string   `json:"structure"`
	ExampleContent string   `json:"example_content"`
	Category       string   `json:"category"`
	Tags           []string `json:"tags"`
	Author         string   `json:"author"`
	// Ecommerce defaults (type="ecommerce" only): default modules/quantities,
	// target platform, brand brief, default image model key. Pointer so nil =
	// leave unchanged on PATCH (Update); non-nil = set (Create or Update).
	Ecommerce *model.EcommerceTemplateDefaults `json:"ecommerce,omitempty"`
}

type updateTemplateRequest struct {
	Name           *string                          `json:"name"`
	Type           *string                          `json:"type"`
	ThumbnailURL   *string                          `json:"thumbnail_url"`
	StylePrompt    *string                          `json:"style_prompt"`
	Visibility     *string                          `json:"visibility"`
	Writer         *string                          `json:"writer"`
	Theme          *string                          `json:"theme"`
	Structure      *string                          `json:"structure"`
	ExampleContent *string                          `json:"example_content"`
	Category       *string                          `json:"category"`
	Tags           *[]string                        `json:"tags"`
	Author         *string                          `json:"author"`
	Ecommerce      *model.EcommerceTemplateDefaults `json:"ecommerce,omitempty"`
}

// scaffoldText wraps a plain-text scaffold value into the model's {"text": ...}
// JSON shape. Empty input returns nil so the column stays NULL/empty (lets the
// PATCH "non-empty = set" rule in the service work correctly on Update).
func scaffoldText(s string) map[string]any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return map[string]any{"text": s}
}

// Create handles POST /api/v1/templates.
func (h *TemplateHandler) Create(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req createTemplateRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Type == "" {
		return Error(c, fiber.StatusBadRequest, "type is required")
	}
	switch req.Type {
	case "poster", "seednote", "article", "ecommerce":
	default:
		return Error(c, fiber.StatusBadRequest, "type must be one of: poster, seednote, article, ecommerce")
	}
	if req.Visibility != "public" && req.Visibility != "private" {
		req.Visibility = "public"
	}

	// 作者署名不得是写作风格的人设名/key（二者语义不同，混用会把模仿对象当成发布作者）。
	if err := service.RejectWriterNameAsAuthor(req.Author); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}

	tmpl := &model.Template{
		Name:           req.Name,
		Type:           req.Type,
		ThumbnailURL:   req.ThumbnailURL,
		VisualStyle:    req.StylePrompt,
		Visibility:     req.Visibility,
		Writer:         req.Writer,
		Theme:          req.Theme,
		Structure:      scaffoldText(req.Structure),
		ExampleContent: scaffoldText(req.ExampleContent),
		Category:       req.Category,
		Tags:           req.Tags,
		Author:         req.Author,
		IsActive:       true,
	}
	if req.Ecommerce != nil {
		tmpl.SetEcommerce(*req.Ecommerce)
	}

	created, err := h.service.Create(c.Context(), tmpl, userID)
	if err != nil {
		if errors.Is(err, service.ErrTemplateNameMissing) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create template failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create template")
	}

	h.signTemplateURLs(c.Context(), created)
	return Success(c, created)
}

// Update handles PUT /api/v1/templates/:id. Only the owner can update.
func (h *TemplateHandler) Update(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "template id is required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid template id format")
	}

	var req updateTemplateRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	// Empty type = leave unchanged (matches service PATCH semantics). Non-empty
	// type must still be a known value.
	if req.Type != nil && *req.Type != "" {
		switch *req.Type {
		case "poster", "seednote", "article", "ecommerce":
		default:
			return Error(c, fiber.StatusBadRequest, "type must be one of: poster, seednote, article, ecommerce")
		}
	}

	// 作者署名不得是写作风格的人设名/key（二者语义不同，混用会把模仿对象当成发布作者）。
	if req.Author != nil {
		if err := service.RejectWriterNameAsAuthor(*req.Author); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
	}

	patch := service.TemplatePatch{
		Name:         req.Name,
		Type:         req.Type,
		ThumbnailURL: req.ThumbnailURL,
		VisualStyle:  req.StylePrompt,
		Visibility:   req.Visibility,
		Writer:       req.Writer,
		Theme:        req.Theme,
		Category:     req.Category,
		Tags:         req.Tags,
		Author:       req.Author,
		Ecommerce:    req.Ecommerce,
	}
	if req.Structure != nil {
		structure := scaffoldText(*req.Structure)
		patch.Structure = &structure
	}
	if req.ExampleContent != nil {
		exampleContent := scaffoldText(*req.ExampleContent)
		patch.ExampleContent = &exampleContent
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
		default:
			h.logger.Error().Err(err).Str("template_id", id).Str("user_id", userID).Msg("update template failed")
			return Error(c, fiber.StatusInternalServerError, "failed to update template")
		}
	}

	h.signTemplateURLs(c.Context(), updated)
	return Success(c, updated)
}

// Delete handles DELETE /api/v1/templates/:id. Only the owner can delete.
func (h *TemplateHandler) Delete(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
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
