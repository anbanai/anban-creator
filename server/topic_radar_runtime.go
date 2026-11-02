package main

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/rs/zerolog"
)

// Each durable queue has its own bounded poller so a slow webhook cannot delay
// schedules or automatic TopicPool adoption. Database claims permit replicas.
func startTopicRadarLoops(ctx context.Context, svc *service.TopicRadarService, log *zerolog.Logger) {
	workers := []struct {
		name string
		run  func(context.Context, int) error
	}{
		{"schedule", svc.ScheduleTopicDigests},
		{"dispatch", svc.RecoverTopicRadarDispatches},
		{"adoption", svc.ReconcileTopicAdoptions},
		{"completion_effects", svc.ReconcileTopicDigestEffects},
		{"notification", svc.ReconcileTopicNotifications},
	}
	for _, worker := range workers {
		go func(name string, run func(context.Context, int) error) {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				batchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				err := run(batchCtx, 100)
				cancel()
				if err != nil && ctx.Err() == nil {
					log.Warn().Err(err).Str("worker", name).Msg("topic radar recovery will retry")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}(worker.name, worker.run)
	}
}
