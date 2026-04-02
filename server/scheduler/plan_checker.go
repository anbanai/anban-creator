package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

// checkInterval is how often the plan checker runs.
const checkInterval = 1 * time.Minute

// StartPlanChecker runs a background goroutine that checks for plans whose
// next_run_at has passed and triggers content generation tasks for them.
// It runs on a 1-minute ticker and stops when the provided context is cancelled.
func StartPlanChecker(ctx context.Context, repo repository.Repository, taskSvc *service.TaskService, logger *zerolog.Logger) {
	logger.Info().Dur("interval", checkInterval).Msg("starting plan checker")

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	// Run once immediately on start.
	checkAndTriggerPlans(ctx, repo, taskSvc, logger)

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("plan checker stopped")
			return
		case <-ticker.C:
			checkAndTriggerPlans(ctx, repo, taskSvc, logger)
		}
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
