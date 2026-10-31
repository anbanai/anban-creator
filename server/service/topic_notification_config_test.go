package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestCreateTopicNotificationChannelRejectsUnknownProviderAndStoresSecretServerSide(t *testing.T) {
	repo := topicRadarTestRepository(t)
	rad := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	if _, err := rad.CreateNotificationChannel(ctx, CreateTopicNotificationChannelParams{UserID: subscription.UserID, ProjectID: subscription.ProjectID, Provider: "unknown", Name: "x", WebhookURL: "https://1.1.1.1/hook"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
	item, err := rad.CreateNotificationChannel(ctx, CreateTopicNotificationChannelParams{UserID: subscription.UserID, ProjectID: subscription.ProjectID, Provider: model.TopicNotificationProviderWeCom, Name: "企业微信", WebhookURL: "https://1.1.1.1/hook"})
	if err != nil {
		t.Fatal(err)
	}
	if item.WebhookURL != "https://1.1.1.1/hook" {
		t.Fatal("webhook should persist for delivery")
	}
}

func TestCreateTopicNotificationChannelRejectsUnsafeWebhookURLs(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{name: "http scheme", url: "http://1.1.1.1/hook"},
		{name: "credentials", url: "https://user:secret@1.1.1.1/hook"},
		{name: "fragment", url: "https://1.1.1.1/hook#secret"},
		{name: "file scheme", url: "file:///etc/passwd"},
		{name: "localhost", url: "https://localhost/hook"},
		{name: "loopback", url: "https://127.0.0.1/hook"},
		{name: "private network", url: "https://10.0.0.1/hook"},
		{name: "metadata address", url: "https://169.254.169.254/latest/meta-data"},
		{name: "ipv6 loopback", url: "https://[::1]/hook"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := topicRadarTestRepository(t)
			subscription := seedTopicRadarSubscription(t, repo)
			rad := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
			_, err := rad.CreateNotificationChannel(context.Background(), CreateTopicNotificationChannelParams{
				UserID: subscription.UserID, ProjectID: subscription.ProjectID,
				Provider: model.TopicNotificationProviderFeishu, Name: tc.name, WebhookURL: tc.url,
			})
			if err == nil {
				t.Fatalf("expected webhook URL %q to be rejected", tc.url)
			}
		})
	}
}
