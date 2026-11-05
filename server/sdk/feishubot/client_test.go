package feishubot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type feishuRoundTripFunc func(*http.Request) (*http.Response, error)

func (f feishuRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func feishuResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestSendMessageContextAcceptsModernAndLegacyAcknowledgements(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "modern", body: `{"code":0,"msg":"success"}`},
		{name: "legacy", body: `{"StatusCode":0,"StatusMessage":"success"}`},
		{name: "legacy snake case", body: `{"status_code":0,"status_message":"success"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient("webhook", WithHTTPClient(&http.Client{
				Transport: feishuRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return feishuResponse(http.StatusOK, test.body), nil
				}),
			}))
			if _, err := client.SendMessageContext(context.Background(), NewTextMessage("hello")); err != nil {
				t.Fatalf("SendMessageContext() error = %v", err)
			}
		})
	}
}

func TestSendMessageContextRedactsWebhookIDFromTransportError(t *testing.T) {
	const webhookID = "sensitive-webhook-id"
	transportFailure := errors.New("dial failed")
	client := NewClient(webhookID, WithHTTPClient(&http.Client{
		Transport: feishuRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportFailure
		}),
	}))

	_, err := client.SendMessageContext(context.Background(), NewTextMessage("hello"))
	if err == nil {
		t.Fatal("SendMessageContext() error = nil, want transport error")
	}
	if strings.Contains(err.Error(), webhookID) {
		t.Fatalf("transport error leaked webhook ID: %v", err)
	}
	if !errors.Is(err, transportFailure) {
		t.Fatalf("transport error identity was not preserved: %v", err)
	}
}

func TestSendMessageContextRejectsMissingAndFailedAcknowledgements(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing acknowledgement", body: `{}`},
		{name: "modern failure", body: `{"code":19001,"msg":"invalid webhook"}`},
		{name: "legacy failure", body: `{"StatusCode":19001,"StatusMessage":"invalid webhook"}`},
		{name: "conflicting acknowledgements", body: `{"code":0,"StatusCode":19001}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient("webhook", WithHTTPClient(&http.Client{
				Transport: feishuRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return feishuResponse(http.StatusOK, test.body), nil
				}),
			}))
			if _, err := client.SendMessageContext(context.Background(), NewTextMessage("hello")); err == nil {
				t.Fatal("SendMessageContext() error = nil, want acknowledgement error")
			}
		})
	}
}

func TestSendMessageContextRejectsHTTPFailure(t *testing.T) {
	client := NewClient("webhook", WithHTTPClient(&http.Client{
		Transport: feishuRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return feishuResponse(http.StatusBadGateway, `{"code":0}`), nil
		}),
	}))

	if _, err := client.SendMessageContext(context.Background(), NewTextMessage("hello")); err == nil {
		t.Fatal("SendMessageContext() error = nil, want HTTP error")
	}
}

func TestSendMessageContextPropagatesCancellation(t *testing.T) {
	client := NewClient("webhook", WithHTTPClient(&http.Client{
		Transport: feishuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.SendMessageContext(ctx, NewTextMessage("hello"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SendMessageContext() error = %v, want context.Canceled", err)
	}
}

func TestNewClientHasFiniteDefaultTimeout(t *testing.T) {
	client := NewClient("webhook")
	if client.httpClient.Timeout() <= 0 {
		t.Fatal("default Resty timeout is not finite")
	}
}
