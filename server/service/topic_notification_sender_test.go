package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type topicNotificationRoundTripper func(*http.Request) (*http.Response, error)

func (f topicNotificationRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func topicNotificationTestClient(transport http.RoundTripper) *http.Client {
	client := newTopicNotificationHTTPClient()
	client.Transport = transport
	return client
}

func seedTopicNotificationDelivery(t *testing.T, webhookURL string) (repository.Repository, *model.TopicNotificationDelivery, *TopicRadarService) {
	t.Helper()
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	channel := &model.TopicNotificationChannel{ID: "send-channel", UserID: "user-1", ProjectID: "project-1", Provider: model.TopicNotificationProviderFeishu, WebhookURL: webhookURL, Enabled: true}
	if err := repo.TopicNotificationChannels().Create(ctx, channel); err != nil {
		t.Fatal(err)
	}
	if err := repo.TopicDigests().Create(ctx, &model.TopicDigest{ID: "digest-1", UserID: channel.UserID, ProjectID: channel.ProjectID, Status: model.TopicDigestStatusSucceeded, OccurrenceKey: "send-digest", InputSnapshot: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	delivery := &model.TopicNotificationDelivery{ID: "send-delivery", DigestID: "digest-1", ChannelID: channel.ID, PayloadVersion: "v1", Payload: []byte(`{"suggestions":[{"title":"选题","introduction":"为什么值得拍","source_summary":"热榜","source_refs":[{"type":"trend","ref_id":"1"}] }]}`), Status: model.TopicNotificationPending}
	if err := repo.TopicNotificationDeliveries().CreateBatch(ctx, []*model.TopicNotificationDelivery{delivery}); err != nil {
		t.Fatal(err)
	}
	return repo, delivery, NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
}

func TestSendTopicNotificationRendersSourceAndIntroduction(t *testing.T) {
	var body []byte
	requests := 0
	repo, delivery, radar := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook")
	radar.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests++
		body, _ = io.ReadAll(req.Body)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0}`)), Request: req}, nil
	}))
	ctx := context.Background()
	if err := radar.SendTopicNotification(ctx, delivery.ID); err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Fatal("expected request body")
	}
	for _, want := range []string{"选题", "为什么值得拍", "来源：热榜", "- 1"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body %s is missing %q", body, want)
		}
	}
	got, _ := repo.TopicNotificationDeliveries().FindByID(ctx, delivery.ID)
	if got.Status != model.TopicNotificationSent {
		t.Fatalf("delivery = %#v", got)
	}
	if err := radar.SendTopicNotification(ctx, delivery.ID); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want idempotent single send", requests)
	}
}

func TestSendTopicNotificationAcceptsCaseInsensitiveProvider(t *testing.T) {
	_, _, radar := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook")
	radar.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0}`)), Request: req}, nil
	}))
	channel := &model.TopicNotificationChannel{Provider: "FEISHU", WebhookURL: "https://1.1.1.1/hook", Enabled: true}
	if _, err := radar.sendTopicNotificationText(context.Background(), channel, "test"); err != nil {
		t.Fatalf("send with uppercase provider: %v", err)
	}
}

func TestSendTopicNotificationRefusesRedirects(t *testing.T) {
	var requests atomic.Int32
	repo, delivery, radar := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook")
	radar.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://1.1.1.1/other"}},
			Body:       io.NopCloser(strings.NewReader("redirect")),
			Request:    req,
		}, nil
	}))

	err := radar.SendTopicNotification(context.Background(), delivery.ID)
	if err == nil {
		t.Fatal("expected redirect response to fail delivery")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("round trips = %d, want one with redirects disabled", got)
	}
	recorded, _ := repo.TopicNotificationDeliveries().FindByID(context.Background(), delivery.ID)
	if recorded.Status != model.TopicNotificationInvalid {
		t.Fatalf("status = %q, want invalid", recorded.Status)
	}
}

func TestSendTopicNotificationHonorsBoundedTimeout(t *testing.T) {
	_, delivery, radar := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook")
	radar.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}))
	radar.notificationClient.Timeout = 20 * time.Millisecond

	started := time.Now()
	err := radar.SendTopicNotification(context.Background(), delivery.ID)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

type topicNotificationTrackingReader struct {
	reader *strings.Reader
	read   int64
}

func (r *topicNotificationTrackingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

func TestSendTopicNotificationBoundsResponseBody(t *testing.T) {
	tracking := &topicNotificationTrackingReader{reader: strings.NewReader(strings.Repeat("x", topicNotificationMaxResponseBody*2))}
	_, delivery, radar := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook")
	radar.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(tracking), Request: req}, nil
	}))

	if err := radar.SendTopicNotification(context.Background(), delivery.ID); err == nil {
		t.Fatal("expected oversized response body to fail delivery")
	}
	if tracking.read > topicNotificationMaxResponseBody+1 {
		t.Fatalf("read %d bytes, want at most %d", tracking.read, topicNotificationMaxResponseBody+1)
	}
}

