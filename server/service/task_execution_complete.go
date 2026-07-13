package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var ErrStaleTaskExecution = errors.New("task execution is no longer current")
var ErrFinalizationLeaseLost = errors.New("task execution finalization lease lost")
var ErrCloudPublishingAmbiguous = errors.New("cloud draft publication outcome is ambiguous and requires reconciliation")

// CompleteCloudExecution records one attempt's immutable terminal outcome, then
// resumes the durable business finalizer. Only the task's current attempt may
// cross the terminal CAS; repeats of that attempt resume incomplete stages.
func (s *TaskService) CompleteCloudExecution(ctx context.Context, executionID string, result *agent.ExecutionResult) error {
	execution, task, err := s.currentExecution(ctx, executionID)
	if err != nil {
		return err
	}

	if !isTerminalExecution(execution.Status) {
		terminal, reason, normalized, err := s.cloudTerminalOutcome(ctx, task, execution, result)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return fmt.Errorf("marshal cloud execution result: %w", err)
		}
		won, err := s.repo.TaskExecutions().Transition(ctx, execution.ID,
			[]string{model.TaskExecutionStarting, model.TaskExecutionRunning}, terminal,
			model.ExecutionTransition{
				TerminalReason:     reason,
				Result:             encoded,
				FinalizationStatus: model.TaskExecutionFinalizationTerminal,
			})
		if err != nil {
			return fmt.Errorf("terminalize cloud execution: %w", err)
		}
		if !won {
			execution, task, err = s.currentExecution(ctx, executionID)
			if err != nil {
				return err
			}
			if !isTerminalExecution(execution.Status) {
				return fmt.Errorf("cloud execution terminal transition lost from status %s", execution.Status)
			}
		} else {
			execution.Status = terminal
			execution.TerminalReason = reason
			execution.Result = encoded
			execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
		}
	}
	return s.finalizeTaskFromExecution(ctx, task, execution)
}

func (s *TaskService) currentExecution(ctx context.Context, executionID string) (*model.TaskExecution, *model.Task, error) {
	execution, err := s.repo.TaskExecutions().FindByID(ctx, strings.TrimSpace(executionID))
	if err != nil {
		return nil, nil, fmt.Errorf("find task execution: %w", err)
	}
	task, err := s.repo.Tasks().FindByID(ctx, execution.TaskID)
	if err != nil {
		return nil, nil, fmt.Errorf("find execution task: %w", err)
	}
	if task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID {
		return nil, nil, ErrStaleTaskExecution
	}
	return execution, task, nil
}

func (s *TaskService) cloudTerminalOutcome(ctx context.Context, task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult) (string, string, *agent.ExecutionResult, error) {
	failureReason := "execution_failed"
	if result == nil {
		result = &agent.ExecutionResult{Success: false, Error: "agent returned no execution result", RemoteArtifacts: true}
	}
	if result.Success && agent.IsNestedAgentDelegationOnly(result.ToolUseSummary) {
		result.Success, result.Error = false, agent.NestedAgentDelegationError
		failureReason = "nested_agent_delegation"
	}
	if result.Success {
		files, err := s.repo.TaskFiles().FindByExecutionID(ctx, execution.ID)
		if err != nil {
			return "", "", nil, fmt.Errorf("list pending execution artifacts: %w", err)
		}
		var validation agent.ArtifactValidation
		switch {
		case model.IsMontagePlatform(task.Type):
			validation = validateMontageCompletionArtifacts(files)
		case model.IsVideoEditorPlatform(task.Type):
			validation = validateVideoEditorCompletionArtifacts(files)
		case model.IsVideoCreatorPlatform(task.Type):
			validation, err = s.validateVideoCreatorCompletionArtifacts(ctx, task, files)
		default:
			validation = agent.ValidateTaskArtifactsFromTaskFiles(task, files)
		}
		if err != nil {
			return "", "", nil, err
		}
		if !validation.Valid {
			result.Success, result.Error = false, validation.Error()
			failureReason = "deliverable_validation_failed"
		}
	}
	if result.Success {
		return model.TaskExecutionSucceeded, "completed", result, nil
	}
	if strings.TrimSpace(result.Error) == "" {
		result.Error = "execution returned unsuccessful result"
	}
	return model.TaskExecutionFailed, failureReason, result, nil
}

