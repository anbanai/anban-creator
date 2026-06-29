package service

import (
	"errors"
	"strings"
	"testing"
)

func TestRejectWriterNameAsAuthor(t *testing.T) {
	tests := []struct {
		name   string
		author string
		reject bool
	}{
		{"empty allowed (resolve from chain)", "", false},
		{"whitespace-only allowed", "   ", false},
		{"real name allowed", "张三", false},
		{"real brand allowed", "某某健康科普", false},

		{"writer display name rejected", "Dan Koe", true},
		{"writer key rejected", "dan-koe", true},
		{"writer display name (CN) rejected", "轻松科普风格", true},
		{"writer key (CN-style) rejected", "casual-science", true},
		{"trim + case-insensitive rejected", "  DAN-KOE  ", true},

		{"near-miss not rejected", "Dan Koe风格", false},
		{"unrelated name not rejected", "李雷", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RejectWriterNameAsAuthor(tt.author)
			if tt.reject {
				if err == nil {
					t.Fatalf("expected rejection for %q, got nil", tt.author)
				}
				if !errors.Is(err, ErrAuthorIsWriterName) {
					t.Fatalf("expected error to wrap ErrAuthorIsWriterName, got %v", err)
				}
				if !strings.Contains(err.Error(), "写作风格") {
					t.Fatalf("error should explain the author vs 写作风格 semantics: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no rejection for %q, got %v", tt.author, err)
				}
			}
		})
	}
}
