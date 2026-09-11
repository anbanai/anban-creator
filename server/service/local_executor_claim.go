package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

// maxExecutorInfoBytes caps the desktop-supplied diagnostics blob written to the
// Task.executor_info JSON column. Prevents a buggy/malicious client from stuffing
// multi-MB blobs or pathologically nested JSON into the column.
const maxExecutorInfoBytes = 4 * 1024

// parseExecutorMeta validates the desktop-supplied executor_info and reduces it
// to a canonical ExecutorMeta, so the JSON column never holds arbitrary bytes.
// Empty / null / whitespace input yields a zero-value ExecutorMeta (a valid JSON
// object on store). Non-object JSON or oversized blobs are rejected.
func parseExecutorMeta(raw []byte) (model.ExecutorMeta, error) {
	var info model.ExecutorMeta
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return info, nil
	}
	if len(raw) > maxExecutorInfoBytes {
		return info, fmt.Errorf("executor_info too large: %d bytes (max %d)", len(raw), maxExecutorInfoBytes)
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		return info, fmt.Errorf("invalid executor_info: %w", err)
	}
	return info, nil
}

// LocalClaimWindow is how long a local-target task waits for a desktop
// executor to claim it before the server falls back to cloud execution. The
// desktop polls /api/v1/agent/claim every couple of seconds, so this only
// elapses when no desktop is online.
const LocalClaimWindow = 30 * time.Second

// ReapStaleLocalExecutions owns recovery for desktop executions. Local attempts
// have no provider workload and must never be inspected by RuntimeReconciler.
// The hard deadline is the same configured execution timeout used by the local
// runner; heartbeatTimeout detects a disappeared desktop before that deadline.
func (s *TaskService) ReapStaleLocalExecutions(ctx context.Context, now time.Time, heartbeatTimeout time.Duration, limit int) (int, error) {
	if s == nil || s.repo == nil {
		return 0, errors.New("task service repository is not configured")
	}
	if heartbeatTimeout <= 0 {
		return 0, errors.New("local execution heartbeat timeout must be positive")
	}
	activeDeadline := s.executionTimeout
	if activeDeadline <= 0 {
		activeDeadline = 60 * time.Minute
	}
	heartbeatBefore := now.Add(-heartbeatTimeout)
	createdBefore := now.Add(-activeDeadline)
	candidates, err := s.repo.TaskExecutions().FindLocalReconcileCandidates(ctx, heartbeatBefore, createdBefore, limit)
	if err != nil {
		return 0, fmt.Errorf("find local execution reconcile candidates: %w", err)
	}
	reaped := 0
	var reconcileErr error
	for _, execution := range candidates {
		if execution == nil {
			continue
		}
		if isTerminalExecution(execution.Status) {
			if err := s.ResumeExecutionFinalization(ctx, execution.ID); err != nil && !errors.Is(err, ErrStaleTaskExecution) {
				reconcileErr = errors.Join(reconcileErr, fmt.Errorf("resume local execution %s finalization: %w", execution.ID, err))
			}
			continue
		}
		won, err := s.reapStaleLocalExecution(ctx, execution.ID, heartbeatBefore, createdBefore)
		if err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("reap local execution %s: %w", execution.ID, err))
			continue
		}
		if won {
			reaped++
		}
	}
	return reaped, reconcileErr
}

