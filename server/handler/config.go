package handler

import (
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// ConfigHandler handles user configuration-related HTTP endpoints.
type ConfigHandler struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewConfigHandler creates a new ConfigHandler.
func NewConfigHandler(repo repository.Repository, logger *zerolog.Logger) *ConfigHandler {
	return &ConfigHandler{repo: repo, logger: logger}
}

// Request types.

type updateConfigRequest struct {
	Name           string `json:"name"`
	Keywords       string `json:"keywords"`
	Positioning    string `json:"positioning"`
	Style          string `json:"style"`
	Theme          string `json:"theme"`
	Author         string `json:"author"`
	WechatAppID    string `json:"wechat_app_id"`
	WechatSecret   string `json:"wechat_secret"`
	ImageAPIConfig string `json:"image_api_config"`
}

// List handles GET /api/v1/configs — returns all configs for the authenticated user.
func (h *ConfigHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	configs, err := h.repo.UserConfigs().ListByUserID(c.Context(), userID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list configs failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list configs")
	}

	if configs == nil {
		configs = []*model.UserConfig{}
	}

	return Success(c, configs)
}

// GetByScope handles GET /api/v1/configs/:scope — returns config for a specific scope.
func (h *ConfigHandler) GetByScope(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	scope := c.Params("scope")
	if !isValidScope(scope) {
		return Error(c, fiber.StatusBadRequest, "scope must be one of: article, xls, rednote")
	}

	config, err := h.repo.UserConfigs().FindByUserAndScope(c.Context(), userID, scope)
	if err != nil {
		// Return empty config instead of 404.
		return Success(c, nil)
	}

	return Success(c, config)
}

// Upsert handles PUT /api/v1/configs/:scope — creates or updates a config.
func (h *ConfigHandler) Upsert(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	scope := c.Params("scope")
	if !isValidScope(scope) {
		return Error(c, fiber.StatusBadRequest, "scope must be one of: article, xls, rednote")
	}

	var req updateConfigRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	config := &model.UserConfig{
		UserID:         userID,
		Scope:          scope,
		Name:           req.Name,
		Keywords:       req.Keywords,
		Positioning:    req.Positioning,
		Style:          req.Style,
		Theme:          req.Theme,
		Author:         req.Author,
		WechatAppID:    req.WechatAppID,
		WechatSecret:   req.WechatSecret,
		ImageAPIConfig: req.ImageAPIConfig,
	}

	if err := h.repo.UserConfigs().Upsert(c.Context(), config); err != nil {
		h.logger.Error().Err(err).Msg("upsert config failed")
		return Error(c, fiber.StatusInternalServerError, "failed to save config")
	}

	return Success(c, config)
}

// isValidScope checks if the given scope is valid.
func isValidScope(scope string) bool {
	switch scope {
	case model.ScopeArticle, model.ScopeXls, model.ScopeRednote:
		return true
	default:
		return false
	}
}
