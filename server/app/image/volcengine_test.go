package image

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/app/config"
	"github.com/rs/zerolog"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
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

func TestVolcengineConvertSDKError_ClassifiesHTMLResponseAtHTTP200(t *testing.T) {
	p := &VolcengineProvider{name: "Volcengine", endpoint: "https://ark.example.test/api/v3/images/generations"}
	err := &model.RequestError{
		HTTPStatusCode: http.StatusOK,
		RequestId:      "request-html-200",
		Err:            errors.New("invalid character '<' looking for beginning of value"),
	}

	converted := p.convertSDKError(err)
	genErr, ok := converted.(*GenerateError)
	if !ok {
		t.Fatalf("converted error = %T, want *GenerateError", converted)
	}
	if genErr.Code != "endpoint_protocol" || genErr.Retryable() {
		t.Fatalf("error = %#v, want non-retryable endpoint_protocol", genErr)
	}
	if genErr.HTTPStatusCode != http.StatusOK || genErr.ProviderRequestID != "request-html-200" {
		t.Fatalf("diagnostics = %#v", genErr)
	}
	if genErr.RequestMethod != http.MethodPost || genErr.RequestEndpoint != "https://ark.example.test/api/v3/images/generations" {
		t.Fatalf("request diagnostics = %#v", genErr)
	}
	if genErr.TransportPhase != "protocol" {
		t.Fatalf("transport phase = %q, want protocol", genErr.TransportPhase)
	}
	if genErr.Message == "" || genErr.HintMsg == "" {
		t.Fatalf("error lacks safe operator guidance: %#v", genErr)
	}
}

func TestVolcengineGenerate_LogsStructuredProtocolFailureWithoutRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>gateway error</html>"))
	}))
	defer srv.Close()

	var logs bytes.Buffer
	logger := zerolog.New(&logs)
	p := makeVolcengineProvider(t, srv.URL, nil)
	p.log = &logger
	_, err := p.Generate(context.Background(), "a cat", nil)
	genErr, ok := err.(*GenerateError)
	if !ok || genErr.Code != "endpoint_protocol" {
		t.Fatalf("error = %#v, want endpoint_protocol GenerateError", err)
	}
	if strings.Contains(logs.String(), "invalid character") || strings.Contains(logs.String(), "gateway error") {
		t.Fatalf("log exposed raw provider response: %s", logs.String())
	}
	for _, field := range []string{"endpoint_protocol", "http_status", "provider_request_id", "retryable"} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("structured log missing %q: %s", field, logs.String())
		}
	}
	for _, field := range []string{"request_method", "request_endpoint", "response_content_type", "transport_phase"} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("structured log missing %q: %s", field, logs.String())
		}
	}
}

func TestNewVolcengineProviderCapturesResponseRequestDiagnostics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>gateway error</html>"))
	}))
	defer srv.Close()

	nopLog := zerolog.Nop()
	p, err := NewVolcengineProvider(&config.ImageAPI{Key: "test-key", BaseURL: srv.URL, Model: "seedream-test"}, &nopLog)
	if err != nil {
		t.Fatalf("NewVolcengineProvider() error = %v", err)
	}
	_, err = p.Generate(context.Background(), "a cat", nil)
	genErr, ok := err.(*GenerateError)
	if !ok {
		t.Fatalf("error = %T, want *GenerateError", err)
	}
	if genErr.RequestMethod != http.MethodPost || genErr.RequestEndpoint != srv.URL+"/images/generations" || genErr.ResponseContentType != "text/html; charset=utf-8" || genErr.TransportPhase != "protocol" {
		t.Fatalf("request diagnostics = %#v", genErr)
	}
}

func TestVolcengineConvertSDKError_ClassifiesTransportFailureWithConcreteRequest(t *testing.T) {
	p := &VolcengineProvider{name: "TokenPlayer", endpoint: "https://token.example.test/api/v3/images/generations"}
	err := &model.RequestError{
		HTTPStatusCode: http.StatusInternalServerError,
		RequestId:      "sdk-request-network",
		Err: &requestTransportError{
			observation: requestObservation{
				Method:         http.MethodPost,
				Endpoint:       "https://token.example.test/api/v3/images/generations",
				TransportPhase: "dns",
			},
			cause: errors.New("dial tcp: lookup token.example.test: no such host"),
		},
	}

	converted := p.convertSDKError(err)
	genErr, ok := converted.(*GenerateError)
	if !ok {
		t.Fatalf("converted error = %T, want *GenerateError", converted)
	}
	if genErr.Code != "network_error" || !genErr.Retryable() {
		t.Fatalf("error = %#v, want retryable network_error", genErr)
	}
	if genErr.Provider != "TokenPlayer" || genErr.RequestMethod != http.MethodPost || genErr.RequestEndpoint != "https://token.example.test/api/v3/images/generations" || genErr.TransportPhase != "dns" {
		t.Fatalf("request diagnostics = %#v", genErr)
	}
	if genErr.HTTPStatusCode != 0 {
		t.Fatalf("transport failure status = %d, want 0 because SDK 500 is a sentinel", genErr.HTTPStatusCode)
	}
}