func (s *TaskService) reapStaleLocalExecution(ctx context.Context, executionID string, heartbeatBefore, createdBefore time.Time) (bool, error) {
	const message = "local execution timed out after its heartbeat or active deadline expired"
	result := normalizeTerminalExecutionResult(nil)
	result.Error = message
	result.TerminalReason = model.TaskBillingTerminalExecutionTimeout
	result.RemoteArtifacts = true
	taskResult, executionResult, err := marshalLocalCompletionEvidence(result)
	if err != nil {
		return false, err
	}

	durableDelivery := false
	if candidate, findErr := s.repo.TaskExecutions().FindByID(ctx, executionID); findErr == nil {
		durableDelivery, err = s.taskHasDurableDelivery(ctx, candidate.TaskID)
		if err != nil {
			return false, fmt.Errorf("inspect durable local task delivery: %w", err)
		}
	} else {
		return false, findErr
	}

	var task *model.Task
	var execution *model.TaskExecution
	won := false
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var lockErr error
		execution, lockErr = tx.TaskExecutions().FindByIDForUpdate(ctx, executionID)
		if errors.Is(lockErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if lockErr != nil {
			return lockErr
		}
		task, lockErr = tx.Tasks().FindByIDForUpdate(ctx, execution.TaskID)
		if errors.Is(lockErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if lockErr != nil {
			return lockErr
		}
		if task.Status != model.TaskStatusRunning || task.ExecutionTarget != model.ExecutionTargetLocalClaimed ||
			task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID ||
			execution.Target != model.ExecutionTargetLocalClaimed || execution.Status != model.TaskExecutionRunning ||
			!localExecutionExpired(execution, heartbeatBefore, createdBefore) {
			return nil
		}
		won, lockErr = tx.Tasks().FinalizeLocalTaskWithArtifactsInTx(ctx, task.ID, execution.ID, model.TaskStatusFailed,
			message, taskResult, executionResult, result.ModelUsage, result.CostStatus, repository.LocalTaskArtifactsCollect)
		if lockErr != nil || !won {
			return lockErr
		}
		billingExecution := &model.TaskExecution{ID: execution.ID, Status: model.TaskExecutionTimedOut, TerminalReason: model.TaskBillingTerminalExecutionTimeout}
		return s.persistTerminalBillingInTx(ctx, tx, task, billingExecution, model.TaskBillingTerminalExecutionTimeout, durableDelivery)
	})
	if err != nil || !won {
		return false, err
	}
	task.Status = model.TaskStatusFailed
	task.ErrorMessage = message
	execution.Status = model.TaskExecutionFailed
	execution.TerminalReason = message
	execution.Result = []byte(executionResult)
	execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
	execution.CleanupStatus = model.TaskExecutionCleanupDone
	if err := s.finalizeLocalTaskFromExecution(ctx, task, execution); err != nil {
		return true, err
	}
	return true, nil
}

func localExecutionExpired(execution *model.TaskExecution, heartbeatBefore, createdBefore time.Time) bool {
	if execution == nil || !execution.CreatedAt.After(createdBefore) {
		return execution != nil
	}
	if execution.LastHeartbeatAt != nil {
		return !execution.LastHeartbeatAt.After(heartbeatBefore)
	}
	return execution.StartedAt != nil && !execution.StartedAt.After(heartbeatBefore)
}

// LocalExecutionConfig is the full task config returned to a desktop local
// executor on a successful claim. The desktop builds the anban run argv
// from this (matching the managed bootstrap defaults) and supplies
// server_url + the user's own API key from its local settings — those are NOT
// echoed here (the desktop already holds them and echoing keys is unsafe).
type LocalExecutionConfig struct {
	TaskID                   string `json:"task_id"`
	ExecutionID              string `json:"execution_id"`
	TaskType                 string `json:"task_type"`
	AgentPackID              string `json:"agent_pack_id"`
	AgentPackVersion         string `json:"agent_pack_version"`
	AgentPackDigest          string `json:"agent_pack_digest"`
	RuntimeAdapter           string `json:"runtime_adapter"`
	RuntimeProfile           string `json:"runtime_profile"`
	Topic                    string `json:"topic"`
	AgentFlag                string `json:"agent_flag"` // "anban:<agent>"
	MaxTurns                 int    `json:"max_turns"`
	Model                    string `json:"model,omitempty"`
	HasContentImage          bool   `json:"has_content_image"`
	HasTailImage             bool   `json:"has_tail_image"`
	ArticleWithCover         bool   `json:"article_with_cover"`
	ArticleWithContentImages bool   `json:"article_with_content_images"`
	ProjectID                string `json:"project_id"`
	ExecutionToken           string `json:"execution_token"`
	ArtifactUploadMode       string `json:"artifact_upload_mode"`
}

