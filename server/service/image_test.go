package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconfig "github.com/anbanai/anban-creator/app/config"
	appimage "github.com/anbanai/anban-creator/app/image"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/rs/zerolog"
)

func TestBuildImageResultIncludesGenerationMetadata(t *testing.T) {
	raw := &appimage.GenerateRawResult{
		URL:             "data:image/png;base64,iVBORw0KGgo=",
		Size:            "3:4",
		Prompt:          "春日饮茶指南",
		Provider:        "Volcengine",
		Model:           "doubao-seedream",
		RevisedPrompt:   "optimized prompt",
		ResponseType:    "url",
		ResponsePreview: "https://example.com/image.png",
		OutputMIME:      "image/png",
	}

	got := buildImageResult(raw, "cover")

	if got.Prompt != "春日饮茶指南" {
		t.Fatalf("Prompt = %q, want 春日饮茶指南", got.Prompt)
	}
	if got.ImageType != "cover" {
		t.Fatalf("ImageType = %q, want cover", got.ImageType)
	}
	if got.Provider != "Volcengine" {
		t.Fatalf("Provider = %q, want Volcengine", got.Provider)
	}
	if got.Model != "doubao-seedream" {
		t.Fatalf("Model = %q, want doubao-seedream", got.Model)
	}
	if got.RevisedPrompt != "optimized prompt" {
		t.Fatalf("RevisedPrompt = %q, want optimized prompt", got.RevisedPrompt)
	}
	if got.ResponseType != "url" {
		t.Fatalf("ResponseType = %q, want url", got.ResponseType)
	}
	if got.OutputMIME != "image/png" {
		t.Fatalf("OutputMIME = %q, want image/png", got.OutputMIME)
	}
}

func TestSaveGeneratedImageBytesReencodesJPEGWhenOutputIsPNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 120, G: 80, B: 40, A: 255})

	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "cover.png")
	outputMIME, err := saveGeneratedImageBytes(outputPath, jpegBuf.Bytes())
	if err != nil {
		t.Fatalf("saveGeneratedImageBytes() error = %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if detected := http.DetectContentType(data); detected != "image/png" {
		t.Fatalf("output MIME = %q, want image/png", detected)
	}
	if outputMIME != "image/png" {
		t.Fatalf("returned MIME = %q, want image/png", outputMIME)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("png.Decode(output) error = %v", err)
	}
}

func TestGeneratedImageTaskOutputPathUsesServerTempFile(t *testing.T) {
	localPath, cleanup, err := generatedImageTaskOutputPath("output/seednote/cover.png")
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("generatedImageTaskOutputPath() error = %v", err)
	}
	if localPath == "output/seednote/cover.png" {
		t.Fatal("generatedImageTaskOutputPath() returned the logical output_path; want server-local temp path")
	}
	if !filepath.IsAbs(localPath) {
		t.Fatalf("generatedImageTaskOutputPath() = %q, want absolute temp path", localPath)
	}
	if filepath.Ext(localPath) != ".png" {
		t.Fatalf("generatedImageTaskOutputPath() ext = %q, want .png", filepath.Ext(localPath))
	}
}

func TestSaveGeneratedImageForOutputKeepsWritableAbsoluteTaskPath(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "output", "cover.png")
	savePath, _, cleanup, err := saveGeneratedImageForOutput(outputPath, tinyImagePNG(t), "task-1")
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("saveGeneratedImageForOutput() error = %v", err)
	}
	if savePath != outputPath {
		t.Fatalf("savePath = %q, want original writable output path %q", savePath, outputPath)
	}
	if cleanup != nil {
		t.Fatal("cleanup should be nil for a durable workspace file")
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected image at workspace output path: %v", err)
	}
}

func TestSaveGeneratedImageForOutputUsesTempForTaskRelativePath(t *testing.T) {
	savePath, _, cleanup, err := saveGeneratedImageForOutput("output/cover.png", tinyImagePNG(t), "task-1")
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("saveGeneratedImageForOutput() error = %v", err)
	}
	if savePath == "output/cover.png" || !filepath.IsAbs(savePath) {
		t.Fatalf("savePath = %q, want server-local temp path", savePath)
	}
	if cleanup == nil {
		t.Fatal("cleanup should be present for task temp files")
	}
}

