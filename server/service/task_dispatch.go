package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anbanai/anban-creator/server/agent"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
)

var (
	ErrDispatchInProgress              = errors.New("runtime dispatch is already in progress")
	ErrRuntimeDispatcherTargetMismatch = errors.New("runtime dispatcher target mismatch")
	ErrTaskPlanPaused                  = errors.New("task plan is paused")
)

const defaultRuntimeDispatchLease = time.Minute

const (
	runtimeScopeMaxLength      = 63
	runtimeWorkloadMaxLength   = 63
	runtimeInstanceIDMaxLength = 64
)

const autocompactThrashingErrorPrefix = "Autocompact is thrashing:"

func (s *TaskService) SetRuntimeDispatcher(dispatcher agent.RuntimeDispatcher) {
	s.runtimeDispatcher = dispatcher
}

func (s *TaskService) runtimeDispatcherScope() (string, error) {
	if s.runtimeDispatcher == nil {
		return "", fmt.Errorf("runtime dispatcher is not configured")
	}
	target := strings.TrimSpace(s.runtimeDispatcher.Scope())
	if target == "" {
		return "", fmt.Errorf("runtime dispatcher scope is required")
	}
	return target, nil
}

func (s *TaskService) SetRuntimeDispatchLease(duration time.Duration) {
	if duration > 0 {
		s.dispatchLeaseDuration = duration
	}
}

func (s *TaskService) runtimeDispatchLease() time.Duration {
	if s.dispatchLeaseDuration > 0 {
		return s.dispatchLeaseDuration
	}
	return defaultRuntimeDispatchLease
}

func (s *TaskService) dispatchRuntime(ctx context.Context, task *model.Task) error {
	if s.dispatchBeforeCreate != nil {
		s.dispatchBeforeCreate()
	}
	execution, created, err := s.createCurrentExecution(ctx, task)
	if err != nil {
		return err
	}
	if !created {
		task, execution, err = s.reloadCurrentDispatchState(ctx, task.ID)
		if err != nil {
			return err
		}
	} else {
		task.Status = model.TaskStatusRunning
	}
	return s.dispatchCurrentExecution(ctx, task, execution)
}

func (s *TaskService) reloadCurrentDispatchState(ctx context.Context, taskID string) (*model.Task, *model.TaskExecution, error) {
	var task *model.Task
	var execution *model.TaskExecution
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		var err error
		task, err = txRepo.Tasks().FindByID(ctx, taskID)
		if err != nil {
			return fmt.Errorf("reload contended task %s: %w", taskID, err)
		}
		execution, err = txRepo.TaskExecutions().FindCurrentByTaskID(ctx, taskID)
		if err != nil {
			return fmt.Errorf("task %s has no readable current execution: %w", taskID, err)
		}
		return nil
	})
	return task, execution, err
}

