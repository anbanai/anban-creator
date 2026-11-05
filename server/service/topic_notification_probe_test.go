package service

import (
	"context"
	"github.com/anbanai/anban-creator/server/model"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTopicNotificationChannelProbeAuthenticatesAndChecksProviderAck(t *testing.T) {
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	svc := NewTopicRadarService(repo, nil, nil)
	ctx := context.Background()
	channel := &model.TopicNotificationChannel{ID: "probe", UserID: sub.UserID, ProjectID: sub.ProjectID, Name: "test", Provider: "feishu", WebhookURL: "https://1.1.1.1/hook", Enabled: false}
	if err := repo.TopicNotificationChannels().Create(ctx, channel); err != nil {
		t.Fatal(err)
	}
	calls := 0
	ack := `{"code":0}`
	svc.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(ack)), Header: make(http.Header)}, nil
	}))
	if err := svc.TestNotificationChannel(ctx, "another-user", channel.ID); err == nil || calls != 0 {
		t.Fatal("probe must authenticate before sending")
	}
	if err := svc.TestNotificationChannel(ctx, sub.UserID, channel.ID); err != nil || calls != 1 {
		t.Fatalf("probe failed: %v calls=%d", err, calls)
	}
	ack = `{"code":1}`
	if err := svc.TestNotificationChannel(ctx, sub.UserID, channel.ID); err == nil {
		t.Fatal("provider failure accepted")
	}
}
