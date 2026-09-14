package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	projectmemory "github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/platform"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

const projectMemoryMaxResponseBytes = 256 << 10

// ProjectHandler handles project-related HTTP endpoints.
type ProjectHandler struct {
	service           *service.ProjectService
	logger            *zerolog.Logger
	visionClient      service.LLMClient
	templateSvc       *service.TemplateService
	store             storage.Provider
	uploadRepo        repository.Repository
	referenceAssets   *service.ReferenceAssetService
	seednoteClient    *seednote.Client
	seednoteReady     service.Readiness
	imageCapabilities map[string]config.ImageGenerationRouteConfig
	projectMemory     interface {
		ReadProject(context.Context, string) (projectmemory.ProjectView, error)
	}
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(svc *service.ProjectService, logger *zerolog.Logger) *ProjectHandler {
	return &ProjectHandler{service: svc, logger: logger}
}

// SetVisionClient injects a dedicated vision-capable LLM client used by AnalyzeImage.
func (h *ProjectHandler) SetVisionClient(client service.LLMClient) {
	h.visionClient = client
}

// SetTemplateService injects an optional TemplateService for template recommendations on project creation.
func (h *ProjectHandler) SetTemplateService(svc *service.TemplateService) {
	h.templateSvc = svc
}

// SetStore injects a storage provider for reading locally uploaded files.
func (h *ProjectHandler) SetStore(s storage.Provider) {
	h.store = s
}

func (h *ProjectHandler) SetUploadRepository(repo repository.Repository) {
	h.uploadRepo = repo
}

func (h *ProjectHandler) SetReferenceAssetService(svc *service.ReferenceAssetService) {
	h.referenceAssets = svc
}

func (h *ProjectHandler) SetProjectMemoryStore(store interface {
	ReadProject(context.Context, string) (projectmemory.ProjectView, error)
}) {
	h.projectMemory = store
}

// SetImageCapabilities wires the public capability catalog used to validate
// new project image defaults.
func (h *ProjectHandler) SetImageCapabilities(routes config.ImageGenerationRoutesConfig) {
	h.imageCapabilities = routes.Capabilities
}

func (h *ProjectHandler) validateProjectImageCapability(ctx context.Context, userID string, req *projectRequest) error {
	if req == nil || req.EcommerceDefaults == nil || len(h.imageCapabilities) == 0 {
		return nil
	}
	key := strings.TrimSpace(req.EcommerceDefaults.ImageCapabilityKey)
	return validateImageCapabilityKeyForUser(ctx, h.uploadRepo, userID, key, h.imageCapabilities)
}

// signProjectURLs resolves the stored avatar URL to
// directly-fetchable signed URLs so any viewer who can see the project can load
// its images regardless of which user originally uploaded them. No-op when no
// store is wired (e.g. unit tests) or the URL is external/empty.
func (h *ProjectHandler) signProjectURLs(ctx context.Context, ch *model.Project) {
	if ch == nil {
		return
	}
	ch.AvatarURL = service.SignURL(ctx, h.store, h.logger, ch.AvatarURL, service.DefaultSignedURLTTL)
}

func (h *ProjectHandler) resolveProjectReference(ctx context.Context, userID string, req *projectRequest) error {
	if req == nil || !req.ReferenceImageSet || req.ReferenceImage == nil {
		return nil
	}
	if h.referenceAssets == nil {
		return service.ErrReferenceAssetUnavailable
	}
	assetID, err := h.referenceAssets.ResolveSelection(ctx, userID, *req.ReferenceImage, []string{service.DirectUploadPurposeProjectReference})
	if err != nil {
		return err
	}
	req.ReferenceImageAssetID = assetID
	return nil
}

func (h *ProjectHandler) resolveProjectPortraitReference(ctx context.Context, userID string, req *projectRequest) error {
	if req == nil || !req.PortraitReferenceImageSet || req.PortraitReferenceImage == nil {
		return nil
	}
	if h.referenceAssets == nil {
		return service.ErrReferenceAssetUnavailable
	}
	assetID, err := h.referenceAssets.ResolveSelection(ctx, userID, *req.PortraitReferenceImage, []string{service.DirectUploadPurposeProjectPortraitReference})
	if err != nil {
		return err
	}
	req.PortraitReferenceImageAssetID = assetID
	return nil
}

func (h *ProjectHandler) presentProjectReference(ctx context.Context, userID string, ch *model.Project) error {
	if ch == nil {
		return nil
	}
	view, err := h.projectReferenceView(ctx, userID, ch.ReferenceImageAssetID)
	if err != nil {
		return err
	}
	ch.ReferenceImage = view
	portraitView, err := h.projectPortraitReferenceView(ctx, userID, ch.PortraitReferenceImageAssetID)
	if err != nil {
		return err
	}
	ch.PortraitReferenceImage = portraitView
	return nil
}

func (h *ProjectHandler) projectReferenceView(ctx context.Context, userID, assetID string) (*model.AssetView, error) {
	if h.referenceAssets == nil {
		if assetID == "" {
			return nil, nil
		}
		return nil, service.ErrReferenceAssetUnavailable
	}
	return h.referenceAssets.Present(ctx, userID, assetID, []string{service.DirectUploadPurposeProjectReference})
}

func (h *ProjectHandler) projectPortraitReferenceView(ctx context.Context, userID, assetID string) (*model.AssetView, error) {
	if h.referenceAssets == nil {
		if assetID == "" {
			return nil, nil
		}
		return nil, service.ErrReferenceAssetUnavailable
	}
	return h.referenceAssets.Present(ctx, userID, assetID, []string{service.DirectUploadPurposeProjectPortraitReference})
}

func (h *ProjectHandler) respondProjectUpdateError(c fiber.Ctx, projectID string, err error) error {
	if errors.Is(err, service.ErrProjectNotFound) {
		return Error(c, fiber.StatusNotFound, "project not found")
	}
	if errors.Is(err, service.ErrProjectOwnedByUser) {
		return Forbidden(c, "you do not have access to this project")
	}
	if errors.Is(err, service.ErrProjectUpdateConflict) {
		return Error(c, fiber.StatusConflict, "project reference changed concurrently; retry the update")
	}
	if errors.Is(err, service.ErrProjectMontageDefaults) {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if errors.Is(err, service.ErrInvalidAgentConfig) {
		return Error(c, fiber.StatusBadRequest, "invalid_agent_config: "+err.Error())
	}
	if errors.Is(err, service.ErrInvalidWechatPublishMode) || errors.Is(err, service.ErrWechatCredentialsRequired) {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	h.logger.Error().Err(err).Str("project_id", projectID).Msg("update project failed")
	return Error(c, fiber.StatusInternalServerError, "failed to update project")
}

// SetSeednoteClient injects the Seednote SDK client.
func (h *ProjectHandler) SetSeednoteClient(client *seednote.Client) {
	h.seednoteClient = client
}

// SetSeednoteReadiness injects optional sidecar readiness state.
func (h *ProjectHandler) SetSeednoteReadiness(readiness service.Readiness) {
	h.seednoteReady = readiness
}

func (h *ProjectHandler) ensureSeednoteReady(c fiber.Ctx) (bool, error) {
	if h.seednoteClient == nil {
		return false, Error(c, fiber.StatusServiceUnavailable, "Seednote sidecar 未配置")
	}
	if h.seednoteReady != nil && !h.seednoteReady.Ready() {
		return false, Error(c, fiber.StatusServiceUnavailable, "Seednote sidecar 暂不可用，正在后台连接")
	}
	return true, nil
}

// projectRequest is the shared request body for creating and updating a project.
type projectRequest struct {
	Platform                      string                           `json:"platform"`
	Name                          string                           `json:"name"`
	ProfileURL                    string                           `json:"profile_url"`
	AvatarURL                     string                           `json:"avatar_url"`
	Positioning                   string                           `json:"positioning"`
	Keywords                      string                           `json:"keywords"`
	VisualStyle                   string                           `json:"visual_style"`
	Writer                        string                           `json:"writer"`
	Theme                         string                           `json:"theme"`
	Author                        string                           `json:"author"`
	ReferenceImage                *service.ReferenceImageSelection `json:"reference_image"`
	ReferenceImageSet             bool                             `json:"-"`
	ReferenceImageAssetID         string                           `json:"-"`
	PortraitReferenceImage        *service.ReferenceImageSelection `json:"portrait_reference_image"`
	PortraitReferenceImageSet     bool                             `json:"-"`
	PortraitReferenceImageAssetID string                           `json:"-"`
	ImageRatio                    string                           `json:"image_ratio"`
	MaxConcurrentTasks            int                              `json:"max_concurrent_tasks"`
	Instructions                  string                           `json:"instructions"`
	InstructionsSet               bool                             `json:"-"`
	EcommerceDefaults             *model.EcommerceProjectDefaults  `json:"ecommerce_defaults,omitempty"`
	MontageDefaults               *model.MontageDefaults           `json:"montage_defaults,omitempty"`
	AgentConfig                   map[string]any                   `json:"agent_config,omitempty"`
	AgentConfigSet                bool                             `json:"-"`
	// Config fields for platform-specific credentials.
	WechatAppID       string `json:"wechat_app_id"`
	WechatSecret      string `json:"wechat_secret"`
	WechatPublishMode string `json:"wechat_publish_mode"`
}

func hasJSONField(body []byte, field string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	_, ok := raw[field]
	return ok
}

// toProject converts a request to a Project model.
func (req *projectRequest) toProject() *model.Project {
	instructions := req.Instructions
	instructionsSet := req.InstructionsSet
	if instructions == "" && req.Positioning != "" {
		instructions = req.Positioning
		instructionsSet = true
	}
	p := &model.Project{
		Platform:                      req.Platform,
		Name:                          req.Name,
		ProfileURL:                    req.ProfileURL,
		AvatarURL:                     req.AvatarURL,
		Keywords:                      req.Keywords,
		VisualStyle:                   req.VisualStyle,
		Writer:                        req.Writer,
		Theme:                         req.Theme,
		Author:                        req.Author,
		ReferenceImageAssetID:         req.ReferenceImageAssetID,
		ReferenceImageSet:             req.ReferenceImageSet,
		PortraitReferenceImageAssetID: req.PortraitReferenceImageAssetID,
		PortraitReferenceImageSet:     req.PortraitReferenceImageSet,
		ImageRatio:                    req.ImageRatio,
		MaxConcurrentTasks:            req.MaxConcurrentTasks,
		Instructions:                  instructions,
		InstructionsSet:               instructionsSet,
		Config:                        model.ProjectConfig{WechatAppID: req.WechatAppID, WechatSecret: req.WechatSecret, WechatPublishMode: req.WechatPublishMode},
	}
	if req.EcommerceDefaults != nil {
		p.SetEcommerceDefaults(*req.EcommerceDefaults)
		p.EcommerceDefaultsSet = true
	}
	if req.MontageDefaults != nil {
		p.SetMontageDefaults(*req.MontageDefaults)
		p.MontageDefaultsSet = true
	}
	if req.AgentConfigSet {
		p.SetAgentConfig(req.AgentConfig)
	}
	return p
}

// List handles GET /projects.
func (h *ProjectHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	status := c.Query("status", "")
	platform := c.Query("platform", "")

	projects, err := h.service.List(c.Context(), userID, repository.ProjectListOptions{
		Status:   status,
		Platform: platform,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("list projects failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list projects")
	}
	if !projectUserIsAdmin(c) {
		visible := projects[:0]
		for _, ch := range projects {
			if !model.IsAdminOnlyProjectPlatform(ch.Platform) {
				visible = append(visible, ch)
			}
		}
		projects = visible
	}

	// Sanitize all projects before returning.
	for _, ch := range projects {
		h.service.SanitizeProjectForResponse(ch)
		h.signProjectURLs(c.Context(), ch)
		if err := h.presentProjectReference(c.Context(), userID, ch); err != nil {
			return respondReferenceAssetError(c, h.logger, err)
		}
	}

	return Success(c, projects)
}

// Stats handles GET /projects/stats?ids=a,b,c and returns a per-project stats map.
func (h *ProjectHandler) Stats(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	idsParam := strings.TrimSpace(c.Query("ids", ""))
	if idsParam == "" {
		return Success(c, fiber.Map{})
	}

	projects, err := h.service.List(c.Context(), userID, repository.ProjectListOptions{})
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("list projects for stats failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list projects")
	}

	allowed := make(map[string]struct{}, len(projects))
	for _, ch := range projects {
		if !projectUserIsAdmin(c) && model.IsAdminOnlyProjectPlatform(ch.Platform) {
			continue
		}
		allowed[ch.ID] = struct{}{}
	}

	projectIDs := make([]string, 0, len(projects))
	for _, rawID := range strings.Split(idsParam, ",") {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, ok := allowed[id]; ok {
			projectIDs = append(projectIDs, id)
		}
	}

	stats, err := h.service.BatchStats(c.Context(), projectIDs)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("get project stats failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get project stats")
	}

	return Success(c, stats)
}

// Create handles POST /projects.
func (h *ProjectHandler) Create(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	if err := rejectRemovedRequestFields(c.Body()); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	var req projectRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if hasJSONField(c.Body(), "instructions") {
		req.InstructionsSet = true
	}
	req.ReferenceImageSet = hasJSONField(c.Body(), "reference_image")
	req.PortraitReferenceImageSet = hasJSONField(c.Body(), "portrait_reference_image")
	req.AgentConfigSet = hasJSONField(c.Body(), "agent_config")

	if req.Platform == "" {
		return Error(c, fiber.StatusBadRequest, "platform is required")
	}
	if model.IsAdminOnlyProjectPlatform(req.Platform) && !projectUserIsAdmin(c) {
		return Forbidden(c, "project platform is currently available to administrators only")
	}

	// Validate required fields per platform.
	pc := model.GetPlatformConfig(req.Platform)
	if pc != nil {
		for _, field := range pc.Fields {
			if !field.Required {
				continue
			}
			val := req.getFieldValue(field.Key)
			if val == "" {
				return Error(c, fiber.StatusBadRequest, field.Label+" is required")
			}
		}
	}

	if err := validateProjectImageRatio(req.Platform, req.ImageRatio); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}

	// 作者署名不得是写作风格的人设名/key（二者语义不同，混用会把模仿对象当成发布作者）。
	if err := service.RejectWriterNameAsAuthor(req.Author); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}

	if err := h.resolveProjectReference(c.Context(), userID, &req); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	if err := h.resolveProjectPortraitReference(c.Context(), userID, &req); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	if err := h.validateProjectImageCapability(c.Context(), userID, &req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid ecommerce image capability")
	}
	ch := req.toProject()
	referenceView, err := h.projectReferenceView(c.Context(), userID, ch.ReferenceImageAssetID)
	if err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	portraitReferenceView, err := h.projectPortraitReferenceView(c.Context(), userID, ch.PortraitReferenceImageAssetID)
	if err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}

	// Force max_concurrent_tasks based on user tier.
	ch.MaxConcurrentTasks = h.getTierMaxConcurrent(c)
	rewrites, err := finalizeUploadSessionURLs(c.Context(), h.store, h.uploadRepo, userID, service.DirectUploadPurposeProjectReference, []string{req.AvatarURL})
	if err != nil {
		return respondUploadSessionFinalizeError(c, h.logger, err)
	}
	req.AvatarURL = rewriteFinalizedUploadURL(req.AvatarURL, rewrites)
	ch.AvatarURL = req.AvatarURL

	created, err := h.service.Create(c.Context(), userID, ch)
	if err != nil {
		if errors.Is(err, service.ErrProjectMontageDefaults) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if errors.Is(err, service.ErrInvalidAgentConfig) {
			return Error(c, fiber.StatusBadRequest, "invalid_agent_config: "+err.Error())
		}
		if errors.Is(err, service.ErrInvalidWechatPublishMode) || errors.Is(err, service.ErrWechatCredentialsRequired) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create project failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create project: "+err.Error())
	}

	h.service.SanitizeProjectForResponse(created)
	h.signProjectURLs(c.Context(), created)
	created.ReferenceImage = referenceView
	created.PortraitReferenceImage = portraitReferenceView

	// For Seednote projects, include recommended templates.
	recommended := []templateResponse{}
	if created.Platform == model.PlatformSeednote && h.templateSvc != nil {
		if rec := h.getRecommendedTemplates(c.Context(), created); rec != nil {
			// Sign each recommended template's image URLs so the cross-account
			// signed-direct path covers them too (these are public templates the
			// viewer may not have uploaded).
			for _, t := range rec {
				service.SignTemplateURLs(c.Context(), h.store, h.logger, t)
				recommended = append(recommended, canonicalTemplateResponse(t))
			}
		}
	}

	return Success(c, fiber.Map{
		"project":               created,
		"recommended_templates": recommended,
	})
}

