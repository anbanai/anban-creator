package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

const (
	agentUserIDContextKey        = "agent_user_id"
	agentExecutionIdentityKey    = "agent_execution_identity"
	agentWorkloadTokenContextKey = "agent_workload_token"
)

type agentBootstrapper interface {
	Bootstrap(context.Context, *serveragent.WorkloadIdentity) (*service.AgentBootstrapResponse, error)
}

// AgentHandler handles agent-to-server communication endpoints.
type AgentHandler struct {
	taskSvc           *service.TaskService
	apiKeySvc         *service.APIKeyService
	artifactUploadCfg service.TaskArtifactUploadConfig
	executionTokens   *auth.ExecutionTokenService
	workloadVerifier  serveragent.WorkloadVerifier
	bootstrapper      agentBootstrapper
	logger            *zerolog.Logger
}

func (h *AgentHandler) SetExecutionTokenService(tokens *auth.ExecutionTokenService) {
	h.executionTokens = tokens
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

func (h *AgentHandler) SetTaskArtifactUploadConfig(cfg service.TaskArtifactUploadConfig) {
	h.artifactUploadCfg = cfg
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
	c.Locals(agentExecutionIdentityKey, auth.ExecutionIdentityFromClaims(*claims))
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
	if identity, ok := c.Locals(agentExecutionIdentityKey).(auth.ExecutionIdentity); ok {
		return identity.UserID
	}
	userID, _ := c.Locals(agentUserIDContextKey).(string)
	return userID
}

func (h *AgentHandler) authenticatedExecutionID(c fiber.Ctx) string {
	return h.authenticatedExecutionIdentity(c).ExecutionID
}

func (h *AgentHandler) authenticatedExecutionIdentity(c fiber.Ctx) auth.ExecutionIdentity {
	identity, _ := c.Locals(agentExecutionIdentityKey).(auth.ExecutionIdentity)
	return identity
}

func (h *AgentHandler) authorizeExecutionScope(c fiber.Ctx, taskID string) error {
	if err := h.authorizeExecutionTaskScope(c, taskID); err != nil {
		return err
	}
	identity := h.authenticatedExecutionIdentity(c)
	if strings.TrimSpace(identity.ExecutionID) == "" {
		return errors.New("execution token is required")
	}
	if h.taskSvc == nil {
		return errors.New("task service unavailable")
	}
	return h.taskSvc.ValidateAgentExecutionAccess(c.Context(), identity.UserID, identity.ProjectID, taskID, identity.ExecutionID)
}

func (h *AgentHandler) authorizeExecutionTaskScope(c fiber.Ctx, taskID string) error {
	claimTaskID := h.authenticatedExecutionIdentity(c).TaskID
	if strings.TrimSpace(claimTaskID) == "" {
		return errors.New("execution token is required")
	}
	if claimTaskID != taskID {
		return errors.New("execution token task mismatch")
	}
	return nil
}

func (h *AgentHandler) Bootstrap(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if h.workloadVerifier == nil || h.bootstrapper == nil {
		return Error(c, fiber.StatusServiceUnavailable, "agent bootstrap unavailable")
	}
	receivedContractVersion := strings.TrimSpace(c.Get("X-Anban-Agent-Contract-Version"))
	expectedContractVersion := fmt.Sprintf("%d", agentRuntimeContractVersion)
	if receivedContractVersion != expectedContractVersion {
		c.Set("X-Anban-Agent-Contract-Version-Expected", expectedContractVersion)
		if h.logger != nil {
			h.logger.Warn().
				Str("received_contract_version", receivedContractVersion).
				Str("expected_contract_version", expectedContractVersion).
				Msg("agent bootstrap rejected: runtime contract mismatch")
		}
		return ErrorWithCode(c, fiber.StatusUpgradeRequired, "agent_runtime_upgrade_required", "agent runtime contract upgrade required")
	}
	if len(c.Body()) != 0 {
		return Error(c, fiber.StatusBadRequest, "bootstrap request body must be empty")
	}
	token, _ := c.Locals(agentWorkloadTokenContextKey).(string)
	identity, err := h.workloadVerifier.Verify(c.Context(), token)
	if err != nil {
		if h.logger != nil {
			h.logger.Warn().Err(err).Msg("agent workload identity verification failed")
		}
		return Error(c, fiber.StatusUnauthorized, "workload identity verification failed")
	}
	if identity == nil || strings.TrimSpace(identity.ExecutionID) == "" || strings.TrimSpace(identity.TaskID) == "" || strings.TrimSpace(identity.ProjectID) == "" || strings.TrimSpace(identity.UserID) == "" {
		return Error(c, fiber.StatusUnauthorized, "workload identity verification failed")
	}
	response, err := h.bootstrapper.Bootstrap(c.Context(), identity)
	if err != nil {
		if h.logger != nil {
			h.logger.Error().Err(err).Str("execution_id", identity.ExecutionID).Msg("agent bootstrap failed")
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
	if response == nil || strings.TrimSpace(response.ExecutionID) != identity.ExecutionID || strings.TrimSpace(response.TaskID) != identity.TaskID || strings.TrimSpace(response.ProjectID) != identity.ProjectID {
		if h.logger != nil {
			h.logger.Error().Str("execution_id", identity.ExecutionID).Msg("agent bootstrap returned an invalid execution identity")
		}
		return Error(c, fiber.StatusConflict, "agent bootstrap identity conflict")
	}
	if h.executionTokens == nil {
		return Error(c, fiber.StatusServiceUnavailable, "agent bootstrap unavailable")
	}
	claims, err := h.executionTokens.Validate(response.ExecutionToken)
	if err != nil || claims.UserID != identity.UserID || claims.ProjectID != identity.ProjectID || claims.TaskID != identity.TaskID || claims.ExecutionID != identity.ExecutionID {
		if h.logger != nil {
			h.logger.Error().Str("execution_id", identity.ExecutionID).Msg("agent bootstrap returned a token outside the verified workload identity")
		}
		return Error(c, fiber.StatusConflict, "agent bootstrap identity conflict")
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
	result, err := h.taskSvc.PrepareTaskArtifactUpload(c.Context(), req.TaskID, h.authenticatedUserID(c), h.authenticatedExecutionID(c), h.artifactUploadCfg, req)
	if err != nil {
		if isAgentTaskAccessError(err) {
			return Error(c, fiber.StatusForbidden, "task access denied")
		}
		if errors.Is(err, service.ErrTaskArtifactUnavailable) {
			h.logArtifactFailure("prepare", req.TaskID, h.authenticatedExecutionID(c), req.RelativePath, err)
			return Error(c, fiber.StatusServiceUnavailable, "task artifact storage temporarily unavailable")
		}
		if errors.Is(err, service.ErrTaskArtifactExecutionConflict) {
			return Error(c, fiber.StatusConflict, "task execution is no longer current")
		}
		if errors.Is(err, service.ErrTaskArtifactInvalid) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		h.logArtifactFailure("prepare", req.TaskID, h.authenticatedExecutionID(c), req.RelativePath, err)
		return Error(c, fiber.StatusInternalServerError, "failed to prepare task artifact upload")
	}
	return Success(c, result)
}

func (h *AgentHandler) logArtifactFailure(operation, taskID, executionID, relativePath string, err error) {
	if h.logger == nil {
		return
	}
	diagnostic := service.NewArtifactDiagnosticFields(operation, err)
	h.logger.Warn().Str("operation", diagnostic.Operation).Str("error_code", diagnostic.Code).
		Int("http_status", diagnostic.HTTPStatus).Str("request_id", diagnostic.RequestID).
		Str("network_code", diagnostic.NetworkCode).Str("task_id", taskID).
		Str("execution_id", executionID).Str("relative_path", relativePath).
		Msg("agent artifact operation failed")
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
			h.logArtifactFailure("manifest", req.TaskID, h.authenticatedExecutionID(c), "", err)
			return Error(c, fiber.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrTaskArtifactUnavailable):
			h.logArtifactFailure("manifest", req.TaskID, h.authenticatedExecutionID(c), "", err)
			return Error(c, fiber.StatusServiceUnavailable, "task artifact storage temporarily unavailable")
		default:
			h.logArtifactFailure("manifest", req.TaskID, h.authenticatedExecutionID(c), "", err)
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
	TaskID      string   `json:"task_id"`
	Message     string   `json:"message"`
	Logs        []string `json:"logs"`
	Stage       *string  `json:"stage"`
	State       *string  `json:"state"`
	Description *string  `json:"description"`
}

func (r *agentProgressRequest) UnmarshalJSON(data []byte) error {
	type wire agentProgressRequest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var value wire
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*r = agentProgressRequest(value)
	return nil
}

type agentProgressPlanRequest struct {
	TaskID string                         `json:"task_id"`
	Stages []model.TaskLifecyclePlanStage `json:"stages"`
}

func (r *agentProgressPlanRequest) UnmarshalJSON(data []byte) error {
	type wire agentProgressPlanRequest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var value wire
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*r = agentProgressPlanRequest(value)
	return nil
}

type agentCompleteRequest struct {
	TaskID string                       `json:"task_id"`
	Result *serveragent.ExecutionResult `json:"result"`
}

func (r *agentCompleteRequest) UnmarshalJSON(data []byte) error {
	type wire agentCompleteRequest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var value wire
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*r = agentCompleteRequest(value)
	return nil
}

const agentRuntimeContractVersion = 4

// ProgressPlan handles POST /api/v1/agent/progress-plan.
func (h *AgentHandler) ProgressPlan(c fiber.Ctx) error {
	if h.taskSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}
	var req agentProgressPlanRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.TaskID) == "" {
		return Error(c, fiber.StatusBadRequest, "task_id is required")
	}
	if err := h.authorizeExecutionScope(c, req.TaskID); err != nil {
		return Error(c, fiber.StatusForbidden, "task access denied")
	}
	executionID := strings.TrimSpace(h.authenticatedExecutionID(c))
	lifecycle, err := h.taskSvc.SetTaskProgressPlan(c.Context(), req.TaskID, executionID, req.Stages)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTaskLifecyclePlan), errors.Is(err, service.ErrTaskLifecycleImmutablePrefix):
			return Error(c, fiber.StatusBadRequest, "invalid task progress plan")
		case errors.Is(err, service.ErrStaleTaskExecution):
			return Error(c, fiber.StatusConflict, "task execution is no longer current")
		default:
			h.logger.Error().Err(err).Str("task_id", req.TaskID).Str("execution_id", executionID).Msg("failed to persist task progress plan")
			return Error(c, fiber.StatusInternalServerError, "failed to persist task progress plan")
		}
	}
	return Success(c, lifecycle)
}

