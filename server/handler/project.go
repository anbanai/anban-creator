package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/billing"
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
	tasks             *service.TaskService
	billingCatalog    *service.BillingCatalogService
	feedbackScheduler *service.FeedbackScheduler
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(svc *service.ProjectService, logger *zerolog.Logger) *ProjectHandler {
	return &ProjectHandler{service: svc, logger: logger}
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

func (h *ProjectHandler) SetTaskService(tasks *service.TaskService) { h.tasks = tasks }

func (h *ProjectHandler) SetBillingCatalogService(catalog *service.BillingCatalogService) {
	h.billingCatalog = catalog
}

// SetFeedbackScheduler wires the periodic feedback coordinator. The handler
// only exposes read-only status and an explicit cadence rerun; eligibility and
// queue creation remain owned by the scheduler.
func (h *ProjectHandler) SetFeedbackScheduler(scheduler *service.FeedbackScheduler) {
	h.feedbackScheduler = scheduler
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
	if errors.Is(err, service.ErrProjectMontageDefaults) || errors.Is(err, service.ErrHypitInput) || errors.Is(err, service.ErrProjectNameRequired) {
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
	VisualStyleSet                bool                             `json:"-"`
	Writer                        string                           `json:"writer"`
	Theme                         string                           `json:"theme"`
	Author                        string                           `json:"author"`
	AuthorSet                     bool                             `json:"-"`
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
	HypitDefaults                 *model.HypitDefaults             `json:"hypit_defaults,omitempty"`
	// Config fields for platform-specific credentials.
	WechatAppID             string  `json:"wechat_app_id"`
	WechatSecret            string  `json:"wechat_secret"`
	LegacyWechatPublishMode *string `json:"wechat_publish_mode"`
}

type projectConfigResponse struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Channel   string         `json:"channel,omitempty"`
	Config    map[string]any `json:"config"`
}

type projectConfigRequest struct {
	Config map[string]any `json:"config"`
}

func redactProjectConfigValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, item := range typed {
			lower := strings.ToLower(strings.TrimSpace(key))
			if strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "private_key") || lower == "app_key" {
				continue
			}
			redacted[key] = redactProjectConfigValue(item)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for i, item := range typed {
			redacted[i] = redactProjectConfigValue(item)
		}
		return redacted
	default:
		return value
	}
}

func redactedProjectConfig(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	redacted, ok := redactProjectConfigValue(value).(map[string]any)
	if !ok || redacted == nil {
		return map[string]any{}
	}
	return redacted
}

func projectChannelConfigResponse(config *model.ProjectChannelConfig) projectConfigResponse {
	return projectConfigResponse{ID: config.ID, ProjectID: config.ProjectID, Channel: config.Channel, Config: redactedProjectConfig(config.Config.Data())}
}

func (h *ProjectHandler) respondProjectConfigError(c fiber.Ctx, err error) error {
	if errors.Is(err, service.ErrProjectNotFound) {
		return Error(c, fiber.StatusNotFound, "project not found")
	}
	if errors.Is(err, service.ErrProjectOwnedByUser) {
		return Forbidden(c, "you do not have access to this project")
	}
	if errors.Is(err, service.ErrInvalidProjectChannel) {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	h.logger.Error().Err(err).Msg("project config operation failed")
	return Error(c, fiber.StatusInternalServerError, "project config operation failed")
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
		VisualStyleSet:                req.VisualStyleSet,
		Writer:                        req.Writer,
		Theme:                         req.Theme,
		Author:                        req.Author,
		AuthorSet:                     req.AuthorSet,
		ReferenceImageAssetID:         req.ReferenceImageAssetID,
		ReferenceImageSet:             req.ReferenceImageSet,
		PortraitReferenceImageAssetID: req.PortraitReferenceImageAssetID,
		PortraitReferenceImageSet:     req.PortraitReferenceImageSet,
		ImageRatio:                    req.ImageRatio,
		MaxConcurrentTasks:            req.MaxConcurrentTasks,
		Instructions:                  instructions,
		InstructionsSet:               instructionsSet,
	}
	if req.EcommerceDefaults != nil {
		p.SetEcommerceDefaults(*req.EcommerceDefaults)
		p.EcommerceDefaultsSet = true
	}
	if req.HypitDefaults != nil {
		p.SetHypitDefaults(*req.HypitDefaults)
		p.HypitDefaultsSet = true
	}
	if req.MontageDefaults != nil {
		p.SetMontageDefaults(*req.MontageDefaults)
		p.MontageDefaultsSet = true
	}
	return p
}

