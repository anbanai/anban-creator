package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/service"
)

const maxTaskPromptCharacters = 5120

// TaskHandler handles task-related HTTP endpoints.
type TaskHandler struct {
	service    *service.TaskService
	logger     *zerolog.Logger
	dataDir    string // local storage data directory (for ServeLocalFile)
	taskLogDir string // task log directory (for GetLog)
}

// NewTaskHandler creates a new TaskHandler.
func NewTaskHandler(svc *service.TaskService, logger *zerolog.Logger, dirs ...string) *TaskHandler {
	h := &TaskHandler{service: svc, logger: logger}
	if svc != nil {
		h.taskLogDir = svc.TaskLogDir()
	}
	if len(dirs) > 0 {
		h.dataDir = dirs[0]
	}
	return h
}

// Request types.

type createTaskRequest struct {
	ChannelID          string `json:"channel_id"`
	Prompt             string `json:"prompt"`
	Quantity           int    `json:"quantity"`
	ImageRatio         string `json:"image_ratio"`
	GenerateVideo      bool   `json:"generate_video"`
	SkipReferenceImage *bool  `json:"skip_reference_image"`
	ReferenceImageURL  string `json:"reference_image_url"`
	Watermark          *bool  `json:"watermark"`
}

type bulkDownloadTaskFilesRequest struct {
	TaskIDs []string `json:"task_ids"`
}

// Create handles POST /api/v1/tasks.
func (h *TaskHandler) Create(c fiber.Ctx) error {
	var req createTaskRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.ChannelID == "" {
		return Error(c, fiber.StatusBadRequest, "channel_id is required")
	}

	prompt := strings.TrimSpace(req.Prompt)
	if utf8.RuneCountInString(prompt) > maxTaskPromptCharacters {
		return Error(c, fiber.StatusBadRequest, "prompt must not exceed 5120 characters")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	quantity := req.Quantity
	if quantity <= 0 {
		quantity = 1
	}
	if quantity > 5 {
		return Error(c, fiber.StatusBadRequest, "quantity must be between 1 and 5")
	}

	if req.ImageRatio != "" && !model.ValidImageRatios[req.ImageRatio] {
		return Error(c, fiber.StatusBadRequest, "image_ratio must be one of: 3:4, 1:1, 4:3, 16:9")
	}

	if !validReferenceImageURL(req.ReferenceImageURL) {
		return Error(c, fiber.StatusBadRequest, "reference_image_url must be an internal file path or an http(s) URL")
	}

	if req.GenerateVideo {
		return Error(c, fiber.StatusBadRequest, "video generation is no longer supported")
	}

	tasks, err := h.service.CreateManual(c.Context(), userID, req.ChannelID, prompt, quantity, req.ImageRatio, req.SkipReferenceImage, req.ReferenceImageURL, req.Watermark)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create task failed")
		if errors.Is(err, service.ErrInsufficientCredits) {
			return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
				"code": 40200,
				"msg":  "insufficient_credits",
			})
		}
		return Error(c, fiber.StatusInternalServerError, "failed to create task")
	}

	// Return single task for quantity=1 (frontend expects Task, not Task[]).
	if quantity == 1 && len(tasks) > 0 {
		return Success(c, tasks[0])
	}
	return Success(c, tasks)
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
	channelID := c.Query("channel_id", "")

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	tasks, total, err := h.service.List(c.Context(), userID, offset, limit, status, channelID)
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
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}

	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	return Success(c, task)
}

// Cancel handles POST /api/v1/tasks/:id/cancel.
func (h *TaskHandler) Cancel(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before cancel.
	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	if err := h.service.Cancel(c.Context(), id); err != nil {
		h.logger.Error().Err(err).Msg("cancel task failed")
		return Error(c, fiber.StatusInternalServerError, "failed to cancel task")
	}

	return Success(c, fiber.Map{"message": "task cancelled"})
}

