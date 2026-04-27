package handler

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/platform"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

// ChannelHandler handles channel-related HTTP endpoints.
type ChannelHandler struct {
	service *service.ChannelService
	logger  *zerolog.Logger
}

// NewChannelHandler creates a new ChannelHandler.
func NewChannelHandler(svc *service.ChannelService, logger *zerolog.Logger) *ChannelHandler {
	return &ChannelHandler{service: svc, logger: logger}
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

	ch := req.toChannel()

	// Force max_concurrent_tasks based on user tier.
	ch.MaxConcurrentTasks = h.getTierMaxConcurrent(c)

	created, err := h.service.Create(c.Context(), userID, ch)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create channel failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create channel: "+err.Error())
	}

	service.SanitizeChannel(created)
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
	// Optional WeChat credentials for article/xls platforms.
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

	// SSRF protection: validate URL scheme and host.
	parsedURL, err := url.ParseRequestURI(req.ProfileURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return Error(c, fiber.StatusBadRequest, "invalid profile URL")
	}

	// Validate URL matches the platform's expected pattern.
	pc := model.GetPlatformConfig(req.Platform)
	if pc != nil && pc.ProfileURLPattern != "" {
		matched, _ := regexp.MatchString(pc.ProfileURLPattern, req.ProfileURL)
		if !matched {
			return Error(c, fiber.StatusBadRequest, "profile URL does not match expected pattern for "+pc.Label)
		}
	}

	// Only pass credentials for publishing platforms.
	var appID, secret string
	if pc != nil && pc.SupportsPublishing {
		appID = req.WechatAppID
		secret = req.WechatSecret
	}

	provider := platform.NewProvider(req.Platform, appID, secret)
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
