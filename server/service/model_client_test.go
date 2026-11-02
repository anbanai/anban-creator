package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func TestChatCompletionUsageSeparatesCachedPromptTokens(t *testing.T) {
	usage := chatCompletionUsage(openai.CompletionUsage{
		PromptTokens:     100,
		CompletionTokens: 7,
		TotalTokens:      107,
		PromptTokensDetails: openai.CompletionUsagePromptTokensDetails{
			CachedTokens: 40,
		},
	})

	if usage.InputTokens != 60 || usage.CachedInputTokens != 40 || usage.OutputTokens != 7 || usage.TotalTokens != 107 {
		t.Fatalf("usage = %#v, want uncached input 60, cached input 40, output 7, total 107", usage)
	}
}

func TestImageUnderstandingProviderResponses(t *testing.T) {
	const successJSON = `{"id":"completion-1","model":"vision-response","choices":[{"index":0,"message":{"role":"assistant","content":"a blue cup"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	for _, tt := range []struct {
		name        string
		status      int
		contentType string
		body        string
		redirect    bool
		protocol    bool
	}{
		{name: "HTML success", status: 200, contentType: "text/html; charset=utf-8", body: "<html>fixture-response-secret</html>", protocol: true},
		{name: "redirect to HTML", status: 200, contentType: "text/html", body: "<html>fixture-response-secret</html>", redirect: true, protocol: true},
		{name: "HTML gateway error", status: 502, contentType: "text/html", body: "<html>fixture-response-secret</html>", protocol: true},
		{name: "invalid JSON", status: 200, contentType: "application/json", body: "<html>fixture-response-secret</html>", protocol: true},
		{name: "missing choices", status: 200, contentType: "application/json", body: `{"id":"fixture-response-secret","choices":[]}`, protocol: true},
		{name: "normal JSON", status: 200, contentType: "application/json", body: successJSON},
		{name: "unauthorized JSON", status: 401, contentType: "application/json"},
		{name: "forbidden JSON", status: 403, contentType: "application/json"},
		{name: "rate limited JSON", status: 429, contentType: "application/json"},
		{name: "server error JSON", status: 500, contentType: "application/json"},
		{name: "gateway error JSON", status: 502, contentType: "application/json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.redirect && r.URL.Path != "/login" {
					http.Redirect(w, r, "/login?token=fixture-query-secret", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", tt.contentType)
				w.Header().Set("X-Request-ID", "request-vision-1")
				w.WriteHeader(tt.status)
				body := tt.body
				if body == "" {
					body = `{"error":{"message":"fixture-response-secret fixture-api-key data:image/png;base64,fixture-image-secret","type":"api_error","code":"fixture-code","param":null}}`
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			endpoint, err := url.Parse(server.URL + "/v1?token=fixture-query-secret")
			if err != nil {
				t.Fatal(err)
			}
			endpoint.User = url.UserPassword("fixture-user", "fixture-password")
			client := NewOpenAILLMClient(endpoint.String(), "fixture-api-key", "vision-route", time.Second).(*openaiLLMClient)
			client.client.Chat.Completions.Options = append(client.client.Chat.Completions.Options, option.WithMaxRetries(0))
			result, err := client.CompleteWithImageResult(context.Background(), "", "fixture-prompt-secret", "data:image/png;base64,fixture-image-secret")
			if !tt.protocol && tt.status == http.StatusOK {
				if err != nil || result == nil || result.Text != "a blue cup" || result.Model != "vision-response" || result.Usage.TotalTokens != 5 {
					t.Fatalf("completion = %#v, %v; want analysis and provider usage", result, err)
				}
				return
			}
			if err == nil || result != nil {
				t.Fatalf("completion = %#v, %v; want provider error", result, err)
			}
			if tt.protocol != strings.Contains(err.Error(), "provider_protocol_error") {
				t.Errorf("error = %v; protocol classification = %v", err, tt.protocol)
			}
			if !tt.protocol {
				var apiError *openai.Error
				if !errors.As(err, &apiError) || apiError.StatusCode != tt.status {
					t.Errorf("error = %v; want distinguishable JSON API status %d", err, tt.status)
				}
			}
			path := "/v1/chat/completions"
			if tt.redirect {
				path = "/login"
			}
			var providerError *imageUnderstandingProviderError
			if !errors.As(err, &providerError) || providerError.Endpoint != endpoint.Host+path {
				t.Errorf("provider error = %#v; want internal endpoint %q", providerError, endpoint.Host+path)
			}
			if strings.Contains(err.Error(), endpoint.Host) || strings.Contains(err.Error(), "endpoint=") {
				t.Errorf("public error exposes internal endpoint: %v", err)
			}
			mediaType := strings.Split(tt.contentType, ";")[0]
			for _, want := range []string{fmt.Sprintf("status=%d", tt.status), fmt.Sprintf("content_type=%q", mediaType), `request_id="request-vision-1"`} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %v; missing safe metadata %s", err, want)
				}
			}
			assertImageUnderstandingErrorRedacted(t, err)
		})
	}
}

func TestImageUnderstandingTransportErrorDoesNotExposeRequestURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()
	client := NewOpenAILLMClient(server.URL+"/v1?token=fixture-query-secret", "fixture-api-key", "vision-route", time.Second).(*openaiLLMClient)
	client.client.Chat.Completions.Options = append(client.client.Chat.Completions.Options, option.WithMaxRetries(0))
	_, err := client.CompleteWithImageResult(context.Background(), "", "inspect", "data:image/png;base64,fixture-image-secret")
	if err == nil {
		t.Fatal("want transport error")
	}
	assertImageUnderstandingErrorRedacted(t, err)
}

func assertImageUnderstandingErrorRedacted(t *testing.T, err error) {
	t.Helper()
	for _, secret := range []string{"fixture-response-secret", "fixture-api-key", "fixture-query-secret", "fixture-user", "fixture-password", "fixture-image-secret", "fixture-prompt-secret", "<html>", "data:image"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error exposes %q: %v", secret, err)
		}
	}
}

func TestImageUnderstandingUsesEarlierParentOrRouteDeadline(t *testing.T) {
	for _, tt := range []struct {
		name          string
		parentTimeout time.Duration
		routeTimeout  time.Duration
	}{
		{name: "route is earlier", parentTimeout: 2 * time.Second, routeTimeout: 50 * time.Millisecond},
		{name: "parent is earlier", parentTimeout: 50 * time.Millisecond, routeTimeout: 2 * time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
			}))
			defer server.Close()
			client := NewOpenAILLMClient(server.URL+"/v1", "fixture-api-key", "vision-route", tt.routeTimeout).(*openaiLLMClient)
			client.client.Chat.Completions.Options = append(client.client.Chat.Completions.Options, option.WithMaxRetries(0))
			ctx, cancel := context.WithTimeout(context.Background(), tt.parentTimeout)
			defer cancel()
			started := time.Now()
			_, err := client.CompleteWithImageResult(ctx, "", "inspect", "data:image/png;base64,fixture-image-secret")
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("completion error = %v, want deadline exceeded", err)
			}
			if elapsed := time.Since(started); elapsed >= time.Second {
				t.Errorf("completion took %s; earlier deadline should terminate before 1s", elapsed)
			}
		})
	}
}

func TestVisionRequestDisablesKimiThinking(t *testing.T) {
	const successJSON = `{"id":"completion-1","model":"vision-response","choices":[{"index":0,"message":{"role":"assistant","content":"a blue cup"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	for _, tt := range []struct {
		name         string
		model        string
		wantThinking bool
	}{
		{name: "kimi k3 disables thinking", model: "kimi-k3", wantThinking: true},
		{name: "kimi k2.6 disables thinking", model: "kimi-k2.6", wantThinking: true},
		{name: "non-kimi model keeps provider defaults", model: "vision-route"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var request map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode vision request: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(successJSON))
			}))
			defer server.Close()

			client := NewOpenAILLMClient(server.URL+"/v1", "fixture-api-key", tt.model, time.Second).(*openaiLLMClient)
			if _, err := client.CompleteWithImageResult(context.Background(), "", "describe", "data:image/png;base64,eA=="); err != nil {
				t.Fatal(err)
			}
			thinking, ok := request["thinking"].(map[string]any)
			if tt.wantThinking {
				if !ok || thinking["type"] != "disabled" {
					t.Fatalf("vision thinking policy = %#v, want disabled", request["thinking"])
				}
				return
			}
			if ok {
				t.Fatalf("non-Kimi model received thinking policy: %#v", request["thinking"])
			}
		})
	}
}