// ClaimLocalTask atomically claims the oldest pending local-target task owned
// by userID and returns its execution config. Returns (nil, nil) when no task
// is claimable (none pending, none local-target, deadline expired, or lost the
// CAS race). executorInfoRaw is the desktop's diagnostics blob (hostname/
// version); it is validated + size-capped + canonicalized into an ExecutorMeta
// before being stored, so the JSON column never holds untrusted bytes.
//
// On a successful claim the task is already status=running +
// execution_target=local_claimed, so cloud Asynq will never pick it up. The
// claiming desktop then spawns anban, which reports progress and a sealed
// execution-scoped artifact manifest before completion.
func (s *TaskService) ClaimLocalTask(ctx context.Context, userID, executorInfo string) (*LocalExecutionConfig, error) {
	info, err := parseExecutorMeta([]byte(executorInfo))
	if err != nil {
		return nil, err
	}
	// Canonical re-marshal: the column is typed datatypes.JSONType[ExecutorMeta],
	// so store a validated ExecutorMeta object, never the raw client bytes.
	canonical, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("marshal executor info: %w", err)
	}
	var task *model.Task
	var execution *model.TaskExecution
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		claimed, err := tx.Tasks().ClaimNextLocalTask(ctx, userID, canonical)
		if err != nil || claimed == nil {
			task = claimed
			return err
		}
		if err := s.validateFrozenTaskImageCapability(ctx, claimed); err != nil {
			return err
		}
		if hasManagedAgentProfile(claimed) {
			// The claim CAS has already staged the local state transition in this
			// transaction. Returning the structured policy error rolls it back
			// before a desktop config or execution record can be emitted.
			return ErrManagedProfileLocalExecutionUnsupported
		}
		attempt, err := tx.TaskExecutions().NextAttempt(ctx, claimed.ID)
		if err != nil {
			return fmt.Errorf("allocate local task execution attempt: %w", err)
		}
		profiledExecution := model.NewTaskExecutionAgentProfile(claimed.AgentProfileSnapshot, claimed.AgentProfileFingerprint)
		execution = &profiledExecution
		if err := applyAgentPackIdentity(execution, claimed.Type); err != nil {
			return err
		}
		execution.ID, execution.TaskID, execution.Attempt = uuid.NewString(), claimed.ID, attempt
		execution.Target, execution.Status = model.ExecutionTargetLocalClaimed, model.TaskExecutionRunning
		execution.Started = true
		now := time.Now()
		execution.StartedAt = &now
		if err := tx.TaskExecutions().Create(ctx, execution); err != nil {
			return fmt.Errorf("create local task execution: %w", err)
		}
		won, err := tx.Tasks().SetCurrentExecution(ctx, claimed.ID, execution.ID)
		if err != nil || !won {
			if err == nil {
				err = fmt.Errorf("claimed local task lost execution authority")
			}
			return err
		}
		claimed.CurrentExecutionID = &execution.ID
		task = claimed
		return nil
	})
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, nil
	}
	return s.buildLocalExecutionConfig(task, execution), nil
}

func hasManagedAgentProfile(task *model.Task) bool {
	if task == nil {
		return false
	}
	snapshot := task.AgentProfileSnapshot
	return strings.TrimSpace(task.ExecutionProfile) != "" ||
		strings.TrimSpace(task.AgentProfileFingerprint) != "" ||
		snapshot.SchemaVersion != 0 ||
		strings.TrimSpace(snapshot.ProfileID) != "" ||
		strings.TrimSpace(snapshot.Provider) != "" ||
		strings.TrimSpace(snapshot.Envs[model.ClaudeEnvModel]) != "" ||
		strings.TrimSpace(snapshot.Protocol) != ""
}

// buildLocalExecutionConfig resolves the agent argv inputs for a task and copies
// the already-frozen Pack identity from the claimed execution. The desktop must
// pass that identity to the runner instead of re-resolving only from task type.
func (s *TaskService) buildLocalExecutionConfig(task *model.Task, execution *model.TaskExecution) *LocalExecutionConfig {
	config := &LocalExecutionConfig{
		TaskID:                   task.ID,
		TaskType:                 task.Type,
		Topic:                    task.Prompt,
		AgentFlag:                "anban:" + agent.TaskToAgent(task),
		MaxTurns:                 agent.DefaultMaxTurns(task.Type, s.maxTurnsOverrides),
		Model:                    task.AgentProfileSnapshot.Envs[model.ClaudeEnvModel],
		HasContentImage:          task.HasContentImage,
		HasTailImage:             task.HasTailImage,
		ArticleWithCover:         task.ArticleWithCover == nil || *task.ArticleWithCover,
		ArticleWithContentImages: task.ArticleWithContentImages == nil || *task.ArticleWithContentImages,
		ProjectID:                task.ProjectID,
		ArtifactUploadMode:       "stream",
	}
	if execution != nil {
		config.ExecutionID = execution.ID
		config.AgentPackID = execution.AgentPackID
		config.AgentPackVersion = execution.AgentPackVersion
		config.AgentPackDigest = execution.AgentPackDigest
		config.RuntimeAdapter = execution.RuntimeAdapter
		config.RuntimeProfile = execution.RuntimeProfile
	}
	return config
}