// Get handles GET /projects/:id.
func (h *ProjectHandler) Get(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	projectID := c.Params("id")
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project id is required")
	}

	ch, stats, err := h.service.Get(c.Context(), userID, projectID)
	if err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			return Error(c, fiber.StatusNotFound, "project not found")
		}
		if errors.Is(err, service.ErrProjectOwnedByUser) {
			return Forbidden(c, "you do not have access to this project")
		}
		h.logger.Error().Err(err).Str("project_id", projectID).Msg("get project failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get project")
	}
	if !projectPlatformIsVisibleToUser(c, ch.Platform) {
		return Forbidden(c, "project platform is currently available to administrators only")
	}

	h.service.SanitizeProjectForResponse(ch)
	h.signProjectURLs(c.Context(), ch)
	if err := h.presentProjectReference(c.Context(), userID, ch); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	return Success(c, fiber.Map{
		"project": ch,
		"stats":   stats,
	})
}

// Memory handles GET /projects/:id/memory.
func (h *ProjectHandler) Memory(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	projectID := c.Params("id")
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project id is required")
	}
	if _, _, err := h.service.Get(c.Context(), userID, projectID); err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			return Error(c, fiber.StatusNotFound, "project not found")
		}
		if errors.Is(err, service.ErrProjectOwnedByUser) {
			return Forbidden(c, "you do not have access to this project")
		}
		h.logger.Error().Err(err).Str("project_id", projectID).Msg("verify project memory ownership failed")
		return Error(c, fiber.StatusInternalServerError, "failed to load project memory")
	}
	if h.projectMemory == nil {
		return Error(c, fiber.StatusServiceUnavailable, "project memory is unavailable")
	}
	view, err := h.projectMemory.ReadProject(c.Context(), projectID)
	if err != nil {
		h.logger.Error().Err(err).Str("project_id", projectID).Msg("read project memory failed")
		return Error(c, fiber.StatusServiceUnavailable, "project memory is unavailable")
	}
	body, err := marshalBoundedProjectMemoryResponse(view)
	if err != nil {
		h.logger.Error().Err(err).Str("project_id", projectID).Msg("encode project memory failed")
		return Error(c, fiber.StatusServiceUnavailable, "project memory is unavailable")
	}
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
	return c.Send(body)
}

