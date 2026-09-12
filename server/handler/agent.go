package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

const (
	agentUserIDContextKey        = "agent_user_id"
	agentProjectIDContextKey     = "agent_project_id"
	agentTaskIDContextKey        = "agent_task_id"
	agentExecutionIDContextKey   = "agent_execution_id"
	agentWorkloadTokenContextKey = "agent_workload_token"
)

type agentBootstrapper interface {
	Bootstrap(context.Context, *serveragent.WorkloadIdentity) (*service.AgentBootstrapResponse, error)
}

// AgentHandler handles agent-to-server communication endpoints.
type AgentHandler struct {
	taskSvc          *service.TaskService
	apiKeySvc        *service.APIKeyService
	directUploadCfg  service.DirectUploadConfig
	executionTokens  *auth.ExecutionTokenService
	localTokenTTL    time.Duration
	workloadVerifier serveragent.WorkloadVerifier
	bootstrapper     agentBootstrapper
	logger           *zerolog.Logger
}

func (h *AgentHandler) SetExecutionTokenService(tokens *auth.ExecutionTokenService) {
	h.executionTokens = tokens
}

// SetLocalExecutionTokenTTL sets the complete local execution credential
// lifetime, including the configured execution and persistence windows.
func (h *AgentHandler) SetLocalExecutionTokenTTL(ttl time.Duration) {
	h.localTokenTTL = ttl
}

func (h *AgentHandler) SetBootstrap(verifier serveragent.WorkloadVerifier, bootstrapper agentBootstrapper) {
	h.workloadVerifier, h.bootstrapper = verifier, bootstrapper
}

// NewAgentHandler creates a new AgentHandler.
func NewAgentHandler(taskSvc *service.TaskService, apiKeySvc *service.APIKeyService, logger *zerolog.Logger) *AgentHandler {
	return &AgentHandler{
		taskSvc:   taskSvc,
		apiKeySvc: apiKeySvc,
		logger:    logger,
	}
}

// SetDirectUploadConfig configures server-issued OSS STS credentials for agent
// artifact uploads.
func (h *AgentHandler) SetDirectUploadConfig(cfg service.DirectUploadConfig) {
	h.directUploadCfg = cfg
}

// ClaimAuthMiddleware accepts only a user API key for the desktop claim route.
// Execution tokens cannot claim new work, and server-wide static keys are not
// user identities.
func (h *AgentHandler) ClaimAuthMiddleware(c fiber.Ctx) error {
	authorization := strings.TrimSpace(c.Get("Authorization"))
	secondary, secondaryCount := extractAgentHeaderCredential(c)
	var token string
	if authorization != "" {
		if secondaryCount > 0 {
			return Error(c, fiber.StatusUnauthorized, "conflicting agent credentials")
		}
		parts := strings.SplitN(authorization, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			return Error(c, fiber.StatusUnauthorized, "invalid agent authorization")
		}
		token = strings.TrimSpace(parts[1])
	} else {
		if secondaryCount == 0 {
			return Error(c, fiber.StatusUnauthorized, "missing agent api key")
		}
		if secondaryCount > 1 {
			return Error(c, fiber.StatusUnauthorized, "conflicting agent credentials")
		}
		token = secondary
	}
	if h.authenticateUserAPIKey(c, token) {
		return c.Next()
	}

	if h.logger != nil {
		h.logger.Warn().Msg("agent auth failed: invalid token")
	}
	return Error(c, fiber.StatusUnauthorized, "invalid agent api key")
}

