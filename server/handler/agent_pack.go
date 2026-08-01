package handler

import (
	"github.com/gofiber/fiber/v3"

	"github.com/anbanai/anban-creator/server/agentpack"
)

// AgentPackHandler exposes the immutable, generated Agent Pack Catalog used by
// both server routing and Studio scenario discovery.
type AgentPackHandler struct {
	catalog *agentpack.Catalog
}

func NewAgentPackHandler() *AgentPackHandler {
	return &AgentPackHandler{catalog: agentpack.Default()}
}

func (h *AgentPackHandler) List(c fiber.Ctx) error {
	return Success(c, h.catalog)
}