func marshalBoundedProjectMemoryResponse(view projectmemory.ProjectView) ([]byte, error) {
	marshal := func() ([]byte, error) {
		return json.Marshal(Response{Code: 0, Msg: "success", Data: view})
	}
	body, err := marshal()
	if err != nil || len(body) <= projectMemoryMaxResponseBytes {
		return body, err
	}

	view.Partial = true
	for index := len(view.Files) - 1; index >= 0 && len(body) > projectMemoryMaxResponseBytes; index-- {
		content := view.Files[index].Content
		if content == "" {
			continue
		}
		removeBytes := min(len(content), len(body)-projectMemoryMaxResponseBytes)
		keepBytes := len(content) - removeBytes
		for keepBytes > 0 && !utf8.ValidString(content[:keepBytes]) {
			keepBytes--
		}
		view.Files[index].Content = content[:keepBytes]
		view.Files[index].Truncated = true
		body, err = marshal()
		if err != nil {
			return nil, err
		}
	}
	if len(body) > projectMemoryMaxResponseBytes {
		return nil, fmt.Errorf("project memory response metadata exceeds %d bytes", projectMemoryMaxResponseBytes)
	}
	return body, nil
}

// Update handles PUT /projects/:id.
func (h *ProjectHandler) Update(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	projectID := c.Params("id")
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project id is required")
	}

	if err := rejectRemovedRequestFields(c.Body()); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	var req projectRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if hasJSONField(c.Body(), "instructions") {
		req.InstructionsSet = true
	}
	req.ReferenceImageSet = hasJSONField(c.Body(), "reference_image")
	req.PortraitReferenceImageSet = hasJSONField(c.Body(), "portrait_reference_image")
	req.AgentConfigSet = hasJSONField(c.Body(), "agent_config")
	if !projectPlatformIsVisibleToUser(c, req.Platform) {
		return Forbidden(c, "project platform is currently available to administrators only")
	}

	// 作者署名不得是写作风格的人设名/key（二者语义不同，混用会把模仿对象当成发布作者）。
	if err := service.RejectWriterNameAsAuthor(req.Author); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}

	current, _, err := h.service.Get(c.Context(), userID, projectID)
	if err != nil {
		return h.respondProjectUpdateError(c, projectID, err)
	}
	if !projectPlatformIsVisibleToUser(c, current.Platform) {
		return Forbidden(c, "project platform is currently available to administrators only")
	}
	targetPlatform := current.Platform
	if req.Platform != "" {
		targetPlatform = req.Platform
	}
	if err := validateProjectImageRatio(targetPlatform, req.ImageRatio); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	targetReferenceAssetID := current.ReferenceImageAssetID
	targetPortraitReferenceAssetID := current.PortraitReferenceImageAssetID
	if err := h.resolveProjectReference(c.Context(), userID, &req); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	if err := h.resolveProjectPortraitReference(c.Context(), userID, &req); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	if err := h.validateProjectImageCapability(c.Context(), userID, &req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid ecommerce image capability")
	}
	if req.ReferenceImageSet {
		targetReferenceAssetID = req.ReferenceImageAssetID
	}
	if req.PortraitReferenceImageSet {
		targetPortraitReferenceAssetID = req.PortraitReferenceImageAssetID
	}
	referenceView, err := h.projectReferenceView(c.Context(), userID, targetReferenceAssetID)
	if err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	ch := req.toProject()
	ch.PortraitReferenceImageAssetID = targetPortraitReferenceAssetID
	ch.PortraitReferenceImageSet = req.PortraitReferenceImageSet

	// Force max_concurrent_tasks based on user tier.
	ch.MaxConcurrentTasks = h.getTierMaxConcurrent(c)
	rewrites, err := finalizeUploadSessionURLs(c.Context(), h.store, h.uploadRepo, userID, service.DirectUploadPurposeProjectReference, []string{req.AvatarURL})
	if err != nil {
		return respondUploadSessionFinalizeError(c, h.logger, err)
	}
	req.AvatarURL = rewriteFinalizedUploadURL(req.AvatarURL, rewrites)
	ch.AvatarURL = req.AvatarURL

	var updated *model.Project
	if req.ReferenceImageSet {
		updated, err = h.service.Update(c.Context(), userID, projectID, ch)
	} else {
		const maxReferenceCASAttempts = 3
		for attempt := 0; attempt < maxReferenceCASAttempts; attempt++ {
			updated, err = h.service.UpdateIfReferenceImageAssetID(c.Context(), userID, projectID, ch, targetReferenceAssetID)
			if !errors.Is(err, service.ErrProjectUpdateConflict) {
				break
			}
			if attempt == maxReferenceCASAttempts-1 {
				break
			}
			current, _, err = h.service.Get(c.Context(), userID, projectID)
			if err != nil {
				break
			}
			targetReferenceAssetID = current.ReferenceImageAssetID
			targetPortraitReferenceAssetID = current.PortraitReferenceImageAssetID
			ch.PortraitReferenceImageAssetID = targetPortraitReferenceAssetID
			referenceView, err = h.projectReferenceView(c.Context(), userID, targetReferenceAssetID)
			if err != nil {
				return respondReferenceAssetError(c, h.logger, err)
			}
		}
	}
	if err != nil {
		return h.respondProjectUpdateError(c, projectID, err)
	}

	h.service.SanitizeProjectForResponse(updated)
	h.signProjectURLs(c.Context(), updated)
	updated.ReferenceImage = referenceView
	updated.PortraitReferenceImage, err = h.projectPortraitReferenceView(c.Context(), userID, updated.PortraitReferenceImageAssetID)
	if err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	return Success(c, updated)
}