// ExecutionAuthMiddleware is used by execution-scoped agent routes. It never
// falls back to a user API key, because an API key is not sufficient authority
// to mutate a particular execution attempt.
func (h *AgentHandler) ExecutionAuthMiddleware(c fiber.Ctx) error {
	authorization := strings.TrimSpace(c.Get("Authorization"))
	if strings.TrimSpace(c.Get("X-Agent-API-Key")) != "" || strings.TrimSpace(c.Get("X-API-Key")) != "" || strings.TrimSpace(c.Get("X-Admin-API-Key")) != "" {
		return Error(c, fiber.StatusUnauthorized, "execution credentials cannot be combined with api keys")
	}
	parts := strings.SplitN(authorization, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" || h.executionTokens == nil {
		return Error(c, fiber.StatusUnauthorized, "execution token is required")
	}
	claims, err := h.executionTokens.Validate(strings.TrimSpace(parts[1]))
	if err != nil {
		return Error(c, fiber.StatusUnauthorized, "invalid agent execution token")
	}
	c.Locals(agentUserIDContextKey, claims.UserID)
	c.Locals(agentProjectIDContextKey, claims.ProjectID)
	c.Locals(agentTaskIDContextKey, claims.TaskID)
	c.Locals(agentExecutionIDContextKey, claims.ExecutionID)
	return c.Next()
}

func (h *AgentHandler) authenticateUserAPIKey(c fiber.Ctx, token string) bool {
	if token == "" {
		return false
	}
	if h.apiKeySvc != nil {
		if apiKey, err := h.apiKeySvc.Validate(c.Context(), token); err == nil && apiKey != nil {
			c.Locals(agentUserIDContextKey, apiKey.UserID)
			return true
		}
	}
	return false
}

