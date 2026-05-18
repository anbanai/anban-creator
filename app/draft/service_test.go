package draft

import "testing"

func TestIsValidMediaID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid alphanumeric", "media_abc123", true},
		{"valid with hyphens", "media-abc-123", true},
		{"valid underscores and hyphens", "media_abc-123_def", true},
		{"valid short", "ab", true},
		{"empty string", "", false},
		{"too long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"http URL", "https://mmbiz.qpic.cn/abc.jpg", false},
		{"http URL short", "http://example.com/img.jpg", false},
		{"absolute path", "/tmp/image.jpg", false},
		{"relative path", "./photo.png", false},
		{"parent path", "../image.jpg", false},
		{"spaces", "media abc", false},
		{"dots", "media.abc", false},
		{"at sign", "media@abc", false},
		{"backslash", "media\\abc", false},
		{"unicode", "媒体abc", false},
		{"just hyphens", "---", true},
		{"just underscores", "___", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidMediaID(tt.input)
			if got != tt.want {
				t.Errorf("isValidMediaID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
