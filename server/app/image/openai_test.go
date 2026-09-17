package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/anbanai/anban-creator/server/app/config"
	"github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/openai/openai-go/v3"
	"github.com/rs/zerolog"
)

func testLogger() *zerolog.Logger {
	l := zerolog.New(io.Discard)
	return &l
}

func TestMapToImageSize(t *testing.T) {
	tests := []struct {
		name  string
		size  string
		model string
		want  string
	}{
		// Empty defaults to 1024x1024
		{"empty defaults to 1024x1024", "", "gpt-image-2", "1024x1024"},
		// "auto" passes through for GPT Image models (OpenAI's default size).
		{"auto passthrough on gpt-image-2", "auto", "gpt-image-2", "auto"},
		{"auto case-insensitive", "AUTO", "gpt-image-2", "auto"},
		{"auto strips 4K tier suffix", "auto:4K", "gpt-image-2", "auto"},
		{"auto strips 2K tier suffix", "auto:2K", "gpt-image-2", "auto"},
		{"auto trims surrounding whitespace", "  auto  ", "gpt-image-2", "auto"},
		{"auto passthrough on gpt-image-1", "auto", "gpt-image-1", "auto"},
		{"auto passthrough on chatgpt-image-latest", "auto", "chatgpt-image-latest", "auto"},
		// Aspect ratio mapping for GPT Image models
		{"gpt-image-1 16:9 maps to 1536x1024", "16:9", "gpt-image-1", "1536x1024"},
		{"gpt-image-1 3:4 maps to 1024x1536", "3:4", "gpt-image-1", "1024x1536"},
		{"gpt-image-1 1:1 maps to 1024x1024", "1:1", "gpt-image-1", "1024x1024"},
		// Tier suffix stripped before ratio lookup
		{"16:9:2K maps to 1536x1024", "16:9:2K", "gpt-image-1", "1536x1024"},
		// GPT-image models pass through raw pixel sizes.
		{"gpt-image-2 pixel passthrough 4K", "3840x2160", "gpt-image-2", "3840x2160"},
		{"gpt-image-1 pixel passthrough HD", "1920x1080", "gpt-image-1", "1920x1080"},
		{"chatgpt-image-latest pixel passthrough", "4096x4096", "chatgpt-image-latest", "4096x4096"},
		{"gpt-image-2 exact portrait video ratio", "9:16", "gpt-image-2", "864x1536"},
		{"gpt-image-2 exact landscape video ratio", "16:9", "gpt-image-2", "1536x864"},
		{"gpt-image-2 square stays standard", "1:1", "gpt-image-2", "1024x1024"},
		{"gpt-image-2 exact reduced ratio", "21:9", "gpt-image-2", "1456x624"},
		// Unknown format defaults to 1024x1024
		{"unknown format defaults", "badformat", "gpt-image-2", "1024x1024"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapToImageSize(tt.size, tt.model)
			if got != tt.want {
				t.Errorf("mapToImageSize(%q, %q) = %q, want %q", tt.size, tt.model, got, tt.want)
			}
		})
	}
}

