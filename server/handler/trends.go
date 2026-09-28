package handler

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
)

type TrendsHandler struct {
	service *service.TrendService
	logger  *zerolog.Logger
}

func NewTrendsHandler(svc *service.TrendService, logger *zerolog.Logger) *TrendsHandler {
	return &TrendsHandler{service: svc, logger: logger}
}
func parseTrendPlatforms(value string) []string {
	var out []string
	for _, p := range strings.Split(value, ",") {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return out
}
func trendLimit(c fiber.Ctx) int {
	limit, _ := strconv.Atoi(c.Query("limit", "12"))
	if limit <= 0 {
		limit = 12
	}
	if limit > 100 {
		limit = 100
	}
	return limit
}
func (h *TrendsHandler) List(c fiber.Ctx) error {
	result, err := h.service.List(c.Context(), parseTrendPlatforms(c.Query("platforms")), trendLimit(c), false)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, result)
}

type refreshTrendsRequest struct {
	Platforms []string `json:"platforms"`
	Limit     int      `json:"limit"`
}

func (h *TrendsHandler) Refresh(c fiber.Ctx) error {
	var req refreshTrendsRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Limit <= 0 {
		req.Limit = 12
	}
	result, err := h.service.List(c.Context(), req.Platforms, req.Limit, true)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, result)
}
