package image

import (
	"testing"
)

func TestMapSizeToOpenRouter(t *testing.T) {
	tests := []struct {
		name        string
		size        string
		wantRatio   string
		wantSize    string
	}{
		{"empty defaults", "", "1:1", "2K"},
		{"1024x1024 1K square", "1024x1024", "1:1", "1K"},
		{"2048x2048 2K square", "2048x2048", "1:1", "2K"},
		{"1344x768 16:9 1K", "1344x768", "16:9", "1K"},
		{"768x1344 9:16 1K", "768x1344", "9:16", "1K"},
		{"valid ratio passthrough", "16:9", "16:9", "2K"},
		{"unknown size defaults", "999x888", "1:1", "2K"},
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