// ReclaimExpiredLocalTasks flips pending local-target tasks past their claim
// deadline back to cloud execution and re-enqueues them. Called periodically
// (every ~10s) by the fallback worker so tasks aren't stuck when no desktop is
// online. Returns the number of tasks reclaimed.
//
// ResetLocalTarget is an atomic CAS (pending+local → cloud); if it returns
// reset=false the task was claimed or changed between FindExpiredLocalTasks and
// the reset, so we MUST skip re-enqueue — otherwise the task would run on both
// the desktop that just claimed it and cloud (double execution, double billing).
func (s *TaskService) ReclaimExpiredLocalTasks(ctx context.Context) (int, error) {
	ids, err := s.repo.Tasks().FindExpiredLocalTasks(ctx, time.Now())
	if err != nil {
		return 0, err
	}
	reclaimed := 0
	for _, id := range ids {
		reset, err := s.repo.Tasks().ResetLocalTarget(ctx, id)
		if err != nil {
			s.logger.Warn().Err(err).Str("task_id", id).Msg("fallback: reset local target")
			continue
		}
		if !reset {
			// Lost the race to a claimer (or another replica). Do NOT enqueue.
			continue
		}
		task, err := s.repo.Tasks().FindByID(ctx, id)
		if err != nil {
			s.logger.Warn().Err(err).Str("task_id", id).Msg("fallback: reload after reset")
			continue
		}
		task.ExecutionTarget = model.ExecutionTargetCloud
		task.LocalClaimDeadline = nil
		if err := s.EnqueueExecution(ctx, task, nil); err != nil {
			s.logger.Error().Err(err).Str("task_id", id).Msg("fallback: re-enqueue failed")
			continue
		}
		reclaimed++
		s.logger.Info().Str("task_id", id).Msg("local task unclaimed past deadline, fell back to cloud execution")
	}
	return reclaimed, nil
}

// CompleteLocalTask finalizes a desktop-executed (local_claimed) task: persists
// the result, transitions it to a terminal status, releases the concurrency
// slot, and dispatches the next pending task. It is the local-execution analog
// of the cloud HandleExecution finalization tail (task_execution.go:288-357).
//
// Key difference from cloud: a local task's WorkDir lives on the desktop, so
// the server cannot host-side upload files or extract the article draft. The
// desktop agent uploads its output files through the execution-scoped artifact
// protocol and may publish through the MCP tool itself. Therefore this path does NOT perform server-side
// auto-publish / hold-for-approval (a cloud-WorkDir-only fallback); if the agent
// did not publish and the project requires publishing, it logs a warning for the
// operator instead of silently skipping.
//
// The execution ID is part of the finalization CAS. Only the current local
// claim may win terminal ownership; a retry of its identical terminal outcome
// is acknowledged without replaying completion side effects.
func (s *TaskService) CompleteLocalTask(ctx context.Context, taskID, executionID string, result *agent.ExecutionResult) error {
	executionID = strings.TrimSpace(executionID)
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	if task == nil {
		return ErrStaleTaskExecution
	}
	if task.ExecutionTarget != model.ExecutionTargetLocalClaimed || executionID == "" || task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
		return ErrStaleTaskExecution
	}
	outcome, err := s.localCompletionOutcome(ctx, task, executionID, result)
	if err != nil {
		return err
	}
	if task.Status != model.TaskStatusRunning {
		return s.confirmLocalCompletionRetry(ctx, task, executionID, outcome)
	}

	if outcome.taskStatus == model.TaskStatusFailed {
		if outcome.artifactValidation != nil {
			validation := outcome.artifactValidation
			s.logger.Warn().
				Str("task_id", taskID).
				Int("meaningful_files", validation.MeaningfulFileCount).
				Strs("missing_files", validation.Missing).
				Msg(outcome.errorMessage)
		}
		return s.failLocalTask(ctx, task, executionID, outcome)
	}

	// Draft delivery is an independent durable lifecycle owned by create_draft.
	// Local completion only finalizes task execution.
	result = outcome.result

	resultJSON, executionResultJSON, err := marshalLocalCompletionEvidence(result)
	if err != nil {
		return err
	}
	execution := &model.TaskExecution{ID: executionID, Status: model.TaskExecutionSucceeded}
	var swapped bool
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var finalizeErr error
		swapped, finalizeErr = tx.Tasks().FinalizeLocalTaskWithArtifactsInTx(ctx, taskID, execution.ID, model.TaskStatusCompleted, "", resultJSON, executionResultJSON, result.ModelUsage, result.CostStatus, repository.LocalTaskArtifactsPublish)
		if finalizeErr != nil || !swapped {
			return finalizeErr
		}
		return s.persistTerminalBillingInTx(ctx, tx, task, execution, model.TaskBillingTerminalCompleted, true)
	})
	if err != nil && !errors.Is(err, repository.ErrLocalTaskExecutionCASLost) {
		return fmt.Errorf("finalize local task as completed: %w", err)
	}
	if err != nil || !swapped {
		return s.confirmLocalCompletionAfterCASLoss(ctx, taskID, executionID, outcome)
	}
	task.Status = model.TaskStatusCompleted
	execution.Target = model.ExecutionTargetLocalClaimed
	execution.Result = []byte(executionResultJSON)
	execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
	if err := s.finalizeLocalTaskFromExecution(ctx, task, execution); err != nil {
		return err
	}
	s.logger.Info().Str("task_id", taskID).Msg("local task completed")
	return nil
}

