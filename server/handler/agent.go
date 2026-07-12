package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"mime"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

const (
	agentUserIDContextKey        = "agent_user_id"
	agentProjectIDContextKey     = "agent_project_id"
	agentTaskIDContextKey        = "agent_task_id"
	agentExecutionIDContextKey   = "agent_execution_id"
	agentWorkloadTokenContextKey = "agent_workload_token"
)

type workloadVerifier interface {
	Verify(context.Context, string, string) (*serveragent.KubernetesWorkloadIdentity, error)
}

type agentBootstrapper interface {
	Bootstrap(context.Context, *serveragent.KubernetesWorkloadIdentity) (*service.AgentBootstrapResponse, error)
}

// AgentHandler handles agent-to-server communication endpoints.
type AgentHandler struct {
	taskSvc          *service.TaskService
	apiKeySvc        *service.APIKeyService
	store            storage.Provider
	staticKey        string
	directUploadCfg  service.DirectUploadConfig
	executionTokens  *auth.ExecutionTokenService
	workloadVerifier workloadVerifier
	bootstrapper     agentBootstrapper
	logger           *zerolog.Logger
}

func (h *AgentHandler) SetExecutionTokenService(tokens *auth.ExecutionTokenService) {
	h.executionTokens = tokens
}

func (h *AgentHandler) SetBootstrap(verifier workloadVerifier, bootstrapper agentBootstrapper) {
	h.workloadVerifier, h.bootstrapper = verifier, bootstrapper
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

// SetDirectUploadConfig configures server-issued OSS STS credentials for agent
// artifact uploads.
func (h *AgentHandler) SetDirectUploadConfig(cfg service.DirectUploadConfig) {
	h.directUploadCfg = cfg
}

// AuthMiddleware validates bearer-style API keys for agent endpoints.
func (h *AgentHandler) AuthMiddleware(c fiber.Ctx) error {
	token := extractAgentToken(c)
	if token == "" {
		return Error(c, fiber.StatusUnauthorized, "missing agent api key")
	}
	if h.executionTokens != nil {
		if claims, err := h.executionTokens.Validate(token); err == nil {
			c.Locals(agentUserIDContextKey, claims.UserID)
			c.Locals(agentProjectIDContextKey, claims.ProjectID)
			c.Locals(agentTaskIDContextKey, claims.TaskID)
			c.Locals(agentExecutionIDContextKey, claims.ExecutionID)
			return c.Next()
		} else if strings.Count(token, ".") == 2 {
			if h.authenticateLegacyToken(c, extractAgentHeaderToken(c)) {
				return c.Next()
			}
			return Error(c, fiber.StatusUnauthorized, "invalid agent execution token")
		}
	}

	if h.authenticateLegacyToken(c, token) {
		return c.Next()
	}

	if h.logger != nil {
		h.logger.Warn().
			Int("token_len", len(token)).
			Msg("agent auth failed: invalid token")
	}
	return Error(c, fiber.StatusUnauthorized, "invalid agent api key")
}

func (h *AgentHandler) authenticateLegacyToken(c fiber.Ctx, token string) bool {
	if token == "" {
		return false
	}
	if h.apiKeySvc != nil {
		if apiKey, err := h.apiKeySvc.Validate(c.Context(), token); err == nil && apiKey != nil {
			c.Locals(agentUserIDContextKey, apiKey.UserID)
			return true
		}
	}
	if h.staticKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(h.staticKey)) == 1 {
		c.Locals(agentUserIDContextKey, "")
		return true
	}
	return false
}

func extractAgentHeaderToken(c fiber.Ctx) string {
	for _, name := range []string{"X-Agent-API-Key", "X-API-Key", "X-Admin-API-Key"} {
		if token := strings.TrimSpace(c.Get(name)); token != "" {
			return token
		}
	}
	return ""
}

// WorkloadAuthMiddleware accepts only the projected Kubernetes bearer token.
func (h *AgentHandler) WorkloadAuthMiddleware(c fiber.Ctx) error {
	raw := strings.TrimSpace(c.Get("Authorization"))
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return Error(c, fiber.StatusUnauthorized, "missing workload bearer token")
	}
	c.Locals(agentWorkloadTokenContextKey, strings.TrimSpace(parts[1]))
	return c.Next()
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