// Progress handles POST /api/v1/agent/progress. Structured requests update a
// declared lifecycle stage; unstructured requests append raw logs and refresh
// the execution heartbeat.
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

	structuredIntent := req.Stage != nil || req.State != nil || req.Description != nil
	executionID := strings.TrimSpace(h.authenticatedExecutionID(c))

	if structuredIntent {
		if strings.TrimSpace(req.Message) != "" || len(req.Logs) > 0 {
			return Error(c, fiber.StatusBadRequest, "structured lifecycle updates cannot include raw logs")
		}
		if req.Stage == nil || strings.TrimSpace(*req.Stage) == "" {
			return Error(c, fiber.StatusBadRequest, "structured progress stage is required")
		}
		if req.State == nil {
			return Error(c, fiber.StatusBadRequest, "structured progress state is required")
		}
		stage := strings.TrimSpace(*req.Stage)
		state := strings.TrimSpace(*req.State)
		if state != "active" && state != "complete" {
			return ErrorWithCode(c, fiber.StatusBadRequest, "progress_invalid_state", "structured progress state must be active or complete")
		}
		description := ""
		if req.Description != nil {
			description = strings.TrimSpace(*req.Description)
		}
		lifecycle, err := h.taskSvc.UpdateTaskProgress(c.Context(), req.TaskID, executionID, stage, state, description)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrTaskLifecycleUnknownStage):
				return ErrorWithCode(c, fiber.StatusBadRequest, "progress_unknown_stage", "unknown task progress stage")
			case errors.Is(err, service.ErrTaskLifecycleOutOfOrder):
				return ErrorWithCode(c, fiber.StatusBadRequest, "progress_out_of_order", "task progress event is out of order")
			case errors.Is(err, service.ErrTaskLifecycleInvalidState):
				return ErrorWithCode(c, fiber.StatusBadRequest, "progress_invalid_state", "invalid task progress state")
			case errors.Is(err, service.ErrStaleTaskExecution):
				return Error(c, fiber.StatusConflict, "task execution is no longer current")
			default:
				h.logger.Error().Err(err).Str("task_id", req.TaskID).Str("execution_id", executionID).Msg("failed to persist structured agent progress")
				return Error(c, fiber.StatusInternalServerError, "failed to persist structured progress")
			}
		}
		return Success(c, lifecycle)
	}

	// Refresh the heartbeat on every unstructured progress report.
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
// Complete handles the final execution result from a managed Agent runtime.
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

	_, err := h.taskSvc.ValidateAgentTaskAccess(c.Context(), req.TaskID, h.authenticatedUserID(c))
	if err != nil {
		if errors.Is(err, service.ErrAgentAccessDenied) {
			return Error(c, fiber.StatusForbidden, "task access denied")
		}
		h.logger.Error().Err(err).Str("task_id", req.TaskID).Msg("validate agent task completion access")
		return Error(c, fiber.StatusInternalServerError, "complete failed")
	}

	executionID := strings.TrimSpace(h.authenticatedExecutionID(c))
	identity := h.authenticatedExecutionIdentity(c)
	if err := h.taskSvc.ValidateAgentCompletionAccess(c.Context(), identity.UserID, identity.ProjectID, req.TaskID, executionID); err != nil {
		if errors.Is(err, service.ErrAgentAccessDenied) {
			return Error(c, fiber.StatusForbidden, "execution access denied")
		}
		h.logger.Error().Err(err).Str("task_id", req.TaskID).Str("execution_id", executionID).Msg("validate execution completion access")
		return Error(c, fiber.StatusInternalServerError, "complete failed")
	}
	err = h.taskSvc.CompleteCloudExecution(c.Context(), executionID, req.Result)
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
