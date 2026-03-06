package image

import (
	"strconv"
	"strings"
)

// SupportedRatios contains the set of valid aspect ratios.
var SupportedRatios = map[string]bool{
	"1:1": true, "16:9": true, "9:16": true,
	"4:3": true, "3:4": true, "3:2": true, "2:3": true,
	"4:5": true, "5:4": true, "21:9": true,
}

// SupportedTiers contains the set of valid size tiers.
var SupportedTiers = map[string]bool{
	"1K": true, "2K": true, "4K": true,
}

// ParseSize parses a size string in "ratio" or "ratio:tier" format.
//
// Examples:
//
//	"16:9"     → ratio="16:9", tier="2K"  (default tier)
//	"3:4:1K"   → ratio="3:4",  tier="1K"
//	"1:1:4K"   → ratio="1:1",  tier="4K"
//	""         → ratio="1:1",  tier="2K"  (defaults)
//	"invalid"  → ratio="1:1",  tier="2K"  (fallback)
//
// Old pixel formats like "2560x1440" are not supported and return defaults.
func ParseSize(size string) (ratio, tier string) {
	if size == "" {
		return "1:1", "2K"
	}

	upper := strings.ToUpper(size)

	// Try "ratio:tier" format: strip tier suffix and validate ratio
	for _, t := range []string{"4K", "2K", "1K"} {
		suffix := ":" + t
		if strings.HasSuffix(upper, suffix) {
			r := size[:len(size)-len(suffix)]
			if SupportedRatios[r] {
				return r, t
			}
			// Tier recognized but ratio invalid → fall through to defaults
			return "1:1", "2K"
		}
	}

	// Try as plain ratio (e.g., "16:9")
	if SupportedRatios[size] {
		return size, "2K"
	}

	return "1:1", "2K"
}

// parseRatioNumbers splits a ratio string like "16:9" into integer w and h.
// Returns (0, 0) if the format is invalid.
func parseRatioNumbers(ratio string) (w, h int) {
	parts := strings.SplitN(ratio, ":", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	wv, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	hv, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return wv, hv
}