func (s *TaskService) finalizeTaskFromExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution) (err error) {
	if execution.FinalizationStatus == model.TaskExecutionFinalizationDone {
		return nil
	}
	token := uuid.NewString()
	won, err := s.repo.TaskExecutions().ClaimFinalization(ctx, execution.ID, token, s.cloudFinalizationLease())
	if err != nil {
		return fmt.Errorf("claim execution finalization: %w", err)
	}
	if !won {
		return nil
	}
	leaseCtx, stopLease, leaseLost := s.renewFinalizationLease(ctx, execution.ID, token)
	defer func() {
		stopLease()
		if releaseErr := s.repo.TaskExecutions().ReleaseFinalization(context.WithoutCancel(ctx), execution.ID, token); err == nil && releaseErr != nil {
			err = releaseErr
		}
	}()

	var result *agent.ExecutionResult
	if len(execution.Result) > 0 {
		if err := json.Unmarshal(execution.Result, &result); err != nil {
			return fmt.Errorf("decode stored execution result: %w", err)
		}
	}
	stage := execution.FinalizationStatus
	if stage == "" {
		stage = model.TaskExecutionFinalizationTerminal
	}
	if stage == model.TaskExecutionFinalizationTerminal && s.finalizationAfterStage != nil {
		if err := s.finalizationAfterStage(stage); err != nil {
			return err
		}
	}
	for stage != model.TaskExecutionFinalizationDone {
		next, step, err := s.cloudFinalizationStep(task, execution, result, stage)
		if err != nil {
			return err
		}
		stepErr := step(leaseCtx)
		if leaseErr := finalizationLeaseError(leaseLost); leaseErr != nil {
			return leaseErr
		}
		if stepErr != nil {
			return stepErr
		}
		if s.finalizationAfterStage != nil {
			if err := s.finalizationAfterStage(next); err != nil {
				return err
			}
		}
		if err := finalizationLeaseError(leaseLost); err != nil {
			return err
		}
		renewed, err := s.renewFinalizationClaim(leaseCtx, execution.ID, token)
		if err != nil {
			if leaseErr := finalizationLeaseError(leaseLost); leaseErr != nil {
				return leaseErr
			}
			return fmt.Errorf("renew finalization before advancing to %s: %w", next, err)
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
	return nil
}

type cloudFinalizationStep func(context.Context) error

func (s *TaskService) cloudFinalizationStep(task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult, stage string) (string, cloudFinalizationStep, error) {
	switch stage {
	case model.TaskExecutionFinalizationTerminal:
		return model.TaskExecutionFinalizationArtifacts, func(ctx context.Context) error {
			if execution.Status == model.TaskExecutionSucceeded {
				return s.repo.TaskFiles().PublishCurrentExecution(ctx, task.ID, execution.ID)
			}
			return s.repo.TaskFiles().DiscardCurrentExecution(ctx, task.ID, execution.ID)
		}, nil
	case model.TaskExecutionFinalizationArtifacts:
		return model.TaskExecutionFinalizationResult, func(ctx context.Context) error {
			return s.UpdateExecutionResult(ctx, task.ID, result)
		}, nil
	case model.TaskExecutionFinalizationResult:
		return model.TaskExecutionFinalizationWorkflow, func(ctx context.Context) error {
			return s.RebuildWorkflowStatus(ctx, task.ID)
		}, nil
	case model.TaskExecutionFinalizationWorkflow:
		return model.TaskExecutionFinalizationPublishing, func(ctx context.Context) error {
			if execution.Status != model.TaskExecutionSucceeded {
				return s.markCloudPublishingSkipped(ctx, execution)
			}
			return s.finalizeCloudPublishing(ctx, task, execution, result)
		}, nil
	case model.TaskExecutionFinalizationPublishing:
		return model.TaskExecutionFinalizationTask, func(ctx context.Context) error {
			return s.finalizeExecutionTaskStatus(ctx, task, execution, result)
		}, nil
	case model.TaskExecutionFinalizationTask:
		return model.TaskExecutionFinalizationSettlement, func(ctx context.Context) error {
			return s.settleCloudExecution(ctx, task, execution, result)
		}, nil
	case model.TaskExecutionFinalizationSettlement:
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
			status, errMsg := taskTerminalFromExecution(execution, result)
			return s.notifyTerminalDurable(ctx, task, status, errMsg)
		}, nil
	case model.TaskExecutionFinalizationNotification:
		return model.TaskExecutionFinalizationDone, func(context.Context) error { return nil }, nil
	default:
		return "", nil, fmt.Errorf("unknown execution finalization stage %q", stage)
	}
}

func (s *TaskService) syncCloudSlot(ctx context.Context, task *model.Task) error {
	if task == nil || task.ProjectID == "" {
		return nil
	}
	if s.pubsub != nil {
		if running, err := s.repo.Tasks().CountRunningByProject(ctx, task.ProjectID); err != nil {
			return fmt.Errorf("count running tasks for cloud slot reconciliation: %w", err)
		} else if err := s.pubsub.SyncProjectCount(ctx, task.ProjectID, running); err != nil {
			return fmt.Errorf("reconcile cloud concurrency slot: %w", err)
		}
	}
	return nil
}

func (s *TaskService) settleCloudExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult) error {
	if s.creditSvc == nil {
		return nil
	}
	if err := s.creditSvc.SettleAgentRuntime(ctx, task, result); err != nil {
		return fmt.Errorf("settle agent runtime: %w", err)
	}
	if execution.Status != model.TaskExecutionSucceeded && !task.GoalMode {
		if err := s.creditSvc.RefundForTask(ctx, task.ID, execution.TerminalReason); err != nil {
			return fmt.Errorf("refund terminal cloud task: %w", err)
		}
	}
	return nil
}