func TestOpenAIRequestSizeUsesExplicitRatioForSemanticTaskGeneration(t *testing.T) {
	if got := openAIRequestSize("1024x1024", "3:4", "gpt-image-2", true); got != "1152x1536" {
		t.Fatalf("semantic request size = %q, want exact ratio", got)
	}
	if got := openAIRequestSize("1024x1024", "3:4", "gpt-image-2", false); got != "1152x1536" {
		t.Fatalf("image generation request size = %q, want fixed provider size", got)
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
		_, _ = w.Write([]byte(`{"created":1777599787,"data":[{"b64_json":"` + b64 + `"}],"usage":{"input_tokens":1,"input_tokens_details":{"text_tokens":1,"image_tokens":0},"output_tokens":1,"total_tokens":2}}`))
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

func TestOpenAISaveBase64ImageRejectsOversizedPayload(t *testing.T) {
	const maxGeneratedBytes = 25 << 20
	provider := &OpenAIProvider{}
	payload := base64.StdEncoding.EncodeToString(make([]byte, maxGeneratedBytes+1))

	path, err := provider.saveBase64Image(payload)
	defer os.Remove(path)
	if err == nil {
		t.Fatal("saveBase64Image error = nil, want oversized payload rejection")
	}
	var generateErr *GenerateError
	if !errors.As(err, &generateErr) || generateErr.Code != "response_too_large" {
		t.Fatalf("saveBase64Image error = %T %v, want response_too_large GenerateError", err, err)
	}
}

func TestOpenAIGenerateCapturesUsage(t *testing.T) {
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D,
	}
	pngData = append(pngData, make([]byte, 120)...)
	b64 := base64.StdEncoding.EncodeToString(pngData)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created":1777599787,
			"data":[{"b64_json":"` + b64 + `"}],
			"size":"1024x1024",
			"quality":"medium",
			"usage":{
				"input_tokens":120,
				"input_tokens_details":{"text_tokens":20,"image_tokens":100},
				"output_tokens":1767,
				"total_tokens":1887
			}
		}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "gpt-image-2",
		Size:     "1024x1024",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	result, err := provider.Generate(context.Background(), "a cat", &GenerateOptions{Quality: "medium"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	defer os.Remove(result.URL)

	if result.Size != "1024x1024" {
		t.Fatalf("Size = %q, want response size 1024x1024", result.Size)
	}
	if result.Usage == nil {
		t.Fatal("Usage is nil, want captured usage")
	}
	if result.Usage.TextInputTokens != 20 || result.Usage.ImageInputTokens != 100 || result.Usage.ImageOutputTokens != 1767 || result.Usage.TotalTokens != 1887 {
		t.Fatalf("Usage = %#v", result.Usage)
	}
}

func TestOpenAIGenerateGPTImage2RequiresUsage(t *testing.T) {
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D,
	}
	pngData = append(pngData, make([]byte, 120)...)
	b64 := base64.StdEncoding.EncodeToString(pngData)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1777599787,"data":[{"b64_json":"` + b64 + `"}],"size":"1024x1024","quality":"medium"}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:      "test-key",
		BaseURL:  srv.URL,
		Provider: "openai",
		Model:    "gpt-image-2",
		Size:     "1024x1024",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	result, err := provider.Generate(context.Background(), "a cat", &GenerateOptions{Quality: "medium"})
	if err == nil {
		if result != nil && result.URL != "" {
			_ = os.Remove(result.URL)
		}
		t.Fatal("Generate error = nil, want usage required error")
	}
	if !strings.Contains(err.Error(), "gpt-image-2 usage is required for billing") {
		t.Fatalf("error = %v, want usage required billing error", err)
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
	var gotResponseFormat any
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/images/generations":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			gotResponseFormat = body["response_format"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"created":1777599787,"data":[{"url":"` + srv.URL + "/generated.png" + `"}],"usage":{"input_tokens":1,"input_tokens_details":{"text_tokens":1,"image_tokens":0},"output_tokens":1,"total_tokens":2}}`))
		case "/generated.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key:            "test-key",
		BaseURL:        srv.URL,
		Provider:       "openai",
		Model:          "gpt-image-2",
		ResponseFormat: "url",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}
	provider.imageDownloader = downloadTestImage

	result, err := provider.Generate(context.Background(), "a cat", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if gotResponseFormat != "url" {
		t.Fatalf("non-semantic response_format = %v, want url", gotResponseFormat)
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

func TestOpenAISemanticGenerationForcesInlineResponse(t *testing.T) {
	var gotResponseFormat any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		gotResponseFormat = body["response_format"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1777599787,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(testGeneratedPNG()) + `"}]}`))
	}))
	defer srv.Close()

	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key: "test-key", BaseURL: srv.URL, Provider: "openai", Model: "gpt-image-1",
		ResponseFormat: "url",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	result, err := provider.Generate(context.Background(), "a cat", &GenerateOptions{SemanticAspectRatio: true})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	defer os.Remove(result.URL)
	if gotResponseFormat != "b64_json" {
		t.Fatalf("semantic response_format = %v, want b64_json", gotResponseFormat)
	}
}