func TestSaveGeneratedImageForOutputFallsBackToTempWhenTaskPathUnavailable(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	outputPath := filepath.Join(parentFile, "cover.png")
	savePath, _, cleanup, err := saveGeneratedImageForOutput(outputPath, tinyImagePNG(t), "task-1")
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("saveGeneratedImageForOutput() error = %v", err)
	}
	if savePath == outputPath || !filepath.IsAbs(savePath) {
		t.Fatalf("savePath = %q, want fallback temp path", savePath)
	}
	if cleanup == nil {
		t.Fatal("cleanup should be present for fallback temp files")
	}
}

func TestDetectTaskFileMIMEUsesImageContentBeforeExtension(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})

	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	path := filepath.Join(t.TempDir(), "image_01.png")
	if err := os.WriteFile(path, jpegBuf.Bytes(), 0644); err != nil {
		t.Fatalf("write jpeg bytes: %v", err)
	}

	if got := DetectTaskFileMIME(path); got != "image/jpeg" {
		t.Fatalf("DetectTaskFileMIME() = %q, want image/jpeg", got)
	}
}

func tinyImagePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// TestBuildProcessor_EcommerceResolvesImageAPI is a regression test for the
// ecommerce gap-closure blocker: ecommerce has no platform-specific app-config
// section (unlike article/seednote), so resolveAppImageAPI returns nil.
// buildProcessor must fall back to the already-resolved effectiveCfg or every
// ecommerce generate_image MCP call errors with "no image API config available".
func TestBuildProcessor_EcommerceResolvesImageAPI(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{
		imageCfg: &srvconfig.ImageAPIConfig{
			Cover: &appconfig.ImageAPI{
				Provider: "openai",
				Key:      "test-key",
				Model:    "gpt-image-2",
			},
		},
		logger: &logger,
	}
	ch := &model.Project{Platform: model.ScopeEcommerce, UserID: "u1"}

	proc, err := svc.buildProcessor(context.Background(), ch, "cover", "")
	if err != nil {
		t.Fatalf("buildProcessor(ecommerce) error = %v, want nil (Cover fallback should resolve a config)", err)
	}
	if proc == nil {
		t.Fatal("buildProcessor(ecommerce) returned nil processor, want non-nil")
	}
}

// TestBuildProcessor_EcommerceFallsBackToContent verifies the Cover→Content
// precedence matches resolveEcommerceImageProvider (billing.go), so the provider
// get_project_profile reports is the one generate_image actually uses.
func TestBuildProcessor_EcommerceFallsBackToContent(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{
		imageCfg: &srvconfig.ImageAPIConfig{
			Content: &appconfig.ImageAPI{
				Provider: "volcengine",
				Key:      "test-key",
				Model:    "seedream-3.0",
			},
		},
		logger: &logger,
	}
	ch := &model.Project{Platform: model.ScopeEcommerce, UserID: "u1"}

	proc, err := svc.buildProcessor(context.Background(), ch, "cover", "")
	if err != nil {
		t.Fatalf("buildProcessor(ecommerce, content-only) error = %v, want nil", err)
	}
	if proc == nil {
		t.Fatal("buildProcessor(ecommerce, content-only) returned nil processor, want non-nil")
	}
}

// TestBuildProcessor_EcommerceErrorsWhenNoImageAPI ensures degraded config
// (no image API configured at all) still surfaces a clear error rather than
// silently producing a processor with no provider.
func TestBuildProcessor_EcommerceErrorsWhenNoImageAPI(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{imageCfg: nil, logger: &logger}
	ch := &model.Project{Platform: model.ScopeEcommerce, UserID: "u1"}

	proc, err := svc.buildProcessor(context.Background(), ch, "cover", "")
	if err == nil {
		t.Fatal("buildProcessor(ecommerce, no image API) error = nil, want error")
	}
	if proc != nil {
		t.Fatal("buildProcessor(ecommerce, no image API) returned non-nil processor, want nil")
	}
}