func extractAgentHeaderCredential(c fiber.Ctx) (string, int) {
	var token string
	count := 0
	for _, name := range []string{"X-Agent-API-Key", "X-API-Key", "X-Admin-API-Key"} {
		if candidate := strings.TrimSpace(c.Get(name)); candidate != "" {
			token = candidate
			count++
		}
	}
	return token, count
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

func (h *AgentHandler) authenticatedUserID(c fiber.Ctx) string {
	userID, _ := c.Locals(agentUserIDContextKey).(string)
	return userID
}

func (h *AgentHandler) authenticatedExecutionID(c fiber.Ctx) string {
	executionID, _ := c.Locals(agentExecutionIDContextKey).(string)
	return executionID
}

func (h *AgentHandler) authorizeExecutionScope(c fiber.Ctx, taskID string) error {
	if err := h.authorizeExecutionTaskScope(c, taskID); err != nil {
		return err
	}
	executionID, _ := c.Locals(agentExecutionIDContextKey).(string)
	if strings.TrimSpace(executionID) == "" {
		return errors.New("execution token is required")
	}
	userID, _ := c.Locals(agentUserIDContextKey).(string)
	projectID, _ := c.Locals(agentProjectIDContextKey).(string)
	if h.taskSvc == nil {
		return errors.New("task service unavailable")
	}
	return h.taskSvc.ValidateAgentExecutionAccess(c.Context(), userID, projectID, taskID, executionID)
}

func (h *AgentHandler) authorizeExecutionTaskScope(c fiber.Ctx, taskID string) error {
	claimTaskID, _ := c.Locals(agentTaskIDContextKey).(string)
	if strings.TrimSpace(claimTaskID) == "" {
		return errors.New("execution token is required")
	}
	if claimTaskID != taskID {
		return errors.New("execution token task mismatch")
	}
	return nil
}

type agentBootstrapRequest struct {
	ExecutionID string `json:"execution_id"`
}

func (h *AgentHandler) Bootstrap(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if h.workloadVerifier == nil || h.bootstrapper == nil {
		return Error(c, fiber.StatusServiceUnavailable, "agent bootstrap unavailable")
	}
	if strings.TrimSpace(c.Get("X-Anban-Agent-Contract-Version")) != fmt.Sprintf("%d", agentRuntimeContractVersion) {
		return ErrorWithCode(c, fiber.StatusUpgradeRequired, "agent_runtime_upgrade_required", "agent runtime contract upgrade required")
	}
	var req agentBootstrapRequest
	if err := c.Bind().Body(&req); err != nil || strings.TrimSpace(req.ExecutionID) == "" {
		return Error(c, fiber.StatusBadRequest, "execution_id is required")
	}
	token, _ := c.Locals(agentWorkloadTokenContextKey).(string)
	identity, err := h.workloadVerifier.Verify(c.Context(), token, strings.TrimSpace(req.ExecutionID))
	if err != nil {
		if h.logger != nil {
			h.logger.Warn().Err(err).Str("execution_id", strings.TrimSpace(req.ExecutionID)).Msg("agent workload identity verification failed")
		}
		return Error(c, fiber.StatusUnauthorized, "workload identity verification failed")
	}
	response, err := h.bootstrapper.Bootstrap(c.Context(), identity)
	if err != nil {
		if h.logger != nil {
			h.logger.Error().Err(err).Str("execution_id", strings.TrimSpace(req.ExecutionID)).Msg("agent bootstrap failed")
		}
		switch {
		case errors.Is(err, service.ErrAgentBootstrapConflict):
			return Error(c, fiber.StatusConflict, "agent bootstrap state conflict")
		case errors.Is(err, service.ErrAgentBootstrapUnavailable):
			return Error(c, fiber.StatusServiceUnavailable, "agent bootstrap dependency unavailable")
		default:
			return Error(c, fiber.StatusInternalServerError, "agent bootstrap failed")
		}
	}
	return Success(c, response)
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
	if err := h.authorizeExecutionScope(c, req.TaskID); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
	}
	result, err := h.taskSvc.PrepareTaskArtifactUpload(c.Context(), req.TaskID, h.authenticatedUserID(c), h.authenticatedExecutionID(c), h.directUploadCfg, req)
	if err != nil {
		if isAgentTaskAccessError(err) {
			return Error(c, fiber.StatusForbidden, "task access denied")
		}
		if errors.Is(err, service.ErrTaskArtifactUnavailable) {
			h.logger.Warn().Err(err).Str("task_id", req.TaskID).Msg("prepare agent artifact upload unavailable")
			return Error(c, fiber.StatusServiceUnavailable, "task artifact storage temporarily unavailable")
		}
		if errors.Is(err, service.ErrTaskArtifactExecutionConflict) {
			return Error(c, fiber.StatusConflict, "task execution is no longer current")
		}
		if errors.Is(err, service.ErrTaskArtifactInvalid) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		h.logger.Error().Err(err).Str("task_id", req.TaskID).Msg("prepare agent artifact upload failed")
		return Error(c, fiber.StatusInternalServerError, "failed to prepare task artifact upload")
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
	if err := h.authorizeExecutionScope(c, req.TaskID); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
	}
	if err := h.taskSvc.FinalizeTaskArtifactManifest(c.Context(), req.TaskID, h.authenticatedUserID(c), h.authenticatedExecutionID(c), req); err != nil {
		if isAgentTaskAccessError(err) {
			return Error(c, fiber.StatusForbidden, "task access denied")
		}
		switch {
		case errors.Is(err, service.ErrTaskArtifactExecutionConflict):
			return Error(c, fiber.StatusConflict, "task execution is no longer current")
		case errors.Is(err, service.ErrTaskArtifactInvalid):
			h.logger.Warn().Err(err).Str("task_id", req.TaskID).Msg("agent artifact manifest rejected")
			return Error(c, fiber.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrTaskArtifactUnavailable):
			h.logger.Warn().Err(err).Str("task_id", req.TaskID).Msg("agent artifact storage unavailable")
			return Error(c, fiber.StatusServiceUnavailable, "task artifact storage temporarily unavailable")
		default:
			h.logger.Error().Err(err).Str("task_id", req.TaskID).Msg("agent artifact manifest failed")
			return Error(c, fiber.StatusInternalServerError, "failed to persist task artifact manifest")
		}
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
	TaskID                 string   `json:"task_id"`
	ExecutionID            string   `json:"execution_id"`
	Message                string   `json:"message"`
	Logs                   []string `json:"logs"`
	Stage                  *string  `json:"stage"`
	State                  *string  `json:"state"`
	Title                  *string  `json:"title"`
	Description            *string  `json:"description"`
	ProgressPercent        *int     `json:"progress_percent"`
	stagePresent           bool
	statePresent           bool
	titlePresent           bool
	descriptionPresent     bool
	progressPercentPresent bool
}

func (r *agentProgressRequest) UnmarshalJSON(data []byte) error {
	type wireRequest struct {
		TaskID      string   `json:"task_id"`
		ExecutionID string   `json:"execution_id"`
		Message     string   `json:"message"`
		Logs        []string `json:"logs"`
	}
	var wire wireRequest
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*r = agentProgressRequest{
		TaskID:      wire.TaskID,
		ExecutionID: wire.ExecutionID,
		Message:     wire.Message,
		Logs:        wire.Logs,
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return err
	}
	seen := make(map[string]struct{}, 5)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("agent progress JSON object key must be a string")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return err
		}
		canonical, structured := canonicalAgentProgressKey(key)
		if !structured {
			continue
		}
		if key != canonical {
			return fmt.Errorf("structured progress key %q must use canonical casing %q", key, canonical)
		}
		if _, duplicate := seen[canonical]; duplicate {
			return fmt.Errorf("duplicate structured progress key %q", canonical)
		}
		seen[canonical] = struct{}{}
		if err := r.decodeStructuredProgressField(canonical, raw); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	return nil
}

