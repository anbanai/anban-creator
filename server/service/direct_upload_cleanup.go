package service

import (
	"context"
	"errors"
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
				const batchSize = 100
				total := 0
				var cleanupErr error
				for {
					result := cleanupExpiredUploadSessionBatch(ctx, store, repo, time.Now(), batchSize)
					total += result.cleaned
					cleanupErr = errors.Join(cleanupErr, result.err, result.fatal)
					if result.fatal != nil {
						break
					}
					if result.candidates < batchSize || result.advanced == 0 || ctx.Err() != nil {
						break
					}
				}
				if cleanupErr != nil && logger != nil {
					logger.Warn().Err(cleanupErr).Int("cleaned", total).Msg("upload session cleanup failed")
				}
				if total > 0 && logger != nil {
					logger.Info().Int("count", total).Msg("cleaned expired upload sessions")
				}
			}
		}
	}()
}
