package image

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/app/config"
	"github.com/rs/zerolog"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
)

func TestParseVolcengineSize(t *testing.T) {
	tests := []struct {
		name            string
		size            string
		wantAspectRatio string
		wantSizeTier    string
	}{
		{"empty defaults to 1:1 2K", "", "1:1", "2K"},
		{"1:1 defaults to 2K", "1:1", "1:1", "2K"},
		{"16:9 defaults to 2K", "16:9", "16:9", "2K"},
		{"9:16 defaults to 2K", "9:16", "9:16", "2K"},
		{"3:4 defaults to 2K", "3:4", "3:4", "2K"},
		{"4:3 defaults to 2K", "4:3", "4:3", "2K"},
		{"2:3 defaults to 2K", "2:3", "2:3", "2K"},
		{"3:2 defaults to 2K", "3:2", "3:2", "2K"},
		{"21:9 defaults to 2K", "21:9", "21:9", "2K"},
		{"3:4:1K explicit tier", "3:4:1K", "3:4", "1K"},
		{"16:9:4K explicit tier", "16:9:4K", "16:9", "4K"},
		{"9:16:2K explicit tier", "9:16:2K", "9:16", "2K"},
		{"lowercase tier", "3:4:2k", "3:4", "2K"},
		{"unknown ratio defaults to 1:1 2K", "5:7", "1:1", "2K"},
		{"pixel format not supported, falls back", "1728x2304", "1:1", "2K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRatio, gotTier := parseVolcengineSize(tt.size)
			if gotRatio != tt.wantAspectRatio {
				t.Errorf("parseVolcengineSize(%q) aspectRatio = %q, want %q", tt.size, gotRatio, tt.wantAspectRatio)
			}
			if gotTier != tt.wantSizeTier {
				t.Errorf("parseVolcengineSize(%q) sizeTier = %q, want %q", tt.size, gotTier, tt.wantSizeTier)
			}
		})
	}
}

func TestVolcengineRequestSizeIsOmittedForSemanticTaskGeneration(t *testing.T) {
	if got := volcengineRequestSize("1728x2304", true); got != nil {
		t.Fatalf("semantic request size = %v, want nil", *got)
	}
	got := volcengineRequestSize("1728x2304", false)
	if got == nil || *got != "1728x2304" {
		t.Fatalf("image generation request size = %v, want fixed pixels", got)
	}
}

func TestIsContentSafetyError(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   bool
	}{
		{"sensitive keyword", "content contains sensitive material", true},
		{"safety keyword", "safety policy violation", true},
		{"content_filter keyword", "content_filter triggered", true},
		{"blocked keyword", "request blocked by safety system", true},
		{"Chinese 违规", "提示词违规，无法生成", true},
		{"Chinese 敏感", "包含敏感词汇", true},
		{"Chinese 审核", "图片审核未通过", true},
		{"normal bad request", "invalid parameter: aspect_ratio", false},
		{"rate limit message", "rate limit exceeded", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isContentSafetyError(tt.errMsg)
			if got != tt.want {
				t.Errorf("isContentSafetyError(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
		})
	}
}

func TestVolcenginePixelSize(t *testing.T) {
	tests := []struct {
		aspectRatio string
		sizeTier    string
		wantPixel   string
	}{
		{"1:1", "2K", "2048x2048"},
		{"4:3", "2K", "2304x1728"},
		{"3:4", "2K", "1728x2304"},
		{"16:9", "2K", "2560x1440"},
		{"9:16", "2K", "1440x2560"},
		{"3:2", "2K", "2496x1664"},
		{"2:3", "2K", "1664x2496"},
		{"21:9", "2K", "3024x1296"},
		{"1:1", "4K", "4096x4096"},
		{"4:3", "4K", "4704x3520"},
		{"3:4", "4K", "3520x4704"},
		{"16:9", "4K", "5504x3040"},
		{"9:16", "4K", "3040x5504"},
		{"3:2", "4K", "4992x3328"},
		{"2:3", "4K", "3328x4992"},
		{"21:9", "4K", "6240x2656"},
		{"unknown", "2K", "2048x2048"}, // fallback
	}

	for _, tt := range tests {
		t.Run(tt.aspectRatio+":"+tt.sizeTier, func(t *testing.T) {
			got := volcenginePixelSize(tt.aspectRatio, tt.sizeTier)
			if got != tt.wantPixel {
				t.Errorf("volcenginePixelSize(%q, %q) = %q, want %q", tt.aspectRatio, tt.sizeTier, got, tt.wantPixel)
			}
		})
	}
}

