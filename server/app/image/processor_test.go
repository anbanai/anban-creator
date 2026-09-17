package image

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/app/config"
	"github.com/rs/zerolog"
)

func TestProcessorResolveRawURLRejectsOversizedTempFile(t *testing.T) {
	const maxGeneratedBytes = 25 << 20
	file, err := os.CreateTemp(t.TempDir(), "provider-*.png")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Truncate(maxGeneratedBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = (&Processor{}).resolveRawURL(path)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("resolveRawURL error = %v, want size-limit rejection", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("provider temp file still exists after rejection: %v", statErr)
	}
}

func newTestProcessor(apiCfg *config.ImageAPI) *Processor {
	nopLog := zerolog.Nop()
	return &Processor{
		apiCfg: apiCfg,
		log:    &nopLog,
	}
}

func TestProcessor_GenerateOnly_NoAPIKey(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{})
	_, err := p.GenerateOnly(context.Background(), "春天的茶园", "output.png")
	if err == nil {
		t.Fatal("expected error when API key is missing, got nil")
	}
}

func TestProcessor_GenerateOnly_NoProvider(t *testing.T) {
	// API key is set but provider is nil (creation failed silently)
	p := newTestProcessor(&config.ImageAPI{Key: "test-key"})
	// provider is nil by default in newTestProcessor
	_, err := p.GenerateOnly(context.Background(), "春天的茶园", "output.png")
	if err == nil {
		t.Fatal("expected error when provider is nil, got nil")
	}
}

func TestProcessor_GenerateOnlyWithSize_NoAPIKey(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{})
	_, err := p.GenerateOnlyWithSize(context.Background(), "春天的茶园", "16:9", "output.png")
	if err == nil {
		t.Fatal("expected error when API key is missing, got nil")
	}
}

func TestProcessor_SetRefImage(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{})
	p.SetRefImage("/path/to/ref.png")
	if p.refImagePath != "/path/to/ref.png" {
		t.Errorf("SetRefImage() refImagePath = %q, want %q", p.refImagePath, "/path/to/ref.png")
	}
	// Zero-value should be empty
	p2 := newTestProcessor(&config.ImageAPI{})
	if p2.refImagePath != "" {
		t.Errorf("default refImagePath should be empty, got %q", p2.refImagePath)
	}
}

func TestProcessor_SetRefImages(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{})
	p.SetRefImages([]string{"/a.png", "/b.png", "/c.png"})
	if got := p.refImagePaths; len(got) != 3 || got[0] != "/a.png" || got[2] != "/c.png" {
		t.Errorf("SetRefImages() refImagePaths = %v, want 3-item [/a /b /c]", got)
	}
	// Multi-ref field is independent of the single-ref field.
	if p.refImagePath != "" {
		t.Errorf("SetRefImages should not touch refImagePath, got %q", p.refImagePath)
	}
	// Single + multi coexist (provider merges both).
	p.SetRefImage("/anchor.png")
	p.SetRefImages([]string{"/extra.png"})
	if p.refImagePath != "/anchor.png" || len(p.refImagePaths) != 1 {
		t.Errorf("single+multi coexistence: refImagePath=%q refImagePaths=%v", p.refImagePath, p.refImagePaths)
	}
	// Zero-value should be empty.
	p2 := newTestProcessor(&config.ImageAPI{})
	if len(p2.refImagePaths) != 0 {
		t.Errorf("default refImagePaths should be empty, got %v", p2.refImagePaths)
	}
}