// Archive handles PATCH /projects/:id/archive.
func (h *ProjectHandler) Archive(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	projectID := c.Params("id")
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project id is required")
	}

	if err := h.service.Archive(c.Context(), userID, projectID); err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			return Error(c, fiber.StatusNotFound, "project not found")
		}
		if errors.Is(err, service.ErrProjectOwnedByUser) {
			return Forbidden(c, "you do not have access to this project")
		}
		h.logger.Error().Err(err).Str("project_id", projectID).Msg("archive project failed")
		return Error(c, fiber.StatusInternalServerError, "failed to archive project")
	}

	return Success(c, fiber.Map{"message": "project archived"})
}

// Restore handles PATCH /projects/:id/restore.
func (h *ProjectHandler) Restore(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	projectID := c.Params("id")
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project id is required")
	}

	if err := h.service.Restore(c.Context(), userID, projectID); err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			return Error(c, fiber.StatusNotFound, "project not found")
		}
		if errors.Is(err, service.ErrProjectOwnedByUser) {
			return Forbidden(c, "you do not have access to this project")
		}
		h.logger.Error().Err(err).Str("project_id", projectID).Msg("restore project failed")
		return Error(c, fiber.StatusInternalServerError, "failed to restore project")
	}

	return Success(c, fiber.Map{"message": "project restored"})
}

