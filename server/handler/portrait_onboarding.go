package handler

import (
	"context"
	"errors"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"time"
)

type PortraitOnboardingAPI interface {
	Available() bool
	SpeechAvailable() bool
	Chat(context.Context, string, []byte) (*service.PortraitCandidate, error)
	Transcribe(context.Context, string, string, []byte) (string, error)
}
type PortraitOnboardingHandler struct{ svc PortraitOnboardingAPI }

func NewPortraitOnboardingHandler(svc PortraitOnboardingAPI) *PortraitOnboardingHandler {
	return &PortraitOnboardingHandler{svc: svc}
}
func (h *PortraitOnboardingHandler) Capabilities(c fiber.Ctx) error {
	if GetUserID(c) == "" {
		return Error(c, 401, "请先登录。")
	}
	c.Set("Cache-Control", "no-store")
	return Success(c, fiber.Map{"configured": h.svc.Available(), "speech_available": h.svc.SpeechAvailable()})
}
func (h *PortraitOnboardingHandler) Chat(c fiber.Ctx) error {
	if GetUserID(c) == "" {
		return Error(c, 401, "请先登录。")
	}
	if len(c.Body()) > 180000 {
		return Error(c, 413, "对话过长，请缩短后重试。")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 100*time.Second)
	defer cancel()
	candidate, err := h.svc.Chat(ctx, GetUserID(c), c.Body())
	if err != nil {
		return portraitError(c, err)
	}
	c.Set("Cache-Control", "no-store")
	return Success(c, fiber.Map{"candidate": candidate})
}
func (h *PortraitOnboardingHandler) Transcribe(c fiber.Ctx) error {
	if GetUserID(c) == "" {
		return Error(c, 401, "请先登录。")
	}
	if len(c.Body()) > 8*1024*1024 {
		return Error(c, 413, "录音过大，请缩短后重试。")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 100*time.Second)
	defer cancel()
	text, err := h.svc.Transcribe(ctx, GetUserID(c), c.Get("Content-Type"), c.Body())
	if err != nil {
		return portraitError(c, err)
	}
	c.Set("Cache-Control", "no-store")
	return Success(c, fiber.Map{"text": text})
}
func portraitError(c fiber.Ctx, err error) error {
	status := 502
	message := service.ErrPortraitProvider.Error()
	switch {
	case errors.Is(err, service.ErrPortraitInput), errors.Is(err, service.ErrPortraitAudio):
		status = 400
		message = err.Error()
	case errors.Is(err, service.ErrPortraitUnavailable):
		status = 503
		message = err.Error()
	case errors.Is(err, service.ErrPortraitBusy):
		status = 429
		message = err.Error()
		c.Set("Retry-After", "60")
	case errors.Is(err, service.ErrPortraitOutput):
		message = err.Error()
	}
	return Error(c, status, message)
}
