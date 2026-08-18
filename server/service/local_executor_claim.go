package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
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
// claiming desktop then spawns anban, which reports progress and completion
// back through the existing /api/v1/agent/progress + /agent/upload endpoints.
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
// desktop agent uploads its output files via /agent/upload and publishes via the
// MCP publish tool itself. Therefore this path does NOT perform server-side
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
	outcome, err := s.localCompletionOutcome(ctx, task, result)
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

	// Success. Publishing model for local: the desktop agent publishes via the
	// server MCP (publish_draft) itself, so check whether it already did. The
	// server-side auto-publish / hold-for-approval fallback is cloud-only (needs
	// the host WorkDir to extract the draft), so it cannot run here — surface the
	// case loudly instead of silently skipping so the operator can act.
	result = outcome.result
	published := outcome.published
	if !published && task.ProjectID != "" {
		if proj, perr := s.repo.Projects().FindByID(ctx, task.ProjectID); perr == nil && proj != nil && proj.GetEnablePublishing() {
			if proj.GetRequirePublishApproval() {
				s.logger.Warn().
					Str("task_id", taskID).Str("project_id", task.ProjectID).
					Msg("local task completed on approval-required project without agent publish; server cannot extract draft (no host WorkDir) — operator must review uploaded files")
			} else {
				s.logger.Warn().
					Str("task_id", taskID).Str("project_id", task.ProjectID).
					Msg("local task completed without agent publish; server-side auto-publish unavailable for local execution (no host WorkDir) — agent should publish via MCP")
			}
		}
	}

	resultJSON, err := marshalExecutionEvidence(result)
	if err != nil {
		return err
	}
	execution := &model.TaskExecution{ID: executionID, Status: model.TaskExecutionSucceeded}
	var swapped bool
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var finalizeErr error
		swapped, finalizeErr = tx.Tasks().FinalizeLocalTaskInTx(ctx, taskID, execution.ID, model.TaskStatusCompleted, "", resultJSON, result.ModelUsage, result.CostStatus)
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
	s.recordTerminalProviderCost(ctx, task, result)
	// Task-admission charges remain posted on success. Provider usage is recorded
	// separately as internal cost evidence and never becomes a retail deduction.
	s.releaseSlotAndDispatch(ctx, task)
	task.Status = model.TaskStatusCompleted
	s.notifyTerminal(ctx, task, model.TaskStatusCompleted, "")
	s.logger.Info().Str("task_id", taskID).Bool("published", published).Msg("local task completed")
	return nil
}

type localCompletionOutcome struct {
	result             *agent.ExecutionResult
	taskStatus         string
	executionStatus    string
	billingReason      string
	errorMessage       string
	published          bool
	artifactValidation *agent.ArtifactValidation
}

func (s *TaskService) localCompletionOutcome(ctx context.Context, task *model.Task, submitted *agent.ExecutionResult) (*localCompletionOutcome, error) {
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
	files, err := s.repo.TaskFiles().FindByTaskID(ctx, task.ID)
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
	return &localCompletionOutcome{
		result: result, taskStatus: model.TaskStatusCompleted, executionStatus: model.TaskExecutionSucceeded,
		billingReason: model.TaskBillingTerminalCompleted,
		published:     result.LogText != "" && wasPublishedByAgent(result.LogText),
	}, nil
}

func (s *TaskService) confirmLocalCompletionRetry(ctx context.Context, task *model.Task, executionID string, outcome *localCompletionOutcome) error {
	if task == nil || task.ExecutionTarget != model.ExecutionTargetLocalClaimed ||
		task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
		return ErrStaleTaskExecution
	}
	if task.Status != model.TaskStatusCompleted && task.Status != model.TaskStatusFailed {
		return ErrTaskCompletionConflict
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrStaleTaskExecution
	}
	if err != nil {
		return fmt.Errorf("find terminal local execution: %w", err)
	}
	if execution.TaskID != task.ID || execution.Target != model.ExecutionTargetLocalClaimed {
		return ErrStaleTaskExecution
	}
	if execution.Status != outcome.executionStatus || task.Status != outcome.taskStatus ||
		execution.CompletedAt == nil || task.CompletedAt == nil ||
		execution.FinalizationStatus != model.TaskExecutionFinalizationDone ||
		execution.CleanupStatus != model.TaskExecutionCleanupDone ||
		execution.TerminalReason != outcome.errorMessage || task.ErrorMessage != outcome.errorMessage ||
		task.BillingTerminalReason != outcome.billingReason {
		return ErrTaskCompletionConflict
	}
	publicResult, err := marshalExecutionEvidence(outcome.result)
	if err != nil {
		return err
	}
	if task.Result == nil {
		return ErrTaskCompletionConflict
	}
	for _, stored := range [][]byte{execution.Result, []byte(*task.Result)} {
		equal, err := semanticJSONEqual(stored, []byte(publicResult))
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

func (s *TaskService) cancelLocalExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution, userID string) error {
	if task == nil || execution == nil || task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID {
		return ErrStaleTaskExecution
	}
	resultJSON, err := marshalExecutionEvidence(&agent.ExecutionResult{
		Success:         false,
		Error:           "用户取消",
		TerminalReason:  model.TaskBillingTerminalUserCancelled,
		RemoteArtifacts: true,
	})
	if err != nil {
		return err
	}
	var swapped bool
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		locked, err := tx.Tasks().FindByID(ctx, task.ID)
		if err != nil {
			return err
		}
		if userID != "" && locked.UserID != userID {
			return fmt.Errorf("task not found")
		}
		if locked.CurrentExecutionID == nil || *locked.CurrentExecutionID != execution.ID {
			return ErrStaleTaskExecution
		}
		current, err := tx.TaskExecutions().FindByID(ctx, execution.ID)
		if err != nil {
			return err
		}
		if current.Target != model.ExecutionTargetLocalClaimed {
			return fmt.Errorf("local cancellation requires local_claimed execution")
		}
		swapped, err = tx.Tasks().FinalizeLocalTaskInTx(ctx, task.ID, execution.ID, model.TaskStatusCancelled, "用户取消", resultJSON, nil, "")
		if err != nil || !swapped {
			return err
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
	resultJSON, err := marshalExecutionEvidence(result)
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
		swapped, finalizeErr = tx.Tasks().FinalizeLocalTaskInTx(ctx, task.ID, execution.ID, model.TaskStatusFailed, errMsg, resultJSON, result.ModelUsage, result.CostStatus)
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
	s.recordTerminalProviderCost(ctx, task, result)
	s.releaseSlotAndDispatch(ctx, task)
	task.Status = model.TaskStatusFailed
	task.ErrorMessage = errMsg
	s.notifyTerminal(ctx, task, model.TaskStatusFailed, errMsg)
	s.logger.Warn().Str("task_id", task.ID).Str("error", errMsg).Msg("local task failed")
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
