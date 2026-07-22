package service

import (
	"html"
	"regexp"
	"strings"
)

var (
	h1Re      = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	titleRe   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	headingRe = regexp.MustCompile(`^#\s+(.+)$`)
	stripRe   = regexp.MustCompile(`<[^>]+>`)
)

func cleanTitle(raw string) string {
	s := stripRe.ReplaceAllString(raw, "")
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > 200 {
		s = string([]rune(s)[:200])
	}
	return s
}