// TestBuildProcessor_VideoResolvesGenericImageAPI protects the videocreator
// visual-anchor bootstrap path: video creator projects have no platform-specific image
// app-config section, but they still need generate_image for temporary subject
// and product anchor references before video generation.
func TestBuildProcessor_VideoResolvesGenericImageAPI(t *testing.T) {
	logger := zerolog.Nop()
	cases := []struct {
		name     string
		imageCfg *srvconfig.ImageAPIConfig
	}{
		{
			name: "content config",
			imageCfg: &srvconfig.ImageAPIConfig{
				Content: &appconfig.ImageAPI{
					Provider: "volcengine",
					Key:      "test-key",
					Model:    "seedream-3.0",
				},
			},
		},
		{
			name: "cover fallback",
			imageCfg: &srvconfig.ImageAPIConfig{
				Cover: &appconfig.ImageAPI{
					Provider: "openai",
					Key:      "test-key",
					Model:    "gpt-image-2",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &ImageService{imageCfg: tc.imageCfg, logger: &logger}
			ch := &model.Project{Platform: model.PlatformVideoCreator, UserID: "u1"}

			proc, err := svc.buildProcessor(context.Background(), ch, "content", "")
			if err != nil {
				t.Fatalf("buildProcessor(video) error = %v, want nil", err)
			}
			if proc == nil {
				t.Fatal("buildProcessor(video) returned nil processor, want non-nil")
			}
		})
	}
}

func TestBuildProcessorForResolvedUsesImageTypeDescriptor(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{logger: &logger}
	ch := &model.Project{
		Platform: model.ScopeSeednote,
		UserID:   "resolved-user",
		Name:     "Resolved project",
	}
	resolved := &ResolvedImageModel{
		Config: &srvconfig.ImageAPIConfig{
			Cover: &appconfig.ImageAPI{
				Provider: "openai",
				Key:      "openai-key",
				BaseURL:  "https://openai.example/v1",
				Model:    "gpt-image-2",
			},
			Content: &appconfig.ImageAPI{
				Provider: "volcengine",
				Key:      "volc-key",
				BaseURL:  "https://ark.example/v3",
				Model:    "doubao-seedream",
			},
		},
		Provider: "volcengine",
		Model:    "doubao-seedream",
	}

	processor, err := svc.buildProcessorForResolved(ch, "content", resolved)
	if err != nil {
		t.Fatalf("buildProcessorForResolved(content) error = %v", err)
	}
	if processor == nil {
		t.Fatal("buildProcessorForResolved(content) returned nil processor")
	}

	_, err = svc.buildProcessorForResolved(ch, "cover", resolved)
	if err == nil || !strings.Contains(err.Error(), "resolved image model does not match image type configuration") {
		t.Fatalf("buildProcessorForResolved(cover) error = %v, want descriptor mismatch", err)
	}
}

func TestGenerateImageUsesResolvedDescriptor(t *testing.T) {
	db := setupTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	project := &model.Project{
		ID:       "resolved-generation-project",
		UserID:   "resolved-generation-user",
		Platform: model.ScopeSeednote,
		Name:     "Resolved generation project",
	}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.Nop()
	svc := &ImageService{repo: repo, logger: &logger}
	resolved := &ResolvedImageModel{
		Config: &srvconfig.ImageAPIConfig{
			Cover: &appconfig.ImageAPI{
				Provider: "openai",
				Key:      "openai-key",
				BaseURL:  "https://openai.example/v1",
				Model:    "gpt-image-2",
			},
			Content: &appconfig.ImageAPI{
				Provider: "volcengine",
				Key:      "volc-key",
				BaseURL:  "https://ark.example/v3",
				Model:    "doubao-seedream",
			},
		},
		Provider:           "openai",
		Model:              "gpt-image-2",
		SelectionReason:    "preferred",
		SupportsReference:  true,
		MaxReferenceImages: 16,
	}

	_, err := svc.GenerateImage(
		context.Background(), project.UserID, project.ID, "test prompt", "content", "", "", nil, "", "", resolved, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "resolved image model does not match image type configuration") {
		t.Fatalf("GenerateImage() error = %v, want descriptor mismatch before provider request", err)
	}
}

// timeoutNetErr is a minimal net.Error whose Timeout()==true, exercising the
// net-timeout branch of isTransientImageError without spinning up a socket.
type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return false }