// Delete handles DELETE /projects/:id.
func (h *ProjectHandler) Delete(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	projectID := c.Params("id")
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project id is required")
	}

	if err := h.service.Delete(c.Context(), userID, projectID); err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			return Error(c, fiber.StatusNotFound, "project not found")
		}
		if errors.Is(err, service.ErrProjectOwnedByUser) {
			return Forbidden(c, "you do not have access to this project")
		}
		if errors.Is(err, service.ErrProjectDeleteConflict) {
			return Error(c, fiber.StatusConflict, err.Error())
		}
		h.logger.Error().Err(err).Str("project_id", projectID).Msg("delete project failed")
		return Error(c, fiber.StatusInternalServerError, "failed to delete project")
	}

	return Success(c, fiber.Map{"message": "project deleted"})
}

// GetPlatformConfigs handles GET /projects/platform-configs.
func (h *ProjectHandler) GetPlatformConfigs(c fiber.Ctx) error {
	configs := model.GetAllPlatformConfigs()
	if projectUserIsAdmin(c) {
		return Success(c, configs)
	}

	visible := make([]*model.PlatformConfig, 0, len(configs))
	for _, config := range configs {
		if !model.IsAdminOnlyProjectPlatform(config.ID) {
			visible = append(visible, config)
		}
	}
	return Success(c, visible)
}

// fetchProfileRequest is the request body for fetching a platform profile.
type fetchProfileRequest struct {
	Platform   string `json:"platform"`
	ProfileURL string `json:"profile_url"`
	// Optional WeChat credentials for article platforms.
	WechatAppID  string `json:"wechat_app_id"`
	WechatSecret string `json:"wechat_secret"`
}