// Delete handles DELETE /api/v1/tasks/:id.
func (h *TaskHandler) Delete(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	if task.Status == model.TaskStatusRunning {
		return Error(c, fiber.StatusConflict, "cannot delete a running task, cancel it first")
	}

	if err := h.service.Delete(c.Context(), id); err != nil {
		h.logger.Error().Err(err).Msg("delete task failed")
		return Error(c, fiber.StatusInternalServerError, "failed to delete task")
	}

	return Success(c, fiber.Map{"message": "task deleted"})
}

// MarkPublished handles PATCH /api/v1/tasks/:id/published.
func (h *TaskHandler) MarkPublished(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var body struct {
		Published bool `json:"published"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if err := h.service.SetPublished(c.Context(), userID, id, body.Published); err != nil {
		h.logger.Error().Err(err).Msg("mark published failed")
		return Error(c, fiber.StatusInternalServerError, "failed to update published status")
	}

	return Success(c, fiber.Map{"published": body.Published})
}

// GetFiles handles GET /api/v1/tasks/:id/files.
func (h *TaskHandler) GetFiles(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before getting files.
	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	files, err := h.service.GetFiles(c.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Msg("get task files failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get task files")
	}

	return Success(c, files)
}

// Stream handles GET /api/v1/tasks/:id/stream — SSE endpoint for real-time progress.
// When Redis pub/sub is available, it subscribes to progress events for immediate
// push delivery. Falls back to 1-second DB polling when Redis is unavailable.
func (h *TaskHandler) Stream(c fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before streaming.
	task, err := h.service.GetByID(c.Context(), taskID)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	ctx := c.Context()

	// Try Redis pub/sub for real-time push; fall back to DB polling.
	pubsub := h.service.PubSub()
	if pubsub != nil && pubsub.Available() {
		return h.streamWithPubSub(c, ctx, taskID, pubsub)
	}
	return h.streamWithPolling(c, ctx, taskID)
}

// streamWithPubSub uses Redis pub/sub for real-time progress delivery.
// It also runs a slow DB poll as a fallback for any events missed by pub/sub
// and for terminal state detection.
func (h *TaskHandler) streamWithPubSub(c fiber.Ctx, ctx context.Context, taskID string, pubsub *service.RedisPubSub) error {
	sub := pubsub.SubscribeProgress(ctx, taskID)
	if sub == nil {
		return h.streamWithPolling(c, ctx, taskID)
	}
	defer sub.Close()

	// Slow fallback poll every 5 seconds to catch any missed pub/sub events
	// and detect terminal states.
	fallbackTicker := time.NewTicker(5 * time.Second)
	defer fallbackTicker.Stop()

	// Max SSE timeout: 30 minutes.
	timeout := time.NewTimer(30 * time.Minute)
	defer timeout.Stop()

	// Fetch initial progress log length from DB.
	lastLogLen := 0
	if task, err := h.service.GetByID(ctx, taskID); err == nil {
		lastLogLen = len(task.ProgressLog)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timeout.C:
			fmt.Fprintf(c, "event: timeout\ndata: {}\n\n")
			return nil
		case msg, ok := <-sub.Events():
			if !ok {
				// Subscription closed, fall back to polling.
				return h.streamWithPolling(c, ctx, taskID)
			}
			var event service.ProgressEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				continue
			}
			escaped, _ := json.Marshal(event.Message)
			if _, err := fmt.Fprintf(c, "event: progress\ndata: %s\n\n", escaped); err != nil {
				return nil
			}
		case <-fallbackTicker.C:
			task, err := h.service.GetByID(ctx, taskID)
			if err != nil {
				return nil
			}
			// Send any progress entries missed by pub/sub.
			if len(task.ProgressLog) > lastLogLen {
				newLog := task.ProgressLog[lastLogLen:]
				lastLogLen = len(task.ProgressLog)
				escaped, _ := json.Marshal(newLog)
				if _, err := fmt.Fprintf(c, "event: progress\ndata: %s\n\n", escaped); err != nil {
					return nil
				}
			}
			// Terminal state detection (reliable via DB).
			if task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
				statusData, _ := json.Marshal(map[string]string{"status": task.Status})
				fmt.Fprintf(c, "event: %s\ndata: %s\n\n", task.Status, statusData)
				return nil
			}
		}
	}
}

// streamWithPolling falls back to 1-second DB polling for progress updates.
func (h *TaskHandler) streamWithPolling(c fiber.Ctx, ctx context.Context, taskID string) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	timeout := time.NewTimer(30 * time.Minute)
	defer timeout.Stop()

	lastLogLen := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timeout.C:
			fmt.Fprintf(c, "event: timeout\ndata: {}\n\n")
			return nil
		case <-ticker.C:
			task, err := h.service.GetByID(ctx, taskID)
			if err != nil {
				return nil
			}

			if len(task.ProgressLog) > lastLogLen {
				newLog := task.ProgressLog[lastLogLen:]
				lastLogLen = len(task.ProgressLog)
				escaped, err := json.Marshal(newLog)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(c, "event: progress\ndata: %s\n\n", escaped); err != nil {
					return nil
				}
			}

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

// verifyTaskOwnership is a helper that fetches a task and verifies the
// authenticated user owns it. Returns the task on success, or writes an
// error response and returns nil.
func (h *TaskHandler) verifyTaskOwnership(c fiber.Ctx, taskID string) (*model.Task, error) {
	task, err := h.service.GetByID(c.Context(), taskID)
	if err != nil {
		Error(c, fiber.StatusNotFound, "task not found")
		return nil, fmt.Errorf("task not found: %w", err)
	}

	userID := GetUserID(c)
	if userID == "" {
		Error(c, fiber.StatusUnauthorized, "unauthorized")
		return nil, fmt.Errorf("unauthorized")
	}

	if task.UserID != userID {
		Forbidden(c, "you do not have access to this task")
		return nil, fmt.Errorf("ownership mismatch: user %s vs task owner %s", userID, task.UserID)
	}

	return task, nil
}

// DownloadFile handles GET /api/v1/tasks/:id/files/:fileId/download.
// It streams the file content as an attachment download.
func (h *TaskHandler) DownloadFile(c fiber.Ctx) error {
	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	fileID, err := validateUUIDParam(c, "fileId")
	if err != nil {
		return err
	}

	if _, err := h.verifyTaskOwnership(c, taskID); err != nil {
		return nil // error response already written
	}

	// Verify the file belongs to the task.
	if err := h.service.VerifyFileBelongsToTask(c.Context(), taskID, fileID); err != nil {
		return Error(c, fiber.StatusNotFound, "file not found")
	}

	stream, file, err := h.service.GetFileStream(c.Context(), fileID)
	if err != nil {
		h.logger.Error().Err(err).Str("file_id", fileID).Msg("get file stream failed")
		return Error(c, fiber.StatusNotFound, "file not found")
	}
	defer stream.Close()

	contentType := file.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, file.FileName))
	c.Set("Content-Length", strconv.FormatInt(file.FileSize, 10))

	if _, err := io.Copy(c.Response().BodyWriter(), stream); err != nil {
		h.logger.Error().Err(err).Str("file_id", fileID).Msg("stream file failed")
	}

	return nil
}

// PreviewHTML handles GET /api/v1/tasks/:id/preview.
// It finds the HTML file for the task and returns it as the response body.
func (h *TaskHandler) PreviewHTML(c fiber.Ctx) error {
	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	if _, err := h.verifyTaskOwnership(c, taskID); err != nil {
		return nil // error response already written
	}

	// Find all files for the task and locate the HTML one.
	files, err := h.service.GetFiles(c.Context(), taskID)
	if err != nil {
		h.logger.Error().Err(err).Str("task_id", taskID).Msg("get task files failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get task files")
	}

	var htmlFile *model.TaskFile
	for _, f := range files {
		if f.Role == model.FileRoleHTML {
			htmlFile = f
			break
		}
	}

	if htmlFile == nil {
		return Error(c, fiber.StatusNotFound, "no HTML file found for this task")
	}

	stream, _, err := h.service.GetFileStream(c.Context(), htmlFile.ID)
	if err != nil {
		h.logger.Error().Err(err).Str("file_id", htmlFile.ID).Msg("get HTML file stream failed")
		return Error(c, fiber.StatusInternalServerError, "failed to read HTML file")
	}
	defer stream.Close()

	htmlContent, err := io.ReadAll(stream)
	if err != nil {
		h.logger.Error().Err(err).Msg("read HTML content failed")
		return Error(c, fiber.StatusInternalServerError, "failed to read HTML file")
	}

	// Rewrite relative image URLs to absolute storage URLs so images render in preview.
	fileMap := make(map[string]string)
	for _, f := range files {
		if (f.Role == model.FileRoleImage || f.Role == model.FileRoleCover) && f.URL != "" {
			fileMap[strings.ToLower(f.FileName)] = f.URL
		}
	}
	if len(fileMap) > 0 {
		htmlContent = service.RewriteHTMLImageURLs(htmlContent, fileMap)
	}

	c.Set("Content-Type", "text/html; charset=utf-8")
	c.Set("X-Content-Type-Options", "nosniff")
	// Allow loading images from external storage (OSS) in the sandboxed preview.
	cspValue := "sandbox allow-scripts"
	if h.service.StorageProviderName() == "oss" {
		cspValue += "; img-src https: data:"
	}
	c.Set("Content-Security-Policy", cspValue)
	c.Set("Content-Length", strconv.Itoa(len(htmlContent)))

	return c.Send(htmlContent)
}

// DownloadZip handles GET /api/v1/tasks/:id/files/zip.
// It creates a ZIP archive of all task files and sends it as a download.
func (h *TaskHandler) DownloadZip(c fiber.Ctx) error {
	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	if _, err := h.verifyTaskOwnership(c, taskID); err != nil {
		return nil // error response already written
	}

	buf, zipName, err := h.service.DownloadZip(c.Context(), taskID)
	if err != nil {
		h.logger.Error().Err(err).Str("task_id", taskID).Msg("download zip failed")
		return Error(c, fiber.StatusNotFound, "failed to create ZIP archive")
	}

	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))
	c.Set("Content-Length", strconv.Itoa(buf.Len()))

	return c.Send(buf.Bytes())
}

// DownloadTasksZip handles POST /api/v1/tasks/files/zip.
// It creates a single ZIP archive containing all readable files from selected completed tasks.
func (h *TaskHandler) DownloadTasksZip(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req bulkDownloadTaskFilesRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if len(req.TaskIDs) == 0 {
		return Error(c, fiber.StatusBadRequest, "task_ids is required")
	}
	if len(req.TaskIDs) > 100 {
		return Error(c, fiber.StatusBadRequest, "task_ids must not exceed 100")
	}
	for _, id := range req.TaskIDs {
		if _, err := uuid.Parse(strings.TrimSpace(id)); err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid task_ids format")
		}
	}

	buf, zipName, err := h.service.DownloadTasksZip(c.Context(), userID, req.TaskIDs)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("bulk download zip failed")
		return Error(c, fiber.StatusNotFound, "failed to create ZIP archive")
	}

	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))
	c.Set("Content-Length", strconv.Itoa(buf.Len()))

	return c.Send(buf.Bytes())
}

// GetLog handles GET /api/v1/tasks/:id/log.
// It returns the per-task agent execution log file content.
func (h *TaskHandler) GetLog(c fiber.Ctx) error {
	if h.taskLogDir == "" {
		return Error(c, fiber.StatusNotFound, "task logging is not configured")
	}

	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	if _, err := h.verifyTaskOwnership(c, taskID); err != nil {
		return nil // error response already written
	}

	logPath := filepath.Join(h.taskLogDir, taskID+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Error(c, fiber.StatusNotFound, "task log not found")
		}
		h.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to read task log")
		return Error(c, fiber.StatusInternalServerError, "failed to read task log")
	}

	c.Set("Content-Type", "text/plain; charset=utf-8")
	if c.Query("download") == "true" {
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="task-%s.log"`, taskID))
	}
	return c.Send(data)
}