type localCompletionOutcome struct {
	result             *agent.ExecutionResult
	taskStatus         string
	executionStatus    string
	billingReason      string
	errorMessage       string
	artifactValidation *agent.ArtifactValidation
}

func (s *TaskService) localCompletionOutcome(ctx context.Context, task *model.Task, executionID string, submitted *agent.ExecutionResult) (*localCompletionOutcome, error) {
	result, err := cloneTerminalExecutionResult(submitted)
	if err != nil {
		return nil, err
	}
	failure := func(reason, message string) *localCompletionOutcome {
		result.Success = false
		result.Error = message
		result.TerminalReason = reason
		return &localCompletionOutcome{
			result: result, taskStatus: model.TaskStatusFailed, executionStatus: model.TaskExecutionFailed,
			billingReason: reason, errorMessage: message,
		}
	}
	if !result.Success {
		message := strings.TrimSpace(result.Error)
		if message == "" {
			message = "local execution failed"
		}
		reason := result.TerminalReason
		if !approvedTaskBillingTerminalReason(reason) {
			reason = model.TaskBillingTerminalProviderError
		}
		return failure(reason, message), nil
	}
	if agent.IsNestedAgentDelegationOnly(result.ToolUseSummary) {
		return failure(model.TaskBillingTerminalPlatformError, agent.NestedAgentDelegationError), nil
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("local complete: find execution: %w", err)
	}
	if execution.TaskID != task.ID || execution.Target != model.ExecutionTargetLocalClaimed {
		return nil, ErrStaleTaskExecution
	}
	if !execution.ManifestSealed {
		return failure(model.TaskBillingTerminalPlatformError, "artifact manifest is not sealed"), nil
	}
	files, err := s.repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("local complete: list task files: %w", err)
	}
	validation := agent.ValidateTaskArtifactsFromTaskFiles(task, files)
	if model.IsMontagePlatform(task.Type) {
		validation = validateMontageCompletionArtifacts(files)
	}
	if !validation.Valid {
		outcome := failure(model.TaskBillingTerminalPlatformError, validation.Error())
		outcome.artifactValidation = &validation
		return outcome, nil
	}
	if deliveryErr := s.validateExecutionDelivery(ctx, task.ID, execution, files); deliveryErr != nil {
		if !errors.Is(deliveryErr, ErrTaskDeliveryObjectInvalid) && !errors.Is(deliveryErr, ErrTaskDeliveryContractUnavailable) {
			return nil, deliveryErr
		}
		validation = agent.ArtifactValidation{Reason: "delivery validation failed: " + deliveryErr.Error()}
		outcome := failure(model.TaskBillingTerminalPlatformError, validation.Error())
		outcome.artifactValidation = &validation
		return outcome, nil
	}
	return &localCompletionOutcome{
		result: result, taskStatus: model.TaskStatusCompleted, executionStatus: model.TaskExecutionSucceeded,
		billingReason: model.TaskBillingTerminalCompleted,
	}, nil
}

func (s *TaskService) confirmLocalCompletionRetry(ctx context.Context, task *model.Task, executionID string, outcome *localCompletionOutcome) error {
	_ = task
	unlock := s.lockLocalFinalization(executionID)
	defer unlock()
	task, execution, err := s.localCompletionSnapshot(ctx, executionID)
	if err != nil {
		return err
	}
	if err := validateLocalCompletionSnapshot(task, execution, outcome, false); err != nil {
		return err
	}
	if execution.FinalizationStatus != model.TaskExecutionFinalizationDone {
		if err := s.finalizeLocalTaskFromExecutionLocked(ctx, task, execution); err != nil {
			return err
		}
		task, execution, err = s.localCompletionSnapshot(ctx, executionID)
		if err != nil {
			return err
		}
	}
	return validateLocalCompletionSnapshot(task, execution, outcome, true)
}

