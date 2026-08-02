package service

import (
	"context"
	"testing"

	appimage "github.com/anbanai/anban-creator/app/image"
)

func TestDesignerValidateProviderCapabilitiesAcceptsExactFixedSizePreset(t *testing.T) {
	caps := DesignerProviderCapabilities{SizePresets: []string{"1:1:2K", "3:4:4K", "4:3:2K", "16:9:2K"}, QualityLevels: []string{"auto"}, MaxBatch: 1}
	if err := validateDesignerGenerateRequest(DesignerGenerateRequest{Prompt: "poster", Size: "3:4:4K", Quality: "auto", N: 1, OutputFormat: "png"}, caps); err != nil {
		t.Fatalf("validateDesignerGenerateRequest() error = %v", err)
	}
}

type fakeImageProvider struct {
	result *appimage.GenerateResult
	err    error
	calls  int
}

func (p *fakeImageProvider) Name() string { return "test" }

func (p *fakeImageProvider) Generate(context.Context, string, *appimage.GenerateOptions) (*appimage.GenerateResult, error) {
	p.calls++
	return p.result, p.err
}

func (p *fakeImageProvider) Capabilities() *appimage.ProviderCapabilities {
	return &appimage.ProviderCapabilities{}
}
