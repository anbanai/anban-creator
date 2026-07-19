package service

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

const (
	defaultBillingOutboxInterval = 5 * time.Second
	defaultBillingExpiryInterval = 5 * time.Minute
	defaultBillingBatchSize      = 100
	defaultBillingMaxBatches     = 10
)

type billingMaintenanceWallet interface {
	processSettlementOutboxBatch(ctx context.Context, limit int) (processed, claimed int, err error)
	ExpirePromotionalCredits(ctx context.Context, now time.Time, limit int) (int, error)
}

type BillingMaintenanceWorkerOptions struct {
	OutboxInterval   time.Duration
	ExpiryInterval   time.Duration
	OutboxBatchSize  int
	ExpiryBatchSize  int
	MaxBatchesPerRun int
	Now              func() time.Time
}

type BillingMaintenanceWorker struct {
	wallet           billingMaintenanceWallet
	outboxInterval   time.Duration
	expiryInterval   time.Duration
	outboxBatchSize  int
	expiryBatchSize  int
	maxBatchesPerRun int
	now              func() time.Time
	logger           *zerolog.Logger
}

func NewBillingMaintenanceWorker(wallet billingMaintenanceWallet, opts BillingMaintenanceWorkerOptions, logger *zerolog.Logger) *BillingMaintenanceWorker {
	if opts.OutboxInterval <= 0 {
		opts.OutboxInterval = defaultBillingOutboxInterval
	}
	if opts.ExpiryInterval <= 0 {
		opts.ExpiryInterval = defaultBillingExpiryInterval
	}
	if opts.OutboxBatchSize <= 0 {
		opts.OutboxBatchSize = defaultBillingBatchSize
	}
	if opts.ExpiryBatchSize <= 0 {
		opts.ExpiryBatchSize = defaultBillingBatchSize
	}
	if opts.MaxBatchesPerRun <= 0 {
		opts.MaxBatchesPerRun = defaultBillingMaxBatches
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &BillingMaintenanceWorker{
		wallet: wallet, outboxInterval: opts.OutboxInterval, expiryInterval: opts.ExpiryInterval,
		outboxBatchSize: opts.OutboxBatchSize, expiryBatchSize: opts.ExpiryBatchSize,
		maxBatchesPerRun: opts.MaxBatchesPerRun, now: opts.Now, logger: logger,
	}
}

func (w *BillingMaintenanceWorker) Run(ctx context.Context) {
	if w == nil || w.wallet == nil || ctx == nil {
		return
	}
	outboxTicker := time.NewTicker(w.outboxInterval)
	expiryTicker := time.NewTicker(w.expiryInterval)
	defer outboxTicker.Stop()
	defer expiryTicker.Stop()

	w.runExpiry(ctx)
	w.runOutbox(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-outboxTicker.C:
			w.runOutbox(ctx)
		case <-expiryTicker.C:
			w.runExpiry(ctx)
		}
	}
}

func (w *BillingMaintenanceWorker) runOutbox(ctx context.Context) {
	for batch := 0; batch < w.maxBatchesPerRun; batch++ {
		if ctx.Err() != nil {
			return
		}
		_, claimed, err := w.wallet.processSettlementOutboxBatch(ctx, w.outboxBatchSize)
		if err != nil {
			if w.logger != nil && ctx.Err() == nil {
				w.logger.Warn().Err(err).Msg("billing settlement maintenance failed")
			}
			return
		}
		if claimed < w.outboxBatchSize {
			return
		}
	}
}

func (w *BillingMaintenanceWorker) runExpiry(ctx context.Context) {
	for batch := 0; batch < w.maxBatchesPerRun; batch++ {
		if ctx.Err() != nil {
			return
		}
		expired, err := w.wallet.ExpirePromotionalCredits(ctx, w.now().UTC(), w.expiryBatchSize)
		if err != nil {
			if w.logger != nil && ctx.Err() == nil {
				w.logger.Warn().Err(err).Msg("billing promotion expiry maintenance failed")
			}
			return
		}
		if expired < w.expiryBatchSize {
			return
		}
	}
}
