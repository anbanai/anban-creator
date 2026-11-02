package wework

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"resty.dev/v3"
)

type ackRoundTripper struct{ body string }

func (rt ackRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(rt.body)), Request: req}, nil
}

func TestCloseTempChatReturnsProviderError(t *testing.T) {
	sdk := NewWework(&Config{})
	sdk.client = resty.New()
	sdk.client.SetTransport(ackRoundTripper{body: `{"errcode":93000,"errmsg":"chat is closed"}`})
	sdk.cache.accessToken = &AccessToken{AccessToken: "test-token", Expiration: time.Now().Add(time.Hour)}

	_, err := sdk.CloseTempChat(&CloseTempChatRequest{UserID: "user", ExternalUserID: "external"})
	if err == nil || !strings.Contains(err.Error(), "93000") {
		t.Fatalf("CloseTempChat() error = %v, want provider error code 93000", err)
	}
}

func TestSetConclusionReturnsProviderError(t *testing.T) {
	sdk := NewWework(&Config{})
	sdk.client = resty.New()
	sdk.client.SetTransport(ackRoundTripper{body: `{"errcode":93000,"errmsg":"invalid conclusion"}`})
	sdk.cache.accessToken = &AccessToken{AccessToken: "test-token", Expiration: time.Now().Add(time.Hour)}

	_, err := sdk.SetConclusion(&SetConclusionRequest{})
	if err == nil || !strings.Contains(err.Error(), "93000") {
		t.Fatalf("SetConclusion() error = %v, want provider error code 93000", err)
	}
}
