package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type KubernetesReconcileService interface {
	FindReconcilableExecutions(context.Context, time.Time, int) ([]*model.TaskExecution, error)
	RecordExecutionInstance(context.Context, string, string) error
	ResumeExecutionDispatch(context.Context, string) error
	ResumeExecutionFinalization(context.Context, string) error
	ReconcileExecutionFailure(context.Context, string, string, string, []byte, int) error
	ClaimExecutionCleanup(context.Context, string, string, time.Duration) (bool, error)
	CompleteExecutionCleanup(context.Context, string, string) (bool, error)
	FailExecutionCleanup(context.Context, string, string, time.Duration) (bool, error)
	ReleaseExecutionCleanup(context.Context, string, string) error
}

type KubernetesReconcilerConfig struct {
	Interval             time.Duration
	BatchSize            int
	Concurrency          int
	CompletionGrace      time.Duration
	MissingResourceGrace time.Duration
	HeartbeatTimeout     time.Duration
	PreStartRetryLimit   int
	CleanupLease         time.Duration
	CleanupRetryBackoff  time.Duration
}

type KubernetesReconciler struct {
	dispatcher RuntimeDispatcher
	service    KubernetesReconcileService
	config     KubernetesReconcilerConfig
	logger     *zerolog.Logger
	now        func() time.Time
}

func NewKubernetesReconciler(dispatcher RuntimeDispatcher, service KubernetesReconcileService, cfg KubernetesReconcilerConfig, logger *zerolog.Logger) *KubernetesReconciler {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.MissingResourceGrace <= 0 {
		cfg.MissingResourceGrace = 30 * time.Second
	}
	if cfg.CleanupLease <= 0 {
		cfg.CleanupLease = time.Minute
	}
	if cfg.CleanupRetryBackoff <= 0 {
		cfg.CleanupRetryBackoff = 10 * time.Second
	}
	return &KubernetesReconciler{dispatcher: dispatcher, service: service, config: cfg, logger: logger, now: time.Now}
}

