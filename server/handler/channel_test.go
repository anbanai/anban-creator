package handler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/platform"
	"github.com/anbanai/anban-creator/server/storage"
)

type fakeProjectLLM struct {
	response string
	err      error
	prompt   string
}

func (f *fakeProjectLLM) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	f.prompt = userPrompt
	return f.response, f.err
}

func (f *fakeProjectLLM) CompleteWithImage(_ context.Context, systemPrompt, userPrompt, imageURL string) (string, error) {
	f.prompt = userPrompt
	return f.response, f.err
}

type fakeStorageProvider struct {
	data map[string][]byte
	read []string
}

var _ storage.Provider = (*fakeStorageProvider)(nil)

func (f *fakeStorageProvider) Name() string { return "fake" }

func (f *fakeStorageProvider) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorageProvider) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorageProvider) UploadURL(context.Context, string, string, int) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (f *fakeStorageProvider) GetURL(key string) string { return "/api/v1/files/" + key }

func (f *fakeStorageProvider) Read(_ context.Context, key string) ([]byte, error) {
	f.read = append(f.read, key)
	if data, ok := f.data[key]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeStorageProvider) Delete(context.Context, string) error { return nil }

func (f *fakeStorageProvider) DownloadURL(context.Context, string, int) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (f *fakeStorageProvider) HasCustomDomain() bool { return false }

// IsOwnedURL is a plumbing fake: it accepts both the Local prefix and a
// fixed OSS hostname so handler tests can exercise the OSS routing branch.
// It does NOT replicate the real SSRF defenses (subdomain spoofing, scheme
// rejection, case-insensitive host match) — those are exercised against the
// production provider in server/storage/owned_url_test.go.
func (f *fakeStorageProvider) IsOwnedURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "/api/v1/files/") ||
		strings.HasPrefix(rawURL, "https://fake-bucket.oss-cn-hangzhou.aliyuncs.com/")
}

func TestProjectFetchProfileAIAnalysisMergesFields(t *testing.T) {
	llm := &fakeProjectLLM{response: `{
		"positioning": "面向职场人的高效生活方式账号",
		"keywords": ["职场", "效率", "生活方式"],
		"style": "清爽明亮的实拍封面，搭配高对比标题字",
		"content_summary": "围绕职场效率和日常习惯做可执行分享"
	}`}
	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetLLMClient(llm, 0)
	profile := &platform.PlatformProfile{
		Name:        "测试账号",
		Positioning: "原始简介",
		RawData: map[string]any{
			"top_posts": []platform.SeednotePost{
				{Title: "爆款选题", LikeCount: 12000, CommentCount: 200, EngagementScore: 12200},
			},
		},
	}

	h.enrichSeednoteProfileWithAI(context.Background(), "user-1", profile)

	if profile.Positioning != "面向职场人的高效生活方式账号" {
		t.Fatalf("Positioning = %q", profile.Positioning)
	}
	if profile.Keywords != "职场, 效率, 生活方式" {
		t.Fatalf("Keywords = %q", profile.Keywords)
	}
	if profile.Style != "清爽明亮的实拍封面，搭配高对比标题字" {
		t.Fatalf("Style = %q", profile.Style)
	}
	if llm.prompt == "" || !containsAll(llm.prompt, "爆款选题", "原始简介") {
		t.Fatalf("prompt missing profile/post context: %q", llm.prompt)
	}
	analysis, ok := profile.RawData["analysis"].(seednoteProfileAnalysis)
	if !ok {
		t.Fatalf("RawData[analysis] type = %T", profile.RawData["analysis"])
	}
	if analysis.ContentSummary == "" {
		t.Fatal("analysis.ContentSummary is empty")
	}
}

func TestProjectFetchProfileAIAnalysisFallbackOnInvalidJSON(t *testing.T) {
	llm := &fakeProjectLLM{response: `not json`}
	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetLLMClient(llm, 0)
	profile := &platform.PlatformProfile{
		Name:        "测试账号",
		Positioning: "原始简介",
		RawData:     map[string]any{},
	}

	h.enrichSeednoteProfileWithAI(context.Background(), "user-1", profile)

	if profile.Positioning != "原始简介" {
		t.Fatalf("Positioning = %q, want 原始简介", profile.Positioning)
	}
	if profile.Keywords != "" {
		t.Fatalf("Keywords = %q, want empty", profile.Keywords)
	}
}

