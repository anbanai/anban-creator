package notification

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type recordingEnqueuer struct {
	device, vx, receiver, text string
	calls                      int
	err                        error
}

func (e *recordingEnqueuer) EnqueueVxActionText(_ context.Context, device, vx, receiver, text string) error {
	e.device, e.vx, e.receiver, e.text = device, vx, receiver, text
	e.calls++
	return e.err
}

func TestWeChatSenderUsesCallerAuthorizedDevice(t *testing.T) {
	for _, tc := range []struct {
		typ      NotifyType
		receiver string
	}{{NotifyTypeWeChatContact, "contact"}, {NotifyTypeWeChatRoom, "group@chatroom"}} {
		t.Run(tc.receiver, func(t *testing.T) {
			enq := &recordingEnqueuer{}
			ctx := context.Background()
			sender := NewWeChatSender(func(got context.Context, id string) (string, error) {
				if got != ctx || id != "device" {
					t.Fatalf("incorrect device lookup %q", id)
				}
				return "authorized-vx", nil
			}, enq, tc.typ)
			err := sender.Send(ctx, json.RawMessage(`{"device_id":"device","contact_id":"contact","room_id":"group@chatroom"}`), "ignored", &NotifyContent{Title: "title", Content: "body"})
			if err != nil {
				t.Fatal(err)
			}
			if enq.calls != 1 || enq.device != "device" || enq.vx != "authorized-vx" || enq.receiver != tc.receiver || enq.text != "title\nbody" {
				t.Fatalf("incorrect enqueue: %+v", enq)
			}
		})
	}
}

func TestWeChatSenderRejectsMissingAdapterAndDeviceErrors(t *testing.T) {
	cfg := json.RawMessage(`{"device_id":"device","contact_id":"contact"}`)
	content := &NotifyContent{Content: "body"}
	if err := NewWeChatSender(nil, nil, NotifyTypeWeChatContact).Send(context.Background(), cfg, "", content); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("missing adapter: %v", err)
	}
	want := errors.New("device not accessible")
	enq := &recordingEnqueuer{}
	sender := NewWeChatSender(func(context.Context, string) (string, error) { return "", want }, enq, NotifyTypeWeChatContact)
	if err := sender.Send(context.Background(), cfg, "", content); !errors.Is(err, want) || enq.calls != 0 {
		t.Fatalf("err=%v enqueued=%d", err, enq.calls)
	}
	if err := sender.Send(context.Background(), cfg, "", nil); err == nil {
		t.Fatal("nil content accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sender.Send(ctx, cfg, "", content); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}
