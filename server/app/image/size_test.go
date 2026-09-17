package image

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		name      string
		size      string
		wantRatio string
		wantTier  string
	}{
		// Empty → defaults
		{"empty defaults to 1:1 2K", "", "1:1", "2K"},

		// Plain ratios → default tier 2K
		{"1:1 defaults to 2K", "1:1", "1:1", "2K"},
		{"16:9 defaults to 2K", "16:9", "16:9", "2K"},
		{"9:16 defaults to 2K", "9:16", "9:16", "2K"},
		{"3:4 defaults to 2K", "3:4", "3:4", "2K"},
		{"4:3 defaults to 2K", "4:3", "4:3", "2K"},
		{"2:3 defaults to 2K", "2:3", "2:3", "2K"},
		{"3:2 defaults to 2K", "3:2", "3:2", "2K"},
		{"4:5 defaults to 2K", "4:5", "4:5", "2K"},
		{"5:4 defaults to 2K", "5:4", "5:4", "2K"},
		{"21:9 defaults to 2K", "21:9", "21:9", "2K"},

		// Explicit tier
		{"3:4:1K explicit 1K tier", "3:4:1K", "3:4", "1K"},
		{"16:9:2K explicit 2K tier", "16:9:2K", "16:9", "2K"},
		{"16:9:4K explicit 4K tier", "16:9:4K", "16:9", "4K"},
		{"9:16:2K explicit tier", "9:16:2K", "9:16", "2K"},
		{"1:1:4K explicit tier", "1:1:4K", "1:1", "4K"},

		// Case-insensitive tier
		{"lowercase tier 2k", "3:4:2k", "3:4", "2K"},
		{"mixed case 4k", "1:1:4k", "1:1", "4K"},

		// Invalid inputs → defaults
		{"unknown ratio falls back", "5:7", "1:1", "2K"},
		{"pixel format not supported", "2560x1440", "1:1", "2K"},
		{"pixel format 1728x2304", "1728x2304", "1:1", "2K"},
		{"garbage string", "foobar", "1:1", "2K"},
		{"valid tier invalid ratio", "5:7:2K", "1:1", "2K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRatio, gotTier := ParseSize(tt.size)
			if gotRatio != tt.wantRatio {
				t.Errorf("ParseSize(%q) ratio = %q, want %q", tt.size, gotRatio, tt.wantRatio)
			}
			if gotTier != tt.wantTier {
				t.Errorf("ParseSize(%q) tier = %q, want %q", tt.size, gotTier, tt.wantTier)
			}
		})
	}
}

func TestIsPixelSize(t *testing.T) {
	tests := []struct {
		name string
		size string
		want bool
	}{
		{"valid 4K", "3840x2160", true},
		{"valid HD", "1920x1080", true},
		{"valid square", "1024x1024", true},
		{"valid small", "1x1", true},
		{"empty string", "", false},
		{"ratio format", "16:9", false},
		{"ratio with tier", "3:4:2K", false},
		{"single number", "1024", false},
		{"trailing x", "3840x", false},
		{"leading x", "x2160", false},
		{"letters", "abc", false},
		{"letters in number", "3840xabc", false},
		{"garbage", "foobar", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPixelSize(tt.size); got != tt.want {
				t.Errorf("IsPixelSize(%q) = %v, want %v", tt.size, got, tt.want)
			}
		})
	}
}

func TestParseRatioNumbers(t *testing.T) {
	tests := []struct {
		ratio string
		wantW int
		wantH int
	}{
		{"16:9", 16, 9},
		{"9:16", 9, 16},
		{"1:1", 1, 1},
		{"3:4", 3, 4},
		{"21:9", 21, 9},
		{"", 0, 0},
		{"invalid", 0, 0},
		{"16:abc", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.ratio, func(t *testing.T) {
			w, h := parseRatioNumbers(tt.ratio)
			if w != tt.wantW || h != tt.wantH {
				t.Errorf("parseRatioNumbers(%q) = (%d, %d), want (%d, %d)", tt.ratio, w, h, tt.wantW, tt.wantH)
			}
		})
	}
}
