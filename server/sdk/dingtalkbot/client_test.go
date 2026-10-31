package dingtalkbot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type dingtalkRoundTripFunc func(*http.Request) (*http.Response, error)

func (f dingtalkRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func dingtalkResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestSendMessageContextEncodesQueryAndPropagatesContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	client := NewClient("token +&/?", WithSecretKey("secret"), WithHTTPClient(&http.Client{
		Transport: dingtalkRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got := req.URL.Query().Get("access_token"); got != "token +&/?" {
				t.Fatalf("access_token = %q, want original token", got)
			}
			if got := req.URL.Query().Get("timestamp"); got == "" {
				t.Fatal("timestamp query parameter is missing")
			}
			if got := req.URL.Query().Get("sign"); got == "" {
				t.Fatal("sign query parameter is missing")
			}
			if got := req.Context().Value(struct{}{}); got != "marker" {
				t.Fatalf("context value = %v, want marker", got)
			}
			return dingtalkResponse(http.StatusOK, `{"errcode":0,"errmsg":"ok"}`), nil
		}),
	}))

	if _, err := client.SendMessageContext(ctx, NewTextMessage("hello")); err != nil {
		t.Fatalf("SendMessageContext() error = %v", err)
	}
}

func TestSendMessageContextRedactsAccessTokenFromTransportError(t *testing.T) {
	const token = "sensitive +&/?"
	transportFailure := errors.New("dial failed")
	client := NewClient(token, WithHTTPClient(&http.Client{
		Transport: dingtalkRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportFailure
		}),
	}))

	_, err := client.SendMessageContext(context.Background(), NewTextMessage("hello"))
	if err == nil {
		t.Fatal("SendMessageContext() error = nil, want transport error")
	}
	if strings.Contains(err.Error(), url.QueryEscape(token)) || strings.Contains(err.Error(), token) {
		t.Fatalf("transport error leaked access token: %v", err)
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
		{name: "missing code", body: `{}`},
		{name: "provider failure", body: `{"errcode":40014,"errmsg":"invalid token"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient("token", WithHTTPClient(&http.Client{
				Transport: dingtalkRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return dingtalkResponse(http.StatusOK, test.body), nil
				}),
			}))
			if _, err := client.SendMessageContext(context.Background(), NewTextMessage("hello")); err == nil {
				t.Fatal("SendMessageContext() error = nil, want acknowledgement error")
			}
		})
	}
}

func TestSendMessageContextRejectsHTTPFailure(t *testing.T) {
	client := NewClient("token", WithHTTPClient(&http.Client{
		Transport: dingtalkRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return dingtalkResponse(http.StatusBadGateway, `{"errcode":0}`), nil
		}),
	}))

	if _, err := client.SendMessageContext(context.Background(), NewTextMessage("hello")); err == nil {
		t.Fatal("SendMessageContext() error = nil, want HTTP error")
	}
}

func TestSendMessageContextPropagatesCancellation(t *testing.T) {
	client := NewClient("token", WithHTTPClient(&http.Client{
		Transport: dingtalkRoundTripFunc(func(req *http.Request) (*http.Response, error) {
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

func TestWithHTTPClientDoesNotMutateInjectedClient(t *testing.T) {
	provided := &http.Client{Transport: dingtalkRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return dingtalkResponse(http.StatusOK, `{"errcode":0,"errmsg":"ok"}`), nil
	})}
	client := NewClient("token", WithHTTPClient(provided))
	if provided.Timeout != 0 {
		t.Fatalf("WithHTTPClient() mutated caller timeout to %s", provided.Timeout)
	}
	if client.httpClient.Timeout() <= 0 {
		t.Fatal("injected client does not have a finite timeout")
	}
}

func TestNewClientHasFiniteDefaultTimeout(t *testing.T) {
	client := NewClient("token")
	if client.httpClient.Timeout() <= 0 {
		t.Fatal("default Resty timeout is not finite")
	}

}
