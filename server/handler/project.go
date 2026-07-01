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

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/platform"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/seednote"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/royalrick/anbanwriter/server/storage"
)

// ProjectHandler handles project-related HTTP endpoints.
type ProjectHandler struct {
	service        *service.ProjectService
	logger         *zerolog.Logger
	llm            service.LLMClient
	llmTimeout     time.Duration
	visionClient   service.LLMClient // dedicated vision model for AnalyzeImage; nil = fall back to llm
	modelConfigSvc *service.ModelConfigService
	templateSvc    *service.TemplateService
	store          storage.Provider
	seednoteClient *seednote.Client
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(svc *service.ProjectService, logger *zerolog.Logger) *ProjectHandler {
	return &ProjectHandler{service: svc, logger: logger}
}

// SetLLMClient injects an optional LLM client for profile analysis.
func (h *ProjectHandler) SetLLMClient(llm service.LLMClient, timeout time.Duration) {
	h.llm = llm
	h.llmTimeout = timeout
}

// SetVisionClient injects a dedicated vision-capable LLM client used by AnalyzeImage.
// When nil or never called, AnalyzeImage falls back to the writing LLM client.
func (h *ProjectHandler) SetVisionClient(client service.LLMClient) {
	h.visionClient = client
}

// SetModelConfigService injects per-user model overrides for profile analysis.
func (h *ProjectHandler) SetModelConfigService(svc *service.ModelConfigService) {
	h.modelConfigSvc = svc
}

// SetTemplateService injects an optional TemplateService for template recommendations on project creation.
func (h *ProjectHandler) SetTemplateService(svc *service.TemplateService) {
	h.templateSvc = svc
}

// SetStore injects a storage provider for reading locally uploaded files.
func (h *ProjectHandler) SetStore(s storage.Provider) {
	h.store = s
}

// signProjectURLs resolves stored image URLs (avatar, reference image) to
// directly-fetchable signed URLs so any viewer who can see the project can load
// its images regardless of which user originally uploaded them. No-op when no
// store is wired (e.g. unit tests) or the URL is external/empty.
func (h *ProjectHandler) signProjectURLs(ctx context.Context, ch *model.Project) {
	if ch == nil {
		return
	}
	ch.AvatarURL = service.SignURL(ctx, h.store, h.logger, ch.AvatarURL, service.DefaultSignedURLTTL)
	ch.ReferenceImageURL = service.SignURL(ctx, h.store, h.logger, ch.ReferenceImageURL, service.DefaultSignedURLTTL)
}

// SetSeednoteClient injects the Seednote SDK client.
func (h *ProjectHandler) SetSeednoteClient(client *seednote.Client) {
	h.seednoteClient = client
}

// projectRequest is the shared request body for creating and updating a project.
type projectRequest struct {
	Platform           string                          `json:"platform"`
	Name               string                          `json:"name"`
	ProfileURL         string                          `json:"profile_url"`
	AvatarURL          string                          `json:"avatar_url"`
	Positioning        string                          `json:"positioning"`
	Keywords           string                          `json:"keywords"`
	VisualStyle        string                          `json:"visual_style"`
	Writer             string                          `json:"writer"`
	Theme              string                          `json:"theme"`
	Author             string                          `json:"author"`
	TemplateID         string                          `json:"template_id"`
	ReferenceImageURL  string                          `json:"reference_image_url"`
	ImageRatio         string                          `json:"image_ratio"`
	MaxConcurrentTasks int                             `json:"max_concurrent_tasks"`
	Instructions       string                          `json:"instructions"`
	InstructionsSet    bool                            `json:"-"`
	EcommerceDefaults  *model.EcommerceProjectDefaults `json:"ecommerce_defaults,omitempty"`
	VideoDefaults      *model.VideoDefaults            `json:"video_defaults,omitempty"`
	VideoModelPolicy   *model.VideoModelPolicy         `json:"video_model_policy,omitempty"`
	// Config fields for platform-specific credentials.
	WechatAppID      string `json:"wechat_app_id"`
	WechatSecret     string `json:"wechat_secret"`
	EnablePublishing bool   `json:"enable_publishing"`
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
		Platform:              req.Platform,
		Name:                  req.Name,
		ProfileURL:            req.ProfileURL,
		AvatarURL:             req.AvatarURL,
		Keywords:              req.Keywords,
		VisualStyle:           req.VisualStyle,
		Writer:                req.Writer,
		Theme:                 req.Theme,
		Author:                req.Author,
		CreatedFromTemplateID: req.TemplateID,
		ReferenceImageURL:     req.ReferenceImageURL,
		ImageRatio:            req.ImageRatio,
		MaxConcurrentTasks:    req.MaxConcurrentTasks,
		Instructions:          instructions,
		InstructionsSet:       instructionsSet,
		Config: model.ProjectConfig{
			WechatAppID:      req.WechatAppID,
			WechatSecret:     req.WechatSecret,
			EnablePublishing: req.EnablePublishing,
		},
	}
	if req.EcommerceDefaults != nil {
		p.SetEcommerceDefaults(*req.EcommerceDefaults)
		p.EcommerceDefaultsSet = true
	}
	if req.VideoDefaults != nil || req.VideoModelPolicy != nil {
		if req.VideoDefaults != nil {
			p.SetVideoDefaults(*req.VideoDefaults)
		}
		if req.VideoModelPolicy != nil {
			p.SetVideoModelPolicy(*req.VideoModelPolicy)
		}
		p.VideoProfileSet = true
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

	// Sanitize all projects before returning.
	for _, ch := range projects {
		service.SanitizeProject(ch)
		h.signProjectURLs(c.Context(), ch)
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

	var req projectRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if hasJSONField(c.Body(), "instructions") {
		req.InstructionsSet = true
	}

	if req.Platform == "" {
		return Error(c, fiber.StatusBadRequest, "platform is required")
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

	if req.ImageRatio != "" && !model.ValidImageRatios[req.ImageRatio] {
		return Error(c, fiber.StatusBadRequest, "image_ratio must be one of: 3:4, 1:1, 4:3, 16:9")
	}

	// 作者署名不得是写作风格的人设名/key（二者语义不同，混用会把模仿对象当成发布作者）。
	if err := service.RejectWriterNameAsAuthor(req.Author); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}

	ch := req.toProject()

	// Force max_concurrent_tasks based on user tier.
	ch.MaxConcurrentTasks = h.getTierMaxConcurrent(c)

	created, err := h.service.Create(c.Context(), userID, ch)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create project failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create project: "+err.Error())
	}

	service.SanitizeProject(created)
	h.signProjectURLs(c.Context(), created)

	// For Seednote projects, include recommended templates.
	recommended := []*model.Template{}
	if created.Platform == model.PlatformSeednote && h.templateSvc != nil {
		if rec := h.getRecommendedTemplates(c.Context(), created); rec != nil {
			// Sign each recommended template's image URLs so the cross-account
			// signed-direct path covers them too (these are public templates the
			// viewer may not have uploaded).
			for _, t := range rec {
				service.SignTemplateURLs(c.Context(), h.store, h.logger, t)
			}
			recommended = rec
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

	service.SanitizeProject(ch)
	h.signProjectURLs(c.Context(), ch)
	return Success(c, fiber.Map{
		"project": ch,
		"stats":   stats,
	})
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

	var req projectRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if hasJSONField(c.Body(), "instructions") {
		req.InstructionsSet = true
	}

	if req.ImageRatio != "" && !model.ValidImageRatios[req.ImageRatio] {
		return Error(c, fiber.StatusBadRequest, "image_ratio must be one of: 3:4, 1:1, 4:3, 16:9")
	}

	// 作者署名不得是写作风格的人设名/key（二者语义不同，混用会把模仿对象当成发布作者）。
	if err := service.RejectWriterNameAsAuthor(req.Author); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}

	ch := req.toProject()

	// Force max_concurrent_tasks based on user tier.
	ch.MaxConcurrentTasks = h.getTierMaxConcurrent(c)

	updated, err := h.service.Update(c.Context(), userID, projectID, ch)
	if err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			return Error(c, fiber.StatusNotFound, "project not found")
		}
		if errors.Is(err, service.ErrProjectOwnedByUser) {
			return Forbidden(c, "you do not have access to this project")
		}
		h.logger.Error().Err(err).Str("project_id", projectID).Msg("update project failed")
		return Error(c, fiber.StatusInternalServerError, "failed to update project")
	}

	service.SanitizeProject(updated)
	h.signProjectURLs(c.Context(), updated)
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
		h.logger.Error().Err(err).Str("project_id", projectID).Msg("delete project failed")
		return Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return Success(c, fiber.Map{"message": "project deleted"})
}