// ServeLocalFile handles GET /api/v1/files/* for the local storage provider.
// It serves files from the local data directory with path traversal protection
// and verifies the requesting user owns the file (key prefix = userID).
func (h *TaskHandler) ServeLocalFile(c fiber.Ctx) error {
	if h.dataDir == "" {
		return Error(c, fiber.StatusNotFound, "local file serving is not configured")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	key := c.Params("*")
	if key == "" {
		return Error(c, fiber.StatusBadRequest, "file path is required")
	}

	// Prevent path traversal.
	cleanKey := filepath.Clean(key)
	if strings.Contains(cleanKey, "..") {
		return Error(c, fiber.StatusBadRequest, "invalid file path")
	}

	// Verify ownership: key format is {userID}/{taskID}/... or uploads/{channels|references}/{userID}/...
	if !strings.HasPrefix(cleanKey, userID+"/") && !strings.HasPrefix(cleanKey, "uploads/channels/"+userID+"/") && !strings.HasPrefix(cleanKey, "uploads/references/"+userID+"/") {
		return Forbidden(c, "you do not have access to this file")
	}

	filePath := filepath.Join(h.dataDir, cleanKey)

	// Ensure the resolved path is within dataDir.
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid file path")
	}
	absDataDir, err := filepath.Abs(h.dataDir)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "server configuration error")
	}
	if !strings.HasPrefix(absPath, absDataDir+string(filepath.Separator)) {
		return Error(c, fiber.StatusForbidden, "access denied")
	}

	// Detect content type from extension.
	ext := filepath.Ext(cleanKey)
	switch strings.ToLower(ext) {
	case ".html", ".htm":
		c.Set("Content-Type", "text/html; charset=utf-8")
	case ".css":
		c.Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		c.Set("Content-Type", "application/javascript")
	case ".json":
		c.Set("Content-Type", "application/json")
	case ".png":
		c.Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		c.Set("Content-Type", "image/jpeg")
	case ".gif":
		c.Set("Content-Type", "image/gif")
	case ".webp":
		c.Set("Content-Type", "image/webp")
	case ".svg":
		c.Set("Content-Type", "image/svg+xml")
		c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		c.Set("X-Content-Type-Options", "nosniff")
	case ".pdf":
		c.Set("Content-Type", "application/pdf")
	}

	return c.SendFile(absPath)
}