func (s *TaskService) markCloudPublishingSkipped(ctx context.Context, execution *model.TaskExecution) error {
	latest, err := s.repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		return err
	}
	switch latest.PublishingStatus {
	case model.TaskExecutionPublishingSkipped, model.TaskExecutionPublishingSucceeded:
		return nil
	case model.TaskExecutionPublishingInFlight, model.TaskExecutionPublishingAmbiguous:
		return ErrCloudPublishingAmbiguous
	}
	won, err := s.repo.TaskExecutions().TransitionPublishing(ctx, execution.ID, "", model.TaskExecutionPublishingSkipped, []byte(`{"reason":"not_applicable"}`))
	if err != nil {
		return err
	}
	if !won {
		return fmt.Errorf("mark cloud publishing skipped: state changed concurrently")
	}
	execution.PublishingStatus = model.TaskExecutionPublishingSkipped
	return nil
}

func (s *TaskService) finalizeCloudPublishing(ctx context.Context, task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult) error {
	latestExecution, err := s.repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		return err
	}
	latestTask, err := s.repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		return err
	}
	switch latestExecution.PublishingStatus {
	case model.TaskExecutionPublishingSucceeded:
		if !latestTask.Published {
			return s.setPublishedAndMaybeTrack(ctx, latestTask.UserID, latestTask, true)
		}
		return nil
	case model.TaskExecutionPublishingSkipped:
		return nil
	case model.TaskExecutionPublishingInFlight, model.TaskExecutionPublishingAmbiguous:
		return ErrCloudPublishingAmbiguous
	}
	if s.cloudPublisher == nil || latestTask.ProjectID == "" || result == nil {
		return s.markCloudPublishingSkipped(ctx, latestExecution)
	}
	project, err := s.repo.Projects().FindByID(ctx, latestTask.ProjectID)
	if err != nil {
		return fmt.Errorf("load project for cloud publishing: %w", err)
	}
	if !project.GetEnablePublishing() {
		return s.markCloudPublishingSkipped(ctx, latestExecution)
	}
	if wasPublishedByAgent(result.LogText) || latestTask.Published {
		if err := s.setPublishedAndMaybeTrack(ctx, latestTask.UserID, latestTask, true); err != nil {
			return err
		}
		won, err := s.repo.TaskExecutions().TransitionPublishing(ctx, execution.ID, "", model.TaskExecutionPublishingSucceeded, []byte(`{"source":"agent"}`))
		if err != nil {
			return err
		}
		if !won {
			return fmt.Errorf("record agent publication: state changed concurrently")
		}
		return nil
	}
	if latestTask.Type != model.ScopeArticle {
		return s.markCloudPublishingSkipped(ctx, latestExecution)
	}
	articles, err := s.extractArticleDraftFromTaskFiles(ctx, latestTask.ID)
	if err != nil {
		return fmt.Errorf("extract cloud article draft: %w", err)
	}
	if len(articles) == 0 {
		return s.markCloudPublishingSkipped(ctx, latestExecution)
	}
	if project.GetRequirePublishApproval() {
		encoded, err := json.Marshal(articles)
		if err != nil {
			return err
		}
		if err := s.repo.Tasks().UpdatePublishApproval(ctx, task.ID, model.PublishApprovalStatePending, encoded); err != nil {
			return fmt.Errorf("persist cloud publish approval: %w", err)
		}
		won, err := s.repo.TaskExecutions().TransitionPublishing(ctx, execution.ID, "", model.TaskExecutionPublishingSkipped, []byte(`{"reason":"approval_pending"}`))
		if err != nil {
			return err
		}
		if !won {
			return fmt.Errorf("record publish approval hold: state changed concurrently")
		}
		return nil
	}
	won, err := s.repo.TaskExecutions().TransitionPublishing(ctx, execution.ID, "", model.TaskExecutionPublishingInFlight, nil)
	if err != nil {
		return err
	}
	if !won {
		return fmt.Errorf("claim cloud publication: state changed concurrently")
	}
	publishCtx, cancel := context.WithTimeout(ctx, time.Minute)
	published, publishErr := s.cloudPublisher.PublishDraft(publishCtx, latestTask.UserID, project.ID, articles)
	cancel()
	if publishErr != nil {
		detail, _ := json.Marshal(map[string]string{"error": publishErr.Error()})
		_, _ = s.repo.TaskExecutions().TransitionPublishing(context.WithoutCancel(ctx), execution.ID, model.TaskExecutionPublishingInFlight, model.TaskExecutionPublishingAmbiguous, detail)
		return fmt.Errorf("publish cloud draft (outcome may be ambiguous): %w", publishErr)
	}
	if s.finalizationAfterEffect != nil {
		if err := s.finalizationAfterEffect(model.TaskExecutionFinalizationPublishing); err != nil {
			return err
		}
	}
	publishResult, err := json.Marshal(published)
	if err != nil {
		return err
	}
	won, err = s.repo.TaskExecutions().TransitionPublishing(ctx, execution.ID, model.TaskExecutionPublishingInFlight, model.TaskExecutionPublishingSucceeded, publishResult)
	if err != nil {
		return err
	}
	if !won {
		return ErrCloudPublishingAmbiguous
	}
	return s.setPublishedAndMaybeTrack(ctx, latestTask.UserID, latestTask, true)
}