func (req *projectRequest) wechatChannelConfig() map[string]any {
	config := map[string]any{}
	if appID := strings.TrimSpace(req.WechatAppID); appID != "" {
		config["wechat_app_id"] = appID
	}
	if secret := strings.TrimSpace(req.WechatSecret); secret != "" {
		config["wechat_secret"] = secret
	}
	return config
}

func rejectProjectAgentConfig(body []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	for field := range fields {
		if strings.EqualFold(strings.TrimSpace(field), "agent_config") {
			return errors.New("agent_config is no longer supported; configure outputs on a plan")
		}
	}
	return nil
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

	items := make([]map[string]any, 0, len(projects))
	for _, project := range projects {
		items = append(items, projectAPIResponse(project))
	}
	return Success(c, items)
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
	if err := rejectProjectAgentConfig(c.Body()); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	var req projectRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.LegacyWechatPublishMode != nil {
		return Error(c, fiber.StatusBadRequest, "wechat_publish_mode is no longer supported; publishing is capability-driven")
	}
	if hasJSONField(c.Body(), "instructions") {
		req.InstructionsSet = true
	}
	req.AuthorSet = hasJSONField(c.Body(), "author")
	req.ReferenceImageSet = hasJSONField(c.Body(), "reference_image")
	req.VisualStyleSet = hasJSONField(c.Body(), "visual_style")
	req.PortraitReferenceImageSet = hasJSONField(c.Body(), "portrait_reference_image")

	if req.Platform != "" && model.IsAdminOnlyProjectPlatform(req.Platform) && !projectUserIsAdmin(c) {
		return Forbidden(c, "project platform is currently available to administrators only")
	}

	// Validate required fields per platform.
	pc := model.GetPlatformConfig(req.Platform)
	if pc != nil {
		for _, field := range pc.Fields {
			if !field.Required {
				continue
			}
			// Account credentials are owned by ProjectChannelConfig and are
			// submitted through that endpoint after the project exists.
			if field.Key == "wechat_app_id" || field.Key == "wechat_secret" {
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
		if errors.Is(err, service.ErrProjectMontageDefaults) || errors.Is(err, service.ErrHypitInput) || errors.Is(err, service.ErrProjectNameRequired) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create project failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create project: "+err.Error())
	}
	if ch.Platform == model.PlatformWechat && (strings.TrimSpace(req.WechatAppID) != "" || strings.TrimSpace(req.WechatSecret) != "") {
		if _, configErr := h.service.UpsertChannelConfig(c.Context(), userID, ch.ID, model.ChannelArticle, req.wechatChannelConfig()); configErr != nil {
			return h.respondProjectConfigError(c, configErr)
		}
	}

	h.service.SanitizeProjectForResponse(created)
	h.signProjectURLs(c.Context(), created)
	created.ReferenceImage = referenceView
	created.PortraitReferenceImage = portraitReferenceView

	// For Seednote projects, include recommended templates.
	recommended := []templateResponse{}
	if created.Platform == model.PlatformSeednote && h.templateSvc != nil {
		if rec := h.getRecommendedTemplates(c.Context(), created); rec != nil {
			for _, t := range rec {
				if h.referenceAssets != nil && t.ThumbnailAssetID != "" {
					thumbnail, presentErr := h.referenceAssets.Present(c.Context(), t.UserID, t.ThumbnailAssetID, []string{service.DirectUploadPurposeTemplateThumbnail})
					if presentErr != nil {
						h.logger.Warn().Err(presentErr).Str("template_id", t.ID).Msg("present recommended template thumbnail")
					} else {
						t.Thumbnail = thumbnail
					}
				}
				recommended = append(recommended, canonicalTemplateResponse(t))
			}
		}
	}
	profileInitialization := h.ensureProfileInitialization(c.Context(), userID, created)

	return Success(c, fiber.Map{
		"project":                projectAPIResponse(created),
		"recommended_templates":  recommended,
		"profile_initialization": profileInitialization,
	})
}

func (h *ProjectHandler) ensureProfileInitialization(ctx context.Context, userID string, project *model.Project) map[string]any {
	result := map[string]any{"status": "not_started"}
	if h.tasks == nil || project == nil || !profileInitializationSupported(project.Platform) {
		return result
	}
	profile, err := h.service.GetProfile(ctx, userID, project.ID)
	if err != nil {
		return map[string]any{"status": model.ProfileInitializationFailed, "error": err.Error()}
	}
	if strings.TrimSpace(profile.AnalysisTaskID) != "" {
		if existing, getErr := h.tasks.GetByID(ctx, profile.AnalysisTaskID); getErr == nil && existing.UserID == userID && existing.ProjectID == project.ID && (existing.Status == model.TaskStatusPending || existing.Status == model.TaskStatusRunning) {
			return map[string]any{"status": profile.InitializationStatus, "task_id": existing.ID, "revision": profile.Version}
		}
	}
	expectedVersion := profile.Version
	tasks, err := h.tasks.CreateManual(ctx, service.CreateManualParams{
		UserID: userID, ProjectID: project.ID, ExecutionProfile: "effective",
		AgentID: model.AgentIDProfileAnalysis, Channel: model.ChannelProfileAnalysis, TaskKind: model.TaskKindProfileAnalysis,
		Prompt:     project.ProfileURL,
		AgentInput: profileAgentInput(project, map[string]any{}, nil),
		Quantity:   1, ProfileAnalysisExpectedVersion: &expectedVersion,
	})
	if err != nil || len(tasks) == 0 {
		message := "profile initialization task was not created"
		if err != nil {
			message = err.Error()
		}
		h.logger.Warn().Err(err).Str("project_id", project.ID).Msg("profile initialization admission failed")
		return map[string]any{"status": model.ProfileInitializationFailed, "error": message, "revision": profile.Version}
	}
	profile, err = h.service.GetProfile(ctx, userID, project.ID)
	if err != nil {
		return map[string]any{"status": model.ProfileInitializationQueued, "task_id": tasks[0].ID, "revision": expectedVersion}
	}
	return map[string]any{"status": profile.InitializationStatus, "task_id": tasks[0].ID, "revision": profile.Version}
}

func profileInitializationSupported(platform string) bool {
	switch strings.TrimSpace(platform) {
	case model.PlatformWechat, model.PlatformSeednote, model.PlatformMoments:
		return true
	default:
		return false
	}
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
		"project": projectAPIResponse(ch),
		"stats":   stats,
	})
}

