package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/platform"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/resources"
	"github.com/royalrick/anbanwriter/server/seednote"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/royalrick/anbanwriter/server/storage"
)

// ChannelHandler handles channel-related HTTP endpoints.
type ChannelHandler struct {
	service        *service.ChannelService
	logger         *zerolog.Logger
	llm            service.LLMClient
	llmTimeout     time.Duration
	modelConfigSvc *service.ModelConfigService
	templateSvc    *service.TemplateService
	store          storage.Provider
	seednoteClient *seednote.Client
}

// NewChannelHandler creates a new ChannelHandler.
func NewChannelHandler(svc *service.ChannelService, logger *zerolog.Logger) *ChannelHandler {
	return &ChannelHandler{service: svc, logger: logger}
}

// SetLLMClient injects an optional LLM client for profile analysis.
func (h *ChannelHandler) SetLLMClient(llm service.LLMClient, timeout time.Duration) {
	h.llm = llm
	h.llmTimeout = timeout
}

// SetModelConfigService injects per-user model overrides for profile analysis.
func (h *ChannelHandler) SetModelConfigService(svc *service.ModelConfigService) {
	h.modelConfigSvc = svc
}

// SetTemplateService injects an optional TemplateService for template recommendations on channel creation.
func (h *ChannelHandler) SetTemplateService(svc *service.TemplateService) {
	h.templateSvc = svc
}

// SetStore injects a storage provider for reading locally uploaded files.
func (h *ChannelHandler) SetStore(s storage.Provider) {
	h.store = s
}

// SetSeednoteClient injects the Seednote SDK client.
func (h *ChannelHandler) SetSeednoteClient(client *seednote.Client) {
	h.seednoteClient = client
}

// channelRequest is the shared request body for creating and updating a channel.
type channelRequest struct {
	Platform           string `json:"platform"`
	Name               string `json:"name"`
	ProfileURL         string `json:"profile_url"`
	AvatarURL          string `json:"avatar_url"`
	Positioning        string `json:"positioning"`
	Keywords           string `json:"keywords"`
	Style              string `json:"style"`
	Theme              string `json:"theme"`
	Author             string `json:"author"`
	ReferenceImageURL  string `json:"reference_image_url"`
	ImageRatio         string `json:"image_ratio"`
	Layout             string `json:"layout"`
	ImagePreset        string `json:"image_preset"`
	MaxConcurrentTasks int    `json:"max_concurrent_tasks"`
	// Config fields for platform-specific credentials.
	WechatAppID      string `json:"wechat_app_id"`
	WechatSecret     string `json:"wechat_secret"`
	EnablePublishing bool   `json:"enable_publishing"`
}

// toChannel converts a request to a Channel model.
func (req *channelRequest) toChannel() *model.Channel {
	return &model.Channel{
		Platform:           req.Platform,
		Name:               req.Name,
		ProfileURL:         req.ProfileURL,
		AvatarURL:          req.AvatarURL,
		Positioning:        req.Positioning,
		Keywords:           req.Keywords,
		Style:              req.Style,
		Theme:              req.Theme,
		Author:             req.Author,
		ReferenceImageURL:  req.ReferenceImageURL,
		ImageRatio:         req.ImageRatio,
		Layout:             req.Layout,
		ImagePreset:        req.ImagePreset,
		MaxConcurrentTasks: req.MaxConcurrentTasks,
		Config: model.ChannelConfig{
			WechatAppID:      req.WechatAppID,
			WechatSecret:     req.WechatSecret,
			EnablePublishing: req.EnablePublishing,
		},
	}
}

