package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

// AssistantHandler exposes mode-oriented routes without allowing the browser
// to select an Agent or DSH preset.
type AssistantHandler struct {
	svc         *service.InteractiveService
	interactive *InteractiveHandler
	tasks       *service.TaskService
	repo        repository.Repository
}

func NewAssistantHandler(svc *service.InteractiveService, tasks *service.TaskService, repo repository.Repository) *AssistantHandler {
	return &AssistantHandler{svc: svc, interactive: NewInteractiveHandler(svc), tasks: tasks, repo: repo}
}

func (h *AssistantHandler) Bootstrap(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	bootstrap, err := h.svc.AssistantBootstrap(c.Context(), uid)
	if err != nil {
		return interactiveError(c, err)
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return Success(c, bootstrap)
}

func (h *AssistantHandler) Works(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	limit, err := strconv.Atoi(c.Query("limit", "0"))
	if err != nil || limit < 0 {
		return Error(c, fiber.StatusBadRequest, "invalid limit")
	}
	var cursor *service.AssistantWorkCursor
	createdAt := strings.TrimSpace(c.Query("before_created_at"))
	beforeID := strings.TrimSpace(c.Query("before_id"))
	if createdAt != "" || beforeID != "" {
		parsedAt, parseErr := time.Parse(time.RFC3339Nano, createdAt)
		if parseErr != nil || beforeID == "" {
			return Error(c, fiber.StatusBadRequest, "invalid work cursor")
		}
		cursor = &service.AssistantWorkCursor{CreatedAt: parsedAt, ID: beforeID}
	}
	page, err := h.svc.AssistantWorks(c.Context(), uid, strings.TrimSpace(c.Params("projectId")), cursor, limit)
	if err != nil {
		return interactiveError(c, err)
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return Success(c, page)
}

func (h *AssistantHandler) LatestCheckpoint(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	modeID := strings.TrimSpace(c.Query("mode_id"))
	projectID := strings.TrimSpace(c.Query("project_id"))
	contentID := strings.TrimSpace(c.Query("content_id"))
	checkpoint, err := h.svc.LatestAssistantCheckpoint(c.Context(), uid, modeID, projectID, contentID)
	if err != nil {
		return interactiveError(c, err)
	}
	if checkpoint == nil {
		return Success(c, nil)
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return Success(c, service.AssistantCheckpointSummary{
		ID: checkpoint.ID, ProjectID: checkpoint.ProjectID, ContentID: checkpoint.ContentID,
		Summary: checkpoint.Summary, Status: checkpoint.Status, UpdatedAt: checkpoint.UpdatedAt,
	})
}

func (h *AssistantHandler) Create(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.AssistantSessionInput
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	sess, err := h.svc.CreateAssistantSessionWithIdempotency(c.Context(), uid, req, c.Get("Idempotency-Key"))
	if err != nil {
		return interactiveError(c, err)
	}
	status := "starting"
	if current, stateErr := h.svc.SessionState(uid, sess.ID); stateErr == nil && current != "" {
		status = current
	}
	return Success(c, fiber.Map{
		"id": sess.ID, "status": status, "mode_id": sess.ModeID, "content_id": sess.ContentID,
		"project_id": sess.ProjectID, "expires_at": sess.ExpiresAt,
	})
}

func (h *AssistantHandler) Events(c fiber.Ctx) error  { return h.interactive.Events(c) }
func (h *AssistantHandler) Message(c fiber.Ctx) error { return h.interactive.Message(c) }
func (h *AssistantHandler) Answers(c fiber.Ctx) error { return h.interactive.Answers(c) }
func (h *AssistantHandler) Delete(c fiber.Ctx) error  { return h.interactive.Delete(c) }

func (h *AssistantHandler) Checkpoint(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.AssistantCheckpointInput
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	checkpoint, err := h.svc.SaveAssistantCheckpoint(c.Context(), uid, c.Params("id"), req)
	if err != nil {
		return interactiveError(c, err)
	}
	return Success(c, checkpoint)
}

func (h *AssistantHandler) Commit(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req service.AssistantCommitRequest
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	result, err := h.svc.CommitAssistant(c.Context(), uid, c.Params("id"), req, h.tasks, h.repo)
	if err != nil {
		return interactiveError(c, err)
	}
	return Success(c, result)
}
