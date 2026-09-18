package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

const planCheckerLockKey = "plan_checker:lock"
const planCheckerLockTTL = 55 * time.Second

// checkInterval is how often the plan checker runs.
const checkInterval = 1 * time.Minute

// StartPlanChecker runs a background goroutine that checks for plans whose
// next_run_at has passed and triggers content generation tasks for them.
// It runs on a 1-minute ticker and stops when the provided context is cancelled.
// When Redis is available, it uses a distributed lock to prevent duplicate
// task creation when multiple server instances are running.
func StartPlanChecker(ctx context.Context, repo repository.Repository, taskSvc *service.TaskService, logger *zerolog.Logger, rdb *redis.Client) {
	logger.Info().Dur("interval", checkInterval).Msg("starting plan checker")

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	// Run once immediately on start.
	tryAcquireAndCheck(ctx, repo, taskSvc, logger, rdb)

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("plan checker stopped")
			return
		case <-ticker.C:
			tryAcquireAndCheck(ctx, repo, taskSvc, logger, rdb)
		}
	}
}

// tryAcquireAndCheck attempts to acquire a distributed lock via Redis SETNX
// before running the plan checker. If Redis is unavailable or lock cannot be
// acquired (another instance is processing), it skips this cycle.
func tryAcquireAndCheck(ctx context.Context, repo repository.Repository, taskSvc *service.TaskService, logger *zerolog.Logger, rdb *redis.Client) {
	if rdb != nil {
		// Try to acquire distributed lock.
		acquired, err := rdb.SetNX(ctx, planCheckerLockKey, "locked", planCheckerLockTTL).Result()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to acquire plan checker lock, skipping")
			return
		}
		if !acquired {
			logger.Debug().Msg("plan checker lock held by another instance, skipping")
			return
		}
		// Ensure lock is released when done.
		defer rdb.Del(ctx, planCheckerLockKey)
	}

	checkAndTriggerPlans(ctx, repo, taskSvc, logger)
	syncProjectRunningCounts(ctx, repo, taskSvc, logger)
	reapStuckTasks(ctx, repo, taskSvc, logger)
}

// stuckTaskThreshold is how long without a heartbeat before a task is considered stuck.
const stuckTaskThreshold = 5 * time.Minute

// reapStuckTasks finds running tasks whose heartbeat has stopped and marks them
// as failed. Uses last_heartbeat_at to distinguish "actively working" from "stuck".
func reapStuckTasks(ctx context.Context, repo repository.Repository, taskSvc *service.TaskService, logger *zerolog.Logger) {
	tasks, err := repo.Tasks().FindRunning(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to list running tasks for stuck reaper")
		return
	}

	now := time.Now()
	reaped := 0
	for _, t := range tasks {
		// Durable managed executions are owned by RuntimeReconciler. The legacy
		// task-only reaper must not race that authoritative state machine.
		if t.CurrentExecutionID != nil {
			continue
		}
		stuck := false
		var staleDuration time.Duration

		if t.LastHeartbeatAt != nil {
			// Heartbeat was set but is stale.
			staleDuration = now.Sub(*t.LastHeartbeatAt)
			if staleDuration > stuckTaskThreshold {
				stuck = true
			}
		} else if t.StartedAt != nil {
			// Task is running but never sent a heartbeat (executor never entered polling).
			staleDuration = now.Sub(*t.StartedAt)
			if staleDuration > stuckTaskThreshold {
				stuck = true
			}
		}

		if !stuck {
			continue
		}

		errMsg := fmt.Sprintf("stuck task reaped: no heartbeat for %s (threshold %s)", staleDuration.Round(time.Second), stuckTaskThreshold)
		logger.Warn().
			Str("task_id", t.ID).
			Str("user_id", t.UserID).
			Str("project_id", t.ProjectID).
			Str("stale_duration", staleDuration.Round(time.Second).String()).
			Msg(errMsg)

		swapped, err := taskSvc.FailStaleRunningTaskForInfrastructure(ctx, t.ID, now.Add(-stuckTaskThreshold), errMsg)
		if err != nil {
			logger.Error().Err(err).Str("task_id", t.ID).Msg("failed to mark stuck task as failed")
			continue
		}
		if !swapped {
			logger.Warn().Str("task_id", t.ID).Msg("stuck task already resolved, skipping")
			continue
		}
		// Release concurrency slot for the reaped task.
		if t.ProjectID != "" && taskSvc.PubSub() != nil {
			if err := taskSvc.PubSub().ReleaseSlot(ctx, t.ProjectID); err != nil {
				logger.Warn().Err(err).Str("project_id", t.ProjectID).Msg("failed to release slot after reaping stuck task")
			}
		}

		if t.ProjectID != "" {
			if err := taskSvc.DispatchPendingTasks(ctx, t.ProjectID); err != nil {
				logger.Warn().Err(err).Str("project_id", t.ProjectID).Msg("failed to dispatch pending tasks after reaping stuck task")
			}
		}
		reaped++
	}
	if reaped > 0 {
		logger.Info().Int("count", reaped).Msg("reaped stuck tasks")
	}
}