func TestSendTopicNotificationRejectsWeComPayloadOverLimit(t *testing.T) {
	requests := 0
	_, _, radar := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook")
	radar.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errcode":0}`)), Request: req}, nil
	}))
	channel := &model.TopicNotificationChannel{Provider: model.TopicNotificationProviderWeCom, WebhookURL: "https://1.1.1.1/hook", Enabled: true}
	status, err := radar.sendTopicNotificationText(context.Background(), channel, strings.Repeat("x", 4090))
	if err == nil || status != model.TopicNotificationInvalid {
		t.Fatalf("status = %q, err = %v; want invalid before sending", status, err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want none", requests)
	}
}

func TestSendTopicNotificationMarksProviderRejectionInvalid(t *testing.T) {
	repo, delivery, radar := seedTopicNotificationDelivery(t, "https://1.1.1.1/hook")
	radar.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":1}`)), Request: req}, nil
	}))
	if err := radar.SendTopicNotification(context.Background(), delivery.ID); err == nil {
		t.Fatal("expected provider rejection")
	}
	got, err := repo.TopicNotificationDeliveries().FindByID(context.Background(), delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.TopicNotificationInvalid || got.NextAttemptAt != nil {
		t.Fatalf("delivery = status %q next=%v, want terminal invalid", got.Status, got.NextAttemptAt)
	}
}

func TestSendTopicNotificationRetryClassification(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		status   int
		ack      string
		want     string
	}{
		{name: "feishu rate limit", provider: model.TopicNotificationProviderFeishu, status: http.StatusOK, ack: `{"code":11232}`, want: model.TopicNotificationFailed},
		{name: "dingtalk rate limit", provider: model.TopicNotificationProviderDingTalk, status: http.StatusOK, ack: `{"errcode":310003}`, want: model.TopicNotificationFailed},
		{name: "wecom rate limit", provider: model.TopicNotificationProviderWeCom, status: http.StatusOK, ack: `{"errcode":45009}`, want: model.TopicNotificationFailed},
		{name: "feishu invalid config", provider: model.TopicNotificationProviderFeishu, status: http.StatusOK, ack: `{"code":19001}`, want: model.TopicNotificationInvalid},
		{name: "wecom invalid config", provider: model.TopicNotificationProviderWeCom, status: http.StatusOK, ack: `{"errcode":93000}`, want: model.TopicNotificationInvalid},
		{name: "missing ack code", provider: model.TopicNotificationProviderFeishu, status: http.StatusOK, ack: `{}`, want: model.TopicNotificationFailed},
		{name: "http 429", provider: model.TopicNotificationProviderFeishu, status: http.StatusTooManyRequests, ack: `{}`, want: model.TopicNotificationFailed},
		{name: "http 500", provider: model.TopicNotificationProviderFeishu, status: http.StatusInternalServerError, ack: `{}`, want: model.TopicNotificationFailed},
		{name: "http 400", provider: model.TopicNotificationProviderFeishu, status: http.StatusBadRequest, ack: `{}`, want: model.TopicNotificationInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			radar := NewTopicRadarService(nil, nil, nil)
			radar.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tt.ack)), Request: req}, nil
			}))
			channel := &model.TopicNotificationChannel{Provider: tt.provider, WebhookURL: "https://1.1.1.1/hook", Enabled: true}
			status, err := radar.sendTopicNotificationText(context.Background(), channel, "content")
			if err == nil {
				t.Fatal("expected delivery error")
			}
			if status != tt.want {
				t.Fatalf("status = %q, want %q (error: %v)", status, tt.want, err)
			}
		})
	}
}

func TestWeComRenderedPayloadBoundary(t *testing.T) {
	for _, tt := range []struct {
		name   string
		length int
		want   string
	}{
		{name: "exact limit", length: 4082, want: model.TopicNotificationSent},
		{name: "one byte over", length: 4083, want: model.TopicNotificationInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			radar := NewTopicRadarService(nil, nil, nil)
			radar.notificationClient = topicNotificationTestClient(topicNotificationRoundTripper(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errcode":0}`)), Request: req}, nil
			}))
			channel := &model.TopicNotificationChannel{Provider: model.TopicNotificationProviderWeCom, WebhookURL: "https://1.1.1.1/hook", Enabled: true}
			status, err := radar.sendTopicNotificationText(context.Background(), channel, strings.Repeat("x", tt.length))
			if status != tt.want {
				t.Fatalf("status = %q, want %q (error: %v)", status, tt.want, err)
			}
		})
	}
}