// ResolveCloudPublishing closes the only ambiguity the WeChat draft API cannot
// resolve itself: the provider accepted a draft but the server died before it
// persisted the response. An operator/reconciliation adapter must confirm the
// downstream outcome; true continues without another provider call, while
// false resets the durable operation and retries it under the finalization lease.
func (s *TaskService) ResolveCloudPublishing(ctx context.Context, executionID string, published bool) error {
	execution, task, err := s.currentExecution(ctx, executionID)
	if err != nil {
		return err
	}
	if execution.PublishingStatus != model.TaskExecutionPublishingInFlight && execution.PublishingStatus != model.TaskExecutionPublishingAmbiguous {
		return fmt.Errorf("cloud publishing is not awaiting resolution: %s", execution.PublishingStatus)
	}
	target := ""
	var detail []byte
	if published {
		target = model.TaskExecutionPublishingSucceeded
		detail = []byte(`{"source":"reconciled"}`)
	}
	won, err := s.repo.TaskExecutions().TransitionPublishing(ctx, execution.ID, execution.PublishingStatus, target, detail)
	if err != nil {
		return err
	}
	if !won {
		return fmt.Errorf("resolve cloud publishing: state changed concurrently")
	}
	execution.PublishingStatus = target
	if published {
		if err := s.setPublishedAndMaybeTrack(ctx, task.UserID, task, true); err != nil {
			return err
		}
	}
	return s.finalizeTaskFromExecution(ctx, task, execution)
}

func (s *TaskService) cloudFinalizationLease() time.Duration {
	if s.finalizationLease > 0 {
		return s.finalizationLease
	}
	return time.Minute
}