// List handles GET /channels.
func (h *ChannelHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	status := c.Query("status", "")
	platform := c.Query("platform", "")

	channels, err := h.service.List(c.Context(), userID, repository.ChannelListOptions{
		Status:   status,
		Platform: platform,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("list channels failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list channels")
	}

	// Sanitize all channels before returning.
	for _, ch := range channels {
		service.SanitizeChannel(ch)
	}

	return Success(c, channels)
}

// Stats handles GET /channels/stats?ids=a,b,c and returns a per-channel stats map.
func (h *ChannelHandler) Stats(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	idsParam := strings.TrimSpace(c.Query("ids", ""))
	if idsParam == "" {
		return Success(c, fiber.Map{})
	}

	channels, err := h.service.List(c.Context(), userID, repository.ChannelListOptions{})
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("list channels for stats failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list channels")
	}

	allowed := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		allowed[ch.ID] = struct{}{}
	}

	channelIDs := make([]string, 0, len(channels))
	for _, rawID := range strings.Split(idsParam, ",") {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, ok := allowed[id]; ok {
			channelIDs = append(channelIDs, id)
		}
	}

	stats, err := h.service.BatchStats(c.Context(), channelIDs)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("get channel stats failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get channel stats")
	}

	return Success(c, stats)
}

// Create handles POST /channels.
func (h *ChannelHandler) Create(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req channelRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
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

	if req.Layout != "" && resources.Manager().Get(resources.CategoryLayout, req.Layout) == nil {
		return Error(c, fiber.StatusBadRequest, "invalid layout: "+req.Layout)
	}
	if req.ImagePreset != "" && resources.Manager().Get(resources.CategoryImagePreset, req.ImagePreset) == nil {
		return Error(c, fiber.StatusBadRequest, "invalid image_preset: "+req.ImagePreset)
	}

	ch := req.toChannel()

	// Force max_concurrent_tasks based on user tier.
	ch.MaxConcurrentTasks = h.getTierMaxConcurrent(c)

	created, err := h.service.Create(c.Context(), userID, ch)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create channel failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create channel: "+err.Error())
	}

	service.SanitizeChannel(created)

	// For Seednote channels with profile data, return recommended templates.
	if created.Platform == model.PlatformSeednote && h.templateSvc != nil {
		recommended := h.getRecommendedTemplates(c.Context(), created)
		if len(recommended) > 0 {
			return Success(c, fiber.Map{
				"channel":               created,
				"recommended_templates": recommended,
			})
		}
	}

	return Success(c, created)
}

// Get handles GET /channels/:id.
func (h *ChannelHandler) Get(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	channelID := c.Params("id")
	if channelID == "" {
		return Error(c, fiber.StatusBadRequest, "channel id is required")
	}

	ch, stats, err := h.service.Get(c.Context(), userID, channelID)
	if err != nil {
		if errors.Is(err, service.ErrChannelNotFound) {
			return Error(c, fiber.StatusNotFound, "channel not found")
		}
		if errors.Is(err, service.ErrChannelOwnedByUser) {
			return Forbidden(c, "you do not have access to this channel")
		}
		h.logger.Error().Err(err).Str("channel_id", channelID).Msg("get channel failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get channel")
	}

	service.SanitizeChannel(ch)
	return Success(c, fiber.Map{
		"channel": ch,
		"stats":   stats,
	})
}

// Update handles PUT /channels/:id.
func (h *ChannelHandler) Update(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	channelID := c.Params("id")
	if channelID == "" {
		return Error(c, fiber.StatusBadRequest, "channel id is required")
	}

	var req channelRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.ImageRatio != "" && !model.ValidImageRatios[req.ImageRatio] {
		return Error(c, fiber.StatusBadRequest, "image_ratio must be one of: 3:4, 1:1, 4:3, 16:9")
	}
	if req.Layout != "" && resources.Manager().Get(resources.CategoryLayout, req.Layout) == nil {
		return Error(c, fiber.StatusBadRequest, "invalid layout: "+req.Layout)
	}
	if req.ImagePreset != "" && resources.Manager().Get(resources.CategoryImagePreset, req.ImagePreset) == nil {
		return Error(c, fiber.StatusBadRequest, "invalid image_preset: "+req.ImagePreset)
	}

	ch := req.toChannel()

	// Force max_concurrent_tasks based on user tier.
	ch.MaxConcurrentTasks = h.getTierMaxConcurrent(c)

	updated, err := h.service.Update(c.Context(), userID, channelID, ch)
	if err != nil {
		if errors.Is(err, service.ErrChannelNotFound) {
			return Error(c, fiber.StatusNotFound, "channel not found")
		}
		if errors.Is(err, service.ErrChannelOwnedByUser) {
			return Forbidden(c, "you do not have access to this channel")
		}
		h.logger.Error().Err(err).Str("channel_id", channelID).Msg("update channel failed")
		return Error(c, fiber.StatusInternalServerError, "failed to update channel")
	}

	service.SanitizeChannel(updated)
	return Success(c, updated)
}

