package service

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/repository"
	"github.com/rs/zerolog"
)

func StartUploadSessionCleanup(ctx context.Context, store DirectUploadFinalizationStorage, repo repository.Repository, interval time.Duration, logger *zerolog.Logger) {
	if store == nil || repo == nil {
		return
	}
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleaned, err := CleanupExpiredUploadSessions(ctx, store, repo, time.Now(), 100)
				if err != nil {
					if logger != nil {
						logger.Warn().Err(err).Msg("upload session cleanup failed")
					}
					continue
				}
				if cleaned > 0 && logger != nil {
					logger.Info().Int("count", cleaned).Msg("cleaned expired upload sessions")
				}
			}
		}
	}()
}
