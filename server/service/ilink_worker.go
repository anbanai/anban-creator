package service

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type IlinkNotificationWorker struct {
	repo       repository.Repository
	sender     ilinkSender
	retryMax   int
	interval   time.Duration
	batchLimit int
	logger     *zerolog.Logger
}

func NewIlinkNotificationWorker(repo repository.Repository, sender ilinkSender, retryMax int, interval time.Duration, logger *zerolog.Logger) *IlinkNotificationWorker {
	if retryMax <= 0 {
		retryMax = 5
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &IlinkNotificationWorker{repo: repo, sender: sender, retryMax: retryMax, interval: interval, batchLimit: 50, logger: logger}
}

func (w *IlinkNotificationWorker) Run(ctx context.Context) {
	if w == nil || w.repo == nil || w.sender == nil {
		return
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		w.SendDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *IlinkNotificationWorker) SendDue(ctx context.Context) {
	items, err := w.repo.IlinkNotifications().ClaimDue(ctx, w.batchLimit, time.Now().Add(2*time.Minute))
	if err != nil {
		if w.logger != nil {
			w.logger.Warn().Err(err).Msg("list due ilink notifications failed")
		}
		return
	}
	for _, item := range items {
		w.sendOne(ctx, item)
	}
}

func (w *IlinkNotificationWorker) sendOne(ctx context.Context, item *model.IlinkNotification) {
	if item == nil {
		return
	}
	err := w.sender.SendText(ctx, item.PlatformAccountID, item.ExternalUserID, item.Body)
	if err == nil {
		_ = w.repo.IlinkNotifications().MarkDelivered(ctx, item.ID)
		return
	}
	attempts := item.Attempts + 1
	if attempts >= w.retryMax {
		_ = w.repo.IlinkNotifications().MarkFailed(ctx, item.ID, attempts, err.Error())
		return
	}
	delay := time.Duration(1<<min(attempts, 6)) * time.Second
	_ = w.repo.IlinkNotifications().MarkRetry(ctx, item.ID, attempts, time.Now().Add(delay), err.Error())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