func TestProjectFetchProfileRejectsNonSeednoteAutoFetch(t *testing.T) {
	h := NewProjectHandler(nil, testProjectLogger(t))
	app := fiber.New()
	app.Post("/fetch", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.FetchProfile(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/fetch", `{"platform":"article","profile_url":"https://mp.weixin.qq.com/test"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestAnalyzeImageRejectsInternalFileFromDifferentUser(t *testing.T) {
	store := &fakeStorageProvider{
		data: map[string][]byte{
			"uploads/projects/user-2/reference.png": tinyPNG(),
		},
	}
	llm := &fakeProjectLLM{response: "清爽自然的视觉风格"}
	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetStore(store)
	h.SetLLMClient(llm, 0)

	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"/api/v1/files/uploads/projects/user-2/reference.png"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	if len(store.read) != 0 {
		t.Fatalf("store.Read should not be called for unauthorized files, got %v", store.read)
	}
}

func TestAnalyzeImageAllowsOwnedInternalFile(t *testing.T) {
	key := "uploads/projects/user-1/reference.png"
	store := &fakeStorageProvider{
		data: map[string][]byte{
			key: tinyPNG(),
		},
	}
	llm := &fakeProjectLLM{response: "清爽自然的视觉风格"}
	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetStore(store)
	h.SetLLMClient(llm, 0)

	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"/api/v1/files/`+key+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	if len(store.read) != 1 || store.read[0] != key {
		t.Fatalf("store.Read keys = %v, want [%s]", store.read, key)
	}
	if llm.prompt == "" {
		t.Fatal("expected image analysis prompt to be sent to LLM")
	}
}

// Regression: template uploads use purpose="reference" → key prefix is
// "uploads/references/{user}/". Previously cleanOwnedUploadKey only allowed
// "uploads/projects/{user}/" so this path returned 403.
func TestAnalyzeImageAllowsReferenceUploadPrefix(t *testing.T) {
	key := "uploads/references/user-1/abc.png"
	store := &fakeStorageProvider{
		data: map[string][]byte{key: tinyPNG()},
	}
	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetStore(store)
	h.SetLLMClient(&fakeProjectLLM{response: "风格"}, 0)

	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"/api/v1/files/`+key+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	if len(store.read) != 1 || store.read[0] != key {
		t.Fatalf("store.Read keys = %v, want [%s]", store.read, key)
	}
}

// Regression (main reported bug): OSS-stored image URLs are HTTPS and look
// external, but private buckets reject unsigned GETs with 403. The fix
// routes server-owned HTTPS URLs through store.Read (which signs OSS URLs
// internally) instead of the external download path.
func TestAnalyzeImageAllowsOwnedOSSURL(t *testing.T) {
	key := "uploads/references/user-1/abc.png"
	store := &fakeStorageProvider{
		data: map[string][]byte{key: tinyPNG()},
	}
	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetStore(store)
	h.SetLLMClient(&fakeProjectLLM{response: "风格"}, 0)

	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})

	imageURL := "https://fake-bucket.oss-cn-hangzhou.aliyuncs.com/" + key
	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"`+imageURL+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d (OSS URL must be read via store.Read, not external download)", resp.StatusCode, fiber.StatusOK)
	}
	if len(store.read) != 1 || store.read[0] != key {
		t.Fatalf("store.Read keys = %v, want [%s] (OSS URL key extraction)", store.read, key)
	}
}

// Cross-user isolation on the Local references/ prefix (the new prefix
// added by this fix). Mirrors TestAnalyzeImageRejectsInternalFileFromDifferentUser
// which only covers uploads/projects/.
func TestAnalyzeImageRejectsReferenceUploadFromDifferentUser(t *testing.T) {
	key := "uploads/references/user-2/abc.png"
	store := &fakeStorageProvider{
		data: map[string][]byte{key: tinyPNG()},
	}
	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetStore(store)
	h.SetLLMClient(&fakeProjectLLM{response: "风格"}, 0)

	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"/api/v1/files/`+key+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	if len(store.read) != 0 {
		t.Fatalf("store.Read should not be called for another user's file, got %v", store.read)
	}
}

// Cross-user isolation also applies to OSS URLs: a URL pointing at another
// user's storage key must be rejected before store.Read.
func TestAnalyzeImageRejectsOwnedOSSURLFromDifferentUser(t *testing.T) {
	key := "uploads/references/user-2/abc.png"
	store := &fakeStorageProvider{
		data: map[string][]byte{key: tinyPNG()},
	}
	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetStore(store)
	h.SetLLMClient(&fakeProjectLLM{response: "风格"}, 0)

	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})

	imageURL := "https://fake-bucket.oss-cn-hangzhou.aliyuncs.com/" + key
	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"`+imageURL+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	if len(store.read) != 0 {
		t.Fatalf("store.Read should not be called for another user's file, got %v", store.read)
	}
}