func (s *TaskService) dispatchCurrentExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution) error {
	dispatcherTarget, err := s.runtimeDispatcherScope()
	if err != nil {
		return err
	}
	executionTarget := strings.TrimSpace(execution.Target)
	if executionTarget != dispatcherTarget {
		return fmt.Errorf("%w: execution target is %q, dispatcher scope is %q", ErrRuntimeDispatcherTargetMismatch, executionTarget, dispatcherTarget)
	}
	if task.Status != model.TaskStatusRunning {
		return fmt.Errorf("task %s has current execution %s but task status is %s", task.ID, execution.ID, task.Status)
	}
	switch execution.Status {
	case model.TaskExecutionStarting, model.TaskExecutionRunning:
		return nil
	case model.TaskExecutionCreated, model.TaskExecutionDispatching:
		// Continue below and claim an unowned or expired dispatch.
	default:
		return fmt.Errorf("task %s is running with current execution %s in status %s", task.ID, execution.ID, execution.Status)
	}

	token := uuid.NewString()
	won, err := s.repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, token, s.runtimeDispatchLease())
	if err != nil {
		return fmt.Errorf("claim runtime dispatch: %w", err)
	}
	if !won {
		latest, findErr := s.repo.TaskExecutions().FindByID(ctx, execution.ID)
		if findErr != nil {
			return fmt.Errorf("reload contended runtime dispatch: %w", findErr)
		}
		if latest.Status == model.TaskExecutionStarting || latest.Status == model.TaskExecutionRunning {
			return nil
		}
		if latest.Status == model.TaskExecutionDispatching {
			return fmt.Errorf("%w: execution %s", ErrDispatchInProgress, execution.ID)
		}
		return fmt.Errorf("runtime dispatch claim lost to execution status %s", latest.Status)
	}
	prepareTimeout := s.runtimeDispatchLease() / 2
	if prepareTimeout <= 0 {
		return fmt.Errorf("runtime dispatch lease is too short")
	}
	prepareCtx, cancelPrepare := context.WithTimeout(ctx, prepareTimeout)
	refreshed, err := s.repo.TaskExecutions().RefreshDispatchClaim(prepareCtx, execution.ID, token)
	if err != nil {
		cancelPrepare()
		return fmt.Errorf("refresh runtime dispatch barrier: %w", err)
	}
	if !refreshed {
		cancelPrepare()
		return fmt.Errorf("runtime dispatch claim %s was lost before preparation", execution.ID)
	}
	claimed, err := s.repo.TaskExecutions().FindByID(prepareCtx, execution.ID)
	if err != nil {
		cancelPrepare()
		abandoned, abandonErr := s.repo.TaskExecutions().AbandonDispatch(context.WithoutCancel(ctx), execution.ID, token)
		if abandonErr != nil {
			return errors.Join(fmt.Errorf("reload claimed runtime dispatch: %w", err), fmt.Errorf("abandon unreadable dispatch claim: %w", abandonErr))
		}
		if !abandoned {
			return errors.Join(fmt.Errorf("reload claimed runtime dispatch: %w", err), fmt.Errorf("abandon unreadable dispatch claim: stale execution %s", execution.ID))
		}
		return fmt.Errorf("reload claimed runtime dispatch: %w", err)
	}
	if claimed.Status != model.TaskExecutionDispatching || claimed.DispatchClaimToken != token || claimed.DispatchClaimedAt == nil {
		cancelPrepare()
		return fmt.Errorf("runtime dispatch claim %s was not durably readable", execution.ID)
	}
	execution = claimed
	runtimeIdentity, err := s.runtimeDispatcher.Prepare(prepareCtx, execution, task)
	cancelPrepare()
	if err != nil {
		barrierErr := s.refreshRuntimeDispatchBarrier(ctx, execution.ID, token)
		if barrierErr != nil {
			return errors.Join(fmt.Errorf("ambiguous runtime dispatch: %w", err), barrierErr)
		}
		if agent.IsPermanentDispatchError(err) {
			return s.failDispatch(ctx, task, execution, token, err)
		}
		return fmt.Errorf("ambiguous runtime dispatch: %w", err)
	}
	normalizedIdentity, err := normalizeRuntimeIdentity(runtimeIdentity)
	if err != nil {
		if barrierErr := s.refreshRuntimeDispatchBarrier(ctx, execution.ID, token); barrierErr != nil {
			return errors.Join(err, barrierErr)
		}
		return s.failDispatch(ctx, task, execution, token, agent.NewPermanentDispatchError(err))
	}
	preparedExecution := executionWithRuntimeIdentity(execution, normalizedIdentity)
	won, err = s.repo.TaskExecutions().CompleteDispatch(ctx, execution.ID, token, normalizedIdentity)
	if errors.Is(err, repository.ErrRuntimeIdentityConflict) {
		return errors.Join(
			s.failDispatch(ctx, task, execution, token, agent.NewPermanentDispatchError(err)),
			s.compensatePreparedRuntime(ctx, preparedExecution),
		)
	}
	if err != nil {
		return errors.Join(
			fmt.Errorf("mark runtime execution starting: %w", err),
			s.compensatePreparedRuntime(ctx, preparedExecution),
		)
	}
	if !won {
		return s.resolveLostDispatchCompletion(ctx, preparedExecution, normalizedIdentity)
	}
	authoritative, err := s.repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		return errors.Join(fmt.Errorf("reload prepared runtime execution: %w", err), s.compensatePreparedRuntime(ctx, preparedExecution))
	}
	if authoritative.Status != model.TaskExecutionStarting || runtimeIdentityFromExecution(authoritative) != normalizedIdentity {
		return errors.Join(
			fmt.Errorf("prepared runtime execution %s lost activation authority", execution.ID),
			s.compensatePreparedRuntime(ctx, preparedExecution),
		)
	}
	activateErr := s.runtimeDispatcher.Activate(ctx, authoritative)
	latest, reloadErr := s.repo.TaskExecutions().FindByID(context.WithoutCancel(ctx), execution.ID)
	if reloadErr != nil {
		return errors.Join(activateErr, fmt.Errorf("reload runtime execution after activation: %w", reloadErr))
	}
	if isTerminalExecution(latest.Status) {
		return s.compensatePreparedRuntime(ctx, authoritative)
	}
	if activateErr != nil {
		if agent.IsPermanentDispatchError(activateErr) {
			diagnostics, _ := json.Marshal(map[string]string{"error": activateErr.Error()})
			terminalErr := s.TerminalizeCurrentExecution(context.WithoutCancel(ctx), execution.ID, model.TaskExecutionFailed, "activation_failed", diagnostics)
			return errors.Join(
				fmt.Errorf("activate runtime execution: %w", activateErr),
				terminalErr,
				s.compensatePreparedRuntime(ctx, authoritative),
			)
		}
		return fmt.Errorf("activate runtime execution: %w", activateErr)
	}
	return nil
}

