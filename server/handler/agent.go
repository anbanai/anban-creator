package handler

import (
	"crypto/subtle"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	serveragent "github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/royalrick/anbanwriter/server/storage"
)

const agentUserIDContextKey = "agent_user_id"

// AgentHandler handles agent-to-server communication endpoints.
type AgentHandler struct {
	taskSvc   *service.TaskService
	apiKeySvc *service.APIKeyService
	store     storage.Provider
	staticKey string
	logger    *zerolog.Logger
}

// NewAgentHandler creates a new AgentHandler.
func NewAgentHandler(taskSvc *service.TaskService, apiKeySvc *service.APIKeyService, store storage.Provider, staticKey string, logger *zerolog.Logger) *AgentHandler {
	return &AgentHandler{
		taskSvc:   taskSvc,
		apiKeySvc: apiKeySvc,
		store:     store,
		staticKey: staticKey,
		logger:    logger,
	}
}

// AuthMiddleware validates bearer-style API keys for agent endpoints.
func (h *AgentHandler) AuthMiddleware(c fiber.Ctx) error {
	token := extractAgentToken(c)
	if token == "" {
		return Error(c, fiber.StatusUnauthorized, "missing agent api key")
	}

	if h.apiKeySvc != nil {
		if apiKey, err := h.apiKeySvc.Validate(c.Context(), token); err == nil && apiKey != nil {
			c.Locals(agentUserIDContextKey, apiKey.UserID)
			return c.Next()
		}
	}

	if h.staticKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(h.staticKey)) == 1 {
		c.Locals(agentUserIDContextKey, "")
		return c.Next()
	}

	if h.logger != nil {
		h.logger.Warn().
			Int("token_len", len(token)).
			Msg("agent auth failed: invalid token")
	}
	return Error(c, fiber.StatusUnauthorized, "invalid agent api key")
}

func extractAgentToken(c fiber.Ctx) string {
	candidates := []string{
		c.Get("Authorization"),
		c.Get("X-Agent-API-Key"),
		c.Get("X-API-Key"),
		c.Get("X-Admin-API-Key"),
	}
	for _, raw := range candidates {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "Bearer ") {
			return strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
		}
		return raw
	}
	return ""
}

func (h *AgentHandler) authenticatedUserID(c fiber.Ctx) string {
	userID, _ := c.Locals(agentUserIDContextKey).(string)
	return userID
}

// Upload handles POST /api/v1/agent/upload.
func (h *AgentHandler) Upload(c fiber.Ctx) error {
	if h.taskSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}

	taskID := strings.TrimSpace(c.FormValue("task_id"))
	if taskID == "" {
		return Error(c, fiber.StatusBadRequest, "task_id is required")
	}

	task, err := h.taskSvc.ValidateAgentTaskAccess(c.Context(), taskID, h.authenticatedUserID(c))
	if err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return Error(c, fiber.StatusBadRequest, "file is required")
	}

	relPath := strings.TrimSpace(c.FormValue("relative_path"))
	if relPath == "" {
		relPath = strings.TrimSpace(c.FormValue("path"))
	}
	if relPath == "" {
		relPath = fileHeader.Filename
	}

	src, err := fileHeader.Open()
	if err != nil {
		h.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to open uploaded file")
		return Error(c, fiber.StatusBadRequest, "failed to read uploaded file")
	}
	defer src.Close()

	mimeType := strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(fileHeader.Filename))
	}

	taskFile, err := h.taskSvc.UploadTaskFileFromReader(c.Context(), task.ID, task.UserID, relPath, src, mimeType, fileHeader.Size)
	if err != nil {
		h.logger.Error().Err(err).Str("task_id", taskID).Str("path", relPath).Msg("agent file upload failed")
		return Error(c, fiber.StatusInternalServerError, "failed to store task file")
	}

	return Success(c, taskFile)
}

// Temp handles GET /api/v1/agent/temp/:id/:file.
func (h *AgentHandler) Temp(c fiber.Ctx) error {
	if h.store == nil {
		return Error(c, fiber.StatusServiceUnavailable, "storage provider unavailable")
	}

	id := strings.TrimSpace(c.Params("id"))
	fileName := filepath.Base(strings.TrimSpace(c.Params("file")))
	if id == "" || fileName == "" {
		return Error(c, fiber.StatusBadRequest, "missing temp file id or name")
	}

	key := service.AgentTempStorageKey(id, fileName)
	data, err := h.store.Read(c.Context(), key)
	if err != nil {
		if h.logger != nil {
			h.logger.Warn().Err(err).Str("key", key).Msg("failed to read agent temp file")
		}
		return Error(c, fiber.StatusNotFound, "temp file not found")
	}

	contentType := mime.TypeByExtension(filepath.Ext(fileName))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	c.Set("Content-Type", contentType)
	return c.Send(data)
}

type agentProgressRequest struct {
	TaskID  string                       `json:"task_id"`
	Message string                       `json:"message"`
	Logs    []string                     `json:"logs"`
	Result  *serveragent.ExecutionResult `json:"result"`
}

// Progress handles POST /api/v1/agent/progress.
func (h *AgentHandler) Progress(c fiber.Ctx) error {
	if h.taskSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}

	var req agentProgressRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.TaskID) == "" {
		return Error(c, fiber.StatusBadRequest, "task_id is required")
	}

	if _, err := h.taskSvc.ValidateAgentTaskAccess(c.Context(), req.TaskID, h.authenticatedUserID(c)); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
	}

	if req.Message != "" {
		req.Logs = append(req.Logs, req.Message)
	}
	for _, line := range req.Logs {
		if err := h.taskSvc.AppendProgressLog(c.Context(), req.TaskID, line); err != nil {
			h.logger.Error().Err(err).Str("task_id", req.TaskID).Msg("failed to append agent progress")
			return Error(c, fiber.StatusInternalServerError, "failed to persist progress")
		}
	}

	if req.Result != nil {
		if err := h.taskSvc.UpdateExecutionResult(c.Context(), req.TaskID, req.Result); err != nil {
			h.logger.Error().Err(err).Str("task_id", req.TaskID).Msg("failed to persist agent result")
			return Error(c, fiber.StatusInternalServerError, "failed to persist result")
		}
	}

	return Success(c, fiber.Map{"ok": true})
}