func validateLocalCompletionSnapshot(task *model.Task, execution *model.TaskExecution, outcome *localCompletionOutcome, requireDone bool) error {
	if task == nil || execution == nil || task.ExecutionTarget != model.ExecutionTargetLocalClaimed ||
		task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID || execution.TaskID != task.ID ||
		execution.Target != model.ExecutionTargetLocalClaimed {
		return ErrStaleTaskExecution
	}
	if task.Status != model.TaskStatusCompleted && task.Status != model.TaskStatusFailed {
		return ErrTaskCompletionConflict
	}
	if execution.Status != outcome.executionStatus || task.Status != outcome.taskStatus ||
		execution.CompletedAt == nil || task.CompletedAt == nil ||
		execution.CleanupStatus != model.TaskExecutionCleanupDone ||
		execution.TerminalReason != outcome.errorMessage || task.ErrorMessage != outcome.errorMessage ||
		task.BillingTerminalReason != outcome.billingReason {
		return ErrTaskCompletionConflict
	}
	if requireDone && execution.FinalizationStatus != model.TaskExecutionFinalizationDone {
		return ErrTaskCompletionConflict
	}
	publicResult, err := marshalExecutionEvidence(outcome.result)
	if err != nil {
		return err
	}
	fullResult, err := json.Marshal(outcome.result)
	if err != nil {
		return err
	}
	if task.Result == nil {
		return ErrTaskCompletionConflict
	}
	for _, pair := range [][2][]byte{{execution.Result, fullResult}, {[]byte(*task.Result), []byte(publicResult)}} {
		equal, err := semanticJSONEqual(pair[0], pair[1])
		if err != nil {
			return ErrTaskCompletionConflict
		}
		if !equal {
			return ErrTaskCompletionConflict
		}
	}
	if !reflect.DeepEqual(task.TerminalModelUsage.Data(), outcome.result.ModelUsage) || task.CostStatus != outcome.result.CostStatus {
		return ErrTaskCompletionConflict
	}
	return nil
}

func (s *TaskService) localCompletionSnapshot(ctx context.Context, executionID string) (task *model.Task, execution *model.TaskExecution, err error) {
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var lockErr error
		execution, lockErr = tx.TaskExecutions().FindByIDForUpdate(ctx, executionID)
		if errors.Is(lockErr, gorm.ErrRecordNotFound) {
			return ErrStaleTaskExecution
		}
		if lockErr != nil {
			return fmt.Errorf("lock terminal local execution: %w", lockErr)
		}
		task, lockErr = tx.Tasks().FindByIDForUpdate(ctx, execution.TaskID)
		if errors.Is(lockErr, gorm.ErrRecordNotFound) {
			return ErrStaleTaskExecution
		}
		if lockErr != nil {
			return fmt.Errorf("lock terminal local task: %w", lockErr)
		}
		return nil
	})
	return task, execution, err
}

func (s *TaskService) confirmLocalCompletionAfterCASLoss(ctx context.Context, taskID, executionID string, outcome *localCompletionOutcome) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrStaleTaskExecution
	}
	if err != nil {
		return fmt.Errorf("reload local task after completion CAS loss: %w", err)
	}
	return s.confirmLocalCompletionRetry(ctx, task, executionID, outcome)
}

func (s *TaskService) cancelLocalExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution) error {
	if task == nil || execution == nil || task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID {
		return ErrStaleTaskExecution
	}
	cancelResult := &agent.ExecutionResult{
		Success:         false,
		Error:           "用户取消",
		TerminalReason:  model.TaskBillingTerminalUserCancelled,
		RemoteArtifacts: true,
	}
	resultJSON, executionResultJSON, err := marshalLocalCompletionEvidence(cancelResult)
	if err != nil {
		return err
	}
	var swapped bool
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var finalizeErr error
		swapped, finalizeErr = tx.Tasks().FinalizeLocalTaskWithArtifactsInTx(
			ctx, task.ID, execution.ID, model.TaskStatusCancelled, "用户取消",
			resultJSON, executionResultJSON, nil, "", repository.LocalTaskArtifactsCollect,
		)
		if finalizeErr != nil || !swapped {
			return finalizeErr
		}
		return tx.Tasks().UpdateBillingTerminalReason(ctx, task.ID, model.TaskBillingTerminalUserCancelled)
	})
	if err != nil {
		return fmt.Errorf("cancel local task: %w", err)
	}
	if !swapped {
		return fmt.Errorf("task is not in a cancellable state")
	}
	task.Status = model.TaskStatusCancelled
	s.notifyTerminal(ctx, task, model.TaskStatusCancelled, "用户取消")
	if s.pubsub != nil {
		s.pubsub.ReleaseSlot(ctx, task.ProjectID)
		s.pubsub.PublishCancel(ctx, task.ID)
	}
	if v, ok := s.cancelFuncs.Load(task.ID); ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
		}
	}
	return nil
}