func (h *AgentHandler) authorizeClaimScope(c fiber.Ctx, taskID string) error {
	claimTaskID, _ := c.Locals(agentTaskIDContextKey).(string)
	if claimTaskID != "" && claimTaskID != taskID {
		return errors.New("execution token task mismatch")
	}
	executionID, _ := c.Locals(agentExecutionIDContextKey).(string)
	if executionID != "" {
		userID, _ := c.Locals(agentUserIDContextKey).(string)
		projectID, _ := c.Locals(agentProjectIDContextKey).(string)
		if h.taskSvc == nil {
			return errors.New("task service unavailable")
		}
		return h.taskSvc.ValidateAgentExecutionAccess(c.Context(), userID, projectID, taskID, executionID)
	}
	return nil
}

type agentBootstrapRequest struct {
	ExecutionID string `json:"execution_id"`
}

func (h *AgentHandler) Bootstrap(c fiber.Ctx) error {
	if h.workloadVerifier == nil || h.bootstrapper == nil {
		return Error(c, fiber.StatusServiceUnavailable, "agent bootstrap unavailable")
	}
	var req agentBootstrapRequest
	if err := c.Bind().Body(&req); err != nil || strings.TrimSpace(req.ExecutionID) == "" {
		return Error(c, fiber.StatusBadRequest, "execution_id is required")
	}
	token, _ := c.Locals(agentWorkloadTokenContextKey).(string)
	identity, err := h.workloadVerifier.Verify(c.Context(), token, strings.TrimSpace(req.ExecutionID))
	if err != nil {
		return Error(c, fiber.StatusUnauthorized, "workload identity verification failed")
	}
	response, err := h.bootstrapper.Bootstrap(c.Context(), identity)
	if err != nil {
		return Error(c, fiber.StatusConflict, err.Error())
	}
	return Success(c, response)
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
	if err := h.authorizeClaimScope(c, taskID); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
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

// PrepareArtifactUpload handles POST /api/v1/agent/artifacts/prepare.
func (h *AgentHandler) PrepareArtifactUpload(c fiber.Ctx) error {
	if h.taskSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}
	var req service.TaskArtifactPrepareRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.TaskID) == "" {
		return Error(c, fiber.StatusBadRequest, "task_id is required")
	}
	if err := h.authorizeClaimScope(c, req.TaskID); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
	}
	result, err := h.taskSvc.PrepareTaskArtifactUpload(c.Context(), req.TaskID, h.authenticatedUserID(c), h.directUploadCfg, req)
	if err != nil {
		if isAgentTaskAccessError(err) {
			return Error(c, fiber.StatusForbidden, "task access denied")
		}
		if strings.Contains(err.Error(), "storage provider") || strings.Contains(err.Error(), "OSS storage") || strings.Contains(err.Error(), "credential") {
			h.logger.Warn().Err(err).Str("task_id", req.TaskID).Msg("prepare agent artifact upload unavailable")
			return Error(c, fiber.StatusServiceUnavailable, err.Error())
		}
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, result)
}

// ReportArtifactManifest handles POST /api/v1/agent/artifacts/manifest.
func (h *AgentHandler) ReportArtifactManifest(c fiber.Ctx) error {
	if h.taskSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}
	var req service.TaskArtifactManifestRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.TaskID) == "" {
		return Error(c, fiber.StatusBadRequest, "task_id is required")
	}
	if err := h.authorizeClaimScope(c, req.TaskID); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
	}
	if err := h.taskSvc.FinalizeTaskArtifactManifest(c.Context(), req.TaskID, h.authenticatedUserID(c), req); err != nil {
		if isAgentTaskAccessError(err) {
			return Error(c, fiber.StatusForbidden, "task access denied")
		}
		h.logger.Warn().Err(err).Str("task_id", req.TaskID).Msg("agent artifact manifest rejected")
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, fiber.Map{"ok": true})
}

func isAgentTaskAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "task does not belong to authenticated user")
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
// (service.LocalExecutionConfig) which the desktop turns into an anban run
// argv (mirroring the cloud DockerExecutor), supplying its own server_url +
// API key. The claimed task is already status=running, so cloud Asynq never
// picks it up. Returns 204 No Content when nothing is claimable.
func (h *AgentHandler) Claim(c fiber.Ctx) error {
	if executionID, _ := c.Locals(agentExecutionIDContextKey).(string); executionID != "" {
		return Error(c, fiber.StatusForbidden, "execution credentials cannot claim local tasks")
	}
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
	if err := h.authorizeClaimScope(c, req.TaskID); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
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
// A desktop local executor calls this once when anban finishes, with
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
	if err := h.authorizeClaimScope(c, req.TaskID); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
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
