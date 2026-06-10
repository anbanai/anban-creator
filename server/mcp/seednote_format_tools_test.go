package mcp

import (
	"strings"
	"testing"
)

func TestParseImageMarkdown(t *testing.T) {
	tests := []struct {
		name, input, wantAlt, wantURL string
	}{
		{"basic", "![cover](cover.png)", "cover", "cover.png"},
		{"url with path", "![alt text](/path/to/image.png)", "alt text", "/path/to/image.png"},
		{"url with https", "![photo](https://example.com/img.jpg)", "photo", "https://example.com/img.jpg"},
		{"chinese alt", "![封面图](cover.png)", "封面图", "cover.png"},
		{"empty alt", "![](image.png)", "", "image.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alt, url := parseImageMarkdown(tt.input)
			if alt != tt.wantAlt {
				t.Errorf("alt = %q, want %q", alt, tt.wantAlt)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
		})
	}
}

func TestSeednoteMarkdownToPlain_BasicImageExtraction(t *testing.T) {
	var images []SeednoteImage
	result := seednoteMarkdownToPlain("Here is ![cover](cover.png) in text.", &images)

	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].Alt != "cover" {
		t.Errorf("alt = %q, want %q", images[0].Alt, "cover")
	}
	if images[0].URL != "cover.png" {
		t.Errorf("url = %q, want %q", images[0].URL, "cover.png")
	}
	if images[0].Position != 1 {
		t.Errorf("position = %d, want 1", images[0].Position)
	}
	if !strings.Contains(result, "[图片:1]") {
		t.Errorf("expected [图片:1] placeholder in result, got %q", result)
	}
}

func TestSeednoteMarkdownToPlain_MultipleImagesPerLine(t *testing.T) {
	var images []SeednoteImage
	result := seednoteMarkdownToPlain("![a](a.png) text ![b](b.png)", &images)

	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	if images[0].Position != 1 || images[1].Position != 2 {
		t.Errorf("positions = %d, %d; want 1, 2", images[0].Position, images[1].Position)
	}
	if !strings.Contains(result, "[图片:1]") || !strings.Contains(result, "[图片:2]") {
		t.Errorf("expected both placeholders in result, got %q", result)
	}
}

func TestSeednoteMarkdownToPlain_CrossLinePositions(t *testing.T) {
	var images []SeednoteImage
	seednoteMarkdownToPlain("![first](a.png)", &images)
	seednoteMarkdownToPlain("![second](b.png)", &images)

	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	if images[0].Position != 1 || images[1].Position != 2 {
		t.Errorf("positions = %d, %d; want 1, 2", images[0].Position, images[1].Position)
	}
}

func TestSeednoteMarkdownToPlain_NoImages(t *testing.T) {
	var images []SeednoteImage
	result := seednoteMarkdownToPlain("Just plain text here.", &images)

	if len(images) != 0 {
		t.Errorf("expected 0 images, got %d", len(images))
	}
	if result != "Just plain text here." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestParseSeednoteContent_ImageExtraction(t *testing.T) {
	input := `# Test Title

Some intro text.

![封面](cover.png)

More text here.

![内容图](image_01.png)

Ending text.

#标签1 #标签2`

	result := parseSeednoteContent(input)

	if result.Title != "Test Title" {
		t.Errorf("title = %q, want %q", result.Title, "Test Title")
	}
	if len(result.Images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(result.Images))
	}
	if result.Images[0].URL != "cover.png" {
		t.Errorf("image 0 url = %q, want %q", result.Images[0].URL, "cover.png")
	}
	if result.Images[1].URL != "image_01.png" {
		t.Errorf("image 1 url = %q, want %q", result.Images[1].URL, "image_01.png")
	}
	if !strings.Contains(result.Content, "[图片:1]") {
		t.Error("content missing [图片:1] placeholder")
	}
	if !strings.Contains(result.Content, "[图片:2]") {
		t.Error("content missing [图片:2] placeholder")
	}
}

func TestParseSeednoteContent_CodeBlockImagesSkipped(t *testing.T) {
	input := `# Title

Text before.

` + "```" + `
![should-skip](skip.png)
` + "```" + `

Text after.`

	result := parseSeednoteContent(input)

	if len(result.Images) != 0 {
		t.Errorf("expected 0 images (code block should be skipped), got %d", len(result.Images))
	}
}

func TestFilterImagesByContent_Truncation(t *testing.T) {
	content := "Some text [图片:1] more text"
	images := []SeednoteImage{
		{Alt: "a", URL: "a.png", Position: 1},
		{Alt: "b", URL: "b.png", Position: 2},
	}

	filtered := filterImagesByContent(content, images)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 image after filter, got %d", len(filtered))
	}
	if filtered[0].Position != 1 {
		t.Errorf("expected position 1, got %d", filtered[0].Position)
	}
}

func TestFilterImagesByContent_AllPresent(t *testing.T) {
	content := "[图片:1] text [图片:2]"
	images := []SeednoteImage{
		{Alt: "a", URL: "a.png", Position: 1},
		{Alt: "b", URL: "b.png", Position: 2},
	}

	filtered := filterImagesByContent(content, images)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 images, got %d", len(filtered))
	}
}

func TestFilterImagesByContent_EmptyInput(t *testing.T) {
	filtered := filterImagesByContent("no images", []SeednoteImage{})

	if len(filtered) != 0 {
		t.Errorf("expected 0 images, got %d", len(filtered))
	}
}

func TestParseSeednoteContent_NoImages(t *testing.T) {
	input := `# Title

Just text without any images.

#标签`

	result := parseSeednoteContent(input)

	if len(result.Images) != 0 {
		t.Errorf("expected 0 images, got %d", len(result.Images))
	}
}
