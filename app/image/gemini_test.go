package image

import (
	"testing"
)

func TestMapSizeToGeminiAspectRatio(t *testing.T) {
	tests := []struct {
		name  string
		size  string
		want  string
	}{
		{"empty defaults to 1:1", "", "1:1"},
		{"valid ratio passthrough 16:9", "16:9", "16:9"},
		{"valid ratio passthrough 9:16", "9:16", "9:16"},
		{"valid ratio passthrough 1:1", "1:1", "1:1"},
		{"1024x1024 maps to 1:1", "1024x1024", "1:1"},
		{"2048x2048 maps to 1:1", "2048x2048", "1:1"},
		{"1376x768 maps to 16:9", "1376x768", "16:9"},
		{"768x1376 maps to 9:16", "768x1376", "9:16"},
		{"1264x848 maps to 3:2", "1264x848", "3:2"},
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
