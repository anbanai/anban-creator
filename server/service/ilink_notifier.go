package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type IlinkNotifier struct {
	repo    repository.Repository
	enabled bool
	logger  *zerolog.Logger
	nowFunc func() time.Time
}

func NewIlinkNotifier(repo repository.Repository, enabled bool, logger *zerolog.Logger) *IlinkNotifier {
	return &IlinkNotifier{repo: repo, enabled: enabled, logger: logger, nowFunc: time.Now}
}

func (n *IlinkNotifier) Available() bool {
	return n != nil && n.enabled && n.repo != nil
}

func (n *IlinkNotifier) NotifyTerminal(ctx context.Context, task *model.Task, status, errMsg string) {
	if err := n.NotifyTerminalDurable(ctx, task, status, errMsg); err != nil && n.logger != nil {
		n.logger.Warn().Err(err).Str("task_id", task.ID).Msg("ilink terminal notification enqueue failed")
	}
}

func (n *IlinkNotifier) NotifyTerminalDurable(ctx context.Context, task *model.Task, status, errMsg string) error {
	if !n.Available() || task == nil {
		return nil
	}
	binding, err := n.repo.IlinkBindings().FindByUserID(ctx, task.UserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if binding == nil || binding.Status != model.IlinkBindingStatusActive {
		return nil
	}
	platformAccountID := stringValue(binding.PlatformAccountID)
	externalUserID := stringValue(binding.ExternalUserID)
	if platformAccountID == "" || externalUserID == "" {
		return nil
	}
	now := n.now()
	item := &model.IlinkNotification{
		ID:                uuid.NewString(),
		UserID:            task.UserID,
		TaskID:            task.ID,
		TaskStatus:        status,
		PlatformAccountID: platformAccountID,
		ExternalUserID:    externalUserID,
		Body:              formatTerminalMessage(task, status, errMsg),
		Status:            model.IlinkNotificationStatusPending,
		NextAttemptAt:     now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	return n.repo.IlinkNotifications().Enqueue(ctx, item)
}

func (n *IlinkNotifier) now() time.Time {
	if n != nil && n.nowFunc != nil {
		return n.nowFunc()
	}
	return time.Now()
}