func (s *TaskService) failLocalTask(ctx context.Context, task *model.Task, executionID string, outcome *localCompletionOutcome) error {
	result := outcome.result
	reason := outcome.billingReason
	errMsg := outcome.errorMessage
	result.Success = false
	result.Error = errMsg
	result.TerminalReason = reason
	resultJSON, executionResultJSON, err := marshalLocalCompletionEvidence(result)
	if err != nil {
		return err
	}
	if strings.TrimSpace(executionID) == "" {
		return fmt.Errorf("finalize local task as failed: durable execution identity is required")
	}
	durableDelivery, err := s.taskHasDurableDelivery(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("inspect durable local task delivery: %w", err)
	}
	execution := &model.TaskExecution{ID: executionID, Status: model.TaskExecutionFailed, TerminalReason: reason}
	var swapped bool
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var finalizeErr error
		swapped, finalizeErr = tx.Tasks().FinalizeLocalTaskWithArtifactsInTx(ctx, task.ID, execution.ID, model.TaskStatusFailed, errMsg, resultJSON, executionResultJSON, result.ModelUsage, result.CostStatus, repository.LocalTaskArtifactsCollect)
		if finalizeErr != nil || !swapped {
			return finalizeErr
		}
		return s.persistTerminalBillingInTx(ctx, tx, task, execution, reason, durableDelivery)
	})
	if err != nil && !errors.Is(err, repository.ErrLocalTaskExecutionCASLost) {
		return fmt.Errorf("finalize local task as failed: %w", err)
	}
	if err != nil || !swapped {
		return s.confirmLocalCompletionAfterCASLoss(ctx, task.ID, executionID, outcome)
	}
	task.Status = model.TaskStatusFailed
	task.ErrorMessage = errMsg
	execution.Target = model.ExecutionTargetLocalClaimed
	execution.Result = []byte(executionResultJSON)
	execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
	if err := s.finalizeLocalTaskFromExecution(ctx, task, execution); err != nil {
		return err
	}
	s.logger.Warn().Str("task_id", task.ID).Str("error", errMsg).Msg("local task failed")
	return nil
}

func marshalLocalCompletionEvidence(result *agent.ExecutionResult) (string, string, error) {
	publicResult, err := marshalExecutionEvidence(result)
	if err != nil {
		return "", "", err
	}
	fullResult, err := json.Marshal(result)
	if err != nil {
		return "", "", fmt.Errorf("marshal full execution result: %w", err)
	}
	return publicResult, string(fullResult), nil
}

func (s *TaskService) finalizeLocalTaskFromExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution) (err error) {
	if execution == nil {
		return ErrStaleTaskExecution
	}
	unlock := s.lockLocalFinalization(execution.ID)
	defer unlock()
	return s.finalizeLocalTaskFromExecutionLocked(ctx, task, execution)
}