func (h *ProjectHandler) ListChannelConfigs(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	configs, err := h.service.ListChannelConfigs(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.respondProjectConfigError(c, err)
	}
	result := make([]projectConfigResponse, 0, len(configs))
	for _, config := range configs {
		result = append(result, projectChannelConfigResponse(config))
	}
	return Success(c, result)
}

func (h *ProjectHandler) GetChannelConfig(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	config, err := h.service.GetChannelConfig(c.Context(), userID, c.Params("id"), c.Params("channel"))
	if err != nil {
		return h.respondProjectConfigError(c, err)
	}
	if config == nil {
		return Error(c, fiber.StatusNotFound, "channel config not found")
	}
	return Success(c, projectChannelConfigResponse(config))
}

func (h *ProjectHandler) UpsertChannelConfig(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req projectConfigRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid channel config body")
	}
	config, err := h.service.UpsertChannelConfig(c.Context(), userID, c.Params("id"), c.Params("channel"), req.Config)
	if err != nil {
		return h.respondProjectConfigError(c, err)
	}
	return Success(c, projectChannelConfigResponse(config))
}

func (h *ProjectHandler) DeleteChannelConfig(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := h.service.DeleteChannelConfig(c.Context(), userID, c.Params("id"), c.Params("channel")); err != nil {
		return h.respondProjectConfigError(c, err)
	}
	return Success(c, fiber.Map{"deleted": true})
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

type profileUpdateRequest struct {
	Version int64                `json:"version"`
	Profile model.ProjectProfile `json:"profile"`
}

type profileDimensionRequest struct {
	Version          int64                  `json:"version"`
	ExpectedRevision *int64                 `json:"expected_revision"`
	Value            model.ProfileDimension `json:"value"`
	Content          map[string]any         `json:"content"`
}

func (h *ProjectHandler) GetProfile(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	profile, err := h.service.GetProfile(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.respondProfileError(c, err)
	}
	return Success(c, profile)
}

func (h *ProjectHandler) GetProfileRevisions(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	revisions, err := h.service.ProfileRevisions(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.respondProfileError(c, err)
	}
	return Success(c, revisions)
}

func (h *ProjectHandler) RestoreProfileRevision(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid revision restore body")
		}
	}
	var revision int64
	if _, err := fmt.Sscan(c.Params("revision"), &revision); err != nil || revision < 0 {
		return Error(c, fiber.StatusBadRequest, "invalid profile revision")
	}
	profile, err := h.service.RestoreProfileRevision(c.Context(), userID, c.Params("id"), revision, req.ExpectedRevision)
	if err != nil {
		return h.respondProfileError(c, err)
	}
	return Success(c, profile)
}