// FetchProfile handles POST /projects/fetch-profile.
// It fetches profile data from the specified platform using the profile URL.
func (h *ProjectHandler) FetchProfile(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req fetchProfileRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.Platform == "" {
		return Error(c, fiber.StatusBadRequest, "platform is required")
	}
	if req.ProfileURL == "" {
		return Error(c, fiber.StatusBadRequest, "profile_url is required")
	}

	if req.Platform != model.PlatformSeednote {
		return Error(c, fiber.StatusBadRequest, "profile auto-fetch is only available for Seednote")
	}
	if ready, err := h.ensureSeednoteReady(c); !ready || err != nil {
		return err
	}

	// Validate URL matches the platform's expected pattern.
	pc := model.GetPlatformConfig(req.Platform)
	if pc != nil && pc.ProfileURLPattern != "" {
		matched, _ := regexp.MatchString(pc.ProfileURLPattern, req.ProfileURL)
		if !matched {
			re, err := regexp.Compile(pc.ProfileURLPattern)
			if err != nil {
				h.logger.Error().Err(err).Str("platform", req.Platform).Msg("invalid profile URL pattern")
				return Error(c, fiber.StatusInternalServerError, "invalid platform profile URL configuration")
			}
			matched = re.FindString(req.ProfileURL) != ""
			if !matched {
				return Error(c, fiber.StatusBadRequest, "profile URL does not match expected pattern for "+pc.Label)
			}
		}
	}

	// Only pass credentials for publishing platforms.
	var appID, secret string
	if pc != nil && pc.SupportsPublishing {
		appID = req.WechatAppID
		secret = req.WechatSecret
	}

	provider := platform.NewProvider(req.Platform, appID, secret, h.seednoteClient)
	if provider == nil {
		return Error(c, fiber.StatusBadRequest, "unsupported platform: "+req.Platform)
	}

	profile, err := provider.FetchProfile(c.Context(), req.ProfileURL)
	if err != nil {
		h.logger.Error().Err(err).
			Str("platform", req.Platform).
			Str("profile_url", req.ProfileURL).
			Msg("fetch profile failed")
		return Error(c, fiber.StatusInternalServerError, "failed to fetch profile: "+err.Error())
	}
	return Success(c, profile)
}

// getRecommendedTemplates returns templates recommended for a project based on its profile keywords.
func (h *ProjectHandler) getRecommendedTemplates(ctx context.Context, ch *model.Project) []*model.Template {
	if ch.Keywords == "" {
		return nil
	}

	// Extract category from keywords (first keyword as category hint) and parse all tags.
	tags := splitKeywords(ch.Keywords)
	category := ""
	if len(tags) > 0 {
		category = tags[0]
	}

	templates, err := h.templateSvc.GetRecommended(ctx, category, tags, 5)
	if err != nil {
		h.logger.Warn().Err(err).Str("project_id", ch.ID).Msg("failed to get recommended templates")
		return nil
	}
	return templates
}

// splitKeywords splits a comma-separated keywords string into individual non-empty tags.
func splitKeywords(keywords string) []string {
	parts := strings.Split(keywords, ",")
	tags := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			tags = append(tags, p)
		}
	}
	return tags
}

// getFieldValue returns the value of a field by key from the request.
func (req *projectRequest) getFieldValue(key string) string {
	switch key {
	case "platform":
		return req.Platform
	case "name":
		return req.Name
	case "profile_url":
		return req.ProfileURL
	case "avatar_url":
		return req.AvatarURL
	case "positioning":
		if req.Instructions != "" {
			return req.Instructions
		}
		return req.Positioning
	case "instructions":
		return req.Instructions
	case "keywords":
		return req.Keywords
	case "visual_style":
		return req.VisualStyle
	case "writer":
		return req.Writer
	case "theme":
		return req.Theme
	case "author":
		return req.Author
	case "image_ratio":
		return req.ImageRatio
	case "max_concurrent_tasks":
		return fmt.Sprintf("%d", req.MaxConcurrentTasks)
	case "wechat_app_id":
		return req.WechatAppID
	case "wechat_secret":
		return req.WechatSecret
	case "wechat_publish_mode":
		return req.WechatPublishMode
	default:
		return ""
	}
}

// getTierMaxConcurrent reads the user's tier from auth middleware locals
// and returns the max concurrent tasks limit for that tier.
func (h *ProjectHandler) getTierMaxConcurrent(c fiber.Ctx) int {
	user, _ := c.Locals("user").(*model.User)
	tier := model.TierFree
	if user != nil {
		tier = model.ResolveTier(user.Tier)
	}
	return model.GetTierMaxConcurrentTasks(tier)
}

func projectUserIsAdmin(c fiber.Ctx) bool {
	user, _ := c.Locals("user").(*model.User)
	return user != nil && user.IsAdmin
}

func projectPlatformIsVisibleToUser(c fiber.Ctx, platform string) bool {
	return projectUserIsAdmin(c) || !model.IsAdminOnlyProjectPlatform(platform)
}