func (s *TaskService) finalizeLocalTaskFromExecutionLocked(ctx context.Context, task *model.Task, execution *model.TaskExecution) (err error) {
	if task == nil || execution == nil {
		return ErrStaleTaskExecution
	}
	for {
		if execution.FinalizationStatus == model.TaskExecutionFinalizationDone {
			return s.ensureLocalFinalizationAuthority(ctx, task.ID, execution.ID)
		}
		token := uuid.NewString()
		won, claimErr := s.repo.TaskExecutions().ClaimFinalization(ctx, execution.ID, token, s.cloudFinalizationLease())
		if claimErr != nil {
			return fmt.Errorf("claim local execution finalization: %w", claimErr)
		}
		if won {
			return s.runClaimedLocalFinalization(ctx, task, execution, token)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
		var reloadErr error
		task, execution, reloadErr = s.localCompletionSnapshot(ctx, execution.ID)
		if reloadErr != nil {
			return reloadErr
		}
	}
}

func (s *TaskService) lockLocalFinalization(executionID string) func() {
	value, _ := s.localFinalizationLocks.LoadOrStore(executionID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *TaskService) runClaimedLocalFinalization(ctx context.Context, task *model.Task, execution *model.TaskExecution, token string) (err error) {
	leaseCtx, stopLease, leaseLost := s.renewFinalizationLease(ctx, execution.ID, token)
	defer func() {
		stopLease()
		if releaseErr := s.repo.TaskExecutions().ReleaseFinalization(context.WithoutCancel(ctx), execution.ID, token); err == nil && releaseErr != nil {
			err = releaseErr
		}
	}()
	var result *agent.ExecutionResult
	if err := json.Unmarshal(execution.Result, &result); err != nil {
		return fmt.Errorf("decode stored local execution result: %w", err)
	}
	stage := execution.FinalizationStatus
	if stage == "" {
		stage = model.TaskExecutionFinalizationTerminal
	}
	for stage != model.TaskExecutionFinalizationDone {
		if err := s.ensureLocalFinalizationAuthority(leaseCtx, task.ID, execution.ID); err != nil {
			return err
		}
		next, step, err := s.localFinalizationStep(task, execution, result, stage)
		if err != nil {
			return err
		}
		if err := step(leaseCtx); err != nil {
			return err
		}
		if leaseErr := finalizationLeaseError(leaseLost); leaseErr != nil {
			return leaseErr
		}
		if s.finalizationAfterStage != nil {
			if err := s.finalizationAfterStage(next); err != nil {
				return err
			}
		}
		if err := s.ensureLocalFinalizationAuthority(leaseCtx, task.ID, execution.ID); err != nil {
			return err
		}
		renewed, err := s.renewFinalizationClaim(leaseCtx, execution.ID, token)
		if err != nil {
			return fmt.Errorf("renew local finalization before advancing to %s: %w", next, err)
		}
		if !renewed {
			return ErrFinalizationLeaseLost
		}
		if err := s.advanceExecutionFinalization(leaseCtx, execution.ID, token, stage, next); err != nil {
			return err
		}
		stage = next
		if s.finalizationAfterAdvance != nil {
			if err := s.finalizationAfterAdvance(stage); err != nil {
				return err
			}
		}
	}
	return s.ensureLocalFinalizationAuthority(leaseCtx, task.ID, execution.ID)
}

func (s *TaskService) localFinalizationStep(task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult, stage string) (string, cloudFinalizationStep, error) {
	switch stage {
	case model.TaskExecutionFinalizationTerminal:
		return model.TaskExecutionFinalizationResult, func(ctx context.Context) error {
			return s.recordTerminalProviderCost(ctx, task, result)
		}, nil
	case model.TaskExecutionFinalizationResult:
		return model.TaskExecutionFinalizationSlot, func(ctx context.Context) error {
			return s.syncCloudSlot(ctx, task)
		}, nil
	case model.TaskExecutionFinalizationSlot:
		return model.TaskExecutionFinalizationDispatch, func(ctx context.Context) error {
			if task.ProjectID == "" {
				return nil
			}
			return s.DispatchPendingTasks(ctx, task.ProjectID)
		}, nil
	case model.TaskExecutionFinalizationDispatch:
		return model.TaskExecutionFinalizationNotification, func(ctx context.Context) error {
			return s.notifyTerminalDurable(ctx, task, task.Status, task.ErrorMessage)
		}, nil
	case model.TaskExecutionFinalizationNotification:
		return model.TaskExecutionFinalizationDone, func(context.Context) error { return nil }, nil
	default:
		return "", nil, fmt.Errorf("unknown local execution finalization stage %q", stage)
	}
}

func (s *TaskService) ensureLocalFinalizationAuthority(ctx context.Context, taskID, executionID string) error {
	task, execution, err := s.localCompletionSnapshot(ctx, executionID)
	if err != nil {
		return err
	}
	if task.ID != taskID || task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID ||
		task.ExecutionTarget != model.ExecutionTargetLocalClaimed || execution.Target != model.ExecutionTargetLocalClaimed ||
		(task.Status != model.TaskStatusCompleted && task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusCancelled) {
		return ErrStaleTaskExecution
	}
	return nil
}

// releaseSlotAndDispatch releases the project concurrency slot and dispatches
// the next pending task. Shared tail of the local success/failure paths;
// mirrors HandleExecution (task_execution.go:345-357).
func (s *TaskService) releaseSlotAndDispatch(ctx context.Context, task *model.Task) {
	if task.ProjectID == "" {
		return
	}
	if s.pubsub != nil {
		s.pubsub.ReleaseSlot(ctx, task.ProjectID)
	}
	if err := s.DispatchPendingTasks(ctx, task.ProjectID); err != nil {
		s.logger.Warn().Err(err).Str("project_id", task.ProjectID).Msg("local complete: dispatch pending tasks")
	}
}
