package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
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
	checkAndDispatchPendingTasks(ctx, repo, taskSvc, logger)
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
			Str("channel_id", t.ChannelID).
			Str("stale_duration", staleDuration.Round(time.Second).String()).
			Msg(errMsg)

		swapped, err := repo.Tasks().CompareAndSwapStatusAndError(ctx, t.ID, model.TaskStatusRunning, model.TaskStatusFailed, errMsg)
		if err != nil {
			logger.Error().Err(err).Str("task_id", t.ID).Msg("failed to mark stuck task as failed")
			continue
		}
		if !swapped {
			logger.Warn().Str("task_id", t.ID).Msg("stuck task already resolved, skipping")
			continue
		}
		if err := repo.Tasks().SetCompletedAt(ctx, t.ID); err != nil {
			logger.Error().Err(err).Str("task_id", t.ID).Msg("failed to set completed_at on reaped task")
		}

		if err := taskSvc.RefundForTask(ctx, t.ID); err != nil {
			logger.Error().Err(err).Str("task_id", t.ID).Msg("failed to refund credits for reaped task")
		}

		if t.ChannelID != "" {
			if err := taskSvc.DispatchPendingTasks(ctx, t.ChannelID); err != nil {
				logger.Warn().Err(err).Str("channel_id", t.ChannelID).Msg("failed to dispatch pending tasks after reaping stuck task")
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
		planLogger := logger.With().
			Str("plan_id", plan.ID).
			Str("type", plan.Type).
			Str("user_id", plan.UserID).
			Logger()

		// Create a task linked to this plan.
		task, err := taskSvc.CreateFromPlan(ctx, plan)
		if err != nil {
			planLogger.Error().Err(err).Msg("failed to create task from plan")
			continue
		}

		planLogger.Info().Str("task_id", task.ID).Msg("task created from plan")

		// Advance next_run_at to the next cron occurrence.
		nextRun, err := advancePlanNextRun(ctx, repo, plan)
		if err != nil {
			planLogger.Error().Err(err).Msg("failed to advance plan next_run_at")
		} else if nextRun != nil {
			planLogger.Info().Time("next_run", *nextRun).Msg("plan next_run_at advanced")
		}
	}
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
	plan.NextRunAt = &next

	if err := repo.Plans().Update(ctx, plan); err != nil {
		return nil, fmt.Errorf("update plan %s: %w", plan.ID, err)
	}

	return &next, nil
}

// checkAndDispatchPendingTasks finds all channels that have pending tasks
// and available concurrency slots, then enqueues them.
// This serves as a fallback for cases where the post-completion dispatch
// was missed (e.g., server restart, crash).
func checkAndDispatchPendingTasks(ctx context.Context, repo repository.Repository, taskSvc *service.TaskService, logger *zerolog.Logger) {
	// Find all active channels.
	channels, err := repo.Channels().ListActiveChannels(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to list channels for pending task dispatch")
		return
	}

	for _, ch := range channels {
		if err := taskSvc.DispatchPendingTasks(ctx, ch.ID); err != nil {
			logger.Warn().Err(err).Str("channel_id", ch.ID).Msg("failed to dispatch pending tasks")
		}
	}
}