func validateProjectImageRatio(platform, ratio string) error {
	ratio = strings.TrimSpace(ratio)
	if ratio == "" {
		return nil
	}
	if !model.ValidImageRatios[ratio] {
		return errors.New(model.ValidImageRatioHint)
	}
	if len(model.SupportedImageRatios(platform)) == 0 {
		return fmt.Errorf("image_ratio is not supported for platform %s", platform)
	}
	if !model.IsBusinessImageRatioAllowed(platform, ratio) {
		return errors.New(model.ValidImageRatioHint)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Image style analysis
// ---------------------------------------------------------------------------

type analyzeImageRequest struct {
	ImageURL string `json:"image_url"`
}

func requireProjectAdmin(c fiber.Ctx) (bool, error) {
	userID := GetUserID(c)
	if userID == "" {
		return false, Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if !projectUserIsAdmin(c) {
		return false, Forbidden(c, "administrator access required")
	}
	return true, nil
}

// AdminSeednoteLoginStatus handles GET /seednote/account/login-status.
func (h *ProjectHandler) AdminSeednoteLoginStatus(c fiber.Ctx) error {
	if ok, err := requireProjectAdmin(c); !ok {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-store")

	if h.seednoteClient == nil {
		return Success(c, fiber.Map{
			"available": false,
			"logged_in": false,
			"message":   "Seednote sidecar 未配置",
		})
	}
	if h.seednoteReady != nil && !h.seednoteReady.Ready() {
		return Success(c, fiber.Map{
			"available": false,
			"logged_in": false,
			"message":   "Seednote sidecar 暂不可用，正在后台连接",
		})
	}

	loggedIn, err := h.seednoteClient.CheckLoginStatus(c.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("check Seednote login status failed")
		return Success(c, fiber.Map{
			"available": false,
			"logged_in": false,
			"message":   "小红书登录状态检查失败",
		})
	}

	msg := "已登录"
	if !loggedIn {
		msg = "未登录，请获取二维码并使用小红书客户端扫码"
	}
	return Success(c, fiber.Map{
		"available": true,
		"logged_in": loggedIn,
		"message":   msg,
	})
}

// AdminSeednoteLoginQRCode handles GET /seednote/account/login-qrcode.
func (h *ProjectHandler) AdminSeednoteLoginQRCode(c fiber.Ctx) error {
	if ok, err := requireProjectAdmin(c); !ok {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	if ready, err := h.ensureSeednoteReady(c); !ready {
		return err
	}

	qrcodeImage, err := h.seednoteClient.GetLoginQRCode(c.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("get Seednote login qrcode failed")
		return Error(c, fiber.StatusBadGateway, "获取小红书登录二维码失败")
	}
	if strings.TrimSpace(qrcodeImage) == "" {
		return Error(c, fiber.StatusBadGateway, "小红书登录二维码为空")
	}
	return Success(c, fiber.Map{"qrcode_image": qrcodeImage})
}

// AdminSeednoteLogout handles DELETE /seednote/account/login.
func (h *ProjectHandler) AdminSeednoteLogout(c fiber.Ctx) error {
	if ok, err := requireProjectAdmin(c); !ok {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	if ready, err := h.ensureSeednoteReady(c); !ready {
		return err
	}

	if err := h.seednoteClient.DeleteCookies(c.Context()); err != nil {
		h.logger.Error().Err(err).Msg("delete Seednote login cookies failed")
		return Error(c, fiber.StatusBadGateway, "退出小红书登录失败")
	}
	return Success(c, fiber.Map{"logged_in": false})
}

// AnalyzeImage handles POST /projects/analyze-image.
// It sends the reference image to a vision LLM and returns a visual style description.
func (h *ProjectHandler) AnalyzeImage(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req analyzeImageRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.ImageURL == "" {
		return Error(c, fiber.StatusBadRequest, "image_url is required")
	}

	llm := h.visionClient
	if llm == nil {
		return Error(c, fiber.StatusServiceUnavailable, "image understanding model is not configured")
	}

	const maxAnalysisImageSize = 10 << 20 // 10 MB

	var imageData []byte

	// Server-owned URLs (Local /api/v1/files/* or OSS bucket / custom domain):
	// read via store.Read to avoid SSRF, signed-URL expiry, and private-bucket
	// 403s. OSSProvider.Read signs the URL internally; LocalProvider.Read hits
	// disk. Truly external URLs (e.g. Unsplash) fall through to getPublicHTTPSImage.
	ownedPath := h.store != nil && h.store.IsOwnedURL(req.ImageURL)
	h.logger.Debug().
		Str("image_url", req.ImageURL).
		Bool("owned", ownedPath).
		Msg("analyze-image routing")
	if ownedPath {
		key, respondErr := h.cleanAnalysisImageKey(c.Context(), req.ImageURL, userID)
		if respondErr != nil {
			return respondErr(c)
		}
		data, err := h.store.Read(c.Context(), key)
		if err != nil {
			h.logger.Error().Err(err).Str("key", key).Msg("failed to read image for analysis")
			return Error(c, fiber.StatusInternalServerError, "failed to read image file")
		}
		imageData = data
	} else if strings.HasPrefix(req.ImageURL, "https://") {
		resp, err := getPublicHTTPSImage(c.Context(), req.ImageURL, maxAnalysisImageSize)
		if err != nil {
			h.logger.Error().Err(err).Str("url", req.ImageURL).Msg("failed to download external image")
			return Error(c, fiber.StatusBadRequest, "failed to download image")
		}
		imageData = resp
	} else {
		return Error(c, fiber.StatusBadRequest, "image_url must be a valid file URL")
	}

	if int64(len(imageData)) > maxAnalysisImageSize {
		return Error(c, fiber.StatusBadRequest, "image is too large for analysis (max 10MB)")
	}

	mimeType := http.DetectContentType(imageData)
	if !strings.HasPrefix(mimeType, "image/") {
		return Error(c, fiber.StatusBadRequest, "the file is not an image")
	}
	imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(imageData))

	systemPrompt := "你是一位专业的小红书视觉风格分析师，擅长把参考图提炼成可直接用于 AI 图片生成的中文风格指令。"
	userPrompt := `请分析这张图片，生成一段详细的中文风格指令，用于指导 AI 生成与图中相同风格的小红书图片。

需要覆盖的维度（按重要性排序）：
- 整体氛围与艺术流派（治愈系水彩 / 极简日系 / 复古胶片 / 二次元插画等）
- 色彩色调：主色调、饱和度高低、明暗对比、冷暖倾向
- 画面质感与笔触：手绘感 / 磨砂颗粒 / 柔焦 / 油画厚涂 / 数码平滑
- 构图手法与视角：留白比例、俯拍/平视、对称/不对称、镜头焦段感
- 光影特征：自然光 / 侧逆光 / 平光 / 戏剧化光影 / 漫反射
- 信息密度与节奏：画面是极简留白还是元素密集叠加；主体与背景的层次关系；视觉焦点的强弱

输出要求：
- 按上述六个维度逐行输出，每行一个维度，格式为「维度名：具体描述」（例如「色彩色调：暖色调为主，低饱和，柔和明暗对比」）
- 维度名使用：整体氛围、色彩色调、画面质感、构图手法、光影特征、信息密度
- 必须输出全部 6 行；若某维度在图中不显著，仍需保留维度名并填入「不适用」或最接近的描述，不得跳过
- 每行聚焦一个维度，避免重复，信息密度高、避免空话套话（如"精美"、"好看"这类无信息量形容词）
- 信息类型为「风格指令」而非「画面描述」：不要描述图中具体的物体、人物、文字、品牌或场景
- 不加总标题或前后缀说明，六行之间用换行分隔，直接输出风格指令文本`

	result, err := llm.CompleteWithImage(c.Context(), systemPrompt, userPrompt, imageURL)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			h.logger.Warn().Err(err).Str("user_id", userID).Msg("image style analysis canceled or timed out")
			return Error(c, fiber.StatusServiceUnavailable, "analysis canceled or timed out")
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("image style analysis failed")
		return Error(c, fiber.StatusInternalServerError, "image style analysis failed")
	}

	style := strings.TrimSpace(result)
	if style == "" {
		return Error(c, fiber.StatusInternalServerError, "failed to analyze image style")
	}

	return Success(c, fiber.Map{"visual_style": style})
}

func getPublicHTTPSImage(ctx context.Context, imageURL string, maxSize int64) ([]byte, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: publicOnlyDialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("external image redirects must use https")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if req.URL.Scheme != "https" || req.URL.Hostname() == "" || req.URL.User != nil {
		return nil, fmt.Errorf("invalid external image URL")
	}
	req.Header.Set("Accept", "image/*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download external image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download external image: unexpected status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxSize {
		return nil, fmt.Errorf("external image exceeds max size")
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read external image: %w", err)
	}
	if int64(len(data)) > maxSize {
		return nil, fmt.Errorf("external image exceeds max size")
	}
	return data, nil
}

func publicOnlyDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("host did not resolve")
	}
	for _, addr := range ips {
		if !isPublicIP(addr.IP) {
			return nil, fmt.Errorf("external image host resolves to a non-public address")
		}
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

func isPublicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsUnspecified() &&
		!ip.IsMulticast()
}

type fiberErrorFunc func(fiber.Ctx) error

func (h *ProjectHandler) cleanAnalysisImageKey(ctx context.Context, imageURL, userID string) (string, fiberErrorFunc) {
	if key, ok := storage.StorageKeyFromURL(imageURL); ok {
		cleanKey := filepath.Clean(key)
		if strings.HasPrefix(cleanKey, "uploads/pending/") {
			var sessions repository.UploadSessionRepository
			if h.uploadRepo != nil {
				sessions = h.uploadRepo.UploadSessions()
			}
			pendingKey, err := service.ValidateUploadSessionURL(ctx, sessions, userID, []string{
				service.DirectUploadPurposeProjectReference,
				service.DirectUploadPurposeTaskReference,
			}, imageURL, time.Now())
			if err != nil {
				return "", uploadSessionAnalyzeError(h.logger, err)
			}
			return pendingKey, nil
		}
	}
	return cleanOwnedUploadKey(imageURL, userID)
}

func uploadSessionAnalyzeError(logger *zerolog.Logger, err error) fiberErrorFunc {
	switch {
	case errors.Is(err, service.ErrUploadSessionInvalidURL):
		return func(c fiber.Ctx) error { return Error(c, fiber.StatusBadRequest, "image_url is invalid") }
	case errors.Is(err, service.ErrUploadSessionExpired):
		return func(c fiber.Ctx) error { return Error(c, fiber.StatusBadRequest, "pending upload has expired") }
	case errors.Is(err, service.ErrUploadSessionStateConflict):
		return func(c fiber.Ctx) error { return Error(c, fiber.StatusBadRequest, "pending upload is not pending") }
	case errors.Is(err, service.ErrUploadSessionAccessDenied):
		return func(c fiber.Ctx) error { return Forbidden(c, "you do not have access to this file") }
	default:
		if logger != nil {
			logger.Error().Err(err).Msg("failed to validate pending upload for image analysis")
		}
		return func(c fiber.Ctx) error {
			return Error(c, fiber.StatusInternalServerError, "failed to validate pending upload")
		}
	}
}

func cleanOwnedUploadKey(imageURL, userID string) (string, fiberErrorFunc) {
	key, ok := storage.StorageKeyFromURL(imageURL)
	if !ok || key == "" {
		return "", func(c fiber.Ctx) error { return Error(c, fiber.StatusBadRequest, "image_url is invalid") }
	}

	// filepath.Clean (not path.Clean) for parity with FileHandler.ServeFile.
	// Storage keys are POSIX-style forward-slash paths on both Linux servers
	// and OSS, so the OS-separator rewrite under non-POSIX builds is a no-op
	// in production.
	cleanKey := filepath.Clean(key)
	if strings.Contains(cleanKey, "..") {
		return "", func(c fiber.Ctx) error { return Error(c, fiber.StatusBadRequest, "image_url is invalid") }
	}
	// Align with FileHandler.ServeFile (file.go) ownership rules:
	if isUserOwnedStorageKey(userID, cleanKey) {
		return cleanKey, nil
	}
	return "", func(c fiber.Ctx) error { return Forbidden(c, "you do not have access to this file") }
}