// Archive handles PATCH /channels/:id/archive.
func (h *ChannelHandler) Archive(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	channelID := c.Params("id")
	if channelID == "" {
		return Error(c, fiber.StatusBadRequest, "channel id is required")
	}

	if err := h.service.Archive(c.Context(), userID, channelID); err != nil {
		if errors.Is(err, service.ErrChannelNotFound) {
			return Error(c, fiber.StatusNotFound, "channel not found")
		}
		if errors.Is(err, service.ErrChannelOwnedByUser) {
			return Forbidden(c, "you do not have access to this channel")
		}
		h.logger.Error().Err(err).Str("channel_id", channelID).Msg("archive channel failed")
		return Error(c, fiber.StatusInternalServerError, "failed to archive channel")
	}

	return Success(c, fiber.Map{"message": "channel archived"})
}

// Restore handles PATCH /channels/:id/restore.
func (h *ChannelHandler) Restore(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	channelID := c.Params("id")
	if channelID == "" {
		return Error(c, fiber.StatusBadRequest, "channel id is required")
	}

	if err := h.service.Restore(c.Context(), userID, channelID); err != nil {
		if errors.Is(err, service.ErrChannelNotFound) {
			return Error(c, fiber.StatusNotFound, "channel not found")
		}
		if errors.Is(err, service.ErrChannelOwnedByUser) {
			return Forbidden(c, "you do not have access to this channel")
		}
		h.logger.Error().Err(err).Str("channel_id", channelID).Msg("restore channel failed")
		return Error(c, fiber.StatusInternalServerError, "failed to restore channel")
	}

	return Success(c, fiber.Map{"message": "channel restored"})
}

