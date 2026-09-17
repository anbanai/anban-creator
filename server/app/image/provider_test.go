package image

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anbanai/anban-creator/server/app/config"
	"github.com/rs/zerolog"
)

func TestReadRefImage_FileNotFound(t *testing.T) {
	_, _, err := ReadRefImage("/nonexistent/path/image.png")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestReadRefImage_ValidPNG(t *testing.T) {
	// Create a temp PNG file (minimal 1x1 PNG bytes)
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
	}
	tmp, err := os.CreateTemp(t.TempDir(), "test_*.png")
	if err != nil {
		t.Fatal(err)
	}
	defer tmp.Close()
	if _, err := tmp.Write(pngData); err != nil {
		t.Fatal(err)
	}

	data, mimeType, err := ReadRefImage(tmp.Name())
	if err != nil {
		t.Fatalf("ReadRefImage() unexpected error: %v", err)
	}
	if len(data) != len(pngData) {
		t.Errorf("ReadRefImage() data length = %d, want %d", len(data), len(pngData))
	}
	if mimeType != "image/png" {
		t.Errorf("ReadRefImage() mimeType = %q, want %q", mimeType, "image/png")
	}
}

func TestReadRefImage_MIMETypes(t *testing.T) {
	tests := []struct {
		ext      string
		wantMIME string
	}{
		{".jpg", "image/jpeg"},
		{".jpeg", "image/jpeg"},
		{".png", "image/png"},
		{".gif", "image/gif"},
		{".webp", "image/webp"},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			tmpPath := filepath.Join(t.TempDir(), "test"+tt.ext)
			if err := os.WriteFile(tmpPath, []byte("fake image data"), 0644); err != nil {
				t.Fatal(err)
			}
			_, mimeType, err := ReadRefImage(tmpPath)
			if err != nil {
				t.Fatalf("ReadRefImage() unexpected error: %v", err)
			}
			if mimeType != tt.wantMIME {
				t.Errorf("ReadRefImage(%q) mimeType = %q, want %q", tt.ext, mimeType, tt.wantMIME)
			}
		})
	}
}

func TestNewProviderAcceptsBillingCatalogProviderIdentities(t *testing.T) {
	log := zerolog.Nop()
	for _, tt := range []struct {
		name     string
		provider string
		model    string
		assert   func(*testing.T, Provider)
	}{
		{
			name:     "wangcai OpenAI",
			provider: "wangcai_openai",
			model:    "gpt-image-2-t",
			assert: func(t *testing.T, provider Provider) {
				t.Helper()
				openAI, ok := provider.(*OpenAIProvider)
				if !ok || openAI.model != "gpt-image-2-t" {
					t.Fatalf("provider = %#v, want OpenAI implementation with configured model", provider)
				}
			},
		},
		{
			name:     "Volcengine Ark",
			provider: "volcengine_ark",
			model:    "doubao-seedream-5-0-pro-260628",
			assert: func(t *testing.T, provider Provider) {
				t.Helper()
				volcengine, ok := provider.(*VolcengineProvider)
				if !ok || volcengine.model != "doubao-seedream-5-0-pro-260628" {
					t.Fatalf("provider = %#v, want Volcengine implementation with configured model", provider)
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewProvider(&config.ImageAPI{
				Provider: tt.provider,
				Model:    tt.model,
				Key:      "test-key",
				BaseURL:  "https://images.example.com/v1",
			}, &log)
			if err != nil {
				t.Fatalf("NewProvider() error = %v", err)
			}
			tt.assert(t, provider)
		})
	}
}
