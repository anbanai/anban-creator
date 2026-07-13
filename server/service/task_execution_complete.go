package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var ErrStaleTaskExecution = errors.New("task execution is no longer current")

const cloudFinalizationLease = time.Minute

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
	won, err := s.repo.TaskExecutions().ClaimFinalization(ctx, execution.ID, token, cloudFinalizationLease)
	if err != nil {
		return fmt.Errorf("claim execution finalization: %w", err)
	}
	if !won {
		return nil
	}
	defer func() {
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
	if s.finalizationAfterStage != nil {
		if err := s.finalizationAfterStage(stage); err != nil {
			return err
		}
	}
	if stage == model.TaskExecutionFinalizationTerminal {
		if execution.Status == model.TaskExecutionSucceeded {
			err = s.repo.TaskFiles().PublishCurrentExecution(ctx, task.ID, execution.ID)
		} else {
			err = s.repo.TaskFiles().DiscardCurrentExecution(ctx, task.ID, execution.ID)
		}
		if err != nil {
			return fmt.Errorf("finalize execution artifacts: %w", err)
		}
		if s.finalizationAfterStage != nil {
			if err := s.finalizationAfterStage(model.TaskExecutionFinalizationArtifacts); err != nil {
				return err
			}
		}
		if err = s.advanceExecutionFinalization(ctx, execution.ID, token, stage, model.TaskExecutionFinalizationArtifacts); err != nil {
			return err
		}
		stage = model.TaskExecutionFinalizationArtifacts
	}
	if stage == model.TaskExecutionFinalizationArtifacts {
		if result != nil {
			if err = s.UpdateExecutionResult(ctx, task.ID, result); err != nil {
				return err
			}
		}
		if s.creditSvc != nil {
			if err = s.creditSvc.SettleAgentRuntime(ctx, task, result); err != nil {
				return fmt.Errorf("settle agent runtime: %w", err)
			}
		}
		if err = s.RebuildWorkflowStatus(ctx, task.ID); err != nil {
			return fmt.Errorf("rebuild workflow status: %w", err)
		}
		if execution.Status == model.TaskExecutionSucceeded {
			s.finalizeCloudPublishing(ctx, task, result)
		}
		if err = s.finalizeExecutionTaskStatus(ctx, task, execution, result); err != nil {
			return err
		}
		if s.finalizationAfterStage != nil {
			if err := s.finalizationAfterStage(model.TaskExecutionFinalizationTask); err != nil {
				return err
			}
		}
		if err = s.advanceExecutionFinalization(ctx, execution.ID, token, stage, model.TaskExecutionFinalizationTask); err != nil {
			return err
		}
		stage = model.TaskExecutionFinalizationTask
	}
	if stage == model.TaskExecutionFinalizationTask {
		status, errMsg := taskTerminalFromExecution(execution, result)
		s.notifyTerminal(ctx, task, status, errMsg)
		s.syncCloudSlotAndDispatch(ctx, task)
		if err = s.advanceExecutionFinalization(ctx, execution.ID, token, stage, model.TaskExecutionFinalizationDone); err != nil {
			return err
		}
	}
	return nil
}

func (s *TaskService) syncCloudSlotAndDispatch(ctx context.Context, task *model.Task) {
	if task == nil || task.ProjectID == "" {
		return
	}
	if s.pubsub != nil {
		if running, err := s.repo.Tasks().CountRunningByProject(ctx, task.ProjectID); err != nil {
			s.logger.Warn().Err(err).Str("project_id", task.ProjectID).Msg("count running tasks for cloud slot reconciliation")
		} else if err := s.pubsub.SyncProjectCount(ctx, task.ProjectID, running); err != nil {
			s.logger.Warn().Err(err).Str("project_id", task.ProjectID).Msg("reconcile cloud concurrency slot")
		}
	}
	if err := s.DispatchPendingTasks(ctx, task.ProjectID); err != nil {
		s.logger.Warn().Err(err).Str("project_id", task.ProjectID).Msg("dispatch pending tasks after cloud finalization")
	}
}

func (s *TaskService) finalizeCloudPublishing(ctx context.Context, task *model.Task, result *agent.ExecutionResult) {
	if s.publishingSvc == nil || task == nil || result == nil || task.ProjectID == "" {
		return
	}
	project, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
	if err != nil || project == nil || !project.GetEnablePublishing() {
		return
	}
	published := wasPublishedByAgent(result.LogText)
	var articles []DraftArticleInput
	if !published && task.Type == model.ScopeArticle {
		articles, err = s.extractArticleDraftFromTaskFiles(ctx, task.ID)
		if err != nil {
			s.logger.Warn().Err(err).Str("task_id", task.ID).Msg("extract cloud article draft for publishing")
			return
		}
	}
	if project.GetRequirePublishApproval() && !published && len(articles) > 0 {
		s.holdPublishForApproval(ctx, task.ID, articles)
		return
	}
	if published || len(articles) > 0 {
		publishCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		taskCopy, projectCopy := *task, *project
		go func() {
			defer cancel()
			s.autoPublishWithData(publishCtx, &taskCopy, &projectCopy, articles, result.LogText)
		}()
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
		s.refundTaskByMode(ctx, task, execution.TerminalReason)
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
	// The observable terminal DB state and artifact discard always precede the
	// external delete. A delete failure cannot reopen either row.
	if err := s.kubernetesDispatcher.Delete(ctx, execution); err != nil {
		return fmt.Errorf("delete cancelled Kubernetes Job: %w", err)
	}
	return nil
}
