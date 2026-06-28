package handler

import (
	"crypto/subtle"
	"encoding/json"
	"mime"
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

type agentProgressRequest struct {
	TaskID  string                       `json:"task_id"`
	Message string                       `json:"message"`
	Logs    []string                     `json:"logs"`
	Result  *serveragent.ExecutionResult `json:"result"`
}

// agentClaimRequest is the optional body for POST /api/v1/agent/claim.
// executor_info is an opaque JSON blob (desktop hostname/version) recorded for
// diagnostics. The body may be empty.
type agentClaimRequest struct {
	ExecutorInfo json.RawMessage `json:"executor_info"`
}

// Claim handles POST /api/v1/agent/claim.
//
// A desktop local executor polls this endpoint to atomically claim its oldest
// pending local-target task. On success it returns the full task config
// (service.LocalExecutionConfig) which the desktop turns into an abwriter-agent
// argv (mirroring the cloud DockerExecutor), supplying its own server_url +
// API key. The claimed task is already status=running, so cloud Asynq never
// picks it up. Returns 204 No Content when nothing is claimable.
func (h *AgentHandler) Claim(c fiber.Ctx) error {
	if h.taskSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}

	userID := h.authenticatedUserID(c)

	var req agentClaimRequest
	// Body is optional; ignore bind errors (empty body is the common case).
	if len(c.Body()) > 0 {
		_ = c.Bind().Body(&req)
	}

	cfg, err := h.taskSvc.ClaimLocalTask(c.Context(), userID, string(req.ExecutorInfo))
	if err != nil {
		h.logger.Error().Err(err).Msg("claim local task failed")
		return Error(c, fiber.StatusInternalServerError, "claim failed")
	}
	if cfg == nil {
		return c.Status(fiber.StatusNoContent).SendString("")
	}
	return Success(c, cfg)
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

	// Refresh the heartbeat on every progress report. This is the local-
	// execution keep-alive: a desktop agent reports progress per turn/line, and
	// each report resets the 5-min stuck-task reaper. Cloud tasks are also kept
	// alive by HandleExecution's HeartbeatFunc, so this is a harmless redundant
	// refresh there. Without it, a long-running local task would be force-failed
	// by reapStuckTasks (plan_checker.go) before it completes.
	if err := h.taskSvc.UpdateHeartbeat(c.Context(), req.TaskID); err != nil {
		h.logger.Warn().Err(err).Str("task_id", req.TaskID).Msg("failed to update task heartbeat")
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

// Complete handles POST /api/v1/agent/complete.
//
// A desktop local executor calls this once when abwriter-agent finishes, with
// the final ExecutionResult. The service finalizes the task (status → completed
// or failed, slot release, dispatch, refund-on-failure) — guarded to
// local_claimed tasks and idempotent, so a cloud task or a repeat call is a
// no-op. This is the terminal half of the local-execution path; without it a
// local task could never reach a terminal state (the agent binary is shared with
// cloud, whose authoritative finalization is server-side HandleExecution).
func (h *AgentHandler) Complete(c fiber.Ctx) error {
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

	if err := h.taskSvc.CompleteLocalTask(c.Context(), req.TaskID, req.Result); err != nil {
		h.logger.Error().Err(err).Str("task_id", req.TaskID).Msg("complete local task failed")
		return Error(c, fiber.StatusInternalServerError, "complete failed")
	}
	return Success(c, fiber.Map{"ok": true})
}
