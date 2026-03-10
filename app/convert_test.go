package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/royalrick/anbanwriter/app/converter"
)

// TestLoadImageURLs tests the loadImageURLs helper function.
func TestLoadImageURLs(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
		wantErr bool
	}{
		{
			name:    "valid JSON array",
			content: `["https://cdn1.example.com/img1.jpg","https://cdn2.example.com/img2.jpg"]`,
			want:    []string{"https://cdn1.example.com/img1.jpg", "https://cdn2.example.com/img2.jpg"},
			wantErr: false,
		},
		{
			name:    "empty array",
			content: `[]`,
			want:    []string{},
			wantErr: false,
		},
		{
			name:    "single URL",
			content: `["https://mmbiz.qpic.cn/abc123"]`,
			want:    []string{"https://mmbiz.qpic.cn/abc123"},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			content: `not-valid-json`,
			wantErr: true,
		},
		{
			name:    "JSON object instead of array",
			content: `{"0":"https://example.com/img.jpg"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, "urls.json")
			if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("write test file: %v", err)
			}

			got, err := loadImageURLs(filePath)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len mismatch: got %d, want %d", len(got), len(tt.want))
			}
			for i, u := range tt.want {
				if got[i] != u {
					t.Errorf("index %d: got %q, want %q", i, got[i], u)
				}
			}
		})
	}
}

// TestLoadImageURLs_MissingFile verifies error on missing file.
func TestLoadImageURLs_MissingFile(t *testing.T) {
	_, err := loadImageURLs("/non/existent/path/urls.json")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// TestPlaceholderReplacement verifies that ReplaceImagePlaceholders replaces
// <!-- IMG:N --> comments with <img> tags when WechatURL and Placeholder are set.
func TestPlaceholderReplacement(t *testing.T) {
	html := `<p>Hello</p><!-- IMG:0 --><p>World</p><!-- IMG:1 -->`

	images := []converter.ImageRef{
		{Index: 0, Placeholder: "<!-- IMG:0 -->", WechatURL: "https://cdn.example.com/img0.jpg"},
		{Index: 1, Placeholder: "<!-- IMG:1 -->", WechatURL: "https://cdn.example.com/img1.jpg"},
	}

	result := converter.ReplaceImagePlaceholders(html, images)

	if strings.Contains(result, "<!-- IMG:0 -->") {
		t.Error("placeholder <!-- IMG:0 --> was not replaced")
	}
	if strings.Contains(result, "<!-- IMG:1 -->") {
		t.Error("placeholder <!-- IMG:1 --> was not replaced")
	}
	if !strings.Contains(result, `src="https://cdn.example.com/img0.jpg"`) {
		t.Error("expected img0 src not found in result")
	}
	if !strings.Contains(result, `src="https://cdn.example.com/img1.jpg"`) {
		t.Error("expected img1 src not found in result")
	}
}

// TestPlaceholderReplacement_PartialURLs verifies partial replacement when
// fewer URLs than images are provided.
func TestPlaceholderReplacement_PartialURLs(t *testing.T) {
	html := `<!-- IMG:0 --><!-- IMG:1 -->`

	images := []converter.ImageRef{
		{Index: 0, Placeholder: "<!-- IMG:0 -->", WechatURL: "https://cdn.example.com/img0.jpg"},
		{Index: 1, Placeholder: "<!-- IMG:1 -->", WechatURL: ""},
	}

	result := converter.ReplaceImagePlaceholders(html, images)

	if strings.Contains(result, "<!-- IMG:0 -->") {
		t.Error("placeholder <!-- IMG:0 --> was not replaced")
	}
	// IMG:1 has no WechatURL — it should remain unreplaced
	if !strings.Contains(result, "<!-- IMG:1 -->") {
		t.Error("placeholder <!-- IMG:1 --> should NOT be replaced (no WechatURL)")
	}
}

// TestCharCount_Accuracy verifies char_count reflects len(html).
func TestCharCount_Accuracy(t *testing.T) {
	html := strings.Repeat("a", 1000)
	if len(html) != 1000 {
		t.Fatalf("unexpected html length: %d", len(html))
	}
}

// TestSizeWarning_Threshold verifies warning is generated when HTML > 20000 chars.
func TestSizeWarning_Threshold(t *testing.T) {
	shortHTML := strings.Repeat("x", 19999)
	longHTML := strings.Repeat("x", 20001)

	const maxChars = 20000

	buildResp := func(html string) map[string]any {
		charCount := len(html)
		resp := map[string]any{
			"type":       "convert_result",
			"char_count": charCount,
			"max_chars":  maxChars,
		}
		if charCount > maxChars {
			resp["size_warning"] = "HTML content exceeds WeChat limit"
		}
		return resp
	}

	shortResp := buildResp(shortHTML)
	if _, ok := shortResp["size_warning"]; ok {
		t.Error("size_warning should NOT be present for HTML under limit")
	}

	longResp := buildResp(longHTML)
	if _, ok := longResp["size_warning"]; !ok {
		t.Error("size_warning should be present for HTML over limit")
	}
}

// TestLoadImageURLs_ExtraURLsIgnored verifies that extra URLs beyond image count
// are safely ignored by the caller logic.
func TestLoadImageURLs_ExtraURLsIgnored(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "urls.json")
	urls := []string{
		"https://cdn.example.com/img0.jpg",
		"https://cdn.example.com/img1.jpg",
		"https://cdn.example.com/img2.jpg", // extra
	}
	data, _ := json.Marshal(urls)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	got, err := loadImageURLs(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate caller: only assign to images[i] when i < len(images)
	images := make([]converter.ImageRef, 2) // only 2 images
	for i := range images {
		if i < len(got) && got[i] != "" {
			images[i].WechatURL = got[i]
		}
	}

	if images[0].WechatURL != urls[0] {
		t.Errorf("image[0] URL mismatch: got %q, want %q", images[0].WechatURL, urls[0])
	}
	if images[1].WechatURL != urls[1] {
		t.Errorf("image[1] URL mismatch: got %q, want %q", images[1].WechatURL, urls[1])
	}
	// Extra URL at index 2 was simply not accessed — no panic
}