func Test_isTransientImageError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"server_error", &appimage.GenerateError{Code: "server_error", Message: "503 no available channel"}, true},
		{"rate_limit", &appimage.GenerateError{Code: "rate_limit"}, true},
		{"network_error", &appimage.GenerateError{Code: "network_error"}, true},
		{"unknown_transient_code", &appimage.GenerateError{Code: "timeout"}, false},
		{"safety_blocked", &appimage.GenerateError{Code: "safety_blocked"}, false},
		{"bad_request", &appimage.GenerateError{Code: "bad_request"}, false},
		{"unauthorized", &appimage.GenerateError{Code: "unauthorized"}, false},
		{"payment_required", &appimage.GenerateError{Code: "payment_required"}, false},
		{"context_deadline", context.DeadlineExceeded, true},
		{"context_canceled", context.Canceled, true},
		{"net_timeout", timeoutNetErr{}, true},
		{"plain_error", fmt.Errorf("something else"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientImageError(tc.err); got != tc.want {
				t.Fatalf("isTransientImageError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestGenerateWithRetry_TransientRetrySucceeds: the SAME provider closure is
// retried on a transient error and succeeds on the second attempt, returning the
// result untouched. Backoffs are zeroed so the test is instant.
func TestGenerateWithRetry_TransientRetrySucceeds(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{
		logger: &logger,
		imageRetry: &imageRetryConfig{
			MaxAttempts: 3,
			Backoffs:    []time.Duration{0, 0, 0},
		},
	}
	calls := 0
	gen := func() (*appimage.GenerateRawResult, error) {
		calls++
		if calls == 1 {
			return nil, &appimage.GenerateError{Code: "server_error", Message: "503"}
		}
		return &appimage.GenerateRawResult{URL: "x", Provider: "openai", Model: "gpt-image-2"}, nil
	}

	got, err := svc.generateWithRetry(context.Background(), gen, "cover")
	if err != nil {
		t.Fatalf("generateWithRetry error = %v, want nil", err)
	}
	if calls != 2 {
		t.Fatalf("closure called %d times, want 2 (1 transient fail + 1 success)", calls)
	}
	if got.Provider != "openai" || got.Model != "gpt-image-2" {
		t.Fatalf("result = %+v, want provider=openai model=gpt-image-2", got)
	}
}

// TestGenerateWithRetry_ExhaustsAttemptsOnPersistentTransient: a provider that
// keeps returning a transient error is retried up to MaxAttempts, then surfaces a
// "failed after N attempts" wrapper around the last error.
func TestGenerateWithRetry_ExhaustsAttemptsOnPersistentTransient(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{
		logger: &logger,
		imageRetry: &imageRetryConfig{
			MaxAttempts: 3,
			Backoffs:    []time.Duration{0, 0, 0},
		},
	}
	calls := 0
	last := &appimage.GenerateError{Code: "server_error", Message: "503"}
	gen := func() (*appimage.GenerateRawResult, error) {
		calls++
		return nil, last
	}

	_, err := svc.generateWithRetry(context.Background(), gen, "cover")
	if err == nil {
		t.Fatal("generateWithRetry error = nil, want error")
	}
	if calls != 3 {
		t.Fatalf("closure called %d times, want 3 (MaxAttempts)", calls)
	}
	if !strings.Contains(err.Error(), "failed after 3 attempts") {
		t.Fatalf("err = %q, want it to mention 'failed after 3 attempts'", err.Error())
	}
	if !errors.Is(err, last) {
		t.Fatalf("err chain does not wrap the last GenerateError: %v", err)
	}
}

// TestGenerateWithRetry_NonRetryableFailFast: a content-policy / auth error is
// NOT retried — the closure runs exactly once and the original error is returned
// (wrapped only with "generate image:").
func TestGenerateWithRetry_NonRetryableFailFast(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{
		logger: &logger,
		imageRetry: &imageRetryConfig{
			MaxAttempts: 3,
			Backoffs:    []time.Duration{0, 0, 0},
		},
	}
	calls := 0
	refused := &appimage.GenerateError{Code: "safety_blocked", Message: "violation"}
	gen := func() (*appimage.GenerateRawResult, error) {
		calls++
		return nil, refused
	}

	_, err := svc.generateWithRetry(context.Background(), gen, "cover")
	if err == nil {
		t.Fatal("generateWithRetry error = nil, want error")
	}
	if calls != 1 {
		t.Fatalf("closure called %d times, want 1 (non-retryable must fail fast)", calls)
	}
	if !strings.Contains(err.Error(), "generate image:") {
		t.Fatalf("err = %q, want 'generate image:' wrapper", err.Error())
	}
	if !errors.Is(err, refused) {
		t.Fatalf("err chain does not wrap the refused GenerateError: %v", err)
	}
}

// TestGenerateWithRetry_ContextCancelAbortsBackoff: cancelling the context
// during a backoff wait returns ctx.Err() promptly and does NOT make another
// generation attempt. Guards against a cancelled task pointlessly retrying.
func TestGenerateWithRetry_ContextCancelAbortsBackoff(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{
		logger: &logger,
		imageRetry: &imageRetryConfig{
			MaxAttempts: 3,
			Backoffs:    []time.Duration{0, 200 * time.Millisecond, 200 * time.Millisecond},
		},
	}
	calls := 0
	gen := func() (*appimage.GenerateRawResult, error) {
		calls++
		return nil, &appimage.GenerateError{Code: "server_error", Message: "503"}
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(5*time.Millisecond, cancel)

	start := time.Now()
	_, err := svc.generateWithRetry(ctx, gen, "cover")
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("closure called %d times, want 1 (cancel must abort before attempt 2)", calls)
	}
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("returned after %v, want it to abort the backoff well under 200ms", elapsed)
	}
}

// TestGenerateWithRetry_PreCancelledContextNoAttempt: a context already
// cancelled before the first attempt must return ctx.Err() immediately without
// calling gen() once — no generation burned on a dead task (the ctx.Err() guard).
func TestGenerateWithRetry_PreCancelledContextNoAttempt(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{
		logger:     &logger,
		imageRetry: &imageRetryConfig{MaxAttempts: 3, Backoffs: []time.Duration{0, 0, 0}},
	}
	calls := 0
	gen := func() (*appimage.GenerateRawResult, error) {
		calls++
		return &appimage.GenerateRawResult{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.generateWithRetry(ctx, gen, "cover")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("gen called %d times, want 0 (pre-cancelled ctx must not attempt)", calls)
	}
}

// TestGenerateWithRetry_DefaultsWhenRetryConfigNil: production leaves imageRetry
// nil; the loop must still apply the package defaults (retry, not no-op). We
// temporarily shorten the package-default backoffs so the test is instant while
// still exercising the real nil-config → defaults code path (not a fast stub).
func TestGenerateWithRetry_DefaultsWhenRetryConfigNil(t *testing.T) {
	savedAttempts, savedBackoffs := defaultImageRetryMaxAttempts, defaultImageRetryBackoffs
	defaultImageRetryMaxAttempts = 3
	defaultImageRetryBackoffs = []time.Duration{0, 0, 0}
	defer func() {
		defaultImageRetryMaxAttempts = savedAttempts
		defaultImageRetryBackoffs = savedBackoffs
	}()

	logger := zerolog.Nop()
	svc := &ImageService{logger: &logger} // imageRetry nil → defaults
	calls := 0
	gen := func() (*appimage.GenerateRawResult, error) {
		calls++
		if calls == 1 {
			return nil, &appimage.GenerateError{Code: "rate_limit"}
		}
		return &appimage.GenerateRawResult{Provider: "volcengine"}, nil
	}
	got, err := svc.generateWithRetry(context.Background(), gen, "content")
	if err != nil {
		t.Fatalf("generateWithRetry error = %v, want nil", err)
	}
	if calls != 2 {
		t.Fatalf("closure called %d times, want 2 (default policy must retry)", calls)
	}
	if got.Provider != "volcengine" {
		t.Fatalf("result.Provider = %q, want volcengine", got.Provider)
	}
}