// makeVolcengineTestServer creates an httptest.Server that returns the given response.
func makeVolcengineTestServer(t *testing.T, statusCode int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

// makeVolcengineProvider creates a VolcengineProvider pointing at the given test server URL.
func makeVolcengineProvider(t *testing.T, serverURL string, volcCfg *config.VolcengineConfig) *VolcengineProvider {
	t.Helper()
	client := arkruntime.NewClientWithApiKey("test-key",
		arkruntime.WithBaseUrl(serverURL),
		arkruntime.WithTimeout(5*time.Second),
	)
	nopLog := zerolog.Nop()
	return &VolcengineProvider{
		client:     client,
		model:      "doubao-seedream-4-5-251128",
		sizePixel:  "1728x2304",
		volcConfig: volcCfg,
		log:        &nopLog,
	}
}

func TestVolcengineGenerate_Success(t *testing.T) {
	imageURL := "https://example.com/image.png"
	srv := makeVolcengineTestServer(t, http.StatusOK, map[string]any{
		"data": []map[string]any{
			{"url": imageURL},
		},
	})
	defer srv.Close()

	p := makeVolcengineProvider(t, srv.URL, nil)
	result, err := p.Generate(context.Background(), "a cat in the garden", nil)
	if err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}
	if result.URL != imageURL {
		t.Errorf("Generate() URL = %q, want %q", result.URL, imageURL)
	}
	if result.Model != p.model {
		t.Errorf("Generate() Model = %q, want %q", result.Model, p.model)
	}
	// Size should be the ratio derived from sizePixel, not the raw pixel string
	wantSize, _ := ParseSize(p.sizePixel)
	if result.Size != wantSize {
		t.Errorf("Generate() Size = %q, want ratio %q", result.Size, wantSize)
	}
	if result.ResponseType != "url" {
		t.Errorf("Generate() ResponseType = %q, want url", result.ResponseType)
	}
	if result.ResponsePreview != imageURL {
		t.Errorf("Generate() ResponsePreview = %q, want %q", result.ResponsePreview, imageURL)
	}
}

func TestVolcengineGenerate_WithAdvancedOptions(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"url": "https://example.com/image.png"},
			},
		})
	}))
	defer srv.Close()

	optimizePrompt := true
	volcCfg := &config.VolcengineConfig{
		OptimizePrompt: &optimizePrompt,
		OutputFormat:   "png",
	}
	p := makeVolcengineProvider(t, srv.URL, volcCfg)
	_, err := p.Generate(context.Background(), "a landscape", nil)
	if err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}
	if v, ok := capturedBody["optimize_prompt"]; !ok || v != true {
		t.Errorf("expected optimize_prompt=true in request body, got %v", capturedBody["optimize_prompt"])
	}
	if v, ok := capturedBody["output_format"]; !ok || v != "png" {
		t.Errorf("expected output_format=png in request body, got %v", capturedBody["output_format"])
	}
}

