package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type SeednoteImportAPI interface {
	Revoke(context.Context, string, string, string) (*service.SeednoteImportSummary, error)
	Import(ctx context.Context, req service.SeednoteImportRequest) (*service.SeednoteImportSummary, error)
	ListBatches(ctx context.Context, userID, projectID string, offset, limit int) ([]*model.SeednoteImportBatch, int64, error)
	GetBatch(ctx context.Context, userID, projectID, batchID string) (*service.SeednoteImportSummary, error)
	Resolve(ctx context.Context, userID, projectID, batchID string, actions []service.SeednoteResolveAction) (*service.SeednoteImportSummary, error)
	Overview(ctx context.Context, userID, projectID string, from, to *time.Time) (*service.SeednoteImportOverview, error)
	ListPosts(ctx context.Context, userID, projectID, search string, offset, limit int) ([]*model.SeednotePost, int64, error)
	GetPost(ctx context.Context, userID, projectID, postID string, from, to *time.Time) (*model.SeednotePost, []*model.SeednoteMetricVersion, error)
	FileURL(ctx context.Context, userID, projectID, batchID string) (string, time.Time, error)
}

type SeednoteImportHandler struct {
	service SeednoteImportAPI
	logger  *zerolog.Logger
}

func NewSeednoteImportHandler(svc SeednoteImportAPI, logger *zerolog.Logger) *SeednoteImportHandler {
	return &SeednoteImportHandler{service: svc, logger: logger}
}

func (h *SeednoteImportHandler) projectID(c fiber.Ctx) (string, error) {
	return validateUUIDParam(c, "id")
}
func (h *SeednoteImportHandler) Import(c fiber.Ctx) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, 401, "unauthorized")
	}
	var req service.SeednoteImportRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, 400, "invalid request body")
	}
	req.UserID, req.ProjectID = userID, projectID
	result, err := h.service.Import(c.Context(), req)
	return h.respond(c, result, err)
}
func (h *SeednoteImportHandler) List(c fiber.Ctx) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	batches, total, err := h.service.ListBatches(c.Context(), userID, projectID, offset, limit)
	if err != nil {
		return h.respond(c, nil, err)
	}
	return Success(c, fiber.Map{"items": batches, "total": total, "offset": offset, "limit": limit})
}
func (h *SeednoteImportHandler) Detail(c fiber.Ctx) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	batchID := strings.TrimSpace(c.Params("batchId"))
	result, err := h.service.GetBatch(c.Context(), GetUserID(c), projectID, batchID)
	return h.respond(c, result, err)
}
func (h *SeednoteImportHandler) Resolve(c fiber.Ctx) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	var body struct {
		Actions []service.SeednoteResolveAction `json:"actions"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return Error(c, 400, "invalid request body")
	}
	result, err := h.service.Resolve(c.Context(), GetUserID(c), projectID, c.Params("batchId"), body.Actions)
	return h.respond(c, result, err)
}
func (h *SeednoteImportHandler) Overview(c fiber.Ctx) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	from, err := parseDateQuery(c.Query("from"))
	if err != nil {
		return Error(c, 400, err.Error())
	}
	to, err := parseDateQuery(c.Query("to"))
	if err != nil {
		return Error(c, 400, err.Error())
	}
	result, err := h.service.Overview(c.Context(), GetUserID(c), projectID, from, to)
	return h.respond(c, result, err)
}
func (h *SeednoteImportHandler) Posts(c fiber.Ctx) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	posts, total, err := h.service.ListPosts(c.Context(), GetUserID(c), projectID, c.Query("search"), offset, limit)
	if err != nil {
		return h.respond(c, nil, err)
	}
	return Success(c, fiber.Map{"items": posts, "total": total})
}
func (h *SeednoteImportHandler) Post(c fiber.Ctx) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	from, err := parseDateQuery(c.Query("from"))
	if err != nil {
		return Error(c, 400, err.Error())
	}
	to, err := parseDateQuery(c.Query("to"))
	if err != nil {
		return Error(c, 400, err.Error())
	}
	post, versions, err := h.service.GetPost(c.Context(), GetUserID(c), projectID, c.Params("postId"), from, to)
	return h.respond(c, fiber.Map{"post": post, "versions": versions}, err)
}
func (h *SeednoteImportHandler) File(c fiber.Ctx) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	url, expires, err := h.service.FileURL(c.Context(), GetUserID(c), projectID, c.Params("batchId"))
	if err != nil {
		return h.respond(c, nil, err)
	}
	return Success(c, fiber.Map{"url": url, "expires_at": expires})
}
func parseDateQuery(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	// Analytics dates are calendar days in the import default timezone. Parsing
	// them as UTC would shift Shanghai data by eight hours at both boundaries.
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	t, err := time.ParseInLocation("2006-01-02", raw, location)
	if err != nil {
		return nil, errors.New("日期必须使用 YYYY-MM-DD 格式")
	}
	return &t, nil
}
func (h *SeednoteImportHandler) respond(c fiber.Ctx, value any, err error) error {
	if err == nil {
		return Success(c, value)
	}
	if errors.Is(err, service.ErrAnalyticsImportRevoked) {
		return Error(c, fiber.StatusConflict, err.Error())
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Error(c, 404, "not found")
	}
	if strings.Contains(err.Error(), "does not belong") {
		return Forbidden(c, "you do not have access to this project")
	}
	if h.logger != nil {
		h.logger.Error().Err(err).Msg("seednote import request failed")
	}
	return Error(c, 400, err.Error())
}

func (h *SeednoteImportHandler) Revoke(c fiber.Ctx) error {
	projectID, err := validateUUIDParam(c, "id")
	if err != nil || projectID == "" {
		return err
	}
	batchID, err := validateUUIDParam(c, "batchId")
	if err != nil || batchID == "" {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	result, err := h.service.Revoke(c.Context(), userID, projectID, batchID)
	return h.respond(c, result, err)
}
