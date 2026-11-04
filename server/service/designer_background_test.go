package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/anbanai/anban-creator/server/app/config"
	appimage "github.com/anbanai/anban-creator/server/app/image"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type designerBackgroundProvider struct {
	fakeImageProvider
	options appimage.GenerateOptions
}

func (p *designerBackgroundProvider) Generate(ctx context.Context, prompt string, options *appimage.GenerateOptions) (*appimage.GenerateResult, error) {
	p.options = *options
	return p.fakeImageProvider.Generate(ctx, prompt, options)
}

func TestDesignerBackgroundOpacityPersistenceAndExport(t *testing.T) {
	for _, format := range []string{"png", "webp", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			f := newDesignerFixedSKUFixture(t, 500)
			route := f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"]
			route.GenerationFeatures.HasBackground = true
			route.GenerationFeatures.QualityLevels = []string{"auto", "high", "medium", "low"}
			route.GenerationFeatures.OutputFormats = []string{"png", "webp", "jpeg"}
			f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"] = route
			opacity := 50
			req := DesignerGenerateRequest{OperationID: uuid.NewString(), Prompt: "product", CapabilityKey: "standard", Quality: "high", Size: "1:1:2K", N: 1, OutputFormat: format, Background: "transparent", BackgroundOpacity: &opacity}
			quote, err := f.service.CreateGenerationQuote(t.Context(), f.userID, req)
			if err != nil {
				t.Fatal(err)
			}
			req.QuoteID, req.RequestFingerprint = quote.QuoteID, quote.RequestFingerprint
			created, err := f.service.CreateGenerationRecord(t.Context(), f.userID, req)
			if err != nil {
				t.Fatal(err)
			}
			source := image.NewNRGBA(image.Rect(0, 0, 8, 8))
			source.SetNRGBA(4, 4, color.NRGBA{R: 50, G: 100, B: 150, A: 255})
			var data bytes.Buffer
			if err := png.Encode(&data, source); err != nil {
				t.Fatal(err)
			}
			sourcePath := filepath.Join(t.TempDir(), "cutout.png")
			if err := os.WriteFile(sourcePath, data.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			provider := &designerBackgroundProvider{fakeImageProvider: fakeImageProvider{result: &appimage.GenerateResult{URL: sourcePath}}}
			f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) { return provider, nil }
			f.service.ExecuteGeneration(t.Context(), created.GenerationID)
			public, err := f.service.GetGeneration(t.Context(), f.userID, created.GenerationID)
			if err != nil {
				t.Fatal(err)
			}
			if public.Status != model.ImageGenerationStatusCompleted || public.BackgroundOpacity == nil || *public.BackgroundOpacity != opacity || public.OutputFormat != format || len(public.Results) != 1 {
				t.Fatalf("generation = %+v", public)
			}
			if provider.options.Quality != "high" || provider.options.Background != "transparent" || provider.options.OutputFormat != "png" {
				t.Fatalf("provider options = %+v", provider.options)
			}
			stored, err := f.service.storage.(storage.BoundedObjectReader).ReadObject(t.Context(), public.Results[0].FileID, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			decoded, gotFormat, err := image.Decode(bytes.NewReader(stored))
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, alpha := decoded.At(0, 0).RGBA()
			wantAlpha := uint32(128 * 257)
			if format == "jpeg" {
				wantAlpha = 65535
			}
			if gotFormat != format || alpha != wantAlpha {
				t.Fatalf("stored format/alpha = %s/%d, want %s/%d", gotFormat, alpha, format, wantAlpha)
			}
			history, _, err := f.service.GetHistory(t.Context(), f.userID, "", 1, 20)
			if err != nil || len(history) != 1 || history[0].BackgroundOpacity == nil || *history[0].BackgroundOpacity != opacity {
				t.Fatalf("history = %+v, err = %v", history, err)
			}
			account, err := f.repo.Billing().FindAccount(t.Context(), f.userID)
			if err != nil || account.PaidCredits != 0 {
				t.Fatalf("unexpected charge: %+v, %v", account, err)
			}
		})
	}
}

func TestDesignerBackgroundOpacityValidationAndFingerprint(t *testing.T) {
	caps := ImageGenerationFeatures{QualityLevels: []string{"auto"}, SizePresets: []string{"1:1:2K"}, OutputFormats: []string{"png", "jpeg", "webp"}, HasBackground: true, MaxBatch: 1}
	req := DesignerGenerateRequest{Size: "1:1:2K", Quality: "auto", N: 1, OutputFormat: "png", Background: "transparent"}
	fingerprints := map[string]bool{DesignerGenerationFingerprint("user", req): true}
	for _, opacity := range []int{-1, 0, 50, 100, 101} {
		req.BackgroundOpacity = &opacity
		err := validateDesignerGenerateRequest(req, caps)
		if (err != nil) != (opacity < 0 || opacity > 100) {
			t.Fatalf("opacity %d: %v", opacity, err)
		}
		fingerprint := DesignerGenerationFingerprint("user", req)
		if fingerprints[fingerprint] {
			t.Fatalf("opacity %d did not affect fingerprint", opacity)
		}
		fingerprints[fingerprint] = true
	}
	opacity := 50
	req.BackgroundOpacity = &opacity
	req.Background = "auto"
	if err := validateDesignerGenerateRequest(req, caps); err == nil {
		t.Fatal("opacity allowed with auto background")
	}
	req.Background = "transparent"
	caps.HasBackground = false
	if err := validateDesignerGenerateRequest(req, caps); err == nil {
		t.Fatal("opacity allowed without background support")
	}
}

func TestDesignerBackgroundOpacityFailureRefundsCharge(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	route := f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"]
	route.GenerationFeatures.HasBackground = true
	f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"] = route
	opacity := 50
	req := DesignerGenerateRequest{OperationID: uuid.NewString(), Prompt: "product", CapabilityKey: "standard", Quality: "auto", Size: "1:1:2K", N: 1, OutputFormat: "png", Background: "transparent", BackgroundOpacity: &opacity}
	quote, err := f.service.CreateGenerationQuote(t.Context(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	req.QuoteID, req.RequestFingerprint = quote.QuoteID, quote.RequestFingerprint
	created, err := f.service.CreateGenerationRecord(t.Context(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "opaque.png")
	source := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return &fakeImageProvider{result: &appimage.GenerateResult{URL: sourcePath}}, nil
	}
	f.service.ExecuteGeneration(t.Context(), created.GenerationID)
	if processed, err := f.wallet.ProcessSettlementOutbox(t.Context(), 10); err != nil || processed != 1 {
		t.Fatalf("process refund = %d, err = %v", processed, err)
	}
	public, err := f.service.GetGeneration(t.Context(), f.userID, created.GenerationID)
	if err != nil || public.Status != model.ImageGenerationStatusFailed || public.BillingStatus != "reversed" || len(public.Results) != 0 {
		t.Fatalf("failed generation = %+v, err = %v", public, err)
	}
	account, err := f.repo.Billing().FindAccount(t.Context(), f.userID)
	if err != nil || account.PaidCredits != 500 {
		t.Fatalf("refund = %+v, err = %v", account, err)
	}
}