func TestOpenAISemanticEditForcesInlineResponse(t *testing.T) {
	var gotResponseFormat string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		gotResponseFormat = r.FormValue("response_format")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1777599787,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(testGeneratedPNG()) + `"}]}`))
	}))
	defer srv.Close()

	referencePath := filepath.Join(t.TempDir(), "reference.png")
	if err := os.WriteFile(referencePath, testGeneratedPNG(), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := NewOpenAIProvider(&config.ImageAPI{
		Key: "test-key", BaseURL: srv.URL, Provider: "openai", Model: "gpt-image-1",
		ResponseFormat: "url",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	result, err := provider.Generate(context.Background(), "edit the cat", &GenerateOptions{
		RefImagePath: referencePath, SemanticAspectRatio: true,
	})
	if err != nil {
		t.Fatalf("Generate edit: %v", err)
	}
	defer os.Remove(result.URL)
	if gotResponseFormat != "b64_json" {
		t.Fatalf("semantic edit response_format = %q, want b64_json", gotResponseFormat)
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

func TestOpenAIImageURLDownloadFailureLogsDiagnostics(t *testing.T) {
	downloadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("Cf-Ray", "diag-ray")
		w.Header().Set("Location", "https://cdn.example/generated.png?token=redirect-secret")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("blocked token=body-secret"))
	}))
	defer downloadSrv.Close()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	provider := testOpenAIImageDownloadProvider(&logger)
	rawURL := downloadSrv.URL + "/generated.png?signature=request-secret"
	_, err := provider.imageDataToResult(context.Background(), openai.Image{
		URL:           rawURL,
		RevisedPrompt: "internal provider prompt",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var generateErr *GenerateError
	if !errors.As(err, &generateErr) {
		t.Fatalf("error type = %T, want *GenerateError", err)
	}
	if generateErr.Code != "url_download_error" || generateErr.HintMsg != "" {
		t.Fatalf("GenerateError = %#v, want url_download_error without hint", generateErr)
	}

	logged := buf.String()
	for _, want := range []string{
		`"attempts":1`,
		`"status_code":403`,
		`"content_type":"text/plain"`,
		`"cf_ray":"diag-ray"`,
		`"message":"openai: failed to download generated image URL"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log %s missing %s", logged, want)
		}
	}
	for _, secret := range []string{
		rawURL,
		"request-secret",
		"https://cdn.example/generated.png?token=redirect-secret",
		"redirect-secret",
		"body-secret",
		"internal provider prompt",
	} {
		if strings.Contains(logged, secret) {
			t.Fatalf("log leaked %q: %s", secret, logged)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}

func TestOpenAIImageResponseRetriesURLDownloadsForBatch(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testGeneratedPNG())
	}))
	defer srv.Close()

	provider := testOpenAIImageDownloadProvider(testLogger())
	result, err := provider.imageResponseToResult(context.Background(), &openai.ImagesResponse{
		Data: []openai.Image{
			{URL: srv.URL + "/first.png"},
			{B64JSON: base64.StdEncoding.EncodeToString([]byte("second"))},
		},
	})
	if err != nil {
		t.Fatalf("imageResponseToResult: %v", err)
	}
	for _, image := range result.Images {
		defer os.Remove(image.URL)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestOpenAIImageURLDownloadExhaustionLogsAttemptCount(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "temporary failure", http.StatusBadGateway)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	provider := testOpenAIImageDownloadProvider(&logger)
	_, err := provider.imageDataToResult(context.Background(), openai.Image{URL: srv.URL + "/generated.png"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
	logged := buf.String()
	if !strings.Contains(logged, `"attempts":3`) {
		t.Fatalf("log %s missing final attempt count", logged)
	}
	if got := strings.Count(logged, `"message":"openai: retrying generated image URL download"`); got != 2 {
		t.Fatalf("retry log count = %d, want 2; log=%s", got, logged)
	}
}

func TestOpenAIImageResultConversionPreservesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	provider := testOpenAIImageDownloadProvider(&logger)
	_, err := provider.imageDataToResult(ctx, openai.Image{
		URL: "https://cdn.example/generated.png?token=context-secret",
	})
	if err == nil {
		t.Fatal("imageDataToResult error = nil, want context cancellation")
	}
	var generateErr *GenerateError
	if !errors.As(err, &generateErr) {
		t.Fatalf("error type = %T, want *GenerateError", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want errors.Is(context.Canceled)", err)
	}
	logged := buf.String()
	if !strings.Contains(logged, `"attempts":0`) {
		t.Fatalf("log missing zero attempt count: %s", logged)
	}
	if strings.Contains(logged, "context-secret") {
		t.Fatalf("log leaked URL secret: %s", logged)
	}
}

func TestOpenAISaveImageDataPreservesExpiredDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	provider := testOpenAIImageDownloadProvider(testLogger())
	_, err := provider.saveImageData(ctx, openai.Image{
		URL: "https://cdn.example/generated.png?token=deadline-secret",
	})
	if err == nil {
		t.Fatal("saveImageData error = nil, want deadline exceeded")
	}
	var generateErr *GenerateError
	if !errors.As(err, &generateErr) {
		t.Fatalf("error type = %T, want *GenerateError", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want errors.Is(context.DeadlineExceeded)", err)
	}
}

func TestOpenAIImageResponseRemovesEarlierBatchFilesOnFailure(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)

	provider := testOpenAIImageDownloadProvider(testLogger())
	_, err := provider.imageResponseToResult(context.Background(), &openai.ImagesResponse{
		Data: []openai.Image{
			{B64JSON: base64.StdEncoding.EncodeToString([]byte("first"))},
			{B64JSON: "not-valid-base64"},
		},
	})
	if err == nil {
		t.Fatal("imageResponseToResult error = nil, want decode failure")
	}
	entries, readErr := os.ReadDir(tempDir)
	if readErr != nil {
		t.Fatalf("read temp dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("discarded batch files remain: %v", entries)
	}
}

func TestOpenAIUsageValidationRemovesDiscardedResultFiles(t *testing.T) {
	provider := &OpenAIProvider{model: "gpt-image-2", log: testLogger()}
	tests := []struct {
		name        string
		buildResult func(t *testing.T) (*GenerateResult, []string)
	}{
		{
			name: "single",
			buildResult: func(t *testing.T) (*GenerateResult, []string) {
				path := createGeneratedResultFile(t)
				return &GenerateResult{URL: path}, []string{path}
			},
		},
		{
			name: "batch with duplicate primary URL",
			buildResult: func(t *testing.T) (*GenerateResult, []string) {
				first := createGeneratedResultFile(t)
				second := createGeneratedResultFile(t)
				return &GenerateResult{
					URL: first,
					Images: []GeneratedImage{
						{URL: first, Index: 0},
						{URL: second, Index: 1},
					},
				}, []string{first, second}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, paths := tt.buildResult(t)
			err := provider.validateGeneratedResult(result)
			if err == nil {
				t.Fatal("validateGeneratedResult error = nil, want missing usage")
			}
			var generateErr *GenerateError
			if !errors.As(err, &generateErr) || generateErr.Code != "missing_usage" {
				t.Fatalf("error = %#v, want missing_usage GenerateError", err)
			}
			for _, path := range paths {
				if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("discarded result file %q still exists: %v", path, statErr)
				}
			}
		})
	}
}

func createGeneratedResultFile(t *testing.T) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "generated-*.png")
	if err != nil {
		t.Fatalf("create generated result file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close generated result file: %v", err)
	}
	return file.Name()
}

func TestSanitizeImageDownloadBodyPreviewPreservesSafeDiagnostics(t *testing.T) {
	preview := "blocked: upstream unavailable; URL=https://cdn.example/image.png?token=url-secret token=token-secret signature=signature-secret Authorization: Bearer auth-secret Cookie: session=cookie-secret"
	got := sanitizeImageDownloadBodyPreview(preview)

	for _, want := range []string{"blocked", "upstream unavailable"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized preview %q missing %q", got, want)
		}
	}
	for _, secret := range []string{
		"/image.png",
		"url-secret",
		"token-secret",
		"signature-secret",
		"auth-secret",
		"cookie-secret",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized preview leaked %q: %s", secret, got)
		}
	}
	if len(got) > 80 {
		t.Fatalf("sanitized preview length = %d, want <= 80: %q", len(got), got)
	}
}

func TestSanitizeImageDownloadBodyPreviewRedactsCompleteSensitiveHeaders(t *testing.T) {
	tests := []struct {
		name    string
		preview string
		secrets []string
	}{
		{
			name:    "cookie",
			preview: "blocked Cookie: session=first-secret; refresh=second-secret, status=upstream unavailable",
			secrets: []string{"first-secret", "second-secret"},
		},
		{
			name:    "set cookie",
			preview: "blocked Set-Cookie: session=first-secret; Secure; refresh=second-secret, status=upstream unavailable",
			secrets: []string{"first-secret", "second-secret"},
		},
		{
			name:    "authorization",
			preview: "blocked Authorization: Bearer authorization-secret; status=upstream unavailable",
			secrets: []string{"authorization-secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeImageDownloadBodyPreview(tt.preview)
			for _, diagnostic := range []string{"blocked", "status", "upstream unavailable"} {
				if !strings.Contains(got, diagnostic) {
					t.Fatalf("sanitized preview %q lost ordinary diagnostic %q", got, diagnostic)
				}
			}
			for _, secret := range tt.secrets {
				if strings.Contains(got, secret) {
					t.Fatalf("sanitized preview leaked %q: %s", secret, got)
				}
			}
		})
	}
}

func TestSanitizeImageDownloadBodyPreviewRedactsJSONCredentials(t *testing.T) {
	preview := `{"message":"blocked","nested":[{"token":"json-secret"}],"status":"up"}`
	got := sanitizeImageDownloadBodyPreview(preview)

	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("sanitized JSON is invalid: %v; body=%q", err, got)
	}
	if decoded["message"] != "blocked" || decoded["status"] != "up" {
		t.Fatalf("safe JSON fields changed: %#v", decoded)
	}
	nested, ok := decoded["nested"].([]any)
	if !ok || len(nested) != 1 {
		t.Fatalf("nested JSON changed: %#v", decoded["nested"])
	}
	item, ok := nested[0].(map[string]any)
	if !ok || item["token"] != "<redacted>" {
		t.Fatalf("nested credential was not redacted: %#v", nested[0])
	}
	if strings.Contains(got, "json-secret") {
		t.Fatalf("sanitized JSON leaked credential: %s", got)
	}
}

func TestSanitizeImageDownloadBodyPreviewRedactsEscapedJSONURL(t *testing.T) {
	preview := `{"message":"blocked","location":"https:\/\/cdn.example\/image.png?token=url-secret"}`
	got := sanitizeImageDownloadBodyPreview(preview)

	if !strings.Contains(got, `"message":"blocked"`) {
		t.Fatalf("sanitized JSON lost safe message: %s", got)
	}
	for _, secret := range []string{
		`https://cdn.example/image.png?token=url-secret`,
		`/image.png`,
		"url-secret",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized JSON leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "cdn.example") {
		t.Fatalf("sanitized JSON lost safe URL host: %s", got)
	}
}

func TestSanitizeImageDownloadBodyPreviewRedactsEveryJSONCredentialKey(t *testing.T) {
	keys := []string{"token", "SIGNATURE", "sig", "api_key", "api-key", "apikey", "key", "Authorization", "Cookie", "Set-Cookie"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			preview := `{"message":"ok","` + key + `":"credential-secret"}`
			got := sanitizeImageDownloadBodyPreview(preview)
			if strings.Contains(got, "credential-secret") {
				t.Fatalf("sanitized JSON leaked key %q: %s", key, got)
			}
			if !strings.Contains(got, `"message":"ok"`) || !strings.Contains(got, `"<redacted>"`) {
				t.Fatalf("sanitized JSON lost safe data or redaction: %s", got)
			}
		})
	}
}