func executionWithRuntimeIdentity(execution *model.TaskExecution, identity model.RuntimeIdentity) *model.TaskExecution {
	if execution == nil {
		return nil
	}
	bound := *execution
	bound.RuntimeScope = identity.Scope
	bound.RuntimeWorkload = identity.Workload
	bound.RuntimeInstanceID = identity.InstanceID
	return &bound
}

func runtimeIdentityFromExecution(execution *model.TaskExecution) model.RuntimeIdentity {
	if execution == nil {
		return model.RuntimeIdentity{}
	}
	return model.RuntimeIdentity{
		Scope: execution.RuntimeScope, Workload: execution.RuntimeWorkload, InstanceID: execution.RuntimeInstanceID,
	}
}

func (s *TaskService) refreshRuntimeDispatchBarrier(ctx context.Context, executionID, token string) error {
	timeout := s.runtimeDispatchLease() / 2
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	refreshed, err := s.repo.TaskExecutions().RefreshDispatchClaim(refreshCtx, executionID, token)
	if err != nil {
		return fmt.Errorf("refresh ambiguous runtime dispatch barrier: %w", err)
	}
	if !refreshed {
		return fmt.Errorf("refresh ambiguous runtime dispatch barrier: stale execution %s", executionID)
	}
	return nil
}

func (s *TaskService) resolveLostDispatchCompletion(ctx context.Context, prepared *model.TaskExecution, identity model.RuntimeIdentity) error {
	latest, err := s.repo.TaskExecutions().FindByID(context.WithoutCancel(ctx), prepared.ID)
	if err != nil {
		return fmt.Errorf("reload stale runtime dispatch %s: %w", prepared.ID, err)
	}
	staleErr := fmt.Errorf("mark runtime execution starting: stale execution %s", prepared.ID)
	authoritativeIdentity := runtimeIdentityFromExecution(latest)
	if !isTerminalExecution(latest.Status) && completeRuntimeIdentity(authoritativeIdentity) && authoritativeIdentity == identity {
		return nil
	}
	if isTerminalExecution(latest.Status) || completeRuntimeIdentity(authoritativeIdentity) {
		return errors.Join(staleErr, s.compensatePreparedRuntime(ctx, prepared))
	}
	return staleErr
}

func (s *TaskService) compensatePreparedRuntime(ctx context.Context, execution *model.TaskExecution) error {
	if execution == nil || s.runtimeDispatcher == nil {
		return nil
	}
	timeout := s.runtimeDispatchLease() / 2
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if err := s.runtimeDispatcher.Delete(deleteCtx, execution); err != nil {
		return fmt.Errorf("delete prepared runtime workload: %w", err)
	}
	return nil
}

