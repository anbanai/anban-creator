package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
)

func TestIlinkNotifier_EnqueuesTerminalNotificationIdempotently(t *testing.T) {
	repo := newIlinkTestRepo(t)
	userID := uuid.NewString()
	taskID := uuid.NewString()
	if err := repo.Users().Create(context.Background(), &model.User{
		ID:         userID,
		Email:      "notify@example.com",
		Password:   "x",
		InviteCode: "invite2",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.IlinkBindings().Create(context.Background(), &model.IlinkBinding{
		ID:                uuid.NewString(),
		UserID:            userID,
		PlatformAccountID: stringPtr("platform-1"),
		ExternalUserID:    stringPtr("wx-user-1"),
		Status:            model.IlinkBindingStatusActive,
	}); err != nil {
		t.Fatalf("create binding: %v", err)
	}

	log := zerolog.Nop()
	notifier := NewIlinkNotifier(repo, true, &log)
	task := &model.Task{ID: taskID, UserID: userID, Type: model.PlatformArticle, Prompt: "夏日防晒指南"}
	notifier.NotifyTerminal(context.Background(), task, model.TaskStatusCompleted, "")
	notifier.NotifyTerminal(context.Background(), task, model.TaskStatusCompleted, "")

	items, err := repo.IlinkNotifications().ListDue(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("outbox items = %d, want 1", len(items))
	}
	got := items[0]
	if got.TaskID != taskID || got.UserID != userID || got.Status != model.IlinkNotificationStatusPending {
		t.Fatalf("unexpected notification: %+v", got)
	}
	if got.PlatformAccountID != "platform-1" || got.ExternalUserID != "wx-user-1" {
		t.Fatalf("unexpected delivery target: %+v", got)
	}
}

func TestIlinkNotificationRepository_ClaimDueLeasesRows(t *testing.T) {
	repo := newIlinkTestRepo(t)
	now := time.Now()
	item := &model.IlinkNotification{
		ID:                uuid.NewString(),
		UserID:            uuid.NewString(),
		TaskID:            uuid.NewString(),
		TaskStatus:        model.TaskStatusCompleted,
		PlatformAccountID: "platform-1",
		ExternalUserID:    "wx-user-1",
		Body:              "done",
		Status:            model.IlinkNotificationStatusPending,
		NextAttemptAt:     now.Add(-time.Second),
	}
	if err := repo.IlinkNotifications().Enqueue(context.Background(), item); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	first, err := repo.IlinkNotifications().ClaimDue(context.Background(), 10, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("first ClaimDue: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first claim len = %d, want 1", len(first))
	}
	second, err := repo.IlinkNotifications().ClaimDue(context.Background(), 10, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("second ClaimDue: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second claim len = %d, want 0", len(second))
	}
}
