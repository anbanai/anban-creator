package robot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type robotRoundTripFunc func(*http.Request) (*http.Response, error)

func (f robotRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func robotResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type writerToFunc func(io.Writer) (int64, error)

func (f writerToFunc) WriteTo(w io.Writer) (int64, error) {
	return f(w)
}

func TestSendMessageContextUsesInjectedClientAndPropagatesContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	client := NewRobot("secret", WithHTTPClient(&http.Client{
		Transport: robotRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Query().Get("key") != "secret" {
				t.Fatalf("key query parameter = %q, want secret", req.URL.Query().Get("key"))
			}
			if req.Context().Value(struct{}{}) != "marker" {
				t.Fatal("request context value was not propagated")
			}
			return robotResponse(http.StatusOK, `{"errcode":0,"errmsg":"ok"}`), nil
		}),
	}))

	if _, err := client.SendMessageContext(ctx, map[string]any{"msgtype": "text"}); err != nil {
		t.Fatalf("SendMessageContext() error = %v", err)
	}
}

func TestSendMessageContextRedactsWebhookKeyFromTransportError(t *testing.T) {
	const key = "sensitive +&/?"
	transportFailure := errors.New("dial failed")
	client := NewRobot(key, WithHTTPClient(&http.Client{
		Transport: robotRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportFailure
		}),
	}))

	_, err := client.SendMessageContext(context.Background(), map[string]any{"msgtype": "text"})
	if err == nil {
		t.Fatal("SendMessageContext() error = nil, want transport error")
	}
	if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "%2B%26%2F%3F") {
		t.Fatalf("transport error leaked webhook key: %v", err)
	}
	if !errors.Is(err, transportFailure) {
		t.Fatalf("transport error identity was not preserved: %v", err)
	}
}

func TestSendMessageContextRejectsHTTPAndBusinessFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "http failure", status: http.StatusBadGateway, body: `{"errcode":0}`},
		{name: "business failure", status: http.StatusOK, body: `{"errcode":93000,"errmsg":"invalid key"}`},
		{name: "missing acknowledgement", status: http.StatusOK, body: `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewRobot("secret", WithHTTPClient(&http.Client{
				Transport: robotRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return robotResponse(test.status, test.body), nil
				}),
			}))
			if _, err := client.SendMessageContext(context.Background(), map[string]any{"msgtype": "text"}); err == nil {
				t.Fatal("SendMessageContext() error = nil, want failure")
			}
		})
	}
}

func TestSendMessageContextPropagatesCancellation(t *testing.T) {
	client := NewRobot("secret", WithHTTPClient(&http.Client{
		Transport: robotRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.SendMessageContext(ctx, map[string]any{"msgtype": "text"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SendMessageContext() error = %v, want context.Canceled", err)
	}
}

func TestUploadMediaContextRejectsBusinessFailure(t *testing.T) {
	client := NewRobot("secret", WithHTTPClient(&http.Client{
		Transport: robotRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return robotResponse(http.StatusOK, `{"errcode":40007,"errmsg":"invalid media"}`), nil
		}),
	}))

	_, err := client.UploadMediaContext(context.Background(), "file.txt", func(w io.Writer) error {
		_, err := io.WriteString(w, "content")
		return err
	})
	if err == nil {
		t.Fatal("UploadMediaContext() error = nil, want business failure")
	}
}

func TestSendFileContextUsesContextForUploadAndSend(t *testing.T) {
	var calls int
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	client := NewRobot("secret", WithHTTPClient(&http.Client{
		Transport: robotRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Context().Value(struct{}{}) != "marker" {
				t.Fatal("request context value was not propagated")
			}
			if strings.Contains(req.URL.Path, "upload_media") {
				return robotResponse(http.StatusOK, `{"errcode":0,"media_id":"media-1"}`), nil
			}
			return robotResponse(http.StatusOK, `{"errcode":0,"errmsg":"ok"}`), nil
		}),
	}))

	file := writerToFunc(func(w io.Writer) (int64, error) {
		n, err := io.WriteString(w, "content")
		return int64(n), err
	})
	if err := client.SendFileContext(ctx, "file.txt", file); err != nil {
		t.Fatalf("SendFileContext() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("HTTP calls = %d, want 2", calls)
	}
}

func TestNewRobotHasFiniteDefaultTimeout(t *testing.T) {
	client := NewRobot("secret")
	if client.httpClient.Timeout <= 0 {
		t.Fatal("default HTTP timeout is not finite")
	}
}