// GetPlatformConfigs handles GET /projects/platform-configs.
func (h *ProjectHandler) GetPlatformConfigs(c fiber.Ctx) error {
	return Success(c, model.GetAllPlatformConfigs())
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
	h.enrichSeednoteProfileWithAI(c.Context(), userID, profile)

	return Success(c, profile)
}

type seednoteProfileAnalysis struct {
	Name           string   `json:"name"`
	AvatarURL      string   `json:"avatar_url"`
	Positioning    string   `json:"positioning"`
	Keywords       []string `json:"keywords"`
	Style          string   `json:"style"`
	ContentSummary string   `json:"content_summary"`
}

func (h *ProjectHandler) enrichSeednoteProfileWithAI(ctx context.Context, userID string, profile *platform.PlatformProfile) {
	llm := h.getLLMClient(ctx, userID)
	if llm == nil || profile == nil {
		return
	}

	// Prefer AI extraction from __INITIAL_STATE__ JSON when available.
	if initialState, ok := profile.RawData["initial_state"].(string); ok && initialState != "" {
		prompt := buildSeednoteProfileExtractionPrompt(initialState, topPostsFromProfile(profile))
		raw, err := llm.Complete(ctx, "", prompt)
		if err != nil {
			h.logger.Warn().Err(err).Str("user_id", userID).Msg("seednote profile AI extraction failed")
			return
		}
		analysis, err := parseSeednoteProfileAnalysis(raw)
		if err != nil {
			h.logger.Warn().Err(err).Str("user_id", userID).Msg("parse seednote profile AI extraction failed")
			return
		}
		if analysis.Name != "" {
			profile.Name = analysis.Name
		}
		if analysis.AvatarURL != "" {
			profile.AvatarURL = analysis.AvatarURL
		}
		if analysis.Positioning != "" {
			profile.Positioning = analysis.Positioning
		}
		profile.Keywords = cleanKeywords(analysis.Keywords)
		if analysis.Style != "" {
			profile.Style = analysis.Style
		}
		if profile.RawData == nil {
			profile.RawData = map[string]any{}
		}
		profile.RawData["analysis"] = analysis
		return
	}

	// Fallback: generate positioning/keywords/style from code-parsed fields.
	prompt := buildSeednoteProfileAnalysisPrompt(profile)
	raw, err := llm.Complete(ctx, "", prompt)
	if err != nil {
		h.logger.Warn().Err(err).Str("user_id", userID).Msg("seednote profile AI analysis failed")
		return
	}
	analysis, err := parseSeednoteProfileAnalysis(raw)
	if err != nil {
		h.logger.Warn().Err(err).Str("user_id", userID).Msg("parse seednote profile AI analysis failed")
		return
	}
	if analysis.Positioning != "" {
		profile.Positioning = analysis.Positioning
	}
	profile.Keywords = cleanKeywords(analysis.Keywords)
	if analysis.Style != "" {
		profile.Style = analysis.Style
	}
	if profile.RawData == nil {
		profile.RawData = map[string]any{}
	}
	profile.RawData["analysis"] = analysis
}

