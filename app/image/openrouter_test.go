package image

import (
	"testing"

	openrouter "github.com/revrost/go-openrouter"
)

func TestMapSizeToOpenRouter(t *testing.T) {
	tests := []struct {
		name      string
		size      string
		wantRatio openrouter.ChatCompletionAspectRatio
		wantSize  openrouter.ChatCompletionImageSize
	}{
		{"empty defaults 1:1 2K", "", openrouter.AspectRatio1x1, openrouter.ImageSize2K},
		{"1:1 defaults to 2K", "1:1", openrouter.AspectRatio1x1, openrouter.ImageSize2K},
		{"16:9 defaults to 2K", "16:9", openrouter.AspectRatio16x9, openrouter.ImageSize2K},
		{"9:16 defaults to 2K", "9:16", openrouter.AspectRatio9x16, openrouter.ImageSize2K},
		{"3:4 defaults to 2K", "3:4", openrouter.AspectRatio3x4, openrouter.ImageSize2K},
		{"4:3 defaults to 2K", "4:3", openrouter.AspectRatio4x3, openrouter.ImageSize2K},
		{"3:4:1K explicit tier", "3:4:1K", openrouter.AspectRatio3x4, openrouter.ImageSize1K},
		{"16:9:4K explicit tier", "16:9:4K", openrouter.AspectRatio16x9, openrouter.ImageSize4K},
		{"1:1:4K explicit tier", "1:1:4K", openrouter.AspectRatio1x1, openrouter.ImageSize4K},
		{"unknown size defaults 1:1 2K", "999x888", openrouter.AspectRatio1x1, openrouter.ImageSize2K},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRatio, gotSize := mapSizeToOpenRouter(tt.size)
			if gotRatio != tt.wantRatio {
				t.Errorf("mapSizeToOpenRouter(%q) ratio = %q, want %q", tt.size, gotRatio, tt.wantRatio)
			}
			if gotSize != tt.wantSize {
				t.Errorf("mapSizeToOpenRouter(%q) size = %q, want %q", tt.size, gotSize, tt.wantSize)
			}
		})
	}
}

func TestOpenRouterProviderName(t *testing.T) {
	p := &OpenRouterProvider{}
	if p.Name() != "OpenRouter" {
		t.Errorf("Name() = %q, want %q", p.Name(), "OpenRouter")
	}
}

func TestParseDataURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantExt string
		wantErr bool
	}{
		{"no data prefix", "https://example.com/image.png", "", true},
		{"no comma", "data:image/png;base64", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ext, err := parseDataURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDataURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && ext != tt.wantExt {
				t.Errorf("parseDataURL() ext = %q, want %q", ext, tt.wantExt)
			}
		})
	}
}
