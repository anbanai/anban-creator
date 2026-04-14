package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/service"
)

// TaskHandler handles task-related HTTP endpoints.
type TaskHandler struct {
	service *service.TaskService
	logger  *zerolog.Logger
	dataDir string // local storage data directory (for ServeLocalFile)
}

// NewTaskHandler creates a new TaskHandler.
func NewTaskHandler(svc *service.TaskService, logger *zerolog.Logger, dataDir ...string) *TaskHandler {
	h := &TaskHandler{service: svc, logger: logger}
	if len(dataDir) > 0 {
		h.dataDir = dataDir[0]
	}
	return h
}

// Request types.

type createTaskRequest struct {
	ChannelID string `json:"channel_id"`
	Topic     string `json:"topic"`
	Quantity  int    `json:"quantity"`
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

	topic := strings.TrimSpace(req.Topic)
	if topic == "" {
		return Error(c, fiber.StatusBadRequest, "topic is required")
	}
	if len(topic) > 500 {
		return Error(c, fiber.StatusBadRequest, "topic must not exceed 500 characters")
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

	tasks, err := h.service.CreateManual(c.Context(), userID, req.ChannelID, topic, quantity)
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
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Max SSE timeout: 30 minutes to prevent indefinitely stuck connections.
	timeout := time.NewTimer(30 * time.Minute)
	defer timeout.Stop()

	lastLogLen := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timeout.C:
			// Send timeout event and close.
			if _, err := fmt.Fprintf(c, "event: timeout\ndata: {}\n\n"); err != nil {
				return nil
			}
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
		if (f.Role == model.FileRoleImage || f.Role == model.FileRoleCover) && f.OSSURL != "" {
			fileMap[strings.ToLower(f.FileName)] = f.OSSURL
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

	// Verify ownership: key format is {userID}/{taskID}/...
	if !strings.HasPrefix(cleanKey, userID+"/") {
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
	case ".mp4":
		c.Set("Content-Type", "video/mp4")
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