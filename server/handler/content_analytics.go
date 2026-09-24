package handler

import (
	"context"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"strconv"
	"strings"
)

type ContentAnalyticsAPI interface {
	Candidates(context.Context, string, string, string, int, int) ([]service.AnalyticsCandidate, int, error)
}
type ContentAnalyticsHandler struct{ service ContentAnalyticsAPI }

func NewContentAnalyticsHandler(svc ContentAnalyticsAPI) *ContentAnalyticsHandler {
	return &ContentAnalyticsHandler{service: svc}
}
func (h *ContentAnalyticsHandler) Candidates(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, 401, "unauthorized")
	}
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "25"))
	items, total, err := h.service.Candidates(c.Context(), userID, projectID, c.Query("search"), offset, limit)
	if err != nil {
		if strings.Contains(err.Error(), "does not belong") {
			return Forbidden(c, "you do not have access to this project")
		}
		return Error(c, 400, err.Error())
	}
	return Success(c, fiber.Map{"items": items, "total": total})
}
