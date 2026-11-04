package handler

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

type TopicRadarHandler struct {
	service *service.TopicRadarService
	repo    repository.Repository
	logger  *zerolog.Logger
}

func NewTopicRadarHandler(svc *service.TopicRadarService, repo repository.Repository, logger *zerolog.Logger) *TopicRadarHandler {
	return &TopicRadarHandler{service: svc, repo: repo, logger: logger}
}

type createTopicSubscriptionRequest struct {
	Name         string         `json:"name"`
	Enabled      bool           `json:"enabled"`
	Timezone     string         `json:"timezone"`
	Schedule     datatypes.JSON `json:"schedule"`
	SourceConfig datatypes.JSON `json:"source_config"`
	Weights      datatypes.JSON `json:"weights"`
	ScriptPolicy datatypes.JSON `json:"script_policy"`
}

type runTopicDigestRequest struct {
	RequestKey string `json:"request_key"`
}
type topicScriptRequest struct {
	ProjectID       string `json:"project_id"`
	TargetPlatform  string `json:"target_platform"`
	DurationSeconds int    `json:"duration_seconds"`
	AspectRatio     string `json:"aspect_ratio"`
}

type createTopicNotificationChannelRequest struct {
	Provider   string `json:"provider"`
	Name       string `json:"name"`
	WebhookURL string `json:"webhook_url"`
	Enabled    bool   `json:"enabled"`
}

func (h *TopicRadarHandler) ListSubscriptions(c fiber.Ctx) error {
	userID := GetUserID(c)
	projectID := strings.TrimSpace(c.Params("project_id"))
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if projectID == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}
	items, err := h.service.ListSubscriptions(c.Context(), userID, projectID)
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, fiber.Map{"items": items})
}

func (h *TopicRadarHandler) CreateSubscription(c fiber.Ctx) error {
	userID := GetUserID(c)
	projectID := strings.TrimSpace(c.Params("project_id"))
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req createTopicSubscriptionRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	item, err := h.service.CreateSubscription(c.Context(), service.CreateTopicSubscriptionParams{UserID: userID, ProjectID: projectID, Name: req.Name, Enabled: req.Enabled, Timezone: req.Timezone, Schedule: req.Schedule, SourceConfig: req.SourceConfig, Weights: req.Weights, ScriptPolicy: req.ScriptPolicy})
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, item)
}

func (h *TopicRadarHandler) RunDigest(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req runTopicDigestRequest
	if c.Request().Body() != nil && len(c.Request().Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid request body")
		}
	}
	item, err := h.service.Run(c.Context(), service.RunTopicDigestParams{UserID: userID, SubscriptionID: c.Params("subscription_id"), RequestKey: req.RequestKey})
	// A durable row is accepted even while the queue is unavailable; recovery owns dispatch.
	if err != nil && item == nil {
		return topicRadarError(c, err)
	}
	return Success(c, item)
}

func (h *TopicRadarHandler) ListDigests(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	items, err := h.service.ListDigests(c.Context(), userID, c.Params("subscription_id"))
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, fiber.Map{"items": items})
}

func (h *TopicRadarHandler) ListSuggestions(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	detail, err := h.service.GetDigest(c.Context(), userID, c.Params("digest_id"))
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, fiber.Map{"items": detail.Suggestions})
}

func (h *TopicRadarHandler) RequestScript(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req topicScriptRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	job, err := h.service.RequestTopicScript(c.Context(), service.RequestTopicScriptParams{UserID: userID, AssetID: c.Params("id"), ProjectID: req.ProjectID, TargetPlatform: req.TargetPlatform, DurationSeconds: req.DurationSeconds, AspectRatio: req.AspectRatio})
	if err != nil && job == nil {
		return topicRadarError(c, err)
	}
	return Success(c, job)
}

func (h *TopicRadarHandler) ListNotificationChannels(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	items, err := h.service.ListNotificationChannels(c.Context(), userID, c.Params("project_id"))
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, fiber.Map{"items": items})
}

func (h *TopicRadarHandler) CreateNotificationChannel(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req createTopicNotificationChannelRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	item, err := h.service.CreateNotificationChannel(c.Context(), service.CreateTopicNotificationChannelParams{UserID: userID, ProjectID: c.Params("project_id"), Provider: req.Provider, Name: req.Name, WebhookURL: req.WebhookURL, Enabled: req.Enabled})
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, item)
}

func topicRadarError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, service.ErrTopicProjectAccessDenied):
		return Error(c, fiber.StatusForbidden, "forbidden")
	case errors.Is(err, service.ErrTopicSubscriptionNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return Error(c, fiber.StatusNotFound, "topic radar resource not found")
	case errors.Is(err, service.ErrTopicSubscriptionInvalid):
		return Error(c, fiber.StatusBadRequest, err.Error())
	default:
		return Error(c, fiber.StatusInternalServerError, "topic radar operation failed; please retry")
	}
}
