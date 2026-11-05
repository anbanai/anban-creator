package service

import (
	"context"
	"errors"
	"github.com/anbanai/anban-creator/server/model"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTopicDigestCompletionEffectsRecoverFromDurableSuccess(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	sub.ScriptPolicy = []byte(`{"enabled":true,"top_n":1,"platform":"douyin"}`)
	if err := repo.TopicSubscriptions().Update(ctx, sub); err != nil {
		t.Fatal(err)
	}
	svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
	if err != nil {
		t.Fatal(err)
	}
	digest.Status = model.TopicDigestStatusSucceeded
	digest.ActiveKey = nil
	if err := repo.TopicDigests().Update(ctx, digest); err != nil {
		t.Fatal(err)
	}
	asset := model.NewContentAsset(sub.UserID, "topic")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	suggestion := &model.TopicSuggestion{ContentAssetID: asset.ID, ID: "effects-suggestion", DigestID: digest.ID, ProjectID: sub.ProjectID, Title: "topic", Introduction: "intro", SourceSummary: "source", SourceRefs: []byte(`[]`), Scores: []byte(`{}`), Rank: 1, AdoptionStatus: model.TopicAdoptionPending}
	if err := repo.TopicSuggestions().CreateBatch(ctx, []*model.TopicSuggestion{suggestion}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileTopicDigestEffects(ctx, 100); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.TopicDigests().FindByID(ctx, digest.ID)
	if err != nil || saved.EffectsQueuedAt == nil {
		t.Fatalf("completion effects not settled: %+v %v", saved, err)
	}
	jobs, err := repo.TopicScriptJobs().ListByDigest(ctx, digest.ID)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("auto script jobs=%d %v", len(jobs), err)
	}
	if err := svc.ReconcileTopicDigestEffects(ctx, 100); err != nil {
		t.Fatal(err)
	}
	again, _ := repo.TopicScriptJobs().ListByDigest(ctx, digest.ID)
	if len(again) != 1 {
		t.Fatal("replay duplicated script jobs")
	}
}

func TestTopicNotificationRecoveryClaimsFailedDelivery(t *testing.T) {
	repo, delivery, _ := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook")
	delivery.Status = model.TopicNotificationFailed
	if err := repo.TopicNotificationDeliveries().Update(context.Background(), delivery); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.TopicNotificationDeliveries().Claim(context.Background(), delivery.ID, "new-lease", time.Now().Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("failed delivery must be reclaimable: %v %v", claimed, err)
	}
}

func TestTopicNotificationRejectsProviderFailureAndRedactsTransportSecrets(t *testing.T) {
	for _, test := range []struct {
		name, body     string
		transportError bool
	}{
		{name: "provider", body: `{"code":19021,"msg":"invalid token"}`},
		{name: "malformed", body: `{}`},
		{name: "transport", transportError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, delivery, svc := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook?token=private-test-value")
			svc.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
				if test.transportError {
					return nil, &url.Error{Op: "Post", URL: req.URL.String(), Err: errors.New("network unavailable")}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.body)), Request: req}, nil
			}))
			err := svc.SendTopicNotification(context.Background(), delivery.ID)
			if err == nil {
				t.Fatal("provider failure must not settle sent")
			}
			saved, findErr := repo.TopicNotificationDeliveries().FindByID(context.Background(), delivery.ID)
			if findErr != nil {
				t.Fatal(findErr)
			}
			if saved.Status == model.TopicNotificationSent || strings.Contains(saved.LastError, "private-test-value") || strings.Contains(err.Error(), "private-test-value") {
				t.Fatalf("unsafe settlement: %s %s / %v", saved.Status, saved.LastError, err)
			}
		})
	}
}
