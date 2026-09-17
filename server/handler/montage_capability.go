package handler

import (
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

type MontageCapabilityHandler struct {
	service *service.MontageCapabilityService
}

func NewMontageCapabilityHandler(svc *service.MontageCapabilityService) *MontageCapabilityHandler {
	return &MontageCapabilityHandler{service: svc}
}

func (h *MontageCapabilityHandler) List(c fiber.Ctx) error {
	if h == nil || h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "montage capabilities unavailable")
	}
	return Success(c, h.service.Catalog())
}
