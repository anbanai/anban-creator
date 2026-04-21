package writer

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxContentLenForPrompt = 500

// TruncateAndSanitizeForPrompt truncates content to a safe length, strips
// common prompt injection patterns, and wraps it in boundary markers so
// the AI model treats it as data rather than instructions.
func TruncateAndSanitizeForPrompt(content string) string {
	if content == "" {
		return ""
	}

	// Strip common injection patterns: system role overrides, instruction
	// directives, and content-policy bypass attempts.
	stripped := stripInjectionPatterns(content)

	// Truncate by rune count (not bytes) to avoid splitting multi-byte chars.
	if utf8.RuneCountInString(stripped) > maxContentLenForPrompt {
		stripped = string([]rune(stripped)[:maxContentLenForPrompt]) + "……"
	}

	return fmt.Sprintf("[文章摘要开始]\n%s\n[文章摘要结束]", stripped)
}

// injectionPatterns matches common prompt-injection attempts.
var injectionPatterns = []struct {
	prefix string
}{
	{"ignore all previous"},
	{"ignore previous"},
	{"disregard all"},
	{"disregard previous"},
	{"forget all"},
	{"forget previous"},
	{"you are now"},
	{"从现在起你是"},
	{"忽略之前的"},
	{"忽略以上"},
	{"无视之前的"},
	{"新的指令"},
	{"system:"},
	{"system prompt:"},
	{"[system]"},
	{"<system>"},
}

func stripInjectionPatterns(content string) string {
	lower := strings.ToLower(content)
	for _, p := range injectionPatterns {
		if strings.Contains(lower, p.prefix) {
			// Remove the injected line entirely.
			content = removeLineContaining(content, p.prefix)
		}
	}
	return content
}

func removeLineContaining(content, substr string) string {
	lines := strings.Split(content, "\n")
	var filtered []string
	lower := strings.ToLower(substr)
	for _, line := range lines {
		if !strings.Contains(strings.ToLower(line), lower) {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}
