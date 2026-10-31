package notification

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestSendUnconfiguredReturnsClearError(t *testing.T) {
	svc := NewService(NewSenderRegistry())
	for _, channel := range []*Channel{nil, {Type: NotifyTypeNone, Enabled: true}} {
		if err := svc.Send(context.Background(), channel, &NotifyContent{Title: "t", Content: "c"}); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("Send(...) = %v, want ErrNotConfigured", err)
		}
	}
}

type fakeSender struct {
	typ        NotifyType
	gotCfg     json.RawMessage
	gotRecv    string
	gotContent *NotifyContent
	gotError   error
	calls      int
}

func (f *fakeSender) Send(_ context.Context, config json.RawMessage, recipient string, content *NotifyContent) error {
	f.gotCfg, f.gotRecv, f.gotContent = config, recipient, content
	f.calls++
	return f.gotError
}
func (f *fakeSender) Type() NotifyType { return f.typ }

func TestSendUsesAuthorizedChannelConfig(t *testing.T) {
	cfg := json.RawMessage(`{"webhook_key":"channel-config"}`)
	channel := &Channel{Type: NotifyTypeWeWork, Config: cfg, Enabled: true}
	sender := &fakeSender{typ: NotifyTypeWeWork}
	registry := NewSenderRegistry()
	registry.Register(sender)
	content := &NotifyContent{Title: "title", Content: "body"}
	svc := NewService(registry)
	if err := svc.SendByChannel(context.Background(), channel, "recipient", content); err != nil {
		t.Fatal(err)
	}
	if string(sender.gotCfg) != string(cfg) || sender.gotRecv != "recipient" || sender.gotContent != content || sender.calls != 1 {
		t.Fatalf("incorrect dispatch: %+v", sender)
	}
	sentinel := errors.New("provider failed")
	sender.gotError = sentinel
	if err := svc.Send(context.Background(), channel, content); !errors.Is(err, sentinel) {
		t.Fatalf("lost sender error: %v", err)
	}
}

func TestSendRejectsInvalidInputBeforeDelivery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		channel *Channel
		content *NotifyContent
		cancel  bool
	}{
		{"disabled", &Channel{Type: NotifyTypeWeWork}, &NotifyContent{Content: "body"}, false},
		{"nil content", &Channel{Type: NotifyTypeWeWork, Enabled: true}, nil, false},
		{"cancelled", &Channel{Type: NotifyTypeWeWork, Enabled: true}, &NotifyContent{Content: "body"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := &fakeSender{typ: NotifyTypeWeWork}
			registry := NewSenderRegistry()
			registry.Register(sender)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			err := NewService(registry).Send(ctx, tc.channel, tc.content)
			if err == nil || sender.calls != 0 {
				t.Fatalf("err=%v deliveries=%d", err, sender.calls)
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
	if err := NewService(nil).Send(context.Background(), &Channel{Type: NotifyTypeWeWork, Enabled: true}, &NotifyContent{}); err == nil {
		t.Fatal("missing sender must fail")
	}
}