func (h *ProjectHandler) respondProfileError(c fiber.Ctx, err error) error {
	if errors.Is(err, service.ErrProjectNotFound) {
		return Error(c, fiber.StatusNotFound, "project not found")
	}
	if errors.Is(err, service.ErrProjectOwnedByUser) {
		return Forbidden(c, "you do not have access to this project")
	}
	if errors.Is(err, service.ErrProjectProfileUnsupportedPlatform) {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if errors.Is(err, service.ErrProjectProfileVersionConflict) {
		return Error(c, fiber.StatusConflict, err.Error())
	}
	if errors.Is(err, service.ErrProjectProfileAnalysisInProgress) {
		return Error(c, fiber.StatusConflict, err.Error())
	}
	if errors.Is(err, service.ErrProjectProfileResultAlreadyStored) {
		return Error(c, fiber.StatusConflict, err.Error())
	}
	h.logger.Error().Err(err).Msg("project profile request failed")
	return Error(c, fiber.StatusInternalServerError, "project profile request failed")
}

type profileAnalysisInput struct {
	ExecutionProfile string         `json:"execution_profile"`
	Answers          map[string]any `json:"answers"`
	Samples          []string       `json:"samples"`
}

func normalizeProfileAnalysisInput(req profileAnalysisInput) (profileAnalysisInput, error) {
	req.ExecutionProfile = firstNonEmpty(req.ExecutionProfile, "effective")
	if req.Answers == nil {
		req.Answers = map[string]any{}
	}
	if req.Samples == nil {
		req.Samples = []string{}
	}
	if len(req.Samples) > 20 {
		return profileAnalysisInput{}, fmt.Errorf("too many profile samples")
	}
	for _, sample := range req.Samples {
		if len([]byte(sample)) > 20000 {
			return profileAnalysisInput{}, fmt.Errorf("profile sample is too large")
		}
	}
	return req, nil
}

func profileAnalysisRequestFingerprint(project *model.Project, profile model.ProjectProfile, req profileAnalysisInput) (string, string, error) {
	payload := struct {
		ProjectID        string         `json:"project_id"`
		ProfileVersion   int64          `json:"profile_version"`
		ExecutionProfile string         `json:"execution_profile"`
		Answers          map[string]any `json:"answers"`
		Samples          []string       `json:"samples"`
	}{project.ID, profile.Version, req.ExecutionProfile, req.Answers, req.Samples}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(digest[:])
	return fingerprint, project.ID + ":" + fingerprint, nil
}

func profileAnalysisQuoteResponse(quote *model.BillingQuote) fiber.Map {
	executionProfile := ""
	var sku billing.SKUConfig
	if err := json.Unmarshal(quote.SKUSnapshot, &sku); err == nil {
		executionProfile = sku.ExecutionProfile
	}
	return fiber.Map{"id": quote.ID, "operation": "task.profile_analysis", "execution_profile": executionProfile, "price_credits": quote.PriceCredits, "list_price_credits": quote.ListPriceCredits, "currency": "credits", "expires_at": quote.ExpiresAt, "request_fingerprint": quote.RequestFingerprint}
}

func (h *ProjectHandler) UpdateProfile(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req profileUpdateRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid profile body")
	}
	profile, err := h.service.ConfirmProfile(c.Context(), userID, c.Params("id"), req.Version, req.Profile)
	if err != nil {
		return h.respondProfileError(c, err)
	}
	return Success(c, profile)
}