func TestVolcengineGenerate_SendsMultipleReferenceImages(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"url": "https://example.com/image.png"},
			},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	refOne := filepath.Join(dir, "one.png")
	refTwo := filepath.Join(dir, "two.jpg")
	if err := os.WriteFile(refOne, []byte("one"), 0644); err != nil {
		t.Fatalf("write ref one: %v", err)
	}
	if err := os.WriteFile(refTwo, []byte("two"), 0644); err != nil {
		t.Fatalf("write ref two: %v", err)
	}

	p := makeVolcengineProvider(t, srv.URL, nil)
	_, err := p.Generate(context.Background(), "a product scene", &GenerateOptions{
		RefImagePaths: []string{refOne, refTwo},
	})
	if err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	images, ok := capturedBody["image"].([]any)
	if !ok {
		t.Fatalf("request image = %#v, want array", capturedBody["image"])
	}
	if len(images) != 2 {
		t.Fatalf("request image count = %d, want 2", len(images))
	}
	wantOne := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("one"))
	wantTwo := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("two"))
	if images[0] != wantOne || images[1] != wantTwo {
		t.Fatalf("request image = %#v, want [%q %q]", images, wantOne, wantTwo)
	}
}

func TestVolcengineGenerate_NoImage(t *testing.T) {
	srv := makeVolcengineTestServer(t, http.StatusOK, map[string]any{
		"data": []map[string]any{},
	})
	defer srv.Close()

	p := makeVolcengineProvider(t, srv.URL, nil)
	_, err := p.Generate(context.Background(), "a cat", nil)
	if err == nil {
		t.Fatal("Generate() expected error for empty data, got nil")
	}
	genErr, ok := err.(*GenerateError)
	if !ok {
		t.Fatalf("expected *GenerateError, got %T", err)
	}
	if genErr.Code != "no_image" {
		t.Errorf("expected code=no_image, got %q", genErr.Code)
	}
}

func TestVolcengineGenerate_Unauthorized(t *testing.T) {
	srv := makeVolcengineTestServer(t, http.StatusUnauthorized, map[string]any{
		"error": map[string]any{
			"message": "invalid api key",
			"type":    "authentication_error",
		},
	})
	defer srv.Close()

	p := makeVolcengineProvider(t, srv.URL, nil)
	_, err := p.Generate(context.Background(), "a cat", nil)
	if err == nil {
		t.Fatal("Generate() expected error, got nil")
	}
	genErr, ok := err.(*GenerateError)
	if !ok {
		t.Fatalf("expected *GenerateError, got %T", err)
	}
	if genErr.Code != "unauthorized" {
		t.Errorf("expected code=unauthorized, got %q", genErr.Code)
	}
}

func TestVolcengineGenerate_RateLimit(t *testing.T) {
	srv := makeVolcengineTestServer(t, http.StatusTooManyRequests, map[string]any{
		"error": map[string]any{
			"message": "rate limit exceeded",
			"type":    "rate_limit_error",
		},
	})
	defer srv.Close()

	p := makeVolcengineProvider(t, srv.URL, nil)
	_, err := p.Generate(context.Background(), "a cat", nil)
	if err == nil {
		t.Fatal("Generate() expected error, got nil")
	}
	genErr, ok := err.(*GenerateError)
	if !ok {
		t.Fatalf("expected *GenerateError, got %T", err)
	}
	if genErr.Code != "rate_limit" {
		t.Errorf("expected code=rate_limit, got %q", genErr.Code)
	}
}

func TestVolcengineGenerate_ContentSafetyBlocked(t *testing.T) {
	srv := makeVolcengineTestServer(t, http.StatusBadRequest, map[string]any{
		"error": map[string]any{
			"message": "提示词违规，包含敏感内容",
			"type":    "invalid_request_error",
		},
	})
	defer srv.Close()

	p := makeVolcengineProvider(t, srv.URL, nil)
	_, err := p.Generate(context.Background(), "bad prompt", nil)
	if err == nil {
		t.Fatal("Generate() expected error, got nil")
	}
	genErr, ok := err.(*GenerateError)
	if !ok {
		t.Fatalf("expected *GenerateError, got %T", err)
	}
	if genErr.Code != "safety_blocked" {
		t.Errorf("expected code=safety_blocked, got %q", genErr.Code)
	}
}
