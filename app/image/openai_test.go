package image

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/royalrick/anbanwriter/app/config"
	"github.com/rs/zerolog"
)

func testLogger() *zerolog.Logger {
	l := zerolog.New(io.Discard)
	return &l
}

func TestMapToDALLESize(t *testing.T) {
	tests := []struct {
		name  string
		size  string
		model string
		want  string
	}{
		// Empty defaults to 1024x1024
		{"empty defaults to 1024x1024", "", "dall-e-3", "1024x1024"},
		// Aspect ratio mapping for DALL-E 3
		{"16:9 maps to 1792x1024", "16:9", "dall-e-3", "1792x1024"},
		{"9:16 maps to 1024x1792", "9:16", "dall-e-3", "1024x1792"},
		{"1:1 maps to 1024x1024", "1:1", "dall-e-3", "1024x1024"},
		{"4:3 maps to 1792x1024", "4:3", "dall-e-3", "1792x1024"},
		{"3:4 maps to 1024x1792", "3:4", "dall-e-3", "1024x1792"},
		{"3:2 maps to 1792x1024", "3:2", "dall-e-3", "1792x1024"},
		{"2:3 maps to 1024x1792", "2:3", "dall-e-3", "1024x1792"},
		{"21:9 maps to 1792x1024", "21:9", "dall-e-3", "1792x1024"},
		// Tier suffix stripped before ratio lookup
		{"16:9:2K maps to 1792x1024", "16:9:2K", "dall-e-3", "1792x1024"},
		{"3:4:1K maps to 1024x1792", "3:4:1K", "dall-e-3", "1024x1792"},
		// DALL-E 2 always returns 1024x1024
		{"dall-e-2 any size defaults to 1024x1024", "16:9", "dall-e-2", "1024x1024"},
		{"dall-e-2 pixel format defaults to 1024x1024", "2560x1440", "dall-e-2", "1024x1024"},
		// GPT image models use a different OpenAI size set.
		{"gpt-image-1 16:9 maps to 1536x1024", "16:9", "gpt-image-1", "1536x1024"},
		{"gpt-image-1 3:4 maps to 1024x1536", "3:4", "gpt-image-1", "1024x1536"},
		{"gpt-image-1 1:1 maps to 1024x1024", "1:1", "gpt-image-1", "1024x1024"},
		// Pixel format not supported, defaults to 1024x1024
		{"pixel format not supported", "2560x1440", "dall-e-3", "1024x1024"},
		// GPT-image models pass through raw pixel sizes.
		{"gpt-image-2 pixel passthrough 4K", "3840x2160", "gpt-image-2", "3840x2160"},
		{"gpt-image-1 pixel passthrough HD", "1920x1080", "gpt-image-1", "1920x1080"},
		{"chatgpt-image-latest pixel passthrough", "4096x4096", "chatgpt-image-latest", "4096x4096"},
		// Unknown format defaults to 1024x1024
		{"unknown format defaults", "badformat", "dall-e-3", "1024x1024"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapToDALLESize(tt.size, tt.model)
			if got != tt.want {
				t.Errorf("mapToDALLESize(%q, %q) = %q, want %q", tt.size, tt.model, got, tt.want)
			}
		})
	}
}

func TestOpenAIGenerateClassifiesHTMLResponseAsEndpointProtocolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>login required</body></html>"))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "dall-e-3",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	_, err = provider.Generate(context.Background(), "a cat", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var genErr *GenerateError
	if !errors.As(err, &genErr) {
		t.Fatalf("error type = %T, want *GenerateError", err)
	}
	if genErr.Code != "endpoint_protocol" {
		t.Fatalf("Code = %q, want endpoint_protocol; error=%v", genErr.Code, err)
	}
	if !strings.Contains(genErr.Message, "OpenAI Images API") {
		t.Fatalf("Message = %q, want OpenAI Images API hint", genErr.Message)
	}
}

