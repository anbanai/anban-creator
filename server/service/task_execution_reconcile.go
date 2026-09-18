package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var (
	ErrRuntimePreparationInFlight = errors.New("runtime preparation may still be in flight")
	ErrExecutionCleanupLeaseLost  = errors.New("runtime execution cleanup lease lost")
)

func (s *TaskService) FindReconcilableExecutions(ctx context.Context, before time.Time, limit int) ([]*model.TaskExecution, error) {
	return s.repo.TaskExecutions().FindReconcilable(ctx, before, limit)
}

func (s *TaskService) RecordExecutionInstance(ctx context.Context, executionID, instanceID string) error {
	execution, _, err := s.currentExecution(ctx, executionID)
	if err != nil {
		return err
	}
	if isTerminalExecution(execution.Status) {
		return nil
	}
	if instanceID != "" && execution.RuntimeInstanceID != "" && execution.RuntimeInstanceID != instanceID {
		return fmt.Errorf("%w: execution instance changed from %s to %s", repository.ErrRuntimeIdentityConflict, execution.RuntimeInstanceID, instanceID)
	}
	if instanceID == "" || execution.RuntimeInstanceID == instanceID {
		return nil
	}
	return s.repo.TaskExecutions().SetRuntimeIdentity(ctx, executionID, model.RuntimeIdentity{InstanceID: instanceID})
}

func (s *TaskService) ResumeExecutionDispatch(ctx context.Context, executionID string) error {
	execution, task, err := s.currentExecution(ctx, executionID)
	if err != nil {
		return err
	}
	if execution.Status != model.TaskExecutionCreated && execution.Status != model.TaskExecutionDispatching {
		return nil
	}
	if err := s.dispatchCurrentExecution(ctx, task, execution); err != nil && !errors.Is(err, ErrDispatchInProgress) {
		return err
	}
	return nil
}

func (s *TaskService) ResumeExecutionFinalization(ctx context.Context, executionID string) error {
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		return err
	}
	if !isTerminalExecution(execution.Status) {
		return nil
	}
	task, err := s.repo.Tasks().FindByID(ctx, execution.TaskID)
	if err != nil {
		return err
	}
	if task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID {
		return ErrStaleTaskExecution
	}
	return s.finalizeTaskFromExecution(ctx, task, execution)
}

func (s *TaskService) ClaimExecutionCleanup(ctx context.Context, executionID, token string, lease time.Duration) (bool, error) {
	return s.repo.TaskExecutions().ClaimCleanup(ctx, executionID, token, lease)
}

func (s *TaskService) ResolveExecutionCleanupRuntime(ctx context.Context, executionID, token string) (*model.TaskExecution, bool, error) {
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		return nil, false, fmt.Errorf("find execution for runtime cleanup: %w", err)
	}
	if execution.CleanupStatus != model.TaskExecutionCleanupPending || execution.CleanupToken != token {
		return nil, false, ErrExecutionCleanupLeaseLost
	}
	dispatcherTarget, err := s.runtimeDispatcherScope()
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(execution.Target) != dispatcherTarget {
		return nil, false, fmt.Errorf("%w: execution target is %q, dispatcher scope is %q", ErrRuntimeDispatcherTargetMismatch, execution.Target, dispatcherTarget)
	}
	if completeRuntimeIdentity(runtimeIdentityFromExecution(execution)) {
		return execution, true, nil
	}
	active, err := s.repo.TaskExecutions().DispatchClaimActive(ctx, execution.ID, s.runtimeDispatchLease())
	if err != nil {
		return nil, false, fmt.Errorf("check runtime preparation barrier: %w", err)
	}
	if active {
		return nil, false, ErrRuntimePreparationInFlight
	}
	task, err := s.repo.Tasks().FindByID(ctx, execution.TaskID)
	if err != nil {
		return nil, false, fmt.Errorf("find task for runtime cleanup: %w", err)
	}
	identity, err := s.runtimeDispatcher.ResolvePrepared(ctx, execution, task)
	if errors.Is(err, agent.ErrRuntimeWorkloadNotFound) {
		return execution, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("resolve prepared runtime workload: %w", err)
	}
	normalized, err := normalizeRuntimeIdentity(identity)
	if err != nil || normalized.InstanceID == "" {
		if err == nil {
			err = fmt.Errorf("runtime dispatcher returned incomplete recovered runtime identity")
		}
		return nil, false, err
	}
	bound, err := s.repo.TaskExecutions().SetCleanupRuntimeIdentity(ctx, execution.ID, token, normalized)
	if err != nil {
		return nil, false, fmt.Errorf("persist recovered runtime identity: %w", err)
	}
	if !bound {
		return nil, false, ErrExecutionCleanupLeaseLost
	}
	authoritative, err := s.repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		return nil, false, fmt.Errorf("reload recovered runtime identity: %w", err)
	}
	if authoritative.CleanupStatus != model.TaskExecutionCleanupPending || authoritative.CleanupToken != token {
		return nil, false, ErrExecutionCleanupLeaseLost
	}
	if runtimeIdentityFromExecution(authoritative) != normalized {
		return nil, false, repository.ErrRuntimeIdentityConflict
	}
	return authoritative, true, nil
}

func completeRuntimeIdentity(identity model.RuntimeIdentity) bool {
	return strings.TrimSpace(identity.Scope) != "" && strings.TrimSpace(identity.Workload) != "" && strings.TrimSpace(identity.InstanceID) != ""
}

func (s *TaskService) CompleteExecutionCleanup(ctx context.Context, executionID, token string) (bool, error) {
	return s.repo.TaskExecutions().CompleteCleanup(ctx, executionID, token)
}

func (s *TaskService) FailExecutionCleanup(ctx context.Context, executionID, token string, backoff time.Duration) (bool, error) {
	return s.repo.TaskExecutions().FailCleanup(ctx, executionID, token, backoff)
}

func (s *TaskService) ReleaseExecutionCleanup(ctx context.Context, executionID, token string) error {
	return s.repo.TaskExecutions().ReleaseCleanup(ctx, executionID, token)
}

// ReconcileExecutionFailure makes the current attempt terminal. Runtime
// failures never create another execution or invoke the provider again.
func (s *TaskService) ReconcileExecutionFailure(ctx context.Context, executionID, status, reason string, diagnostics []byte) error {
	_, _, err := s.currentExecution(ctx, executionID)
	if err != nil {
		return err
	}
	return s.TerminalizeCurrentExecution(ctx, executionID, status, reason, diagnostics)
}
