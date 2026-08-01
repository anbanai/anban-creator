package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
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
	TaskType                 string `json:"task_type"`
	AgentPackID              string `json:"agent_pack_id"`
	AgentPackVersion         string `json:"agent_pack_version"`
	AgentPackDigest          string `json:"agent_pack_digest"`
	RuntimeAdapter           string `json:"runtime_adapter"`
	Topic                    string `json:"topic"`
	AgentFlag                string `json:"agent_flag"` // "anban:<agent>"
	MaxTurns                 int    `json:"max_turns"`
	Model                    string `json:"model,omitempty"`
	Goal                     string `json:"goal,omitempty"`
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
		execution.Started, execution.RuntimeProfile = true, "local"
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
		Goal:                     task.Goal,
		HasContentImage:          task.HasContentImage,
		HasTailImage:             task.HasTailImage,
		ArticleWithCover:         task.ArticleWithCover == nil || *task.ArticleWithCover,
		ArticleWithContentImages: task.ArticleWithContentImages == nil || *task.ArticleWithContentImages,
		ProjectID:                task.ProjectID,
	}
	if execution != nil {
		config.AgentPackID = execution.AgentPackID
		config.AgentPackVersion = execution.AgentPackVersion
		config.AgentPackDigest = execution.AgentPackDigest
		config.RuntimeAdapter = execution.RuntimeAdapter
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
// Guarded + idempotent: finalizes ONLY when the task is still
// execution_target=local_claimed AND status=running (CAS). A repeat /complete
// call, a task already reaped/terminal, or a cloud task that happens to call
// /complete is a no-op — so the shared anban binary (used by both cloud
// Docker and the desktop) can call this endpoint in both modes without
// double-finalizing cloud tasks, whose authoritative finalization remains the
// server-side HandleExecution.
func (s *TaskService) CompleteLocalTask(ctx context.Context, taskID string, result *agent.ExecutionResult) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	if task == nil {
		return nil
	}
	// Only finalize live local tasks. Anything else (cloud task, already
	// terminal, cancelled) is a no-op — keeps the endpoint idempotent and safe
	// for the shared agent binary.
	if task.ExecutionTarget != model.ExecutionTargetLocalClaimed || task.Status != model.TaskStatusRunning {
		return nil
	}

	result = normalizeTerminalExecutionResult(result)
	// Failure → terminal-fail (no cloud retry). Local execution is an explicit
	// user choice; silently re-running a failed local task on cloud would
	// surprise the user and could double-bill. Mirrors the terminal branch of
	// HandleExecutionFailure minus the retry-enqueue logic.
	if !result.Success {
		errMsg := "local execution failed"
		if result.Error != "" {
			errMsg = result.Error
		}
		reason := result.TerminalReason
		if !approvedTaskBillingTerminalReason(reason) {
			reason = model.TaskBillingTerminalProviderError
		}
		return s.failLocalTask(ctx, task, result, reason, errMsg)
	}

	if agent.IsNestedAgentDelegationOnly(result.ToolUseSummary) {
		return s.failLocalTask(ctx, task, result, model.TaskBillingTerminalPlatformError, agent.NestedAgentDelegationError)
	}

	var artifactValidation agent.ArtifactValidation
	if model.IsMontagePlatform(task.Type) {
		files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
		if err != nil {
			return fmt.Errorf("local complete: list montage task files: %w", err)
		}
		artifactValidation = validateMontageCompletionArtifacts(files)
	} else {
		files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
		if err != nil {
			return fmt.Errorf("local complete: list task files: %w", err)
		}
		artifactValidation = agent.ValidateTaskArtifactsFromTaskFiles(task, files)
	}
	if !artifactValidation.Valid {
		errMsg := artifactValidation.Error()
		s.logger.Warn().
			Str("task_id", taskID).
			Int("meaningful_files", artifactValidation.MeaningfulFileCount).
			Strs("missing_files", artifactValidation.Missing).
			Msg(errMsg)
		return s.failLocalTask(ctx, task, result, model.TaskBillingTerminalPlatformError, errMsg)
	}

	// Success. Publishing model for local: the desktop agent publishes via the
	// server MCP (publish_draft) itself, so check whether it already did. The
	// server-side auto-publish / hold-for-approval fallback is cloud-only (needs
	// the host WorkDir to extract the draft), so it cannot run here — surface the
	// case loudly instead of silently skipping so the operator can act.
	published := result.LogText != "" && wasPublishedByAgent(result.LogText)
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
	if task.CurrentExecutionID == nil {
		return fmt.Errorf("finalize local task as completed: durable execution identity is required")
	}
	execution := &model.TaskExecution{ID: *task.CurrentExecutionID, Status: model.TaskExecutionSucceeded}
	var swapped bool
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var finalizeErr error
		swapped, finalizeErr = tx.Tasks().FinalizeLocalTaskInTx(ctx, taskID, execution.ID, model.TaskStatusCompleted, "", resultJSON, result.ModelUsage, result.CostStatus)
		if finalizeErr != nil || !swapped {
			return finalizeErr
		}
		return s.persistTerminalBillingInTx(ctx, tx, task, execution, model.TaskBillingTerminalCompleted, true)
	})
	if err != nil {
		return fmt.Errorf("finalize local task as completed: %w", err)
	}
	if !swapped {
		return nil // already terminal (e.g. reaped as failed meanwhile)
	}
	s.recordTerminalProviderCost(ctx, task, result)
	// Task-admission charges remain posted on success. Provider usage is recorded
	// separately as internal cost evidence and never becomes a retail deduction.
	s.releaseSlotAndDispatch(ctx, task)
	s.logger.Info().Str("task_id", taskID).Bool("published", published).Msg("local task completed")
	return nil
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

func (s *TaskService) failLocalTask(ctx context.Context, task *model.Task, result *agent.ExecutionResult, reason, errMsg string) error {
	result.Success = false
	result.Error = errMsg
	result.TerminalReason = reason
	resultJSON, err := marshalExecutionEvidence(result)
	if err != nil {
		return err
	}
	if task.CurrentExecutionID == nil {
		return fmt.Errorf("finalize local task as failed: durable execution identity is required")
	}
	durableDelivery, err := s.taskHasDurableDelivery(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("inspect durable local task delivery: %w", err)
	}
	execution := &model.TaskExecution{ID: *task.CurrentExecutionID, Status: model.TaskExecutionFailed, TerminalReason: reason}
	var swapped bool
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var finalizeErr error
		swapped, finalizeErr = tx.Tasks().FinalizeLocalTaskInTx(ctx, task.ID, execution.ID, model.TaskStatusFailed, errMsg, resultJSON, result.ModelUsage, result.CostStatus)
		if finalizeErr != nil || !swapped {
			return finalizeErr
		}
		return s.persistTerminalBillingInTx(ctx, tx, task, execution, reason, durableDelivery)
	})
	if err != nil {
		return fmt.Errorf("finalize local task as failed: %w", err)
	}
	if !swapped {
		return nil // already terminal (e.g. reaped meanwhile)
	}
	s.recordTerminalProviderCost(ctx, task, result)
	s.releaseSlotAndDispatch(ctx, task)
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
