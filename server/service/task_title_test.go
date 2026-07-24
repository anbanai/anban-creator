package service

import (
	"strings"
	"testing"
)

func TestCleanTitle(t *testing.T) {
	if got, want := cleanTitle("  <span>Title &amp; More</span>  "), "Title & More"; got != want {
		t.Fatalf("cleanTitle() = %q, want %q", got, want)
	}
}

func TestCleanTitleTruncatesLongUnicode(t *testing.T) {
	got := cleanTitle(strings.Repeat("字", 300))
	if len([]rune(got)) != 200 {
		t.Fatalf("cleanTitle() length = %d, want 200", len([]rune(got)))
	}
}