// Delete handles DELETE /channels/:id.
func (h *ChannelHandler) Delete(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	channelID := c.Params("id")
	if channelID == "" {
		return Error(c, fiber.StatusBadRequest, "channel id is required")
	}

	if err := h.service.Delete(c.Context(), userID, channelID); err != nil {
		if errors.Is(err, service.ErrChannelNotFound) {
			return Error(c, fiber.StatusNotFound, "channel not found")
		}
		if errors.Is(err, service.ErrChannelOwnedByUser) {
			return Forbidden(c, "you do not have access to this channel")
		}
		h.logger.Error().Err(err).Str("channel_id", channelID).Msg("delete channel failed")
		return Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return Success(c, fiber.Map{"message": "channel deleted"})
}

// GetPlatformConfigs handles GET /channels/platform-configs.
func (h *ChannelHandler) GetPlatformConfigs(c fiber.Ctx) error {
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

// FetchProfile handles POST /channels/fetch-profile.
// It fetches profile data from the specified platform using the profile URL.
func (h *ChannelHandler) FetchProfile(c fiber.Ctx) error {
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

func (h *ChannelHandler) enrichSeednoteProfileWithAI(ctx context.Context, userID string, profile *platform.PlatformProfile) {
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

func (h *ChannelHandler) getLLMClient(ctx context.Context, userID string) service.LLMClient {
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

// getRecommendedTemplates returns templates recommended for a channel based on its profile keywords.
func (h *ChannelHandler) getRecommendedTemplates(ctx context.Context, ch *model.Channel) []*model.Template {
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
		h.logger.Warn().Err(err).Str("channel_id", ch.ID).Msg("failed to get recommended templates")
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
func (req *channelRequest) getFieldValue(key string) string {
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
		return req.Positioning
	case "keywords":
		return req.Keywords
	case "style":
		return req.Style
	case "theme":
		return req.Theme
	case "author":
		return req.Author
	case "reference_image_url":
		return req.ReferenceImageURL
	case "image_ratio":
		return req.ImageRatio
	case "layout":
		return req.Layout
	case "image_preset":
		return req.ImagePreset
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
func (h *ChannelHandler) getTierMaxConcurrent(c fiber.Ctx) int {
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
func (h *ChannelHandler) SeednoteLoginStatus(c fiber.Ctx) error {
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

// AnalyzeImage handles POST /channels/analyze-image.
// It sends the reference image to a vision LLM and returns a visual style description.
func (h *ChannelHandler) AnalyzeImage(c fiber.Ctx) error {
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

	llm := h.getLLMClient(c.Context(), userID)
	if llm == nil {
		return Error(c, fiber.StatusServiceUnavailable, "LLM service is not configured")
	}

	const maxAnalysisImageSize = 10 << 20 // 10 MB

	var imageData []byte

	// Always read the image server-side and convert to base64 to avoid SSRF.
	if strings.HasPrefix(req.ImageURL, "/api/v1/files/") || strings.HasPrefix(req.ImageURL, "/files/") {
		if h.store == nil {
			return Error(c, fiber.StatusInternalServerError, "file storage is not available")
		}
		key := strings.TrimPrefix(req.ImageURL, "/api/v1/files/")
		key = strings.TrimPrefix(key, "/files/")
		var err error
		imageData, err = h.store.Read(c.Context(), key)
		if err != nil {
			h.logger.Error().Err(err).Str("key", key).Msg("failed to read image for analysis")
			return Error(c, fiber.StatusInternalServerError, "failed to read image file")
		}
	} else if strings.HasPrefix(req.ImageURL, "https://") {
		resp, err := http.Get(req.ImageURL)
		if err != nil {
			h.logger.Error().Err(err).Str("url", req.ImageURL).Msg("failed to download external image")
			return Error(c, fiber.StatusInternalServerError, "failed to download image")
		}
		defer resp.Body.Close()
		imageData, err = io.ReadAll(io.LimitReader(resp.Body, maxAnalysisImageSize+1))
		if err != nil {
			return Error(c, fiber.StatusInternalServerError, "failed to read image")
		}
		if resp.StatusCode != http.StatusOK {
			return Error(c, fiber.StatusBadRequest, "failed to download image")
		}
	} else {
		return Error(c, fiber.StatusBadRequest, "image_url must start with /api/v1/files/ or https://")
	}

	if int64(len(imageData)) > maxAnalysisImageSize {
		return Error(c, fiber.StatusBadRequest, "image is too large for analysis (max 10MB)")
	}

	mimeType := http.DetectContentType(imageData)
	if !strings.HasPrefix(mimeType, "image/") {
		return Error(c, fiber.StatusBadRequest, "the file is not an image")
	}
	imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(imageData))

	systemPrompt := "你是一位专业的视觉风格分析师，专注于 AI 图片生成的风格描述。"
	userPrompt := "请分析这张图片的视觉风格，输出一段简洁的中文描述（50-100字），涵盖：配色方案、整体氛围、构图特点、元素风格。描述将用于 AI 生成图片的风格提示，请直接输出描述文本，不要有标题或其他格式。"

	result, err := llm.CompleteWithImage(c.Context(), systemPrompt, userPrompt, imageURL)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("image style analysis failed")
		return Error(c, fiber.StatusInternalServerError, "image style analysis failed")
	}

	style := strings.TrimSpace(result)
	if style == "" {
		return Error(c, fiber.StatusInternalServerError, "failed to analyze image style")
	}

	return Success(c, fiber.Map{"style": style})
}
