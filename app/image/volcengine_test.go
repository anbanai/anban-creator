package image

import (
	"testing"
)

func TestParseVolcengineSize(t *testing.T) {
	tests := []struct {
		name            string
		size            string
		wantAspectRatio string
		wantSizeTier    string
	}{
		{"empty defaults to 1:1 2K", "", "1:1", "2K"},
		{"1:1 defaults to 2K", "1:1", "1:1", "2K"},
		{"16:9 defaults to 2K", "16:9", "16:9", "2K"},
		{"9:16 defaults to 2K", "9:16", "9:16", "2K"},
		{"3:4 defaults to 2K", "3:4", "3:4", "2K"},
		{"4:3 defaults to 2K", "4:3", "4:3", "2K"},
		{"2:3 defaults to 2K", "2:3", "2:3", "2K"},
		{"3:2 defaults to 2K", "3:2", "3:2", "2K"},
		{"21:9 defaults to 2K", "21:9", "21:9", "2K"},
		{"3:4:1K explicit tier", "3:4:1K", "3:4", "1K"},
		{"16:9:4K explicit tier", "16:9:4K", "16:9", "4K"},
		{"9:16:2K explicit tier", "9:16:2K", "9:16", "2K"},
		{"lowercase tier", "3:4:2k", "3:4", "2K"},
		{"unknown ratio defaults to 1:1 2K", "5:7", "1:1", "2K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRatio, gotTier := parseVolcengineSize(tt.size)
			if gotRatio != tt.wantAspectRatio {
				t.Errorf("parseVolcengineSize(%q) aspectRatio = %q, want %q", tt.size, gotRatio, tt.wantAspectRatio)
			}
			if gotTier != tt.wantSizeTier {
				t.Errorf("parseVolcengineSize(%q) sizeTier = %q, want %q", tt.size, gotTier, tt.wantSizeTier)
			}
		})
	}
}