// checkAndTriggerPlans queries all active plans that are due and creates
// tasks for each one, then advances their next_run_at to the next cron occurrence.
func checkAndTriggerPlans(ctx context.Context, repo repository.Repository, taskSvc *service.TaskService, logger *zerolog.Logger) {
	now := time.Now()

	plans, err := repo.Plans().ListDue(ctx, now)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list due plans")
		return
	}

	if len(plans) == 0 {
		return
	}

	logger.Info().Int("count", len(plans)).Msg("found due plans, triggering tasks")

	for _, plan := range plans {
		if err := triggerPlan(ctx, repo, taskSvc, plan, logger); err != nil {
			logger.Error().Err(err).Str("plan_id", plan.ID).Msg("failed to trigger due plan")
		}
	}
}

// TriggerPlanNow creates a task from an active plan and advances its next run
// time. It is used by explicit async plan trigger jobs.
func TriggerPlanNow(ctx context.Context, repo repository.Repository, taskSvc *service.TaskService, planID string, logger *zerolog.Logger) error {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return fmt.Errorf("plan_id is required")
	}

	plan, err := repo.Plans().FindByID(ctx, planID)
	if err != nil {
		return fmt.Errorf("find plan %s: %w", planID, err)
	}
	if plan.Status != model.PlanStatusActive {
		logger.Info().
			Str("plan_id", plan.ID).
			Str("status", plan.Status).
			Msg("skipping inactive plan trigger")
		return nil
	}

	return triggerPlan(ctx, repo, taskSvc, plan, logger)
}

func triggerPlan(ctx context.Context, repo repository.Repository, taskSvc *service.TaskService, plan *model.Plan, logger *zerolog.Logger) error {
	planLogger := logger.With().
		Str("plan_id", plan.ID).
		Str("type", plan.Type).
		Str("user_id", plan.UserID).
		Logger()

	task, err := taskSvc.CreateFromPlan(ctx, plan)
	if err != nil {
		return fmt.Errorf("create task from plan %s: %w", plan.ID, err)
	}

	if task == nil {
		planLogger.Info().Msg("plan task not created")
		return nil
	}

	planLogger.Info().Str("task_id", task.ID).Msg("task created from plan")

	nextRun, err := advancePlanNextRun(ctx, repo, plan)
	if err != nil {
		return fmt.Errorf("advance plan %s next_run_at: %w", plan.ID, err)
	}
	if nextRun != nil {
		planLogger.Info().Time("next_run", *nextRun).Msg("plan next_run_at advanced")
	}
	return nil
}

// advancePlanNextRun advances the plan's next_run_at to the next cron occurrence
// after the current next_run_at time.
func advancePlanNextRun(ctx context.Context, repo repository.Repository, plan *model.Plan) (*time.Time, error) {
	schedule, err := cron.ParseStandard(plan.CronExpr)
	if err != nil {
		return nil, fmt.Errorf("parse cron expression for plan %s: %w", plan.ID, err)
	}

	base := time.Now()
	if plan.NextRunAt != nil {
		base = *plan.NextRunAt
	}

	next := schedule.Next(base)
	won, err := repo.Plans().UpdateNextRunAtIf(ctx, plan.ID, &next, plan.NextRunAt)
	if err != nil {
		return nil, fmt.Errorf("update plan %s: %w", plan.ID, err)
	}
	if !won {
		return nil, nil
	}

	return &next, nil
}

func syncProjectRunningCounts(ctx context.Context, repo repository.Repository, taskSvc *service.TaskService, logger *zerolog.Logger) {
	if taskSvc.PubSub() == nil {
		return
	}
	projects, err := repo.Projects().ListActiveProjects(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to list projects for running task reconciliation")
		return
	}
	for _, project := range projects {
		count, err := repo.Tasks().CountRunningByProject(ctx, project.ID)
		if err != nil {
			logger.Warn().Err(err).Str("project_id", project.ID).Msg("failed to count running tasks")
			continue
		}
		if err := taskSvc.PubSub().SyncProjectCount(ctx, project.ID, count); err != nil {
			logger.Warn().Err(err).Str("project_id", project.ID).Msg("failed to sync running task count")
		}
	}
}