func TestVolcengineConvertSDKError_APIErrorIncludesConcreteRequest(t *testing.T) {
	p := &VolcengineProvider{name: "TokenPlayer", endpoint: "https://token.example.test/api/v3/images/generations"}
	converted := p.convertSDKError(&model.APIError{HTTPStatusCode: http.StatusUnauthorized, RequestId: "upstream-request-401", Message: "unauthorized"})
	genErr, ok := converted.(*GenerateError)
	if !ok {
		t.Fatalf("converted error = %T, want *GenerateError", converted)
	}
	if genErr.RequestMethod != http.MethodPost || genErr.RequestEndpoint != "https://token.example.test/api/v3/images/generations" {
		t.Fatalf("request diagnostics = %#v", genErr)
	}
}

func TestSanitizeImageProviderEndpointRemovesCredentialsAndQuery(t *testing.T) {
	got := sanitizeImageProviderEndpoint("https://user:secret@example.test/api/v3?key=do-not-log#fragment")
	if got != "https://example.test/api/v3" {
		t.Fatalf("sanitized endpoint = %q", got)
	}
}

func TestDiagnosticRoundTripperOnlyCachesCorrelatableHTTPErrorObservations(t *testing.T) {
	for _, tc := range []struct {
		name             string
		echoRequestID    bool
		wantCachedRecord bool
	}{
		{name: "provider echoes client request id", echoRequestID: true, wantCachedRecord: true},
		{name: "provider omits client request id", echoRequestID: false, wantCachedRecord: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.echoRequestID {
					w.Header().Set(model.ClientRequestHeader, r.Header.Get(model.ClientRequestHeader))
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
			}))
			defer srv.Close()

			diagnostics := &imageProviderDiagnostics{}
			transport := &diagnosticRoundTripper{base: http.DefaultTransport, diagnostics: diagnostics}
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/images/generations", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set(model.ClientRequestHeader, "client-request-1")
			resp, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			_ = resp.Body.Close()

			_, cached := diagnostics.observations.Load("client-request-1")
			if cached != tc.wantCachedRecord {
				t.Fatalf("cached request observation = %v, want %v", cached, tc.wantCachedRecord)
			}
		})
	}
}

func TestGenerateErrorIncludesSafeRequestDiagnostics(t *testing.T) {
	err := &GenerateError{
		Provider:          "TokenPlayer",
		Code:              "network_error",
		Message:           "网络请求失败，请检查网络连接",
		HintMsg:           "确认网络连接、API 地址和请求路径正确",
		RequestMethod:     http.MethodPost,
		RequestEndpoint:   "https://user:secret@token.example.test/api/v3/images/generations?key=secret",
		HTTPStatusCode:    http.StatusInternalServerError,
		ProviderRequestID: "sdk-request-1",
		TransportPhase:    "dns",
	}
	message := err.Error()
	for _, want := range []string{"POST", "https://token.example.test/api/v3/images/generations", "HTTP 500", "dns", "sdk-request-1"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error = %q, missing %q", message, want)
		}
	}
	for _, secret := range []string{"user:secret", "?key=secret"} {
		if strings.Contains(message, secret) {
			t.Fatalf("error leaked %q: %s", secret, message)
		}
	}
}

func TestClassifyImageTransportPhase(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "dns", err: &net.DNSError{Err: "no such host", Name: "images.example.test"}, want: "dns"},
		{name: "connect", err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}, want: "connect"},
		{name: "tls", err: &net.OpError{Op: "dial", Err: &tls.CertificateVerificationError{Err: errors.New("x509: unknown authority")}}, want: "tls"},
		{name: "timeout", err: context.DeadlineExceeded, want: "timeout"},
		{name: "canceled", err: context.Canceled, want: "canceled"},
		{name: "unknown", err: errors.New("unexpected transport failure"), want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyImageTransportPhase(tt.err); got != tt.want {
				t.Fatalf("classifyImageTransportPhase(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