// When a dedicated vision client is wired, AnalyzeImage must route the image
// to it instead of the writing LLM. This is the regression test for the bug
// where Kimi (text-only writing model) was being asked to analyze images,
// causing every recognition request to fail.
func TestAnalyzeImagePrefersVisionClient(t *testing.T) {
	key := "uploads/references/user-1/abc.png"
	store := &fakeStorageProvider{data: map[string][]byte{key: tinyPNG()}}
	writingLLM := &fakeProjectLLM{response: "from-writing"}
	visionLLM := &fakeProjectLLM{response: "from-vision"}

	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetStore(store)
	h.SetLLMClient(writingLLM, 0)
	h.SetVisionClient(visionLLM)

	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"/api/v1/files/`+key+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	if visionLLM.prompt == "" {
		t.Fatal("vision LLM should have been called when SetVisionClient was wired")
	}
	if writingLLM.prompt != "" {
		t.Fatal("writing LLM should NOT be called when vision client is available")
	}
}

// Without a vision client, AnalyzeImage falls back to the writing LLM so
// existing deployments without a vision model configured still work.
func TestAnalyzeImageFallsBackToWritingLLM(t *testing.T) {
	key := "uploads/references/user-1/abc.png"
	store := &fakeStorageProvider{data: map[string][]byte{key: tinyPNG()}}
	writingLLM := &fakeProjectLLM{response: "from-writing"}

	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetStore(store)
	h.SetLLMClient(writingLLM, 0)
	// No SetVisionClient call.

	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"/api/v1/files/`+key+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	if writingLLM.prompt == "" {
		t.Fatal("writing LLM should be used as fallback when no vision client is configured")
	}
}

// Server-side timeout (context.DeadlineExceeded) must surface as 503, not 500,
// so the client can distinguish transient capacity issues from real failures.
func TestAnalyzeImageReturns503OnDeadlineExceeded(t *testing.T) {
	key := "uploads/references/user-1/abc.png"
	store := &fakeStorageProvider{data: map[string][]byte{key: tinyPNG()}}
	visionLLM := &fakeProjectLLM{err: context.DeadlineExceeded}

	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetStore(store)
	h.SetVisionClient(visionLLM)

	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"/api/v1/files/`+key+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (DeadlineExceeded → 503)", resp.StatusCode, fiber.StatusServiceUnavailable)
	}
}

// Client cancellation (context.Canceled) must also surface as 503, not 500.
// Although Fiber v3 does not currently propagate client disconnects as
// context.Canceled, defensive handling keeps the log signal correct if the
// upstream behavior ever changes.
func TestAnalyzeImageReturns503OnCanceled(t *testing.T) {
	key := "uploads/references/user-1/abc.png"
	store := &fakeStorageProvider{data: map[string][]byte{key: tinyPNG()}}
	visionLLM := &fakeProjectLLM{err: context.Canceled}

	h := NewProjectHandler(nil, testProjectLogger(t))
	h.SetStore(store)
	h.SetVisionClient(visionLLM)

	app := fiber.New()
	app.Post("/analyze", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.AnalyzeImage(c)
	})

	resp, err := app.Test(httptestJSON("POST", "/analyze", `{"image_url":"/api/v1/files/`+key+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (Canceled → 503)", resp.StatusCode, fiber.StatusServiceUnavailable)
	}
}

func TestGetPublicHTTPSImageRejectsLoopbackHost(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tinyPNG())
	}))
	defer server.Close()

	if _, err := getPublicHTTPSImage(context.Background(), server.URL, 10<<20); err == nil {
		t.Fatal("expected loopback HTTPS URL to be rejected")
	}
}

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "public ipv4", ip: "8.8.8.8", want: true},
		{name: "private ipv4", ip: "10.0.0.1", want: false},
		{name: "loopback ipv4", ip: "127.0.0.1", want: false},
		{name: "link local ipv4", ip: "169.254.1.1", want: false},
		{name: "public ipv6", ip: "2001:4860:4860::8888", want: true},
		{name: "unique local ipv6", ip: "fc00::1", want: false},
		{name: "loopback ipv6", ip: "::1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPublicIP(net.ParseIP(tt.ip)); got != tt.want {
				t.Fatalf("isPublicIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func httptestJSON(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func testProjectLogger(t *testing.T) *zerolog.Logger {
	t.Helper()
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	return &logger
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}

func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde,
	}
}
