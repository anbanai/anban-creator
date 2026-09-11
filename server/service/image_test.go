package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
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

func TestSaveGeneratedImageBytesRejectsOversizedDimensionsBeforePNGDecode(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, maxTaskImageDimension+1, 1))
	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, img, nil); err != nil {
		t.Fatalf("encode oversized-dimension jpeg: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "cover.png")
	if _, err := saveGeneratedImageBytes(outputPath, jpegBuf.Bytes()); err == nil || !strings.Contains(err.Error(), "safety limits") {
		t.Fatalf("saveGeneratedImageBytes() error = %v, want dimension safety rejection", err)
	}
	if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversized image created output file: %v", err)
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

func TestPopulateImageResultDimensionsReportsSavedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.png")
	if err := os.WriteFile(path, tinyImagePNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	result := &ImageResult{FilePath: path, Size: "1:1"}
	if err := populateImageResultDimensions(result); err != nil {
		t.Fatalf("populateImageResultDimensions: %v", err)
	}
	if result.Width != 1 || result.Height != 1 {
		t.Fatalf("actual dimensions = %dx%d, want 1x1", result.Width, result.Height)
	}
	if result.Size != "1:1" {
		t.Fatalf("provider size metadata changed to %q", result.Size)
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

func TestDetectTaskFileMIMEFromContentPreservesPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		body []byte
		want string
	}{
		{name: "image content", path: "article.md", body: tinyImagePNG(t), want: "image/png"},
		{name: "known extension", path: "article.md", body: []byte("# article"), want: "text/markdown"},
		{name: "detected fallback", path: "artifact.bin", body: []byte("plain text"), want: "text/plain; charset=utf-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectTaskFileMIMEFromContent(tc.path, tc.body); got != tc.want {
				t.Fatalf("DetectTaskFileMIMEFromContent() = %q, want %q", got, tc.want)
			}
		})
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

// TestBuildProcessor_EcommerceResolvesImageAPI verifies ecommerce uses the same
// capability API as every other artifact role.
func TestBuildProcessor_EcommerceResolvesImageAPI(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{
		imageCfg: &srvconfig.ImageAPIConfig{
			API: &appconfig.ImageAPI{
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
		t.Fatalf("buildProcessor(ecommerce) error = %v, want nil", err)
	}
	if proc == nil {
		t.Fatal("buildProcessor(ecommerce) returned nil processor, want non-nil")
	}
}

func TestBuildProcessor_EcommerceContentRoleUsesSameAPI(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{
		imageCfg: &srvconfig.ImageAPIConfig{
			API: &appconfig.ImageAPI{
				Provider: "volcengine",
				Key:      "test-key",
				Model:    "seedream-3.0",
			},
		},
		logger: &logger,
	}
	ch := &model.Project{Platform: model.ScopeEcommerce, UserID: "u1"}

	proc, err := svc.buildProcessor(context.Background(), ch, "content", "")
	if err != nil {
		t.Fatalf("buildProcessor(ecommerce content role) error = %v, want nil", err)
	}
	if proc == nil {
		t.Fatal("buildProcessor(ecommerce, content-only) returned nil processor, want non-nil")
	}
}

func TestBuildProcessorWithoutTaskCapabilityUsesBaseConfigWhenResolverIsPresent(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{
		imageCfg: &srvconfig.ImageAPIConfig{API: &appconfig.ImageAPI{
			Provider: "openai", Key: "test-key", Model: "gpt-image-2",
		}},
		capabilityResolver: NewImageCapabilityResolver(nil, &srvconfig.Config{
			ModelRoutes: srvconfig.ModelRoutesConfig{ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
				DefaultCapability: "standard",
				Capabilities: map[string]srvconfig.ImageGenerationRouteConfig{
					"standard": {Enabled: true, MinTier: "free"},
				},
			}},
		}),
		logger: &logger,
	}
	ch := &model.Project{Platform: model.ScopeArticle, UserID: "u1"}

	proc, err := svc.buildProcessor(context.Background(), ch, "content", "")
	if err != nil {
		t.Fatalf("buildProcessor without task capability: %v", err)
	}
	if proc == nil {
		t.Fatal("buildProcessor without task capability returned nil")
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

func TestBuildProcessorForResolvedUsesCapabilityDescriptorForEveryImageType(t *testing.T) {
	logger := zerolog.Nop()
	svc := &ImageService{logger: &logger}
	ch := &model.Project{
		Platform: model.ScopeSeednote,
		UserID:   "resolved-user",
		Name:     "Resolved project",
	}
	resolved := &ResolvedImageModel{
		Config: &srvconfig.ImageAPIConfig{
			API: &appconfig.ImageAPI{
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

	coverProcessor, err := svc.buildProcessorForResolved(ch, "cover", resolved)
	if err != nil || coverProcessor == nil {
		t.Fatalf("buildProcessorForResolved(cover) = %#v, %v", coverProcessor, err)
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
			API: &appconfig.ImageAPI{
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
		context.Background(), project.UserID, project.ID, "test prompt", "content", "", "", nil, "", "3:4", resolved, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "resolved image model does not match image type configuration") {
		t.Fatalf("GenerateImage() error = %v, want descriptor mismatch before provider request", err)
	}
}

func TestProviderAttemptTimeoutUsesResolvedImageType(t *testing.T) {
	resolved := &ResolvedImageModel{Config: &srvconfig.ImageAPIConfig{
		API: &appconfig.ImageAPI{TimeoutSec: 180},
	}}
	if got := providerAttemptTimeout(resolved, "cover"); got != 3*time.Minute {
		t.Fatalf("cover attempt timeout = %s, want 3m", got)
	}
	if got := providerAttemptTimeout(resolved, "content"); got != 3*time.Minute {
		t.Fatalf("content attempt timeout = %s, want 3m", got)
	}
}

func TestGenerateProviderImageDoesNotOwnWorkflowRetry(t *testing.T) {
	svc := &ImageService{}
	calls := 0
	firstErr := &appimage.GenerateError{Code: "server_error", Message: "503"}
	_, err := svc.generateProviderImage(context.Background(), 5*time.Minute, func(context.Context) (*appimage.GenerateRawResult, error) {
		calls++
		if calls == 1 {
			return nil, firstErr
		}
		return &appimage.GenerateRawResult{Provider: "openai", Model: "gpt-image-2"}, nil
	}, "cover")
	if calls != 1 {
		t.Fatalf("provider calls=%d, want exactly 1 so Agent/Skill owns retry", calls)
	}
	if !errors.Is(err, firstErr) {
		t.Fatalf("error=%v, want first provider error", err)
	}
}

func TestGenerateProviderImagePreCancelledContextMakesNoAttempt(t *testing.T) {
	svc := &ImageService{}
	calls := 0
	gen := func(context.Context) (*appimage.GenerateRawResult, error) {
		calls++
		return &appimage.GenerateRawResult{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.generateProviderImage(ctx, 5*time.Minute, gen, "cover")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("gen called %d times, want 0 (pre-cancelled ctx must not attempt)", calls)
	}
}

func TestGenerateProviderImageBoundsProviderAttempt(t *testing.T) {
	svc := &ImageService{}
	started := time.Now()
	_, err := svc.generateProviderImage(context.Background(), 20*time.Millisecond, func(ctx context.Context) (*appimage.GenerateRawResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, "cover")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("provider attempt returned after %s, want bounded near 20ms", elapsed)
	}
}

func TestImageServiceRejectsProviderImageURLOnPrivateHTTPHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(taskImageTinyPNG())
	}))
	defer server.Close()

	path, err := (&ImageService{}).downloadURLToTempFile(context.Background(), server.URL+"/generated.png")
	if path != "" {
		_ = os.RemoveAll(filepath.Dir(path))
	}
	if err == nil || !strings.Contains(err.Error(), "invalid external image URL") {
		t.Fatalf("private provider URL = path %q err %v; want URL policy rejection", path, err)
	}
}