func TestOpenAIGenerateSavesBase64Image(t *testing.T) {
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D,
	}
	pngData = append(pngData, make([]byte, 120)...)
	b64 := base64.StdEncoding.EncodeToString(pngData)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["response_format"] != "b64_json" {
			t.Fatalf("response_format = %v, want b64_json", body["response_format"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1777599787,"data":[{"b64_json":"` + b64 + `"}]}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "dall-e-3",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	result, err := provider.Generate(context.Background(), "a cat", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.ResponseType != "b64_json" {
		t.Fatalf("ResponseType = %q, want b64_json", result.ResponseType)
	}
	if !strings.Contains(result.ResponsePreview, "truncated") {
		t.Fatalf("ResponsePreview = %q, want truncated base64 preview", result.ResponsePreview)
	}
	if result.URL == "" {
		t.Fatal("result.URL is empty")
	}
	defer os.Remove(result.URL)

	got, err := os.ReadFile(result.URL)
	if err != nil {
		t.Fatalf("read generated temp file: %v", err)
	}
	if string(got) != string(pngData) {
		t.Fatalf("generated bytes = %v, want %v", got, pngData)
	}
}

func TestOpenAIGenerateForcesGPTImageBase64ResponseFormat(t *testing.T) {
	pngData := []byte{0x89, 0x50, 0x4E, 0x47}
	b64 := base64.StdEncoding.EncodeToString(pngData)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["response_format"] != "b64_json" {
			t.Fatalf("response_format = %v, want b64_json", body["response_format"])
		}
		if body["size"] != "1536x1024" {
			t.Fatalf("size = %v, want 1536x1024", body["size"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1777599787,"data":[{"b64_json":"` + b64 + `"}]}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "gpt-image-1",
		Size:     "16:9",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	result, err := provider.Generate(context.Background(), "a cat", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	defer os.Remove(result.URL)
	got, err := os.ReadFile(result.URL)
	if err != nil {
		t.Fatalf("read generated temp file: %v", err)
	}
	if string(got) != string(pngData) {
		t.Fatalf("generated bytes = %v, want %v", got, pngData)
	}
}

func TestOpenAIGenerateAcceptsDataURLBase64Result(t *testing.T) {
	pngData := []byte{0x89, 0x50, 0x4E, 0x47}
	b64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1777599787,"data":[{"b64_json":"` + b64 + `"}]}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "gpt-image-1",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	result, err := provider.Generate(context.Background(), "a cat", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	defer os.Remove(result.URL)
	if result.ResponseType != "b64_json" {
		t.Fatalf("ResponseType = %q, want b64_json", result.ResponseType)
	}
	if !strings.HasPrefix(result.ResponsePreview, "data:image/png;base64,") {
		t.Fatalf("ResponsePreview = %q, want data URL prefix", result.ResponsePreview)
	}

	got, err := os.ReadFile(result.URL)
	if err != nil {
		t.Fatalf("read generated temp file: %v", err)
	}
	if string(got) != string(pngData) {
		t.Fatalf("generated bytes = %v, want %v", got, pngData)
	}
}

func TestOpenAIGenerateDownloadsURLOnlyResult(t *testing.T) {
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D,
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/images/generations":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"created":1777599787,"data":[{"url":"` + srv.URL + "/generated.png" + `"}]}`))
		case "/generated.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "dall-e-3",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	result, err := provider.Generate(context.Background(), "a cat", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.URL == "" {
		t.Fatal("result.URL is empty")
	}
	if result.ResponseType != "url" {
		t.Fatalf("ResponseType = %q, want url", result.ResponseType)
	}
	if result.ResponsePreview != srv.URL+"/generated.png" {
		t.Fatalf("ResponsePreview = %q, want generated image URL", result.ResponsePreview)
	}
	defer os.Remove(result.URL)

	got, readErr := os.ReadFile(result.URL)
	if readErr != nil {
		t.Fatalf("read generated temp file: %v", readErr)
	}
	if string(got) != string(pngData) {
		t.Fatalf("generated bytes = %v, want %v", got, pngData)
	}
}

func TestOpenAIGenerateReturnsErrorForEmptyImagePayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1777599787,"data":[{}]}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "dall-e-3",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	_, err = provider.Generate(context.Background(), "a cat", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var genErr *GenerateError
	if !errors.As(err, &genErr) {
		t.Fatalf("error type = %T, want *GenerateError", err)
	}
	if genErr.Code != "no_image" {
		t.Fatalf("Code = %q, want no_image", genErr.Code)
	}
}

func TestOpenAIProviderUsesCustomBaseURLAndKey(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotModel any
	var gotSize any
	var gotResponseFormat any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		gotModel = body["model"]
		gotSize = body["size"]
		gotResponseFormat = body["response_format"]

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1777599787,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4E, 0x47}) + `"}]}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "custom-test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "gpt-image-1",
		Size:     "16:9",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	result, err := provider.Generate(context.Background(), "a cat in a garden", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	defer os.Remove(result.URL)

	if gotAuth != "Bearer custom-test-key" {
		t.Fatalf("Authorization = %q, want Bearer custom-test-key", gotAuth)
	}
	if gotPath == "" {
		t.Fatal("request path is empty")
	}
	if !strings.Contains(gotPath, "images") {
		t.Fatalf("request path = %q, want images endpoint", gotPath)
	}
	if gotModel != "gpt-image-1" {
		t.Fatalf("model = %v, want gpt-image-1", gotModel)
	}
	if gotSize != "1536x1024" {
		t.Fatalf("size = %v, want 1536x1024", gotSize)
	}
	if gotResponseFormat != "b64_json" {
		t.Fatalf("response_format = %v, want b64_json", gotResponseFormat)
	}
}

func TestOpenAIProviderManualSmoke(t *testing.T) {
	baseURL := os.Getenv("OPENAI_TEST_BASE_URL")
	apiKey := os.Getenv("OPENAI_TEST_API_KEY")
	model := os.Getenv("OPENAI_TEST_MODEL")
	size := os.Getenv("OPENAI_TEST_SIZE")
	prompt := os.Getenv("OPENAI_TEST_PROMPT")
	outputPath := os.Getenv("OPENAI_TEST_OUTPUT")

	if baseURL == "" || apiKey == "" {
		t.Skip("set OPENAI_TEST_BASE_URL and OPENAI_TEST_API_KEY to run this smoke test")
	}
	if model == "" {
		model = "gpt-image-1"
	}
	if size == "" {
		size = "1:1"
	}
	if prompt == "" {
		prompt = "a clean studio photo of a white mug on a wooden table"
	}

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      apiKey,
		BaseURL:  baseURL,
		Provider: "openai",
		Model:    model,
		Size:     size,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	result, err := provider.Generate(context.Background(), prompt, nil)
	if err != nil {
		var genErr *GenerateError
		if errors.As(err, &genErr) && genErr.Original != nil {
			t.Fatalf("Generate: %v\noriginal: %T: %v", err, genErr.Original, genErr.Original)
		}
		t.Fatalf("Generate: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.URL == "" {
		t.Fatal("result.URL is empty")
	}
	if outputPath == "" {
		outputPath = "openai_smoke_result.png"
	}

	data, readErr := os.ReadFile(result.URL)
	if readErr != nil {
		t.Fatalf("read generated file: %v", readErr)
	}
	if writeErr := os.WriteFile(outputPath, data, 0644); writeErr != nil {
		t.Fatalf("write output file: %v", writeErr)
	}
	t.Logf("model=%s size=%s response_type=%s response_preview=%s output=%s revised_prompt=%q", result.Model, result.Size, result.ResponseType, result.ResponsePreview, outputPath, result.RevisedPrompt)
}

func TestGenerateError_Retryable(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"server_error", true},
		{"rate_limit", true},
		{"network_error", true},
		{"unauthorized", false},
		{"bad_request", false},
		{"safety_blocked", false},
		{"payment_required", false},
		{"endpoint_protocol", false},
		{"no_image", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			e := &GenerateError{Code: tt.code}
			if got := e.Retryable(); got != tt.want {
				t.Errorf("Retryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenAIGenerateClassifies5xxAsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"Upstream service temporarily unavailable","type":"upstream_error","param":"","code":null}}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "gpt-image-2",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	_, err = provider.Generate(context.Background(), "a cat", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var genErr *GenerateError
	if !errors.As(err, &genErr) {
		t.Fatalf("error type = %T, want *GenerateError", err)
	}
	if genErr.Code != "server_error" {
		t.Fatalf("Code = %q, want server_error; error=%v", genErr.Code, err)
	}
	if !genErr.Retryable() {
		t.Fatalf("Retryable() = false, want true for server_error")
	}
}

func TestOpenAIGenerateClassifies401AsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error","param":"","code":"invalid_api_key"}}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "gpt-image-2",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	_, err = provider.Generate(context.Background(), "a cat", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var genErr *GenerateError
	if !errors.As(err, &genErr) {
		t.Fatalf("error type = %T, want *GenerateError", err)
	}
	if genErr.Code != "unauthorized" {
		t.Fatalf("Code = %q, want unauthorized; error=%v", genErr.Code, err)
	}
	if genErr.Retryable() {
		t.Fatalf("Retryable() = true, want false for unauthorized")
	}
}

func TestOpenAIGenerateClassifies429AsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error","param":"","code":"rate_limit_exceeded"}}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "gpt-image-2",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	_, err = provider.Generate(context.Background(), "a cat", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var genErr *GenerateError
	if !errors.As(err, &genErr) {
		t.Fatalf("error type = %T, want *GenerateError", err)
	}
	if genErr.Code != "rate_limit" {
		t.Fatalf("Code = %q, want rate_limit; error=%v", genErr.Code, err)
	}
	if !genErr.Retryable() {
		t.Fatalf("Retryable() = false, want true for rate_limit")
	}
}