func canonicalAgentProgressKey(key string) (string, bool) {
	for _, canonical := range [...]string{"stage", "state", "title", "description", "progress_percent"} {
		if strings.EqualFold(key, canonical) {
			return canonical, true
		}
	}
	return "", false
}

func (r *agentProgressRequest) decodeStructuredProgressField(key string, raw json.RawMessage) error {
	null := bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	switch key {
	case "stage":
		r.stagePresent = true
		if !null {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			r.Stage = &value
		}
	case "state":
		r.statePresent = true
		if !null {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			r.State = &value
		}
	case "title":
		r.titlePresent = true
		if !null {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			r.Title = &value
		}
	case "description":
		r.descriptionPresent = true
		if !null {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			r.Description = &value
		}
	case "progress_percent":
		r.progressPercentPresent = true
		if !null {
			var value int
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			r.ProgressPercent = &value
		}
	}
	return nil
}

type agentCompleteRequest struct {
	TaskID      string                       `json:"task_id"`
	ExecutionID string                       `json:"execution_id"`
	Result      *serveragent.ExecutionResult `json:"result"`
}

const agentPackContractVersion = 3
const agentRuntimeContractVersion = 1

// agentClaimRequest is the body for POST /api/v1/agent/claim.
// executor_info is an opaque JSON blob (desktop hostname/version) recorded for
// diagnostics. The contract version prevents older executors from claiming a
// task whose Agent Pack fields and runner arguments they cannot consume.
type agentClaimRequest struct {
	AgentPackContractVersion int             `json:"agent_pack_contract_version"`
	ExecutorInfo             json.RawMessage `json:"executor_info"`
}