func (h *ProjectHandler) getLLMClient(ctx context.Context, userID string) service.LLMClient {
	if h.modelConfigSvc != nil {
		if baseURL, key, modelName, ok := h.modelConfigSvc.GetEffectiveWritingConfig(ctx, userID); ok {
			h.logger.Info().
				Str("user_id", userID).
				Str("endpoint", baseURL).
				Str("model", modelName).
				Msg("using user custom model for seednote profile analysis")
			return service.NewOpenAILLMClient(baseURL, key, modelName, h.llmTimeout)
		}
	}
	return h.llm
}

func cleanKeywords(keywords []string) string {
	cleaned := make([]string, 0, len(keywords))
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw != "" {
			cleaned = append(cleaned, kw)
		}
	}
	return strings.Join(cleaned, ", ")
}

func buildSeednoteProfileAnalysisPrompt(profile *platform.PlatformProfile) string {
	var b strings.Builder
	b.WriteString("你是种草笔记账号定位和视觉策略分析师。请基于账号主页信息和表现最好的可见作品，生成账号定位、关键词和视觉风格。\n\n")
	b.WriteString("## 账号信息\n")
	b.WriteString("- 昵称: " + truncateRunes(profile.Name, 80) + "\n")
	b.WriteString("- 简介: " + truncateRunes(profile.Positioning, 240) + "\n\n")
	b.WriteString("## 表现较好的可见作品\n")
	for i, post := range topPostsFromProfile(profile) {
		b.WriteString(fmt.Sprintf("%d. 标题: %s；点赞: %d；收藏: %d；评论: %d；分享: %d；总互动: %d\n",
			i+1,
			truncateRunes(post.Title, 120),
			post.LikeCount,
			post.CollectCount,
			post.CommentCount,
			post.ShareCount,
			post.EngagementScore,
		))
	}
	b.WriteString("\n## 输出要求\n")
	b.WriteString("只输出 JSON，不要 markdown 代码块，不要解释。格式如下：\n")
	b.WriteString(`{"positioning":"80字以内账号定位","keywords":["关键词1","关键词2","关键词3"],"style":"120字以内视觉风格描述","content_summary":"120字以内内容方向摘要"}`)
	return b.String()
}