func TestSanitizeImageDownloadBodyPreviewFallsBackForIncompleteJSON(t *testing.T) {
	preview := `{"message":"blocked","token":"incomplete-secret"`
	got := sanitizeImageDownloadBodyPreview(preview)
	if !strings.Contains(got, `"message":"blocked"`) {
		t.Fatalf("fallback sanitizer lost safe text: %s", got)
	}
	if strings.Contains(got, "incomplete-secret") {
		t.Fatalf("fallback sanitizer leaked credential: %s", got)
	}

	singleQuoted := `'api_key'='single-quoted-secret'; status=upstream unavailable`
	got = sanitizeImageDownloadBodyPreview(singleQuoted)
	if strings.Contains(got, "single-quoted-secret") || !strings.Contains(got, "status=upstream unavailable") {
		t.Fatalf("single-quoted fallback sanitization failed: %s", got)
	}
}

func TestSanitizeImageDownloadBodyPreviewRedactsEscapedURLInIncompleteJSON(t *testing.T) {
	preview := `{"message":"已阻止","location":"https:\/\/cdn.example\/image.png?token=url-secret"`
	got := sanitizeImageDownloadBodyPreview(preview)

	for _, want := range []string{`"message":"已阻止"`, "cdn.example"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized fallback %q missing %q", got, want)
		}
	}
	for _, secret := range []string{"/image.png", "token=", "url-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized fallback leaked %q: %s", secret, got)
		}
	}
	assertBoundedUTF8ImageDownloadPreview(t, got)
}