// Claim handles POST /api/v1/agent/claim.
//
// A desktop local executor polls this endpoint to atomically claim its oldest
// pending local-target task. On success it returns the full task config
// (service.LocalExecutionConfig) which the desktop turns into an anban run
// argv (matching managed bootstrap defaults), supplying its own server_url +
// API key. The claimed task is already status=running, so cloud Asynq never
// picks it up. Returns 204 No Content when nothing is claimable.
func (h *AgentHandler) Claim(c fiber.Ctx) error {
	if executionID, _ := c.Locals(agentExecutionIDContextKey).(string); executionID != "" {
		return Error(c, fiber.StatusForbidden, "execution credentials cannot claim local tasks")
	}
	if h.taskSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}
	if h.executionTokens == nil || h.localTokenTTL <= 0 {
		return Error(c, fiber.StatusServiceUnavailable, "local execution credentials unavailable")
	}

	userID := h.authenticatedUserID(c)

	var req agentClaimRequest
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid request body")
		}
	}
	if req.AgentPackContractVersion != agentPackContractVersion {
		return Error(c, fiber.StatusUpgradeRequired, "desktop Agent Pack contract upgrade required")
	}

	cfg, err := h.taskSvc.ClaimLocalTask(c.Context(), userID, string(req.ExecutorInfo))
	if err != nil {
		h.logger.Error().Err(err).Msg("claim local task failed")
		return Error(c, fiber.StatusInternalServerError, "claim failed")
	}
	if cfg == nil {
		return c.Status(fiber.StatusNoContent).SendString("")
	}
	token, err := h.executionTokens.Issue(auth.ExecutionClaims{
		UserID: userID, ProjectID: cfg.ProjectID, TaskID: cfg.TaskID, ExecutionID: cfg.ExecutionID,
	}, time.Now().Add(h.localTokenTTL))
	if err != nil {
		failure := &serveragent.ExecutionResult{Success: false, Error: "local execution credential issuance failed", TerminalReason: "platform_error"}
		if completeErr := h.taskSvc.CompleteLocalTask(c.Context(), cfg.TaskID, cfg.ExecutionID, failure); completeErr != nil && h.logger != nil {
			h.logger.Error().Err(completeErr).Str("task_id", cfg.TaskID).Msg("failed to terminalize local task after credential issuance failure")
		}
		return Error(c, fiber.StatusServiceUnavailable, "local execution credentials unavailable")
	}
	cfg.ExecutionToken = token
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
	if err := h.authorizeExecutionScope(c, req.TaskID); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
	}

	structuredIntent := req.stagePresent || req.statePresent || req.titlePresent || req.descriptionPresent || req.progressPercentPresent
	executionID := strings.TrimSpace(h.authenticatedExecutionID(c))
	requestedExecutionID := strings.TrimSpace(req.ExecutionID)
	if requestedExecutionID != "" && requestedExecutionID != executionID {
		return Error(c, fiber.StatusForbidden, "execution access denied")
	}

	if structuredIntent {
		stage, state, title, description, percent := "", "", "", "", 0
		if !req.stagePresent || req.Stage == nil || strings.TrimSpace(*req.Stage) == "" {
			return Error(c, fiber.StatusBadRequest, "structured progress stage is required")
		}
		stage = strings.TrimSpace(*req.Stage)
		if !req.statePresent || req.State == nil {
			return Error(c, fiber.StatusBadRequest, "structured progress state is required")
		}
		state = strings.TrimSpace(*req.State)
		if state != "active" && state != "complete" {
			return Error(c, fiber.StatusBadRequest, "structured progress state must be active or complete")
		}
		if !req.titlePresent || req.Title == nil {
			return Error(c, fiber.StatusBadRequest, "structured progress title is required")
		}
		title = *req.Title
		if req.descriptionPresent {
			if req.Description == nil {
				return Error(c, fiber.StatusBadRequest, "structured progress description must not be null")
			}
			description = *req.Description
		}
		if !req.progressPercentPresent || req.ProgressPercent == nil {
			return Error(c, fiber.StatusBadRequest, "structured progress_percent is required")
		}
		percent = *req.ProgressPercent
		logs := append([]string(nil), req.Logs...)
		if req.Message != "" {
			logs = append(logs, req.Message)
		}
		if err := h.taskSvc.UpdateProgressFromAgent(c.Context(), req.TaskID, executionID, stage, state, title, description, percent, logs...); err != nil {
			switch {
			case errors.Is(err, service.ErrAgentProgressUnknownStage), errors.Is(err, service.ErrAgentProgressStateMismatch), errors.Is(err, service.ErrAgentProgressTitleMismatch), errors.Is(err, service.ErrAgentProgressPercentMismatch):
				return Error(c, fiber.StatusBadRequest, "invalid structured progress event")
			case errors.Is(err, service.ErrStaleTaskExecution):
				return Error(c, fiber.StatusConflict, "task execution is no longer current")
			case errors.Is(err, service.ErrAgentProgressExecutionMismatch), errors.Is(err, service.ErrAgentProgressPackMismatch):
				return Error(c, fiber.StatusConflict, "structured progress contract conflict")
			default:
				h.logger.Error().Err(err).Str("task_id", req.TaskID).Str("execution_id", executionID).Msg("failed to persist structured agent progress")
				return Error(c, fiber.StatusInternalServerError, "failed to persist structured progress")
			}
		}
		return Success(c, fiber.Map{"ok": true})
	}

	// Refresh the heartbeat on every unstructured progress report. This is the local-
	// execution keep-alive: a desktop agent reports progress per turn/line, and
	// each report resets the 5-min stuck-task reaper. Cloud tasks are also kept
	// alive by HandleExecution's HeartbeatFunc, so this is a harmless redundant
	// refresh there. Without it, a long-running local task would be force-failed
	// by reapStuckTasks (plan_checker.go) before it completes.
	if err := h.taskSvc.UpdateAgentHeartbeat(c.Context(), req.TaskID, executionID); err != nil {
		h.logger.Warn().Err(err).Str("task_id", req.TaskID).Str("execution_id", executionID).Msg("failed to update agent heartbeat")
		return Error(c, fiber.StatusInternalServerError, "failed to persist heartbeat")
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
	return Success(c, fiber.Map{"ok": true})
}

