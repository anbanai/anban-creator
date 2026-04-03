package handler

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
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

// createChannelRequest is the request body for creating a channel.
type createChannelRequest struct {
	Platform      string `json:"platform"`
	Name          string `json:"name"`
	WechatAppID   string `json:"wechat_app_id"`
	WechatSecret  string `json:"wechat_secret"`
	Keywords      string `json:"keywords"`
	Positioning   string `json:"positioning"`
	Style         string `json:"style"`
	Theme         string `json:"theme"`
	Author        string `json:"author"`
	ImageAPIConfig string `json:"image_api_config"`
	Description   string `json:"description"`
	AvatarURL     string `json:"avatar_url"`
}

// updateChannelRequest is the request body for updating a channel.
type updateChannelRequest struct {
	Platform      string `json:"platform"`
	Name          string `json:"name"`
	WechatAppID   string `json:"wechat_app_id"`
	WechatSecret  string `json:"wechat_secret"`
	Keywords      string `json:"keywords"`
	Positioning   string `json:"positioning"`
	Style         string `json:"style"`
	Theme         string `json:"theme"`
	Author        string `json:"author"`
	ImageAPIConfig string `json:"image_api_config"`
	Description   string `json:"description"`
	AvatarURL     string `json:"avatar_url"`
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

	return Success(c, channels)
}

// Create handles POST /channels.
func (h *ChannelHandler) Create(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req createChannelRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.Platform == "" {
		return Error(c, fiber.StatusBadRequest, "platform is required")
	}
	if req.Name == "" {
		return Error(c, fiber.StatusBadRequest, "name is required")
	}

	ch := &model.Channel{
		Platform:      req.Platform,
		Name:          req.Name,
		WechatAppID:   req.WechatAppID,
		WechatSecret:  req.WechatSecret,
		Keywords:      req.Keywords,
		Positioning:   req.Positioning,
		Style:         req.Style,
		Theme:         req.Theme,
		Author:        req.Author,
		ImageAPIConfig: req.ImageAPIConfig,
		Description:   req.Description,
		AvatarURL:     req.AvatarURL,
	}

	created, err := h.service.Create(c.Context(), userID, ch)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create channel failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create channel")
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

	var req updateChannelRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	ch := &model.Channel{
		Platform:      req.Platform,
		Name:          req.Name,
		WechatAppID:   req.WechatAppID,
		WechatSecret:  req.WechatSecret,
		Keywords:      req.Keywords,
		Positioning:   req.Positioning,
		Style:         req.Style,
		Theme:         req.Theme,
		Author:        req.Author,
		ImageAPIConfig: req.ImageAPIConfig,
		Description:   req.Description,
		AvatarURL:     req.AvatarURL,
	}

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
		return Error(c, fiber.StatusInternalServerError, "failed to delete channel")
	}

	return Success(c, fiber.Map{"message": "channel deleted"})
}
