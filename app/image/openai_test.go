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
		// DALL-E 3 exact sizes pass through
		{"dall-e-3 1024x1024 passthrough", "1024x1024", "dall-e-3", "1024x1024"},
		{"dall-e-3 1792x1024 passthrough", "1792x1024", "dall-e-3", "1792x1024"},
		{"dall-e-3 1024x1792 passthrough", "1024x1792", "dall-e-3", "1024x1792"},
		// Aspect ratio mapping
		{"16:9 maps to 1792x1024", "16:9", "dall-e-3", "1792x1024"},
		{"9:16 maps to 1024x1792", "9:16", "dall-e-3", "1024x1792"},
		{"1:1 maps to 1024x1024", "1:1", "dall-e-3", "1024x1024"},
		{"4:3 maps to 1792x1024", "4:3", "dall-e-3", "1792x1024"},
		{"3:4 maps to 1024x1792", "3:4", "dall-e-3", "1024x1792"},
		{"3:2 maps to 1792x1024", "3:2", "dall-e-3", "1792x1024"},
		{"2:3 maps to 1024x1792", "2:3", "dall-e-3", "1024x1792"},
		// Pixel format: landscape
		{"2560x1440 wide -> 1792x1024", "2560x1440", "dall-e-3", "1792x1024"},
		// Pixel format: portrait
		{"1440x2560 tall -> 1024x1792", "1440x2560", "dall-e-3", "1024x1792"},
		// DALL-E 2 valid sizes
		{"dall-e-2 256x256", "256x256", "dall-e-2", "256x256"},
		{"dall-e-2 512x512", "512x512", "dall-e-2", "512x512"},
		{"dall-e-2 1024x1024", "1024x1024", "dall-e-2", "1024x1024"},
		// DALL-E 2 invalid size defaults to 1024x1024
		{"dall-e-2 unknown -> 1024x1024", "16:9", "dall-e-2", "1024x1024"},
		{"dall-e-2 2560x1440 -> 1024x1024", "2560x1440", "dall-e-2", "1024x1024"},
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