func TestSanitizeImageDownloadBodyPreviewRedactsExpandedJSONCredentials(t *testing.T) {
	tests := []struct {
		name    string
		preview string
		secrets []string
	}{
		{
			name:    "valid client secret",
			preview: `{"message":"ok","client_secret":"client-value","status":"up"}`,
			secrets: []string{"client-value"},
		},
		{
			name:    "valid nested password",
			preview: `{"message":"ok","nested":{"password":"password-value"},"status":"up"}`,
			secrets: []string{"password-value"},
		},
		{
			name:    "incomplete JSON",
			preview: `{"message":"blocked","client-secret":"client-value","passwd":"password-value"`,
			secrets: []string{"client-value", "password-value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeImageDownloadBodyPreview(tt.preview)
			for _, want := range []string{"message", "status"} {
				if tt.name == "incomplete JSON" && want == "status" {
					continue
				}
				if !strings.Contains(got, want) {
					t.Fatalf("sanitized preview %q missing safe field %q", got, want)
				}
			}
			for _, secret := range tt.secrets {
				if strings.Contains(got, secret) {
					t.Fatalf("sanitized preview leaked %q: %s", secret, got)
				}
			}
			assertBoundedUTF8ImageDownloadPreview(t, got)
		})
	}
}

func TestSanitizeImageDownloadBodyPreviewRedactsAWSQueryCredentials(t *testing.T) {
	preview := "X-Amz-Credential=credential-secret; X-Amz-Signature=signature-secret; status=upstream unavailable"
	got := sanitizeImageDownloadBodyPreview(preview)

	for _, want := range []string{"status", "upstream unavailable"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized preview %q missing %q", got, want)
		}
	}
	for _, secret := range []string{"credential-secret", "signature-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized preview leaked %q: %s", secret, got)
		}
	}
	assertBoundedUTF8ImageDownloadPreview(t, got)
}