func (s *TaskService) cloudFinalizationRenewInterval() time.Duration {
	lease := s.cloudFinalizationLease()
	interval := s.finalizationRenewEvery
	if interval <= 0 || interval >= lease/3 {
		interval = lease / 4
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	return interval
}

func (s *TaskService) renewFinalizationLease(parent context.Context, executionID, token string) (context.Context, func(), <-chan error) {
	ctx, cancel := context.WithCancel(parent)
	stop := make(chan struct{})
	stopped := make(chan struct{})
	lost := make(chan error, 1)
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(s.cloudFinalizationRenewInterval())
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				won, err := s.renewFinalizationClaim(ctx, executionID, token)
				if err == nil && won {
					continue
				}
				if err == nil {
					err = ErrFinalizationLeaseLost
				}
				select {
				case lost <- err:
				default:
				}
				cancel()
				return
			}
		}
	}()
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			close(stop)
			<-stopped
			cancel()
		})
	}, lost
}

func (s *TaskService) renewFinalizationClaim(ctx context.Context, executionID, token string) (bool, error) {
	if s.finalizationRenewClaim != nil {
		return s.finalizationRenewClaim(ctx, executionID, token)
	}
	return s.repo.TaskExecutions().RenewFinalizationClaim(ctx, executionID, token)
}

func finalizationLeaseError(lost <-chan error) error {
	select {
	case err := <-lost:
		return fmt.Errorf("%w: %v", ErrFinalizationLeaseLost, err)
	default:
		return nil
	}
}

func (s *TaskService) advanceExecutionFinalization(ctx context.Context, id, token, from, to string) error {
	won, err := s.repo.TaskExecutions().AdvanceFinalization(ctx, id, token, from, to)
	if err != nil {
		return fmt.Errorf("advance execution finalization to %s: %w", to, err)
	}
	if !won {
		return fmt.Errorf("execution finalization lease lost while advancing to %s", to)
	}
	return nil
}

func (s *TaskService) finalizeExecutionTaskStatus(ctx context.Context, task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult) error {
	target, errMsg := taskTerminalFromExecution(execution, result)
	var won bool
	var err error
	if target == model.TaskStatusCompleted {
		won, err = s.repo.Tasks().CompareAndSwapStatus(ctx, task.ID, model.TaskStatusRunning, target)
	} else {
		won, err = s.repo.Tasks().CompareAndSwapStatusAndError(ctx, task.ID, model.TaskStatusRunning, target, errMsg)
	}
	if err != nil {
		return fmt.Errorf("finalize task status: %w", err)
	}
	if !won {
		latest, findErr := s.repo.Tasks().FindByID(ctx, task.ID)
		if findErr != nil || latest.Status != target {
			return ErrStaleTaskExecution
		}
	}
	if err := s.repo.Tasks().SetCompletedAt(ctx, task.ID); err != nil {
		return fmt.Errorf("set task completed_at: %w", err)
	}
	if target != model.TaskStatusCompleted {
		task.Status = target
	}
	return nil
}

func taskTerminalFromExecution(execution *model.TaskExecution, result *agent.ExecutionResult) (string, string) {
	errMsg := execution.TerminalReason
	if result != nil && strings.TrimSpace(result.Error) != "" {
		errMsg = result.Error
	}
	switch execution.Status {
	case model.TaskExecutionSucceeded:
		return model.TaskStatusCompleted, ""
	case model.TaskExecutionCancelled:
		return model.TaskStatusCancelled, errMsg
	default:
		return model.TaskStatusFailed, errMsg
	}
}

func isTerminalExecution(status string) bool {
	switch status {
	case model.TaskExecutionSucceeded, model.TaskExecutionFailed, model.TaskExecutionCancelled, model.TaskExecutionTimedOut:
		return true
	default:
		return false
	}
}

// terminalizeCurrentExecution is the narrow service callback used by the
// Kubernetes reconciler. Infrastructure failures have no successful result.
func (s *TaskService) TerminalizeCurrentExecution(ctx context.Context, executionID, status, reason string, diagnostics []byte) error {
	execution, task, err := s.currentExecution(ctx, executionID)
	if err != nil {
		return err
	}
	if !isTerminalExecution(execution.Status) {
		result := &agent.ExecutionResult{Success: false, Error: reason, RemoteArtifacts: true}
		encoded, _ := json.Marshal(result)
		won, transitionErr := s.repo.TaskExecutions().Transition(ctx, execution.ID,
			[]string{model.TaskExecutionCreated, model.TaskExecutionDispatching, model.TaskExecutionStarting, model.TaskExecutionRunning}, status,
			model.ExecutionTransition{TerminalReason: reason, Diagnostics: diagnostics, Result: encoded, FinalizationStatus: model.TaskExecutionFinalizationTerminal})
		if transitionErr != nil {
			return transitionErr
		}
		if !won {
			return nil
		}
		execution.Status, execution.TerminalReason, execution.Result = status, reason, encoded
		execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
	}
	return s.finalizeTaskFromExecution(ctx, task, execution)
}

