package handler

import (
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

type AgentProfileHandler struct {
	repo     repository.Repository
	registry *service.AgentProfileRegistry
	logger   *zerolog.Logger
}

func NewAgentProfileHandler(repo repository.Repository, registry *service.AgentProfileRegistry, logger *zerolog.Logger) *AgentProfileHandler {
	return &AgentProfileHandler{repo: repo, registry: registry, logger: logger}
}

func (h *AgentProfileHandler) List(c fiber.Ctx) error {
	userID, _ := c.Locals("user_id").(string)
	if strings.TrimSpace(userID) == "" {
		return Error(c, fiber.StatusUnauthorized, "authentication required")
	}
	if h == nil || h.repo == nil || h.registry == nil {
		return Error(c, fiber.StatusServiceUnavailable, "agent execution profiles unavailable")
	}
	user, err := h.repo.Users().FindByID(c.Context(), userID)
	if err != nil {
		if h.logger != nil {
			h.logger.Error().Err(err).Str("user_id", userID).Msg("load agent profile capabilities failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to load agent execution profiles")
	}
	return Success(c, h.registry.CapabilitiesForTier(model.ResolveTier(user.Tier)))
}