func (r *KubernetesReconciler) Run(ctx context.Context) {
	if r == nil || r.dispatcher == nil || r.service == nil {
		return
	}
	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()
	for {
		if err := r.ReconcileOnce(ctx); err != nil && r.logger != nil {
			r.logger.Error().Err(err).Msg("Kubernetes execution reconciliation batch failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *KubernetesReconciler) ReconcileOnce(ctx context.Context) error {
	if r == nil || r.dispatcher == nil || r.service == nil {
		return errors.New("Kubernetes reconciler is not configured")
	}
	now := r.now()
	executions, err := r.service.FindReconcilableExecutions(ctx, now, r.config.BatchSize)
	if err != nil {
		return err
	}
	sem := make(chan struct{}, r.config.Concurrency)
	var wg sync.WaitGroup
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		wg.Add(1)
		go func(execution *model.TaskExecution) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if err := r.reconcileOne(ctx, execution, now); err != nil && r.logger != nil {
				r.logger.Warn().Err(err).Str("execution_id", execution.ID).Str("task_id", execution.TaskID).Msg("Kubernetes execution reconciliation item failed")
			}
		}(execution)
	}
	wg.Wait()
	return ctx.Err()
}

func (r *KubernetesReconciler) reconcileOne(ctx context.Context, execution *model.TaskExecution, now time.Time) error {
	if isTerminalKubernetesExecution(execution.Status) {
		finalizeErr := r.service.ResumeExecutionFinalization(ctx, execution.ID)
		cleanupErr := r.cleanupExecution(ctx, execution)
		return errors.Join(finalizeErr, cleanupErr)
	}
	if execution.Status == model.TaskExecutionCreated || execution.Status == model.TaskExecutionDispatching {
		return r.service.ResumeExecutionDispatch(ctx, execution.ID)
	}
	state, err := r.dispatcher.Inspect(ctx, execution)
	if err != nil {
		missingSince := execution.UpdatedAt
		if missingSince.IsZero() {
			missingSince = execution.CreatedAt
		}
		if errors.Is(err, ErrRuntimeWorkloadNotFound) && now.Sub(missingSince) >= r.config.MissingResourceGrace {
			return r.fail(ctx, execution, model.TaskExecutionFailed, "job_missing", "Kubernetes Job or Pod was not found", nil)
		}
		return err
	}
	if state.InstanceID != "" {
		if err := r.service.RecordExecutionInstance(ctx, execution.ID, state.InstanceID); err != nil {
			return err
		}
	}

	reason, terminalStatus := kubernetesTerminalReason(state)
	if reason != "" {
		return r.fail(ctx, execution, terminalStatus, reason, state.Message, state.ExitCode)
	}
	if state.Phase == RuntimePhaseSucceeded {
		completedAt := execution.UpdatedAt
		if state.CompletedAt != nil {
			completedAt = *state.CompletedAt
		}
		if now.Sub(completedAt) < r.config.CompletionGrace {
			return nil
		}
		return r.fail(ctx, execution, model.TaskExecutionFailed, "missing_completion", "Job succeeded without an Agent completion callback", state.ExitCode)
	}
	if execution.Started && r.config.HeartbeatTimeout > 0 {
		heartbeatBase := execution.LastHeartbeatAt
		if heartbeatBase == nil {
			heartbeatBase = execution.StartedAt
		}
		if heartbeatBase != nil && now.Sub(*heartbeatBase) >= r.config.HeartbeatTimeout {
			return r.fail(ctx, execution, model.TaskExecutionTimedOut, "heartbeat_timeout", "Agent heartbeat expired", state.ExitCode)
		}
	}
	return nil
}

func (r *KubernetesReconciler) fail(ctx context.Context, execution *model.TaskExecution, status, reason, message string, exitCode *int32) error {
	diagnostics, _ := json.Marshal(map[string]any{
		"reason":    reason,
		"message":   sanitizeKubernetesDiagnostic(message),
		"exit_code": exitCodeValue(exitCode),
	})
	if err := r.service.ReconcileExecutionFailure(ctx, execution.ID, status, reason, diagnostics, r.config.PreStartRetryLimit); err != nil {
		return err
	}
	return r.cleanupExecution(ctx, execution)
}

func (r *KubernetesReconciler) cleanupExecution(ctx context.Context, execution *model.TaskExecution) (err error) {
	if execution == nil || execution.CleanupStatus == model.TaskExecutionCleanupDone {
		return nil
	}
	token := uuid.NewString()
	won, err := r.service.ClaimExecutionCleanup(ctx, execution.ID, token, r.config.CleanupLease)
	if err != nil || !won {
		return err
	}
	defer func() {
		if releaseErr := r.service.ReleaseExecutionCleanup(context.WithoutCancel(ctx), execution.ID, token); err == nil && releaseErr != nil {
			err = releaseErr
		}
	}()
	deleteCtx, cancel := context.WithTimeout(ctx, r.config.CleanupLease/2)
	deleteErr := r.dispatcher.Delete(deleteCtx, execution)
	cancel()
	if deleteErr != nil {
		failed, failErr := r.service.FailExecutionCleanup(context.WithoutCancel(ctx), execution.ID, token, r.config.CleanupRetryBackoff)
		if failErr != nil {
			return errors.Join(deleteErr, failErr)
		}
		if !failed {
			return errors.Join(deleteErr, errors.New("Kubernetes execution cleanup lease lost while recording retry"))
		}
		return deleteErr
	}
	completed, err := r.service.CompleteExecutionCleanup(ctx, execution.ID, token)
	if err != nil {
		return err
	}
	if !completed {
		return errors.New("Kubernetes execution cleanup lease lost")
	}
	return nil
}

func exitCodeValue(exitCode *int32) any {
	if exitCode == nil {
		return nil
	}
	return *exitCode
}

func kubernetesTerminalReason(state *RuntimeExecutionState) (string, string) {
	if state == nil {
		return "inspection_empty", model.TaskExecutionFailed
	}
	reason := strings.ToLower(strings.TrimSpace(state.Reason))
	switch {
	case reason == "deadlineexceeded" || strings.Contains(reason, "deadline"):
		return "deadline_exceeded", model.TaskExecutionTimedOut
	case reason == "oomkilled" || (state.ExitCode != nil && *state.ExitCode == 137):
		return "oom_killed", model.TaskExecutionFailed
	case reason == "failedscheduling" || strings.Contains(reason, "unschedul"):
		return "scheduling_failed", model.TaskExecutionFailed
	case reason == "failedmount" || reason == "failedattachvolume" || strings.Contains(reason, "mount") || strings.Contains(reason, "provision"):
		return "volume_mount_failed", model.TaskExecutionFailed
	case reason == "errimagepull" || reason == "imagepullbackoff" || strings.Contains(reason, "imagepull"):
		return "image_pull_failed", model.TaskExecutionFailed
	case strings.Contains(reason, "containerconfig") || strings.Contains(reason, "crashloop") || strings.Contains(reason, "runcontainer"):
		return "container_start_failed", model.TaskExecutionFailed
	case state.Phase == RuntimePhaseFailed:
		return "job_failed", model.TaskExecutionFailed
	default:
		return "", ""
	}
}

func sanitizeKubernetesDiagnostic(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) > 1024 {
		value = value[:1024]
	}
	for _, marker := range []string{"token=", "authorization:", "bearer "} {
		if index := strings.Index(strings.ToLower(value), marker); index >= 0 {
			value = value[:index] + "[redacted]"
		}
	}
	return value
}

func isTerminalKubernetesExecution(status string) bool {
	switch status {
	case model.TaskExecutionSucceeded, model.TaskExecutionFailed, model.TaskExecutionCancelled, model.TaskExecutionTimedOut:
		return true
	default:
		return false
	}
}