func assertBoundedUTF8ImageDownloadPreview(t *testing.T, preview string) {
	t.Helper()
	if len(preview) > 80 {
		t.Fatalf("preview length = %d, want <= 80: %q", len(preview), preview)
	}
	if !utf8.ValidString(preview) {
		t.Fatalf("preview is not valid UTF-8: %q", preview)
	}
}

func testOpenAIImageDownloadProvider(log *zerolog.Logger) *OpenAIProvider {
	return &OpenAIProvider{
		log:             log,
		imageDownloader: downloadTestImage,
		imageDownloadRetryPolicy: &openAIImageDownloadRetryPolicy{
			maxAttempts:  3,
			totalTimeout: time.Second,
			backoffs:     []time.Duration{time.Millisecond, time.Millisecond},
		},
	}
}

func downloadTestImage(ctx context.Context, rawURL string) (string, error) {
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", &wechat.DownloadError{URL: rawURL, Original: err}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", &wechat.DownloadError{URL: rawURL, Elapsed: time.Since(started), Original: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 81))
		return "", &wechat.DownloadError{
			URL:           rawURL,
			StatusCode:    resp.StatusCode,
			ContentType:   resp.Header.Get("Content-Type"),
			ContentLength: resp.Header.Get("Content-Length"),
			Server:        resp.Header.Get("Server"),
			CFRay:         resp.Header.Get("Cf-Ray"),
			Location:      resp.Header.Get("Location"),
			BodyPreview:   string(preview),
			Elapsed:       time.Since(started),
		}
	}
	file, err := os.CreateTemp("", "anban-openai-download-test-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	_, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return "", &wechat.DownloadError{URL: rawURL, Elapsed: time.Since(started), Original: copyErr}
	}
	if closeErr != nil {
		return "", closeErr
	}
	keep = true
	return path, nil
}

