package image

import (
	"testing"
)

func TestMapSizeToGeminiAspectRatio(t *testing.T) {
	tests := []struct {
		name string
		size string
		want string
	}{
		{"empty defaults to 1:1", "", "1:1"},
		{"valid ratio 16:9", "16:9", "16:9"},
		{"valid ratio 9:16", "9:16", "9:16"},
		{"valid ratio 1:1", "1:1", "1:1"},
		{"valid ratio 3:4", "3:4", "3:4"},
		{"valid ratio 4:3", "4:3", "4:3"},
		{"valid ratio 3:2", "3:2", "3:2"},
		{"valid ratio 2:3", "2:3", "2:3"},
		{"valid ratio 21:9", "21:9", "21:9"},
		// Tier suffix is stripped, ratio is returned
		{"3:4:1K strips tier", "3:4:1K", "3:4"},
		{"16:9:4K strips tier", "16:9:4K", "16:9"},
		// Pixel format not supported, defaults to 1:1
		{"pixel format not supported", "1024x1024", "1:1"},
		{"pixel format 2560x1440", "2560x1440", "1:1"},
		{"unknown size defaults to 1:1", "999x888", "1:1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapSizeToGeminiAspectRatio(tt.size)
			if got != tt.want {
				t.Errorf("mapSizeToGeminiAspectRatio(%q) = %q, want %q", tt.size, got, tt.want)
			}
		})
	}
}

func TestGeminiProviderName(t *testing.T) {
	p := &GeminiProvider{}
	if p.Name() != "Gemini" {
		t.Errorf("Name() = %q, want %q", p.Name(), "Gemini")
	}
}
