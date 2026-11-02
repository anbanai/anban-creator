package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type notificationRoundTripper func(*http.Request) (*http.Response, error)

func (f notificationRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBotSendersValidateConfigAndUseCallerTransport(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		newSender                 func(*http.Client) Sender
		ack                       string
		messageField, messageType string
	}{
		{"dingtalk", func(c *http.Client) Sender { return &DingTalkSender{HTTPClient: c} }, `{"errcode":0}`, "msgtype", "markdown"},
		{"feishu", func(c *http.Client) Sender { return &FeishuSender{HTTPClient: c} }, `{"code":0}`, "msg_type", "interactive"},
		{"wecom", func(c *http.Client) Sender { return &WeWorkSender{HTTPClient: c} }, `{"errcode":0}`, "msgtype", "markdown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: notificationRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body[tc.messageField] != tc.messageType {
					t.Fatalf("bad payload: %#v", body)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.ack)), Request: req}, nil
			})}
			sender := tc.newSender(client)
			content := &NotifyContent{Title: "title", Content: "body"}
			for _, cfg := range []string{`{}`, `null`, `{"webhook_key":" "}`, `invalid`} {
				if err := sender.Send(context.Background(), json.RawMessage(cfg), "", content); err == nil {
					t.Fatalf("invalid config accepted: %s", cfg)
				}
			}
			cfg := json.RawMessage(`{"webhook_key":"test-key"}`)
			if err := sender.Send(context.Background(), cfg, "", nil); err == nil {
				t.Fatal("nil content accepted")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := sender.Send(ctx, cfg, "", content); !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if calls != 0 {
				t.Fatalf("invalid input triggered %d requests", calls)
			}
			if err := sender.Send(context.Background(), cfg, "", content); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("requests=%d, want 1", calls)
			}
		})
	}
}

func TestBotSendersLeaveRetryOwnershipWithApplication(t *testing.T) {
	for _, newSender := range []func(*http.Client) Sender{
		func(c *http.Client) Sender { return &DingTalkSender{HTTPClient: c} },
		func(c *http.Client) Sender { return &FeishuSender{HTTPClient: c} },
		func(c *http.Client) Sender { return &WeWorkSender{HTTPClient: c} },
	} {
		calls := 0
		want := errors.New("transport unavailable")
		client := &http.Client{Transport: notificationRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, want })}
		sender := newSender(client)
		err := sender.Send(context.Background(), json.RawMessage(`{"webhook_key":"test-key"}`), "", &NotifyContent{Content: "body"})
		if !errors.Is(err, want) || calls != 1 {
			t.Fatalf("type=%v error=%v attempts=%d", sender.Type(), err, calls)
		}
		if strings.Contains(err.Error(), "test-key") {
			t.Fatalf("credential in error: %v", err)
		}
	}
}

func TestMarkdownBodyUsesProviderSDKContracts(t *testing.T) {
	for _, tc := range []struct {
		typ         NotifyType
		field, kind string
	}{
		{NotifyTypeLark, "msg_type", "interactive"},
		{NotifyTypeWeWork, "msgtype", "markdown"},
		{NotifyTypeDingTalk, "msgtype", "markdown"},
	} {
		body, err := MarkdownBody(tc.typ, "选题雷达", "# 选题雷达\n选题正文")
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if got[tc.field] != tc.kind {
			t.Fatalf("wrong provider contract: %s", body)
		}
		if !strings.Contains(string(body), "选题正文") {
			t.Fatalf("missing content: %s", body)
		}
	}
	if _, err := MarkdownBody(NotifyTypeNone, "title", "text"); err == nil {
		t.Fatal("unknown provider accepted")
	}
}
