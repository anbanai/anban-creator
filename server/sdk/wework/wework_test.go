package wework

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"resty.dev/v3"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUserLockForIsSafeDuringConcurrentInitialization(t *testing.T) {
	sdk := NewWework(&Config{})
	const callers = 64
	locks := make([]*sync.Mutex, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			locks[i] = sdk.userLockFor(fmt.Sprintf("external-%d", i%2))
		}(i)
	}
	wg.Wait()
	if locks[0] == nil || locks[1] == nil || locks[0] == locks[1] {
		t.Fatal("userLockFor() did not return separate initialized locks")
	}
	for i := 2; i < callers; i++ {
		want := locks[i%2]
		if locks[i] != want {
			t.Fatalf("userLockFor() returned inconsistent lock for user %d", i%2)
		}
	}
}

func TestNewWeworkHasFiniteTimeout(t *testing.T) {
	if got := NewWework(&Config{}).client.Timeout(); got <= 0 {
		t.Fatalf("NewWework() timeout = %s, want a positive finite timeout", got)
	}
	provided := &http.Client{}
	injected := NewWeworkWithHTTPClient(&Config{}, provided)
	if got := injected.client.Client().Timeout; got <= 0 {
		t.Fatalf("NewWeworkWithHTTPClient() underlying HTTP timeout = %s, want a positive finite timeout", got)
	}
	if provided.Timeout != 0 {
		t.Fatalf("constructor mutated injected http.Client timeout to %s", provided.Timeout)
	}
}

func TestWithContextPropagatesCancellation(t *testing.T) {
	sdk := NewWeworkWithHTTPClient(&Config{CropID: "corp-id", Secret: "secret"}, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sdk.WithContext(ctx).GetAccessToken()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetAccessToken() error = %v, want context.Canceled", err)
	}
}

func TestLockingStopsThrottleWaitWhenContextIsCanceled(t *testing.T) {
	sdk := NewWework(&Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sdk.WithContext(ctx).locking(func() error { return context.Canceled }); !errors.Is(err, context.Canceled) {
		t.Fatalf("locking() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("locking() waited %s after cancellation", elapsed)
	}
}

func TestLockingThrottleCancellationDoesNotMaskSuccessfulRequest(t *testing.T) {
	sdk := NewWework(&Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requestDone := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- sdk.WithContext(ctx).locking(func() error {
			close(requestDone)
			return nil
		})
	}()
	<-requestDone
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("locking() masked a successful request with cancellation: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("locking() did not stop the throttle wait after cancellation")
	}
}

func TestLockingCancellationWhileWaitingSkipsRequest(t *testing.T) {
	sdk := NewWework(&Config{})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstStarted := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- sdk.WithContext(firstCtx).locking(func() error {
			close(firstStarted)
			return nil
		})
	}()
	<-firstStarted

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	var secondCalled atomic.Bool
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- sdk.WithContext(secondCtx).locking(func() error {
			secondCalled.Store(true)
			return nil
		})
	}()
	time.AfterFunc(30*time.Millisecond, cancelSecond)
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second locking() error = %v, want context.Canceled", err)
		}
		if secondCalled.Load() {
			t.Fatal("canceled request ran after waiting for the throttle")
		}
	case <-time.After(300 * time.Millisecond):
		cancelFirst()
		<-firstDone
		t.Fatal("canceled request remained blocked on the throttle")
	}
	cancelFirst()
	if err := <-firstDone; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("first locking() error = %v", err)
	}
}

func TestWithContextSharesTokenCache(t *testing.T) {
	var requests atomic.Int32
	client := resty.New()
	client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"errcode":0,"access_token":"cached-token","expires_in":3600}`)),
			Request:    req,
		}, nil
	}))
	sdk := NewWework(&Config{CropID: "corp-id", Secret: "secret"})
	sdk.client = client

	first, err := sdk.WithContext(context.Background()).AccessToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := sdk.AccessToken()
	if err != nil {
		t.Fatal(err)
	}
	if first.AccessToken != second.AccessToken || requests.Load() != 1 {
		t.Fatalf("WithContext() did not share token cache: first=%q second=%q requests=%d", first.AccessToken, second.AccessToken, requests.Load())
	}
}

func TestEndpointTransportErrorDoesNotExposeAccessToken(t *testing.T) {
	const token = "sensitive-endpoint-token"
	transportFailure := errors.New("dial failed")
	sdk := NewWeworkWithHTTPClient(&Config{CropID: "corp-id", Secret: "secret"}, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportFailure
		}),
	})
	sdk.cache.accessToken = &AccessToken{AccessToken: token, Expiration: time.Now().Add(time.Hour)}

	_, err := sdk.ExternalcontactList(&ExternalcontactListRequest{Userid: "user-id"})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("transport error leaked access token: %v", err)
	}
	if !errors.Is(err, transportFailure) {
		t.Fatalf("transport error identity was not preserved: %v", err)
	}
}

func TestGetAccessTokenTransportErrorDoesNotExposeSecret(t *testing.T) {
	const secret = "sensitive-corp-secret"
	transportFailure := errors.New("dial failed")
	sdk := NewWeworkWithHTTPClient(&Config{CropID: "corp-id", Secret: secret}, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportFailure
		}),
	})

	_, err := sdk.GetAccessToken()
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("transport error leaked configured secret: %v", err)
	}
	if !errors.Is(err, transportFailure) {
		t.Fatalf("transport error identity was not preserved: %v", err)
	}
}

func TestRefreshAccessTokenTransportErrorDoesNotExposeToken(t *testing.T) {
	const token = "sensitive-access-token"
	transportFailure := errors.New("dial failed")
	client := resty.New()
	client.SetTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, transportFailure
	}))
	sdk := NewWework(&Config{CropID: "corp-id", Secret: "secret"})
	sdk.client = client
	sdk.cache.accessToken = &AccessToken{AccessToken: token, Expiration: time.Now().Add(time.Hour)}

	_, err := sdk.RefreshAccessToken()
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("RefreshAccessToken() leaked token or returned nil error: %v", err)
	}
	if !errors.Is(err, transportFailure) {
		t.Fatalf("transport error identity was not preserved: %v", err)
	}
}

func TestTicketCachesAreSafeForConcurrentAccess(t *testing.T) {
	var jsapiCalls, agentCalls atomic.Int32
	client := resty.New()
	client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var ticket string
		switch req.URL.Path {
		case "/cgi-bin/get_jsapi_ticket":
			jsapiCalls.Add(1)
			ticket = "jsapi-ticket"
		case "/cgi-bin/ticket/get":
			agentCalls.Add(1)
			ticket = "agent-ticket"
		default:
			return nil, fmt.Errorf("unexpected API path: %s", req.URL.Path)
		}
		body := fmt.Sprintf(`{"errcode":0,"ticket":%q,"expires_in":3600}`, ticket)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}))
	sdk := NewWework(&Config{CropID: "corp-id", Secret: "secret"})
	sdk.client = client
	sdk.cache.accessToken = &AccessToken{AccessToken: "access-token", Expiration: time.Now().Add(time.Hour)}

	const callers = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, callers*2)
	for i := 0; i < callers; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := sdk.JsapiTicket()
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err := sdk.AgentJsapiTicket()
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := jsapiCalls.Load(); got != 1 {
		t.Errorf("JSAPI ticket fetched %d times, want once", got)
	}
	if got := agentCalls.Load(); got != 1 {
		t.Errorf("agent JSAPI ticket fetched %d times, want once", got)
	}
}
