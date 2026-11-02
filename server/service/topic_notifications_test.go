package service

import (
	"context"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestQueueTopicDigestNotificationsSnapshotsSourcesAndDeduplicates(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	channel := &model.TopicNotificationChannel{ID: "channel-1", UserID: subscription.UserID, ProjectID: subscription.ProjectID, Provider: model.TopicNotificationProviderFeishu, Name: "团队飞书", WebhookURL: "https://1.1.1.1/hook", Enabled: true}
	if err := repo.TopicNotificationChannels().Create(ctx, channel); err != nil {
		t.Fatal(err)
	}
	radar := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := radar.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-notify"})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	if err := radar.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"通知选题","introduction":"解释","source_summary":"飞书热榜","source_refs":[{"type":"trend","platform":"douyin","ref_id":"hot-1","title":"热榜"}],"scores":{},"video_fit":"口播"}]}`))); err != nil {
		t.Fatal(err)
	}
	first, err := radar.QueueDigestNotifications(ctx, digest.ID)
	if err != nil || len(first) != 1 {
		t.Fatalf("first deliveries = %d, err=%v", len(first), err)
	}
	second, err := radar.QueueDigestNotifications(ctx, digest.ID)
	if err != nil || len(second) != 1 || second[0].ID != first[0].ID {
		t.Fatalf("second deliveries = %#v, err=%v", second, err)
	}
	if len(first[0].Payload) == 0 || first[0].PayloadVersion == "" {
		t.Fatalf("delivery payload was not snapshotted: %#v", first[0])
	}
	for _, want := range []string{"通知选题", "解释", "飞书热榜", "hot-1"} {
		if !strings.Contains(string(first[0].Payload), want) {
			t.Fatalf("payload %s is missing %q", first[0].Payload, want)
		}
	}
}