func (s *TaskService) cancelCloudExecution(ctx context.Context, task *model.Task, userID string) error {
	if task == nil || task.CurrentExecutionID == nil {
		return fmt.Errorf("task has no current cloud execution")
	}
	executionID := *task.CurrentExecutionID
	encoded, _ := json.Marshal(&agent.ExecutionResult{Success: false, Error: "用户取消", RemoteArtifacts: true})
	var execution *model.TaskExecution
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		lockedTask, err := txRepo.Tasks().FindByID(ctx, task.ID)
		if err != nil {
			return err
		}
		if userID != "" && lockedTask.UserID != userID {
			return fmt.Errorf("task not found")
		}
		if lockedTask.CurrentExecutionID == nil || *lockedTask.CurrentExecutionID != executionID {
			return ErrStaleTaskExecution
		}
		execution, err = txRepo.TaskExecutions().FindByID(ctx, executionID)
		if err != nil {
			return err
		}
		if !isTerminalExecution(execution.Status) {
			won, err := txRepo.TaskExecutions().Transition(ctx, execution.ID,
				[]string{model.TaskExecutionCreated, model.TaskExecutionDispatching, model.TaskExecutionStarting, model.TaskExecutionRunning},
				model.TaskExecutionCancelled,
				model.ExecutionTransition{TerminalReason: "user_cancelled", Result: encoded, FinalizationStatus: model.TaskExecutionFinalizationTerminal})
			if err != nil {
				return err
			}
			if !won {
				return ErrStaleTaskExecution
			}
			execution.Status = model.TaskExecutionCancelled
			execution.TerminalReason = "user_cancelled"
			execution.Result = encoded
			execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
		} else if execution.Status != model.TaskExecutionCancelled {
			return fmt.Errorf("task is not in a cancellable state")
		}
		if lockedTask.Status != model.TaskStatusCancelled {
			won, err := txRepo.Tasks().CompareAndSwapStatus(ctx, task.ID, model.TaskStatusRunning, model.TaskStatusCancelled)
			if err != nil {
				return err
			}
			if !won {
				return fmt.Errorf("task is not in a cancellable state")
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("cancel cloud task: %w", err)
	}
	task.Status = model.TaskStatusCancelled
	if err := s.finalizeTaskFromExecution(ctx, task, execution); err != nil {
		return err
	}
	return s.cleanupCancelledExecution(ctx, execution)
}

func (s *TaskService) cleanupCancelledExecution(ctx context.Context, execution *model.TaskExecution) (err error) {
	token := uuid.NewString()
	won, err := s.repo.TaskExecutions().ClaimCleanup(ctx, execution.ID, token, s.cloudFinalizationLease())
	if err != nil || !won {
		return err
	}
	defer func() {
		if releaseErr := s.repo.TaskExecutions().ReleaseCleanup(context.WithoutCancel(ctx), execution.ID, token); err == nil && releaseErr != nil {
			err = releaseErr
		}
	}()
	deleteCtx, cancel := context.WithTimeout(ctx, s.cloudFinalizationLease()/2)
	deleteErr := s.kubernetesDispatcher.Delete(deleteCtx, execution)
	cancel()
	if deleteErr != nil {
		backoff := s.cleanupRetryBackoff
		if backoff <= 0 {
			backoff = 10 * time.Second
		}
		failed, failErr := s.repo.TaskExecutions().FailCleanup(context.WithoutCancel(ctx), execution.ID, token, backoff)
		if failErr != nil {
			return errors.Join(fmt.Errorf("delete cancelled Kubernetes Job: %w", deleteErr), failErr)
		}
		if !failed {
			return errors.Join(fmt.Errorf("delete cancelled Kubernetes Job: %w", deleteErr), errors.New("cancelled execution cleanup lease lost while recording retry"))
		}
		return fmt.Errorf("delete cancelled Kubernetes Job: %w", deleteErr)
	}
	completed, err := s.repo.TaskExecutions().CompleteCleanup(ctx, execution.ID, token)
	if err != nil {
		return err
	}
	if !completed {
		return errors.New("cancelled execution cleanup lease lost")
	}
	return nil
}
