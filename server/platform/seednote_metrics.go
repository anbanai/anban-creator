package platform

import (
	"regexp"
	"strconv"
	"strings"
)

// SeednotePostMetrics holds public engagement counters captured by a platform
// analytics provider.
type SeednotePostMetrics struct {
	LikeCount    int  `json:"like_count"`
	CollectCount int  `json:"collect_count"`
	CommentCount int  `json:"comment_count"`
	ShareCount   int  `json:"share_count"`
	ViewCount    *int `json:"view_count,omitempty"`
}

var seednoteNoteIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`/explore/([^/?#]+)`),
	regexp.MustCompile(`/discovery/item/([^/?#]+)`),
}

// ExtractSeednoteNoteID extracts a note ID from a public note URL.
func ExtractSeednoteNoteID(raw string) string {
	raw = strings.ReplaceAll(raw, `\/`, "/")
	for _, pattern := range seednoteNoteIDPatterns {
		match := pattern.FindStringSubmatch(raw)
		if len(match) >= 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

var seednoteNumberPattern = regexp.MustCompile(`\d+(?:,\d{3})*(?:\.\d+)?`)

// NormalizeSeednoteMetricCount converts public metric text into an integer.
func NormalizeSeednoteMetricCount(text string) int {
	text = strings.ReplaceAll(strings.TrimSpace(text), ",", "")
	if text == "" {
		return 0
	}
	match := seednoteNumberPattern.FindString(text)
	if match == "" {
		return 0
	}
	value, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}
	switch {
	case strings.Contains(text, "万"):
		value *= 10000
	case strings.Contains(text, "千"):
		value *= 1000
	}
	return int(value)
}
