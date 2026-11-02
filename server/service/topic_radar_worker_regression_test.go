package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
)

func TestTopicNotificationClaimPersistsAttemptBeforeNetwork(t *testing.T) {
	repo, delivery, _ := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook")
	ctx := context.Background()
	claimed, err := repo.TopicNotificationDeliveries().Claim(ctx, delivery.ID, "worker-1", time.Now().Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	saved, err := repo.TopicNotificationDeliveries().FindByID(ctx, delivery.ID)
	if err != nil || saved.Attempt != 1 {
		t.Fatalf("attempt must survive a worker crash: %+v %v", saved, err)
	}
}

func TestTopicNotificationRejectsCrossScopeDelivery(t *testing.T) {
	repo, delivery, svc := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook")
	ctx := context.Background()
	channel, err := repo.TopicNotificationChannels().FindByID(ctx, delivery.ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	channel.UserID = "another-user"
	if err := repo.TopicNotificationChannels().Update(ctx, channel); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0}`)), Request: req}, nil
	}))
	if err := svc.SendTopicNotification(ctx, delivery.ID); err == nil {
		t.Fatal("cross-scope delivery accepted")
	}
	saved, err := repo.TopicNotificationDeliveries().FindByID(ctx, delivery.ID)
	if err != nil || calls != 0 || saved.Status != model.TopicNotificationInvalid {
		t.Fatalf("scope failed: calls=%d delivery=%+v err=%v", calls, saved, err)
	}
}

func TestTopicDigestReconcilesTerminalOwnerWithoutExecution(t *testing.T) {
	for _, status := range []string{model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusCancelled} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			sub := seedTopicRadarSubscription(t, repo)
			if err := repo.Users().Create(ctx, &model.User{ID: sub.UserID, Email: "terminal@example.test"}); err != nil {
				t.Fatal(err)
			}
			logger := zerolog.New(io.Discard)
			tasks := newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, t.TempDir(), nil, nil)
			svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
			svc.SetTaskService(tasks)
			digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
			if err != nil {
				t.Fatal(err)
			}
			task, err := svc.EnsureTopicDigestManagedTask(ctx, digest.ID)
			if err != nil {
				t.Fatal(err)
			}
			task.Status = status
			if err := repo.Tasks().Update(ctx, task); err != nil {
				t.Fatal(err)
			}
			_ = svc.ProcessTopicDigest(ctx, digest.ID)
			saved, err := repo.TopicDigests().FindByID(ctx, digest.ID)
			if err != nil || saved.Status != model.TopicDigestStatusFailed || saved.ActiveKey != nil {
				t.Fatalf("terminal owner stranded digest: %+v %v", saved, err)
			}
		})
	}
}