func TestVideoRequestDisablesKimiThinking(t *testing.T) {
	const successJSON = `{"id":"completion-1","model":"video-response","choices":[{"index":0,"message":{"role":"assistant","content":"a blue cup"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode video request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(successJSON))
	}))
	defer server.Close()

	client := NewOpenAILLMClient(server.URL+"/v1", "fixture-api-key", "kimi-k3", time.Second).(*openaiLLMClient)
	if _, err := client.CompleteWithVideoURLResult(context.Background(), "", "describe", "https://example.com/video.mp4"); err != nil {
		t.Fatal(err)
	}
	thinking, ok := request["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("video thinking policy = %#v, want disabled", request["thinking"])
	}
}

func TestKimiThinkingModelCoverage(t *testing.T) {
	for _, tt := range []struct {
		model string
		want  bool
	}{
		{model: "kimi-k3", want: true},
		{model: " kimi-k3[1m] ", want: true},
		{model: "kimi-k2.5", want: true},
		{model: "kimi-k2.6", want: true},
		{model: "kimi-k2-thinking", want: true},
		{model: "kimi-k3-code", want: false},
		{model: "gpt-5", want: false},
		{model: "", want: false},
	} {
		if got := isKimiThinkingModel(tt.model); got != tt.want {
			t.Errorf("isKimiThinkingModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestVisionTimeoutDiagnostics(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	run := func(t *testing.T, ctx context.Context, timeout time.Duration) *imageUnderstandingProviderError {
		t.Helper()
		client := NewOpenAILLMClient(server.URL+"/v1", "fixture-api-key", "vision-route", timeout).(*openaiLLMClient)
		client.client.Chat.Completions.Options = append(client.client.Chat.Completions.Options, option.WithMaxRetries(0))
		_, err := client.CompleteWithImageResult(ctx, "", "describe", "data:image/png;base64,eA==")
		var providerErr *imageUnderstandingProviderError
		if !errors.As(err, &providerErr) {
			t.Fatalf("error = %v, want image understanding provider error", err)
		}
		return providerErr
	}

	t.Run("route timeout", func(t *testing.T) {
		providerErr := run(t, context.Background(), 500*time.Millisecond)
		if providerErr.Code != "provider_timeout" || providerErr.TimeoutSource != "route_timeout" || !providerErr.RequestWritten {
			t.Fatalf("timeout diagnostics = %#v", providerErr)
		}
	})

	t.Run("caller deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		providerErr := run(t, ctx, 5*time.Second)
		if providerErr.Code != "provider_timeout" || providerErr.TimeoutSource != "caller_deadline" || !providerErr.RequestWritten {
			t.Fatalf("timeout diagnostics = %#v", providerErr)
		}
	})
}