func (h *ProjectHandler) UpdateProfileDimension(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req profileDimensionRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid profile dimension body")
	}
	if req.ExpectedRevision != nil {
		req.Version = *req.ExpectedRevision
	}
	if req.Content != nil {
		req.Value.Content = req.Content
	}
	profile, err := h.service.UpdateProfileDimension(c.Context(), userID, c.Params("id"), c.Params("dimension"), req.Version, req.Value)
	if err != nil {
		return h.respondProfileError(c, err)
	}
	return Success(c, profile)
}

func (h *ProjectHandler) ProfileAnalysisQuote(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	_, _, err := h.service.Get(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.respondProfileError(c, err)
	}
	var req profileAnalysisInput
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid analysis quote body")
		}
	}
	req, err = normalizeProfileAnalysisInput(req)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, fiber.Map{"id": "free-profile-analysis", "operation": "task.profile_analysis", "execution_profile": req.ExecutionProfile, "price_credits": 0, "list_price_credits": 0, "currency": "credits", "expires_at": time.Now().UTC().Add(24 * time.Hour)})
}

func (h *ProjectHandler) StartProfileAnalysis(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.tasks == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}
	var req struct {
		ExecutionProfile string         `json:"execution_profile"`
		QuoteConfirmed   bool           `json:"quote_confirmed"`
		QuoteID          string         `json:"quote_id"`
		Answers          map[string]any `json:"answers"`
		Samples          []string       `json:"samples"`
		ExpectedRevision *int64         `json:"expected_revision"`
	}
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid analysis body")
		}
	}
	project, _, err := h.service.Get(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.respondProfileError(c, err)
	}
	profile := project.Profile.Data()
	if profile.SchemaVersion == 0 {
		profile = model.NewProjectProfile()
	}
	if req.ExpectedRevision != nil && *req.ExpectedRevision != profile.Version {
		return Error(c, fiber.StatusConflict, "profile_revision_conflict")
	}
	analysisInput, err := normalizeProfileAnalysisInput(profileAnalysisInput{ExecutionProfile: req.ExecutionProfile, Answers: req.Answers, Samples: req.Samples})
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	// A second click while the current analysis is still admitted must not
	// create another billable task. Failed and cancelled analyses may be retried.
	if strings.TrimSpace(profile.AnalysisTaskID) != "" {
		if existing, getErr := h.tasks.GetByID(c.Context(), profile.AnalysisTaskID); getErr == nil &&
			existing.UserID == userID && existing.ProjectID == project.ID &&
			(existing.Status == model.TaskStatusPending || existing.Status == model.TaskStatusRunning) {
			return Success(c, fiber.Map{"task": existing, "profile": profile})
		}
	}
	input := profileAgentInput(project, analysisInput.Answers, analysisInput.Samples)
	expectedVersion := profile.Version
	if strings.TrimSpace(analysisInput.ExecutionProfile) == "" {
		analysisInput.ExecutionProfile = "effective"
	}
	tasks, err := h.tasks.CreateManual(c.Context(), service.CreateManualParams{UserID: userID, ProjectID: project.ID, ExecutionProfile: analysisInput.ExecutionProfile, AgentID: model.AgentIDProfileAnalysis, Channel: model.ChannelProfileAnalysis, TaskKind: model.TaskKindProfileAnalysis, Prompt: project.ProfileURL, AgentInput: input, Quantity: 1, ProfileAnalysisExpectedVersion: &expectedVersion})
	if err != nil {
		return h.respondTaskError(c, err)
	}
	if len(tasks) == 0 {
		return Error(c, fiber.StatusInternalServerError, "analysis task was not created")
	}
	profile, err = h.service.GetProfile(c.Context(), userID, project.ID)
	if err != nil {
		return h.respondProfileError(c, err)
	}
	return Success(c, fiber.Map{"task": tasks[0], "profile": profile})
}

// profileAgentInput keeps task input JSON-shaped before it reaches Pack schema
// validation. In particular, []string is not a JSON array when passed through
// the schema validator's generic map representation.
func profileAgentInput(project *model.Project, answers map[string]any, samples []string) map[string]any {
	values := make([]any, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample)
	}
	return map[string]any{
		"project_id":  project.ID,
		"platform":    project.Platform,
		"profile_url": project.ProfileURL,
		"answers":     answers,
		"samples":     values,
	}
}

