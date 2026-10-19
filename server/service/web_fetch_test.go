package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	serverconfig "github.com/anbanai/anban-creator/server/config"
)

func TestValidatePublicHTTPURLRejectsSSRFTargets(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd", "http://127.0.0.1/", "http://[::1]/", "http://169.254.169.254/latest/meta-data", "http://192.168.1.1/", "http://user:pass@example.com/", "http://localhost/",
	} {
		t.Run(raw, func(t *testing.T) {
			if err := ValidatePublicHTTPURL(raw); err == nil {
				t.Fatalf("ValidatePublicHTTPURL(%q) accepted SSRF target", raw)
			}
		})
	}
}

func TestDeepSeekFetchProviderParsesMarkdownWithoutExposingKey(t *testing.T) {
	const key = "super-secret-fetch-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if got := r.Header.Get("Authorization"); got != "Bearer "+key {
			t.Fatalf("authorization = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["url"] != "https://example.com/article" {
			t.Fatalf("request body = %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"title": "Example", "url": body["url"], "markdown": "# Article\n\nBody"})
	}))
	defer server.Close()
	provider := NewDeepSeekFetchProvider(serverconfig.SearchDeepSeekConfig{FetchEndpoint: server.URL, APIKey: key, Timeout: time.Second})
	result, err := provider.Fetch(context.Background(), PageFetchRequest{URL: "https://example.com/article"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ContentMarkdown != "# Article\n\nBody" || result.Provider != "deepseek" {
		t.Fatalf("result = %#v", result)
	}
	if strings.Contains(result.ContentMarkdown, key) {
		t.Fatal("API key leaked into result")
	}
}

func TestDeepSeekFetchProviderRejectsMalformedResponseAndDoesNotReturnBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream secret body", http.StatusBadGateway)
	}))
	defer server.Close()
	provider := NewDeepSeekFetchProvider(serverconfig.SearchDeepSeekConfig{FetchEndpoint: server.URL, APIKey: "key", Timeout: time.Second})
	_, err := provider.Fetch(context.Background(), PageFetchRequest{URL: "https://example.com/article"})
	if err == nil || !strings.Contains(err.Error(), "upstream status") || strings.Contains(err.Error(), "upstream secret body") {
		t.Fatalf("error = %v", err)
	}
}
