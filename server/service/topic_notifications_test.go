package service

import (
	"context"
	"encoding/json"
	"net/url"
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

func TestQueueTopicDigestNotificationLinksUseConfiguredPublicBase(t *testing.T) {
	for _, tc := range []struct {
		name     string
		base     string
		wantLink bool
	}{
		{name: "missing public base"},
		{name: "configured public base", base: " https://studio.example.com/ ", wantLink: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := topicRadarTestRepository(t)
			ctx := context.Background()
			subscription := seedTopicRadarSubscription(t, repo)
			asset := model.NewContentAsset(subscription.UserID, "通知选题")
			if err := repo.ContentAssets().Create(ctx, asset); err != nil {
				t.Fatal(err)
			}
			digest := &model.TopicDigest{ID: "digest-link", UserID: subscription.UserID, ProjectID: subscription.ProjectID, Status: model.TopicDigestStatusSucceeded, OccurrenceKey: "notification-link", InputSnapshot: []byte(`{}`)}
			if err := repo.TopicDigests().Create(ctx, digest); err != nil {
				t.Fatal(err)
			}
			if err := repo.TopicSuggestions().CreateBatch(ctx, []*model.TopicSuggestion{{ID: "suggestion-link", DigestID: digest.ID, ProjectID: subscription.ProjectID, ContentAssetID: asset.ID, Title: asset.Title, Introduction: "解释", SourceRefs: []byte(`[]`), Scores: []byte(`{}`)}}); err != nil {
				t.Fatal(err)
			}
			channel := &model.TopicNotificationChannel{ID: "channel-link", UserID: subscription.UserID, ProjectID: subscription.ProjectID, Provider: model.TopicNotificationProviderFeishu, WebhookURL: "https://1.1.1.1/hook", Enabled: true}
			if err := repo.TopicNotificationChannels().Create(ctx, channel); err != nil {
				t.Fatal(err)
			}
			radar := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
			radar.SetPublicBaseURL(tc.base)
			deliveries, err := radar.QueueDigestNotifications(ctx, digest.ID)
			if err != nil || len(deliveries) != 1 {
				t.Fatalf("deliveries=%#v err=%v", deliveries, err)
			}
			var payload struct {
				Suggestions []topicNotificationPayload `json:"suggestions"`
			}
			if err := json.Unmarshal(deliveries[0].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Suggestions) != 1 {
				t.Fatalf("payload=%+v", payload)
			}
			item := payload.Suggestions[0]
			text := renderTopicNotificationText(payload.Suggestions)
			if !strings.Contains(text, asset.Title) || !strings.Contains(text, "解释") {
				t.Fatalf("notification content lost: %s", text)
			}
			if !tc.wantLink {
				if item.TopicURL != "" || strings.Contains(string(deliveries[0].Payload), "topic_url") || strings.Contains(text, "打开选题：") {
					t.Fatalf("unconfigured deployment emitted a link: %s", deliveries[0].Payload)
				}
				return
			}
			link, err := url.Parse(item.TopicURL)
			if err != nil {
				t.Fatal(err)
			}
			if link.Scheme != "https" || link.Host != "studio.example.com" || link.Path != "/topics" || link.Query().Get("asset_id") != asset.ID || link.Query().Get("project_id") != subscription.ProjectID || len(link.Query()) != 2 {
				t.Fatalf("incorrect canonical topic link: %s", item.TopicURL)
			}
			if !strings.Contains(text, "打开选题："+item.TopicURL) {
				t.Fatalf("configured link not rendered: %s", text)
			}
		})
	}
}