// RefreshProfile and RetryProfile are free, explicit aliases for the same
// server-owned profile Agent flow. They intentionally do not expose quote or
// confirmation UI concepts.
func (h *ProjectHandler) RefreshProfile(c fiber.Ctx) error { return h.StartProfileAnalysis(c) }
func (h *ProjectHandler) RetryProfile(c fiber.Ctx) error   { return h.StartProfileAnalysis(c) }

// GetProfileAnalysis returns only server-owned profile state and task metadata.
// Profile results are submitted structurally through MCP and are never exposed
// as downloadable task draft artifacts.
func (h *ProjectHandler) GetProfileAnalysis(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if h.tasks == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}
	profile, err := h.service.GetProfile(c.Context(), userID, c.Params("id"))
	if err != nil {
		return h.respondProfileError(c, err)
	}
	if strings.TrimSpace(profile.AnalysisTaskID) == "" {
		return Success(c, fiber.Map{"status": "not_started", "profile": profile})
	}
	task, err := h.tasks.GetByID(c.Context(), profile.AnalysisTaskID)
	if err != nil || task.UserID != userID || task.ProjectID != c.Params("id") || task.Type != model.TaskTypeProfileAnalysis {
		return Error(c, fiber.StatusNotFound, "profile analysis task not found")
	}
	result := fiber.Map{"status": task.Status, "task": taskAPIResponse(task, h.store), "profile": profile}
	return Success(c, result)
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func decorateProfileDraft(draft, current model.ProjectProfile) model.ProjectProfile {
	draft.Version = current.Version
	draft.AnalysisTaskID = current.AnalysisTaskID
	return draft
}

func (h *ProjectHandler) respondTaskError(c fiber.Ctx, err error) error {
	if errors.Is(err, service.ErrBillingQuoteConsumed) || errors.Is(err, service.ErrBillingQuoteExpired) || errors.Is(err, service.ErrBillingQuoteMismatch) {
		return Error(c, fiber.StatusConflict, err.Error())
	}
	if errors.Is(err, service.ErrBillingInsufficientForTask) {
		return Error(c, fiber.StatusPaymentRequired, err.Error())
	}
	h.logger.Error().Err(err).Msg("profile analysis task failed")
	return Error(c, fiber.StatusBadRequest, err.Error())
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
	if err := rejectProjectAgentConfig(c.Body()); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	var req projectRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.LegacyWechatPublishMode != nil {
		return Error(c, fiber.StatusBadRequest, "wechat_publish_mode is no longer supported; publishing is capability-driven")
	}
	if hasJSONField(c.Body(), "instructions") {
		req.InstructionsSet = true
	}
	req.AuthorSet = hasJSONField(c.Body(), "author")
	req.ReferenceImageSet = hasJSONField(c.Body(), "reference_image")
	req.VisualStyleSet = hasJSONField(c.Body(), "visual_style")
	req.PortraitReferenceImageSet = hasJSONField(c.Body(), "portrait_reference_image")
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
	if req.VisualStyleSet && req.VisualStyle == current.VisualStyle {
		req.VisualStyleSet = false
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
	if updated.Platform == model.PlatformWechat && (strings.TrimSpace(req.WechatAppID) != "" || strings.TrimSpace(req.WechatSecret) != "") {
		if _, configErr := h.service.UpsertChannelConfig(c.Context(), userID, projectID, model.ChannelArticle, req.wechatChannelConfig()); configErr != nil {
			return h.respondProjectConfigError(c, configErr)
		}
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
			"message":   "种草笔记登录状态检查失败",
		})
	}

	msg := "已登录"
	if !loggedIn {
		msg = "未登录，请获取二维码并使用种草笔记客户端扫码"
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
		return Error(c, fiber.StatusBadGateway, "获取种草笔记登录二维码失败")
	}
	if strings.TrimSpace(qrcodeImage) == "" {
		return Error(c, fiber.StatusBadGateway, "种草笔记登录二维码为空")
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
		return Error(c, fiber.StatusBadGateway, "退出种草笔记登录失败")
	}
	return Success(c, fiber.Map{"logged_in": false})
}
