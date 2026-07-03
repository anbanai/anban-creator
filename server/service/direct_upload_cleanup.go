package service

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

func StartPendingUploadCleanup(ctx context.Context, store directUploadStorage, repo PendingUploadRepository, interval time.Duration, logger *zerolog.Logger) {
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
				cleaned, err := CleanupExpiredPendingUploads(ctx, store, repo, time.Now(), 100)
				if err != nil {
					if logger != nil {
						logger.Warn().Err(err).Msg("pending upload cleanup failed")
					}
					continue
				}
				if cleaned > 0 && logger != nil {
					logger.Info().Int("count", cleaned).Msg("cleaned expired pending uploads")
				}
			}
		}
	}()
}