func normalizeRuntimeIdentity(identity *model.RuntimeIdentity) (model.RuntimeIdentity, error) {
	if identity == nil {
		return model.RuntimeIdentity{}, fmt.Errorf("runtime dispatcher returned incomplete runtime identity")
	}
	normalized := model.RuntimeIdentity{
		Scope:      strings.TrimSpace(identity.Scope),
		Workload:   strings.TrimSpace(identity.Workload),
		InstanceID: strings.TrimSpace(identity.InstanceID),
	}
	if normalized.Scope == "" || normalized.Workload == "" {
		return model.RuntimeIdentity{}, fmt.Errorf("runtime dispatcher returned incomplete runtime identity")
	}
	for _, member := range []struct {
		name  string
		value string
		max   int
	}{
		{name: "scope", value: normalized.Scope, max: runtimeScopeMaxLength},
		{name: "workload", value: normalized.Workload, max: runtimeWorkloadMaxLength},
		{name: "instance", value: normalized.InstanceID, max: runtimeInstanceIDMaxLength},
	} {
		if utf8.RuneCountInString(member.value) > member.max {
			return model.RuntimeIdentity{}, fmt.Errorf("runtime dispatcher returned %s identity longer than %d characters", member.name, member.max)
		}
	}
	return normalized, nil
}

