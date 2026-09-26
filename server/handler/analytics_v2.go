package handler

import (
	"errors"
	"strconv"

	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type AnalyticsV2Handler struct{ service *service.AnalyticsService }

func NewAnalyticsV2Handler(s *service.AnalyticsService) *AnalyticsV2Handler {
	return &AnalyticsV2Handler{service: s}
}
func analyticsQuery(c fiber.Ctx) (service.AnalyticsQuery, error) {
	q := service.AnalyticsQuery{From: c.Query("from"), To: c.Query("to"), Granularity: c.Query("granularity"), MetricBasis: c.Query("metric_basis"), Search: c.Query("search"), ContentType: c.Query("content_type"), Sort: c.Query("sort"), Direction: c.Query("direction")}
	var err error
	q.Offset, err = strconv.Atoi(c.Query("offset", "0"))
	if err != nil {
		return q, errors.New("invalid offset")
	}
	q.Limit, err = strconv.Atoi(c.Query("limit", "25"))
	if err != nil {
		return q, errors.New("invalid limit")
	}
	if raw := c.Query("expected_revision"); raw != "" {
		revision, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || revision < 0 {
			return q, errors.New("invalid revision")
		}
		q.ExpectedRevision = &revision
	}
	return q, nil
}
func analyticsResponse(c fiber.Ctx, value any, err error) error {
	if err == nil {
		return Success(c, value)
	}
	switch {
	case errors.Is(err, service.ErrAnalyticsForbidden):
		return Forbidden(c, "you do not have access to this project")
	case errors.Is(err, service.ErrAnalyticsRevisionConflict), errors.Is(err, repository.ErrAnalyticsIdempotencyConflict):
		return Error(c, 409, err.Error())
	case errors.Is(err, repository.ErrAnalyticsRebuilding):
		return Error(c, 503, "统计数据正在重建，请稍后重试")
	case errors.Is(err, gorm.ErrRecordNotFound):
		return Error(c, 404, "content not found")
	default:
		return Error(c, 400, err.Error())
	}
}
func (h *AnalyticsV2Handler) read(c fiber.Ctx, operation string) error {
	project, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	user := GetUserID(c)
	if user == "" {
		return Error(c, 401, "unauthorized")
	}
	q, err := analyticsQuery(c)
	if err != nil {
		return Error(c, 400, err.Error())
	}
	switch operation {
	case "overview":
		value, err := h.service.Overview(c.Context(), user, project, q)
		return analyticsResponse(c, value, err)
	case "contents":
		value, err := h.service.Contents(c.Context(), user, project, q)
		return analyticsResponse(c, value, err)
	case "detail":
		value, err := h.service.Detail(c.Context(), user, project, c.Params("contentId"), q)
		return analyticsResponse(c, value, err)
	case "observations":
		value, err := h.service.Observations(c.Context(), user, project, c.Params("contentId"), q)
		return analyticsResponse(c, value, err)
	case "dates":
		year, e := strconv.Atoi(c.Query("year"))
		if e != nil {
			return Error(c, 400, "invalid year")
		}
		value, err := h.service.Dates(c.Context(), user, project, year, q)
		return analyticsResponse(c, value, err)
	}
	return Error(c, 404, "not found")
}
func (h *AnalyticsV2Handler) Overview(c fiber.Ctx) error     { return h.read(c, "overview") }
func (h *AnalyticsV2Handler) Contents(c fiber.Ctx) error     { return h.read(c, "contents") }
func (h *AnalyticsV2Handler) Detail(c fiber.Ctx) error       { return h.read(c, "detail") }
func (h *AnalyticsV2Handler) Observations(c fiber.Ctx) error { return h.read(c, "observations") }
func (h *AnalyticsV2Handler) Dates(c fiber.Ctx) error        { return h.read(c, "dates") }