// validateUUIDParam extracts and validates that a path parameter is a valid UUID.
// Returns the UUID string or writes an error response and returns empty string.
func validateUUIDParam(c fiber.Ctx, paramName string) (string, error) {
	val := c.Params(paramName)
	if val == "" {
		return "", Error(c, fiber.StatusBadRequest, paramName+" is required")
	}
	if _, err := uuid.Parse(val); err != nil {
		return "", Error(c, fiber.StatusBadRequest, "invalid "+paramName+" format")
	}
	return val, nil
}

// UsageStats handles GET /api/v1/usage/stats.
// Returns aggregated LLM token usage and cost statistics for the authenticated user.
func (h *TaskHandler) UsageStats(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Parse optional date range. Defaults to last 30 days.
	from := time.Now().AddDate(0, 0, -30)
	to := time.Now()

	if v := c.Query("from"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid 'from' date format, use YYYY-MM-DD")
		}
		from = parsed
	}
	if v := c.Query("to"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid 'to' date format, use YYYY-MM-DD")
		}
		// Include the entire end day.
		to = parsed.AddDate(0, 0, 1)
	}

	channelID := c.Query("channel_id")

	stats, err := h.service.GetUsageStats(c.Context(), userID, from, to, channelID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("get usage stats failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get usage stats")
	}

	return Success(c, stats)
}