func (s *TaskService) createCurrentExecution(ctx context.Context, task *model.Task) (*model.TaskExecution, bool, error) {
	if err := s.validateFrozenTaskImageCapability(ctx, task); err != nil {
		return nil, false, err
	}
	if err := s.validateFrozenTaskProfileRuntime(task); err != nil {
		return nil, false, err
	}
	target, err := s.runtimeDispatcherScope()
	if err != nil {
		return nil, false, err
	}
	var execution *model.TaskExecution
	created := false
	err = s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		if task.PlanID != nil && strings.TrimSpace(*task.PlanID) != "" {
			plan, err := txRepo.Plans().FindByIDForUpdate(ctx, strings.TrimSpace(*task.PlanID))
			if err != nil {
				return fmt.Errorf("check task plan before dispatch: %w", err)
			}
			if plan.Status != model.PlanStatusActive {
				return ErrTaskPlanPaused
			}
		}
		swapped, err := txRepo.Tasks().CompareAndSwapStatusAndStartedAt(
			ctx, task.ID, model.TaskStatusPending, model.TaskStatusRunning,
		)
		if err != nil {
			return fmt.Errorf("claim task for runtime dispatch: %w", err)
		}
		if !swapped {
			return nil
		}
		dispatchTask, err := txRepo.Tasks().FindByIDForUpdate(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("reload claimed task for runtime dispatch: %w", err)
		}

		attempt, err := txRepo.TaskExecutions().NextAttempt(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("allocate task execution attempt: %w", err)
		}
		parent, resumeSessionID, refreshRuntime, err := resumeExecutionLineage(ctx, txRepo, dispatchTask)
		if err != nil {
			return err
		}
		var runtime srvconfig.RuntimeImageSelection
		if parent != nil && !refreshRuntime && hasCompleteAgentPackIdentity(parent) {
			runtime = srvconfig.RuntimeImageSelection{
				Profile: strings.TrimSpace(parent.RuntimeProfile),
				Image:   strings.TrimSpace(parent.RuntimeImage),
			}
			if runtime.Profile == "" || runtime.Image == "" {
				return fmt.Errorf("resume parent execution runtime identity is missing: execution %s", parent.ID)
			}
		} else {
			runtime = s.runtimeDispatcher.ResolveRuntime(dispatchTask.Type)
			if model.IsHypitPlatform(dispatchTask.Type) {
				if snap := readHypitSnapshot(dispatchTask); snap.Image != "" {
					runtime.Image = snap.Image
					runtime.Profile = model.PlatformHypit
				}
			}
			runtime.Profile = strings.TrimSpace(runtime.Profile)
			runtime.Image = strings.TrimSpace(runtime.Image)
			if runtime.Profile == "" || runtime.Image == "" {
				return fmt.Errorf("resolve runtime image for task type %q returned incomplete identity", dispatchTask.Type)
			}
		}
		parentExecutionID := ""
		if parent != nil {
			parentExecutionID = parent.ID
		}
		profiledExecution := model.NewTaskExecutionAgentProfile(dispatchTask.AgentProfileSnapshot, dispatchTask.AgentProfileFingerprint)
		execution = &profiledExecution
		if parent == nil || refreshRuntime || !inheritAgentPackIdentity(execution, parent) {
			resumeSessionID = ""
			if err := applyAgentPackIdentity(execution, dispatchTask.Type); err != nil {
				return err
			}
		}
		if model.IsHypitPlatform(dispatchTask.Type) && parent == nil {
			if snap := readHypitSnapshot(dispatchTask); snap.SourceExecution != nil {
				if !inheritAgentPackIdentity(execution, snap.SourceExecution.execution()) {
					return fmt.Errorf("hypit frozen Pack contract is incomplete")
				}
			}
		}
		if execution.RuntimeProfile != runtime.Profile {
			return fmt.Errorf("resolved runtime profile %q does not match frozen Agent Pack profile %q", runtime.Profile, execution.RuntimeProfile)
		}
		execution.ID = uuid.NewString()
		execution.TaskID = task.ID
		execution.Attempt = attempt
		execution.Purpose = model.TaskExecutionPurposePrimary
		if taskHasPublicationRecoveryInput(dispatchTask) {
			execution.Purpose = model.TaskExecutionPurposePublicationRecovery
		}
		execution.ParentExecutionID = parentExecutionID
		execution.ResumeSessionID = resumeSessionID
		execution.RuntimeImage = runtime.Image
		execution.Target = target
		execution.Status = model.TaskExecutionCreated
		if err := txRepo.TaskExecutions().Create(ctx, execution); err != nil {
			return fmt.Errorf("create task execution: %w", err)
		}
		if execution.Purpose == model.TaskExecutionPurposePublicationRecovery {
			input := dispatchTask.AgentInput.Data()
			delete(input, "publication_recovery")
			delete(input, "source_execution_id")
			delete(input, "resume_from")
			dispatchTask.SetAgentInput(input)
			if err := txRepo.Tasks().Update(ctx, dispatchTask); err != nil {
				return fmt.Errorf("clear publication recovery controls: %w", err)
			}
			task.AgentInput = dispatchTask.AgentInput
		}
		updated, err := txRepo.Tasks().SetCurrentExecution(ctx, task.ID, execution.ID)
		if err != nil {
			return fmt.Errorf("set current task execution: %w", err)
		}
		if !updated {
			return fmt.Errorf("set current task execution: task %s is no longer running", task.ID)
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return execution, created, nil
}

func (s *TaskService) validateFrozenTaskProfileRuntime(task *model.Task) error {
	if task == nil {
		return fmt.Errorf("%w: task is required", ErrAgentProfileSnapshotInvalid)
	}
	if s.agentProfiles == nil {
		return fmt.Errorf("%w: agent profile registry is unavailable", ErrAgentProviderUnavailable)
	}
	_, err := s.agentProfiles.ResolveRuntime(task.ExecutionProfile, task.AgentProfileSnapshot, task.AgentProfileFingerprint)
	return err
}

func (s *TaskService) validateFrozenTaskImageCapability(ctx context.Context, task *model.Task) error {
	if task == nil {
		return ErrTaskImageCapabilityMissing
	}
	if !taskUsesFrozenImageCapability(task.Type) {
		return nil
	}
	if strings.TrimSpace(task.ImageCapabilityKey) == "" {
		return ErrTaskImageCapabilityMissing
	}
	if s.imageCapabilities == nil {
		return ErrImageCapabilityResolverUnavailable
	}
	snapshot := task.ImageCapabilitySnapshot.Data()
	if snapshot.Key != strings.TrimSpace(task.ImageCapabilityKey) {
		return ErrTaskImageCapabilityInvalid
	}
	if err := s.imageCapabilities.ValidateFrozenImageCapability(ctx, task.UserID, snapshot); err != nil {
		return fmt.Errorf("validate frozen task image capability: %w", err)
	}
	return nil
}

func taskUsesFrozenImageCapability(taskType string) bool {
	return strings.TrimSpace(taskType) != model.TaskTypeViralAnalysis
}

func resumeExecutionLineage(ctx context.Context, repo repository.Repository, task *model.Task) (*model.TaskExecution, string, bool, error) {
	publicationRecovery := taskHasPublicationRecoveryInput(task)
	if task == nil || task.CurrentExecutionID == nil || (!taskHasResumeInput(task) && !publicationRecovery) {
		return nil, "", false, nil
	}
	parentID := strings.TrimSpace(*task.CurrentExecutionID)
	if parentID == "" {
		return nil, "", false, nil
	}
	parent, err := repo.TaskExecutions().FindByID(ctx, parentID)
	if err != nil {
		return nil, "", false, fmt.Errorf("load resume parent execution: %w", err)
	}
	if parent.TaskID != task.ID || !isTerminalExecution(parent.Status) {
		return nil, "", false, fmt.Errorf("resume parent execution %s is not a terminal attempt of task %s", parent.ID, task.ID)
	}
	if publicationRecovery {
		// Publication recovery needs the source artifacts but must run the
		// currently deployed image and Agent Pack without resuming an old session.
		return parent, "", true, nil
	}
	if len(parent.Result) == 0 {
		return parent, "", false, nil
	}
	var result agent.ExecutionResult
	if err := json.Unmarshal(parent.Result, &result); err != nil {
		return nil, "", false, fmt.Errorf("decode resume parent execution result: %w", err)
	}
	if isAutocompactThrashingError(result.Error) {
		// The old session and runtime image jointly produced an unrecoverable
		// context shape. Keep lineage, but use the currently deployed image so
		// a workflow/config fix can take effect on resume.
		return parent, "", true, nil
	}
	return parent, strings.TrimSpace(result.SessionID), false, nil
}

func isAutocompactThrashingError(message string) bool {
	return strings.HasPrefix(strings.TrimSpace(message), autocompactThrashingErrorPrefix)
}

func taskHasResumeInput(task *model.Task) bool {
	for _, attachment := range task.InputAttachments.Data() {
		if attachment.Role == model.EntryAttachmentRoleResumeLatest {
			return true
		}
	}
	return false
}

func taskHasPublicationRecoveryInput(task *model.Task) bool {
	if task == nil {
		return false
	}
	publicationRecovery, _ := task.AgentInput.Data()["publication_recovery"].(bool)
	return publicationRecovery
}

func (s *TaskService) failDispatch(ctx context.Context, task *model.Task, execution *model.TaskExecution, token string, dispatchErr error) error {
	diagnostics, _ := json.Marshal(map[string]string{"error": dispatchErr.Error()})
	executionResult, _ := json.Marshal(&agent.ExecutionResult{
		Success:         false,
		Error:           dispatchErr.Error(),
		RemoteArtifacts: true,
	})
	terminalized := false
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		won, err := txRepo.TaskExecutions().FailDispatch(ctx, execution.ID, token, "dispatch_failed", diagnostics, executionResult)
		if err != nil {
			return fmt.Errorf("fail task execution: %w", err)
		}
		if !won {
			return nil
		}
		terminalized = true
		return nil
	})
	if err != nil {
		return fmt.Errorf("dispatch runtime execution: %w (terminalize: %v)", dispatchErr, err)
	}
	if !terminalized {
		return fmt.Errorf("dispatch runtime execution: %w", dispatchErr)
	}

	execution.Status = model.TaskExecutionFailed
	execution.TerminalReason = "dispatch_failed"
	execution.Diagnostics = diagnostics
	execution.Result = executionResult
	execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
	execution.CleanupStatus = model.TaskExecutionCleanupPending
	if finalizeErr := s.finalizeTaskFromExecution(ctx, task, execution); finalizeErr != nil {
		return errors.Join(
			fmt.Errorf("dispatch runtime execution: %w", dispatchErr),
			fmt.Errorf("finalize failed runtime dispatch: %w", finalizeErr),
		)
	}
	return fmt.Errorf("dispatch runtime execution: %w", dispatchErr)
}
