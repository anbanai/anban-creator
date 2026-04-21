package main

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/redis/go-redis/v9"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/scheduler"
	"github.com/royalrick/anbanwriter/server/service"
)

// workerInstances holds references to background workers that need cleanup.
type workerInstances struct {
	asynqServer *scheduler.TaskProcessor
	// cancellations holds cancel functions for background goroutines.
	schedulerCancel context.CancelFunc
	cleanupCancel   context.CancelFunc
}

// startWorkers starts all background workers (Asynq, plan checker, periodic cleanup).
func startWorkers(cfg *config.Config, core *coreServices, repo repository.Repository, rdb *redis.Client, log *zerolog.Logger) *workerInstances {
	w := &workerInstances{}

	// Start Asynq worker if Redis is available.
	if core.asynqClient != nil && core.taskSvc != nil {
		w.asynqServer = startAsynqServer(core.taskSvc, cfg, log)
	}

	// Start plan checker if repository and task service are available.
	if repo != nil && core.taskSvc != nil {
		schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
		w.schedulerCancel = schedulerCancel
		go scheduler.StartPlanChecker(schedulerCtx, repo, core.taskSvc, log, rdb)
	}

	// Start periodic workspace cleanup (every hour).
	if repo != nil && core.taskSvc != nil {
		cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
		w.cleanupCancel = cleanupCancel
		go startPeriodicCleanup(cleanupCtx, core.taskSvc, log)
	}

	return w
}

// startAsynqServer starts the Asynq task processor in a background goroutine.
func startAsynqServer(taskSvc *service.TaskService, cfg *config.Config, log *zerolog.Logger) *scheduler.TaskProcessor {
	srv := scheduler.NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error {
			return taskSvc.HandleExecutionFromPayload(ctx, taskID, userID)
		},
		func(ctx context.Context, planID string) error {
			log.Info().Str("plan_id", planID).Msg("plan trigger: placeholder")
			return nil
		},
		func(ctx context.Context) error {
			return taskSvc.CleanupExpiredWorkspaces(ctx)
		},
		cfg.Redis.Addr,
		cfg.Redis.Password,
		cfg.Redis.DB,
		cfg.Asynq.Concurrency,
		log,
	)

	go func() {
		log.Info().Int("concurrency", cfg.Asynq.Concurrency).Msg("starting Asynq task processor")
		if err := srv.Start(); err != nil {
			log.Error().Err(err).Msg("Asynq server start error")
		}
	}()

	return srv
}

// startPeriodicCleanup runs CleanupExpiredWorkspaces on a ticker until ctx is cancelled.
func startPeriodicCleanup(ctx context.Context, taskSvc *service.TaskService, log *zerolog.Logger) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Run once at startup.
	if err := taskSvc.CleanupExpiredWorkspaces(ctx); err != nil {
		log.Error().Err(err).Msg("initial workspace cleanup failed")
	}

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("periodic cleanup stopped")
			return
		case <-ticker.C:
			if err := taskSvc.CleanupExpiredWorkspaces(ctx); err != nil {
				log.Error().Err(err).Msg("periodic workspace cleanup failed")
			}
		}
	}
}
