package handler

import (
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

func (h *TopicRadarHandler) UpdateSubscription(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.UpdateTopicSubscriptionParams
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	item, err := h.service.UpdateSubscription(c.Context(), userID, c.Params("subscription_id"), req)
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, item)
}
func (h *TopicRadarHandler) DeleteSubscription(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := h.service.DeleteSubscription(c.Context(), userID, c.Params("subscription_id")); err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, fiber.Map{"deleted": true})
}

func (h *TopicRadarHandler) GetDigest(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	item, err := h.service.GetDigest(c.Context(), userID, c.Params("digest_id"))
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, item)
}
func (h *TopicRadarHandler) RetryDigest(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	item, err := h.service.Retry(c.Context(), userID, c.Params("digest_id"))
	if err != nil && item == nil {
		return topicRadarError(c, err)
	}
	return Success(c, item)
}
func (h *TopicRadarHandler) GetScript(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	item, err := h.service.GetScript(c.Context(), userID, c.Params("script_id"))
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, item)
}
func (h *TopicRadarHandler) RetryScript(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	item, err := h.service.RetryScript(c.Context(), userID, c.Params("script_id"))
	if err != nil && item == nil {
		return topicRadarError(c, err)
	}
	return Success(c, item)
}
func (h *TopicRadarHandler) UpdateNotificationChannel(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.UpdateTopicNotificationChannelParams
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	item, err := h.service.UpdateNotificationChannel(c.Context(), userID, c.Params("channel_id"), req)
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, item)
}
func (h *TopicRadarHandler) DeleteNotificationChannel(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := h.service.DeleteNotificationChannel(c.Context(), userID, c.Params("channel_id")); err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, fiber.Map{"deleted": true})
}

func (h *TopicRadarHandler) GetScriptArtifact(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	item, err := h.service.GetScriptArtifact(c.Context(), userID, c.Params("script_id"))
	if err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, item)
}
func (h *TopicRadarHandler) TestNotificationChannel(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := h.service.TestNotificationChannel(c.Context(), userID, c.Params("channel_id")); err != nil {
		return topicRadarError(c, err)
	}
	return Success(c, fiber.Map{"sent": true})
}