func buildSeednoteProfileExtractionPrompt(initialState string, topPosts []platform.SeednotePost) string {
	var b strings.Builder
	b.WriteString("你是种草笔记账号分析专家。请从以下种草笔记页面数据中提取账号信息并生成分析。\n\n")
	b.WriteString("## 页面数据\n")
	b.WriteString(truncateRunes(initialState, 15000))
	b.WriteString("\n\n")
	if len(topPosts) > 0 {
		b.WriteString("## 表现较好的可见作品\n")
		for i, post := range topPosts {
			b.WriteString(fmt.Sprintf("%d. 标题: %s；点赞: %d；收藏: %d；评论: %d；分享: %d\n",
				i+1,
				truncateRunes(post.Title, 120),
				post.LikeCount,
				post.CollectCount,
				post.CommentCount,
				post.ShareCount,
			))
		}
		b.WriteString("\n")
	}
	b.WriteString("## 提取要求\n")
	b.WriteString("1. 从页面数据中找到用户信息（昵称、头像URL、简介）。注意区分用户数据与平台自身数据，用户信息通常在 user 节点下。\n")
	b.WriteString("2. 基于简介和作品分析账号定位、关键词和视觉风格。\n")
	b.WriteString("3. 只输出 JSON，不要 markdown 代码块，不要解释。\n\n")
	b.WriteString(`{"name":"用户昵称","avatar_url":"头像URL","positioning":"80字以内账号定位","keywords":["关键词1","关键词2","关键词3"],"style":"120字以内视觉风格描述","content_summary":"120字以内内容方向摘要"}`)
	return b.String()
}

func topPostsFromProfile(profile *platform.PlatformProfile) []platform.SeednotePost {
	if profile == nil || profile.RawData == nil {
		return nil
	}
	posts, ok := profile.RawData["top_posts"].([]platform.SeednotePost)
	if !ok {
		return nil
	}
	if len(posts) > 5 {
		return posts[:5]
	}
	return posts
}

func parseSeednoteProfileAnalysis(raw string) (seednoteProfileAnalysis, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var analysis seednoteProfileAnalysis
	if err := json.Unmarshal([]byte(raw), &analysis); err != nil {
		return analysis, fmt.Errorf("unmarshal seednote profile analysis: %w", err)
	}
	analysis.Positioning = truncateRunes(strings.TrimSpace(analysis.Positioning), 120)
	analysis.Style = truncateRunes(strings.TrimSpace(analysis.Style), 180)
	analysis.ContentSummary = truncateRunes(strings.TrimSpace(analysis.ContentSummary), 180)
	if len(analysis.Keywords) > 8 {
		analysis.Keywords = analysis.Keywords[:8]
	}
	return analysis, nil
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
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
	case "reference_image_url":
		return req.ReferenceImageURL
	case "image_ratio":
		return req.ImageRatio
	case "max_concurrent_tasks":
		return fmt.Sprintf("%d", req.MaxConcurrentTasks)
	case "wechat_app_id":
		return req.WechatAppID
	case "wechat_secret":
		return req.WechatSecret
	case "enable_publishing":
		return fmt.Sprintf("%v", req.EnablePublishing)
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

// ---------------------------------------------------------------------------
// Image style analysis
// ---------------------------------------------------------------------------

type analyzeImageRequest struct {
	ImageURL string `json:"image_url"`
}

// SeednoteLoginStatus handles GET /seednote/login-status.
func (h *ProjectHandler) SeednoteLoginStatus(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	if h.seednoteClient == nil {
		return Success(c, fiber.Map{
			"available": false,
			"logged_in": false,
			"message":   "Seednote sidecar 未配置",
		})
	}

	loggedIn, err := h.seednoteClient.CheckLoginStatus(c.Context())
	if err != nil {
		return Success(c, fiber.Map{
			"available": true,
			"logged_in": false,
			"message":   err.Error(),
		})
	}

	msg := "已登录"
	if !loggedIn {
		msg = "未登录，请使用 get_seednote_login_qrcode 获取二维码扫描登录"
	}
	return Success(c, fiber.Map{
		"available": true,
		"logged_in": loggedIn,
		"message":   msg,
	})
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

	// Prefer the dedicated vision client for image analysis; fall back to the
	// writing LLM client (with per-user override) when no vision model is wired.
	llm := h.visionClient
	if llm == nil {
		llm = h.getLLMClient(c.Context(), userID)
	}
	if llm == nil {
		return Error(c, fiber.StatusServiceUnavailable, "LLM service is not configured")
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
		key, respondErr := cleanOwnedUploadKey(req.ImageURL, userID)
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
	// projects/references are user uploads, {userID}/designer/ are designer images.
	if isUserOwnedStorageKey(userID, cleanKey, userID+"/designer/") {
		return cleanKey, nil
	}
	return "", func(c fiber.Ctx) error { return Forbidden(c, "you do not have access to this file") }
}