func TestProcessor_buildPrompt(t *testing.T) {
	tests := []struct {
		name          string
		configStyle   string
		overrideStyle string
		userPrompt    string
		size          string
		want          string
	}{
		{
			name:       "无风格时返回原始 prompt",
			userPrompt: "春天的茶园",
			want:       "春天的茶园",
		},
		{
			name:        "config 风格与 prompt 拼接",
			configStyle: "扁平插画，莫兰迪色系",
			userPrompt:  "封面标题",
			want:        "扁平插画，莫兰迪色系\n\n封面标题",
		},
		{
			name:          "CLI 覆盖 config 风格（CLI 优先）",
			configStyle:   "config 风格",
			overrideStyle: "CLI 风格",
			userPrompt:    "内容图",
			want:          "CLI 风格\n\n内容图",
		},
		{
			name:          "仅 CLI 风格无 config 风格",
			overrideStyle: "CLI 风格",
			userPrompt:    "内容图",
			want:          "CLI 风格\n\n内容图",
		},
		{
			name:        "空 prompt 只返回风格",
			configStyle: "扁平插画，莫兰迪色系",
			userPrompt:  "",
			want:        "扁平插画，莫兰迪色系",
		},
		{
			name:       "空风格空 prompt 返回空字符串",
			userPrompt: "",
			want:       "",
		},
		{
			name:        "空格裁剪：风格有前后空格",
			configStyle: "  扁平插画  ",
			userPrompt:  "  封面  ",
			want:        "扁平插画\n\n封面",
		},
		{
			name:          "空格裁剪：override 有前后空格",
			overrideStyle: "  CLI 风格  ",
			userPrompt:    "  内容  ",
			want:          "CLI 风格\n\n内容",
		},
		{
			name:          "override 为纯空格时回退到 config 风格",
			configStyle:   "config 风格",
			overrideStyle: "   ",
			userPrompt:    "内容图",
			want:          "config 风格\n\n内容图",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiCfg := &config.ImageAPI{
				Size: tt.size,
			}
			p := newTestProcessor(apiCfg)
			if strings.TrimSpace(tt.overrideStyle) != "" {
				p.stylePrompt = strings.TrimSpace(tt.overrideStyle)
			} else if tt.configStyle != "" {
				p.stylePrompt = tt.configStyle
			}
			got := p.buildPrompt(tt.userPrompt)
			if got != tt.want {
				t.Errorf("buildPrompt(%q) =\n  %q\nwant\n  %q", tt.userPrompt, got, tt.want)
			}
		})
	}
}

type rawMetadataProvider struct {
	result *GenerateResult
}

type blockingContextProvider struct{}

func (blockingContextProvider) Name() string { return "blocking" }
func (blockingContextProvider) Capabilities() *ProviderCapabilities {
	return &ProviderCapabilities{}
}
func (blockingContextProvider) Generate(ctx context.Context, _ string, _ *GenerateOptions) (*GenerateResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestProcessorGenerateRawPropagatesCancellation(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{Provider: "test", Key: "test-key"})
	p.provider = blockingContextProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.GenerateRaw(ctx, "prompt")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestProcessorGenerateOnlyPropagatesCancellation(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{Provider: "test", Key: "test-key"})
	p.provider = blockingContextProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.GenerateOnly(ctx, "prompt", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func (p rawMetadataProvider) Name() string {
	return "metadata-provider"
}

func (p rawMetadataProvider) Generate(_ context.Context, _ string, _ *GenerateOptions) (*GenerateResult, error) {
	return p.result, nil
}

func (p rawMetadataProvider) Capabilities() *ProviderCapabilities {
	return &ProviderCapabilities{}
}

func TestProcessor_GenerateRawIncludesProviderMetadata(t *testing.T) {
	p := newTestProcessor(&config.ImageAPI{Provider: "test", Key: "test-key"})
	p.provider = rawMetadataProvider{
		result: &GenerateResult{
			URL:             "data:image/png;base64,iVBORw0KGgo=",
			RevisedPrompt:   "revised spring tea prompt",
			Model:           "image-model",
			Size:            "3:4",
			ResponseType:    "b64_json",
			ResponsePreview: "preview",
		},
	}

	got, err := p.GenerateRaw(context.Background(), "春日饮茶")
	if err != nil {
		t.Fatalf("GenerateRaw() error = %v", err)
	}

	if got.Prompt != "春日饮茶" {
		t.Fatalf("Prompt = %q, want original prompt", got.Prompt)
	}
	if got.Provider != "metadata-provider" {
		t.Fatalf("Provider = %q, want metadata-provider", got.Provider)
	}
	if got.Model != "image-model" {
		t.Fatalf("Model = %q, want image-model", got.Model)
	}
	if got.RevisedPrompt != "revised spring tea prompt" {
		t.Fatalf("RevisedPrompt = %q, want revised spring tea prompt", got.RevisedPrompt)
	}
	if got.ResponseType != "b64_json" {
		t.Fatalf("ResponseType = %q, want b64_json", got.ResponseType)
	}
	if got.OutputMIME != "image/png" {
		t.Fatalf("OutputMIME = %q, want image/png", got.OutputMIME)
	}
}
