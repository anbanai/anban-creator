package handler

import (
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

type LocalLifeCapabilityHandler struct {
	service *service.LocalLifeCapabilityService
}

func NewLocalLifeCapabilityHandler(svc *service.LocalLifeCapabilityService) *LocalLifeCapabilityHandler {
	return &LocalLifeCapabilityHandler{service: svc}
}

func (h *LocalLifeCapabilityHandler) List(c fiber.Ctx) error {
	if h == nil || h.service == nil {
		return Error(c, fiber.StatusServiceUnavailable, "素材混剪能力不可用")
	}
	return Success(c, h.service.Catalog())
}