// Complete handles POST /api/v1/agent/complete.
//
// A desktop local executor calls this once when anban finishes, with
// the final ExecutionResult. The service finalizes the task (status → completed
// or failed, slot release, dispatch, refund-on-failure) — guarded to the
// current local_claimed execution. Identical terminal retries are acknowledged.
// This is the terminal half of the local-execution path; without it a
// local task could never reach a terminal state (the agent binary is shared with
// cloud, whose authoritative finalization is server-side HandleExecution).
func (h *AgentHandler) Complete(c fiber.Ctx) error {
	if h.taskSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}

	var req agentCompleteRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.TaskID) == "" {
		return Error(c, fiber.StatusBadRequest, "task_id is required")
	}
	if err := h.authorizeExecutionTaskScope(c, req.TaskID); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
	}

	task, err := h.taskSvc.ValidateAgentTaskAccess(c.Context(), req.TaskID, h.authenticatedUserID(c))
	if err != nil {
		if errors.Is(err, service.ErrAgentAccessDenied) {
			return Error(c, fiber.StatusForbidden, "task access denied")
		}
		h.logger.Error().Err(err).Str("task_id", req.TaskID).Msg("validate agent task completion access")
		return Error(c, fiber.StatusInternalServerError, "complete failed")
	}

	executionID := strings.TrimSpace(h.authenticatedExecutionID(c))
	requestedExecutionID := strings.TrimSpace(req.ExecutionID)
	if requestedExecutionID != "" && requestedExecutionID != executionID {
		return Error(c, fiber.StatusForbidden, "execution access denied")
	}
	projectID, _ := c.Locals(agentProjectIDContextKey).(string)
	if err := h.taskSvc.ValidateAgentCompletionAccess(c.Context(), h.authenticatedUserID(c), projectID, req.TaskID, executionID); err != nil {
		if errors.Is(err, service.ErrAgentAccessDenied) {
			return Error(c, fiber.StatusForbidden, "execution access denied")
		}
		h.logger.Error().Err(err).Str("task_id", req.TaskID).Str("execution_id", executionID).Msg("validate execution completion access")
		return Error(c, fiber.StatusInternalServerError, "complete failed")
	}
	if task.ExecutionTarget == model.ExecutionTargetLocalClaimed {
		err = h.taskSvc.CompleteLocalTask(c.Context(), req.TaskID, executionID, req.Result)
	} else {
		err = h.taskSvc.CompleteCloudExecution(c.Context(), executionID, req.Result)
	}
	if err != nil {
		status, message := agentCompletionErrorResponse(err)
		if status == fiber.StatusConflict {
			return Error(c, status, message)
		}
		h.logger.Error().Err(err).Str("task_id", req.TaskID).Str("execution_id", executionID).Msg("complete agent task failed")
		return Error(c, status, message)
	}
	return Success(c, fiber.Map{"ok": true})
}

func agentCompletionErrorResponse(err error) (int, string) {
	if errors.Is(err, service.ErrTaskCompletionConflict) {
		return fiber.StatusConflict, "completion result conflicts with terminal outcome"
	}
	if errors.Is(err, service.ErrStaleTaskExecution) {
		return fiber.StatusConflict, "task execution is no longer current"
	}
	return fiber.StatusInternalServerError, "complete failed"
}
