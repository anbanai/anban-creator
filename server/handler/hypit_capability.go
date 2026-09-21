package handler

import (
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"strings"
)

type HypitCapabilityHandler struct {
	service *service.HypitCapabilityService
	tasks   *service.TaskService
}

func NewHypitCapabilityHandler(s *service.HypitCapabilityService) *HypitCapabilityHandler {
	return &HypitCapabilityHandler{service: s}
}
func (h *HypitCapabilityHandler) SetTaskService(tasks *service.TaskService) { h.tasks = tasks }
func (h *HypitCapabilityHandler) List(c fiber.Ctx) error {
	if sourceID := strings.TrimSpace(c.Query("source_task_id")); sourceID != "" {
		if h.tasks == nil {
			return Error(c, fiber.StatusServiceUnavailable, "source capabilities unavailable")
		}
		catalog, err := h.tasks.HypitCapabilitiesForSource(c.Context(), GetUserID(c), sourceID)
		if err != nil {
			return Error(c, fiber.StatusForbidden, "source task unavailable")
		}
		return Success(c, catalog)
	}
	return Success(c, h.service.Catalog())
}