func TestOpenAIGeneratedImageDownloadRejectsUnsafeURLWithoutRetry(t *testing.T) {
	provider := &OpenAIProvider{
		log: testLogger(),
		imageDownloadRetryPolicy: &openAIImageDownloadRetryPolicy{
			maxAttempts:  3,
			totalTimeout: time.Second,
			backoffs:     []time.Duration{time.Millisecond, time.Millisecond},
		},
	}

	path, attempts, err := provider.downloadGeneratedImage(context.Background(), "http://127.0.0.1/generated.png")
	defer os.Remove(path)
	if !errors.Is(err, wechat.ErrUnsafeDownloadURL) {
		t.Fatalf("downloadGeneratedImage error = %v, want ErrUnsafeDownloadURL", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func testGeneratedPNG() []byte {
	return []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
}

func TestEffectiveImageDownloadRetryPolicyNormalizesUnsafeValues(t *testing.T) {
	t.Run("missing backoffs do not panic", func(t *testing.T) {
		source := &openAIImageDownloadRetryPolicy{
			maxAttempts:  3,
			totalTimeout: time.Second,
		}
		effective := effectiveImageDownloadRetryPolicy(source)

		if effective.maxAttempts != 3 {
			t.Fatalf("maxAttempts = %d, want 3", effective.maxAttempts)
		}
		if got := effective.backoffAfterAttempt(1); got != 0 {
			t.Fatalf("backoffAfterAttempt(1) = %s, want 0", got)
		}
	})

	t.Run("invalid attempts and timeout use safe defaults", func(t *testing.T) {
		effective := effectiveImageDownloadRetryPolicy(&openAIImageDownloadRetryPolicy{})
		if effective.maxAttempts != 1 {
			t.Fatalf("maxAttempts = %d, want 1", effective.maxAttempts)
		}
		if effective.totalTimeout != 120*time.Second {
			t.Fatalf("totalTimeout = %s, want 2m", effective.totalTimeout)
		}
	})

	t.Run("backoffs are copied", func(t *testing.T) {
		source := &openAIImageDownloadRetryPolicy{
			maxAttempts:  2,
			totalTimeout: time.Second,
			backoffs:     []time.Duration{time.Millisecond},
		}
		effective := effectiveImageDownloadRetryPolicy(source)
		source.backoffs[0] = time.Hour
		if got := effective.backoffAfterAttempt(1); got != time.Millisecond {
			t.Fatalf("backoffAfterAttempt(1) = %s, want 1ms", got)
		}
	})
}

func TestOpenAIGeneratedImageDownloadDoesNotStartWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, attempts, err := testOpenAIImageDownloadProvider(testLogger()).downloadGeneratedImage(ctx, "https://cdn.example/image.png")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if attempts != 0 {
		t.Fatalf("attempts = %d, want 0", attempts)
	}
}

func TestOpenAIGeneratedImageDownloadRetries503(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(testGeneratedPNG())
	}))
	defer srv.Close()

	path, attempts, err := testOpenAIImageDownloadProvider(testLogger()).downloadGeneratedImage(context.Background(), srv.URL+"/generated.png")
	if err != nil {
		t.Fatalf("downloadGeneratedImage: %v", err)
	}
	defer os.Remove(path)
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestOpenAIGeneratedImageDownloadRetriesTruncatedResponse(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Length", "64")
			_, _ = w.Write([]byte("short"))
			return
		}
		_, _ = w.Write(testGeneratedPNG())
	}))
	defer srv.Close()

	path, attempts, err := testOpenAIImageDownloadProvider(testLogger()).downloadGeneratedImage(context.Background(), srv.URL+"/generated.png")
	if err != nil {
		t.Fatalf("downloadGeneratedImage: %v", err)
	}
	defer os.Remove(path)
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestOpenAIGeneratedImageDownloadDoesNotRetry403(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	_, attempts, err := testOpenAIImageDownloadProvider(testLogger()).downloadGeneratedImage(context.Background(), srv.URL+"/generated.png")
	if err == nil {
		t.Fatal("downloadGeneratedImage error = nil, want error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestOpenAIGeneratedImageDownloadStopsAfterThree502Responses(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, attempts, err := testOpenAIImageDownloadProvider(testLogger()).downloadGeneratedImage(context.Background(), srv.URL+"/generated.png")
	if err == nil {
		t.Fatal("downloadGeneratedImage error = nil, want error")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}

func TestOpenAIGeneratedImageDownloadStopsWhenCallerCancels(t *testing.T) {
	var requests atomic.Int32
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan struct {
		attempts int
		err      error
	}, 1)
	go func() {
		_, attempts, err := testOpenAIImageDownloadProvider(testLogger()).downloadGeneratedImage(ctx, srv.URL+"/generated.png")
		result <- struct {
			attempts int
			err      error
		}{attempts: attempts, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download request did not start")
	}
	cancel()
	var got struct {
		attempts int
		err      error
	}
	select {
	case got = <-result:
	case <-time.After(time.Second):
		t.Fatal("download did not stop after cancellation")
	}
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", got.err)
	}
	if got.attempts != 1 {
		t.Fatalf("attempts = %d, want 1", got.attempts)
	}
	if gotRequests := requests.Load(); gotRequests != 1 {
		t.Fatalf("requests = %d, want 1", gotRequests)
	}
}

func TestOpenAIGeneratedImageDownloadHonorsTotalTimeout(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		<-r.Context().Done()
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		log:             testLogger(),
		imageDownloader: downloadTestImage,
		imageDownloadRetryPolicy: &openAIImageDownloadRetryPolicy{
			maxAttempts:  3,
			totalTimeout: 20 * time.Millisecond,
			backoffs:     []time.Duration{time.Millisecond, time.Millisecond},
		},
	}
	_, attempts, err := provider.downloadGeneratedImage(context.Background(), srv.URL+"/generated.png")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestRetryableGeneratedImageDownload(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"transport body error", &wechat.DownloadError{Original: io.ErrUnexpectedEOF}, true},
		{"request timeout", &wechat.DownloadError{StatusCode: http.StatusRequestTimeout}, true},
		{"rate limited", &wechat.DownloadError{StatusCode: http.StatusTooManyRequests}, true},
		{"server error", &wechat.DownloadError{StatusCode: http.StatusInternalServerError}, true},
		{"gateway error", &wechat.DownloadError{StatusCode: 599}, true},
		{"forbidden", &wechat.DownloadError{StatusCode: http.StatusForbidden}, false},
		{"not found", &wechat.DownloadError{StatusCode: http.StatusNotFound}, false},
		{"invalid status", &wechat.DownloadError{StatusCode: 600}, false},
		{"plain error", errors.New("network failed"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableGeneratedImageDownload(tt.err); got != tt.want {
				t.Fatalf("isRetryableGeneratedImageDownload(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestSanitizeImageDownloadErrorRedactsRedirectURL(t *testing.T) {
	initialURL := "https://api.example/generated.png?token=initial-secret"
	redirectURL := "https://cdn.example/image.png?token=redirect-secret"
	err := &wechat.DownloadError{
		URL: initialURL,
		Original: &neturl.Error{
			Op:  "Get",
			URL: redirectURL,
			Err: io.ErrUnexpectedEOF,
		},
	}

	got := sanitizeImageDownloadError(err).Error()
	for _, secret := range []string{initialURL, redirectURL, "initial-secret", "redirect-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized error contains %q: %s", secret, got)
		}
	}
	for _, diagnostic := range []string{"Get", "cdn.example", io.ErrUnexpectedEOF.Error()} {
		if !strings.Contains(got, diagnostic) {
			t.Fatalf("sanitized error missing %q: %s", diagnostic, got)
		}
	}
}

func TestSanitizeImageDownloadErrorPreservesStatusWithoutSourceURL(t *testing.T) {
	err := &wechat.DownloadError{StatusCode: http.StatusServiceUnavailable}
	if got, want := sanitizeImageDownloadError(err).Error(), err.Error(); got != want {
		t.Fatalf("sanitizeImageDownloadError() = %q, want %q", got, want)
	}
}

func TestOpenAIGeneratedImageDownloadRetryLogsSanitizedDiagnostics(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	rawURL := srv.URL + "/generated.png?signature=very-secret"
	_, attempts, err := testOpenAIImageDownloadProvider(&logger).downloadGeneratedImage(context.Background(), rawURL)
	if err == nil {
		t.Fatal("downloadGeneratedImage error = nil, want error")
	}
	if attempts != 3 || requests.Load() != 3 {
		t.Fatalf("attempts/requests = %d/%d, want 3/3", attempts, requests.Load())
	}

	logged := buf.String()
	if got := strings.Count(logged, `"message":"openai: retrying generated image URL download"`); got != 2 {
		t.Fatalf("retry warning count = %d, want 2; logs=%s", got, logged)
	}
	for _, want := range []string{
		`"url_host":"` + strings.TrimPrefix(srv.URL, "http://") + `"`,
		`"attempt":1`,
		`"max_attempts":3`,
		`"status_code":503`,
		`"download_elapsed":`,
		`"retry_delay":1,`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log missing %s: %s", want, logged)
		}
	}
	if strings.Contains(logged, rawURL) {
		t.Fatalf("log contains raw generated URL: %s", logged)
	}
}
