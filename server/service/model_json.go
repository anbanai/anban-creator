package service

import (
	"fmt"
	"regexp"
	"strings"
)

var trailingCommaRe = regexp.MustCompile(`,\s*([}])`)

// extractJSONObject extracts the first balanced JSON object from model output.
func extractJSONObject(raw string) (string, error) {
	raw = strings.TrimPrefix(raw, "\xEF\xBB\xBF")
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	start := -1
	inString := false
	escape := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' && inString {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if !inString && ch == '{' {
			start = i
			break
		}
	}
	if start == -1 {
		return "", fmt.Errorf("no JSON { found in response")
	}

	depth := 0
	inString = false
	escape = false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' && inString {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return trailingCommaRe.ReplaceAllString(raw[start:i+1], "$1"), nil
			}
		}
	}
	return "", fmt.Errorf("unmatched JSON { bracket")
}
