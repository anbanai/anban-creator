package handler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/scheduler"
	"github.com/royalrick/anbanwriter/server/service"
)

// TaskHandler handles task-related HTTP endpoints.
type TaskHandler struct {
	service *service.TaskService
	logger  *zerolog.Logger
}

// NewTaskHandler creates a new TaskHandler.
func NewTaskHandler(svc *service.TaskService, logger *zerolog.Logger) *TaskHandler {
	return &TaskHandler{service: svc, logger: logger}
}

// Request types.

type createTaskRequest struct {
	Type  string `json:"type"`
	Topic string `json:"topic"`
}

// Create handles POST /api/v1/tasks.
func (h *TaskHandler) Create(c fiber.Ctx) error {
	var req createTaskRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if !scheduler.IsValidType(req.Type) {
		return Error(c, fiber.StatusBadRequest, "type must be one of: article, xls, rednote")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	task, err := h.service.CreateManual(c.Context(), userID, req.Type, req.Topic)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create task failed")
		return Error(c, fiber.StatusInternalServerError, "failed to create task")
	}

	return Success(c, task)
}

// List handles GET /api/v1/tasks.
func (h *TaskHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	status := c.Query("status", "")

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	tasks, total, err := h.service.List(c.Context(), userID, offset, limit, status)
	if err != nil {
		h.logger.Error().Err(err).Msg("list tasks failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list tasks")
	}

	return Success(c, fiber.Map{
		"items": tasks,
		"total": total,
	})
}

// GetByID handles GET /api/v1/tasks/:id.
func (h *TaskHandler) GetByID(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "task id is required")
	}

	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}

	return Success(c, task)
}

// Cancel handles POST /api/v1/tasks/:id/cancel.
func (h *TaskHandler) Cancel(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "task id is required")
	}

	if err := h.service.Cancel(c.Context(), id); err != nil {
		h.logger.Error().Err(err).Msg("cancel task failed")
		return Error(c, fiber.StatusInternalServerError, "failed to cancel task")
	}

	return Success(c, fiber.Map{"message": "task cancelled"})
}

// GetFiles handles GET /api/v1/tasks/:id/files.
func (h *TaskHandler) GetFiles(c fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return Error(c, fiber.StatusBadRequest, "task id is required")
	}

	files, err := h.service.GetFiles(c.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Msg("get task files failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get task files")
	}

	return Success(c, files)
}

// Stream handles GET /api/v1/tasks/:id/stream — SSE endpoint for real-time progress.
func (h *TaskHandler) Stream(c fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	taskID := c.Params("id")
	if taskID == "" {
		return Error(c, fiber.StatusBadRequest, "task id is required")
	}

	ctx := c.Context()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastLogLen := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			task, err := h.service.GetByID(ctx, taskID)
			if err != nil {
				// Client likely disconnected or task no longer exists.
				return nil
			}

			// Send new progress entries if the log grew.
			newLog := ""
			if len(task.ProgressLog) > lastLogLen {
				newLog = task.ProgressLog[lastLogLen:]
				lastLogLen = len(task.ProgressLog)
			}

			if len(newLog) > 0 {
				escaped, err := json.Marshal(newLog)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(c, "event: progress\ndata: %s\n\n", escaped); err != nil {
					return nil
				}
			}

			// Terminal states: close the stream.
			if task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
				statusData, _ := json.Marshal(map[string]string{
					"status": task.Status,
				})
				if _, err := fmt.Fprintf(c, "event: %s\ndata: %s\n\n", task.Status, statusData); err != nil {
					return nil
				}
				return nil
			}
		}
	}
}
