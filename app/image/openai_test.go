package image

import (
	"testing"
)

func TestMapToDALLESize(t *testing.T) {
	tests := []struct {
		name  string
		size  string
		model string
		want  string
	}{
		// Empty defaults to 1024x1024
		{"empty defaults to 1024x1024", "", "dall-e-3", "1024x1024"},
		// Aspect ratio mapping for DALL-E 3
		{"16:9 maps to 1792x1024", "16:9", "dall-e-3", "1792x1024"},
		{"9:16 maps to 1024x1792", "9:16", "dall-e-3", "1024x1792"},
		{"1:1 maps to 1024x1024", "1:1", "dall-e-3", "1024x1024"},
		{"4:3 maps to 1792x1024", "4:3", "dall-e-3", "1792x1024"},
		{"3:4 maps to 1024x1792", "3:4", "dall-e-3", "1024x1792"},
		{"3:2 maps to 1792x1024", "3:2", "dall-e-3", "1792x1024"},
		{"2:3 maps to 1024x1792", "2:3", "dall-e-3", "1024x1792"},
		{"21:9 maps to 1792x1024", "21:9", "dall-e-3", "1792x1024"},
		// Tier suffix stripped before ratio lookup
		{"16:9:2K maps to 1792x1024", "16:9:2K", "dall-e-3", "1792x1024"},
		{"3:4:1K maps to 1024x1792", "3:4:1K", "dall-e-3", "1024x1792"},
		// DALL-E 2 always returns 1024x1024
		{"dall-e-2 any size defaults to 1024x1024", "16:9", "dall-e-2", "1024x1024"},
		{"dall-e-2 pixel format defaults to 1024x1024", "2560x1440", "dall-e-2", "1024x1024"},
		// Pixel format not supported, defaults to 1024x1024
		{"pixel format not supported", "2560x1440", "dall-e-3", "1024x1024"},
		// Unknown format defaults to 1024x1024
		{"unknown format defaults", "badformat", "dall-e-3", "1024x1024"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapToDALLESize(tt.size, tt.model)
			if got != tt.want {
				t.Errorf("mapToDALLESize(%q, %q) = %q, want %q", tt.size, tt.model, got, tt.want)
			}
		})
	}
}
