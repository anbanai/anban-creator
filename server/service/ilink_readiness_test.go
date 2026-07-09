package service

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/wcf"
)

type staticReadiness bool

func (r staticReadiness) Ready() bool { return bool(r) }

type fakeIlinkEventClient struct {
	calls int
}

func (f *fakeIlinkEventClient) ListEvents(context.Context, int64, int) ([]wcf.Event, error) {
	f.calls++
	return []wcf.Event{{ID: 7, AccountID: "acct", Direction: "outbound", EventType: "text"}}, nil
}

func TestIlinkPollerSkipsSidecarWhenNotReady(t *testing.T) {
	client := &fakeIlinkEventClient{}
	poller := NewIlinkPoller(client, nil, nil, time.Second, nil)
	poller.SetReadiness(staticReadiness(false))

	if got := poller.prime(context.Background()); got != 0 {
		t.Fatalf("prime returned %d, want 0", got)
	}
	if err := poller.poll(context.Background()); err != nil {
		t.Fatalf("poll returned error: %v", err)
	}
	if client.calls != 0 {
		t.Fatalf("ListEvents calls = %d, want 0", client.calls)
	}
}

func TestIlinkPollerCallsSidecarWhenReady(t *testing.T) {
	client := &fakeIlinkEventClient{}
	poller := NewIlinkPoller(client, nil, nil, time.Second, nil)
	poller.SetReadiness(staticReadiness(true))

	if got := poller.prime(context.Background()); got != 7 {
		t.Fatalf("prime returned %d, want 7", got)
	}
	if client.calls != 1 {
		t.Fatalf("ListEvents calls = %d, want 1", client.calls)
	}
}

type fakeIlinkNotificationRepo struct {
	claimCalls int
}

func (r *fakeIlinkNotificationRepo) Enqueue(context.Context, *model.IlinkNotification) error {
	return nil
}

func (r *fakeIlinkNotificationRepo) ListDue(context.Context, int) ([]*model.IlinkNotification, error) {
	return nil, nil
}

func (r *fakeIlinkNotificationRepo) ClaimDue(context.Context, int, time.Time) ([]*model.IlinkNotification, error) {
	r.claimCalls++
	return nil, nil
}

func (r *fakeIlinkNotificationRepo) MarkDelivered(context.Context, string) error {
	return nil
}

func (r *fakeIlinkNotificationRepo) MarkRetry(context.Context, string, int, time.Time, string) error {
	return nil
}

func (r *fakeIlinkNotificationRepo) MarkFailed(context.Context, string, int, string) error {
	return nil
}

type fakeReadyIlinkSender struct {
	calls int
}

func (s *fakeReadyIlinkSender) SendText(context.Context, string, string, string) error {
	s.calls++
	return nil
}

func TestIlinkNotificationWorkerSkipsOutboxWhenNotReady(t *testing.T) {
	notifications := &fakeIlinkNotificationRepo{}
	worker := NewIlinkNotificationWorkerWithStore(notifications, &fakeReadyIlinkSender{}, 3, time.Second, nil)
	worker.SetReadiness(staticReadiness(false))

	worker.SendDue(context.Background())

	if notifications.claimCalls != 0 {
		t.Fatalf("ClaimDue calls = %d, want 0", notifications.claimCalls)
	}
}

func TestIlinkNotificationWorkerClaimsOutboxWhenReady(t *testing.T) {
	notifications := &fakeIlinkNotificationRepo{}
	worker := NewIlinkNotificationWorkerWithStore(notifications, &fakeReadyIlinkSender{}, 3, time.Second, nil)
	worker.SetReadiness(staticReadiness(true))

	worker.SendDue(context.Background())

	if notifications.claimCalls != 1 {
		t.Fatalf("ClaimDue calls = %d, want 1", notifications.claimCalls)
	}
}

func TestIlinkReadinessHelpersTreatNilReceiversAsNotReady(t *testing.T) {
	var poller *IlinkPoller
	if poller.ready() {
		t.Fatal("nil poller ready = true, want false")
	}
	var worker *IlinkNotificationWorker
	if worker.ready() {
		t.Fatal("nil worker ready = true, want false")
	}
	worker.SendDue(context.Background())
}
