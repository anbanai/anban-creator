package converter

import (
	"strings"
	"testing"
)

func TestExtractImages_LocalImages(t *testing.T) {
	markdown := "一些文字\n![photo](./images/photo1.jpg)\n更多文字\n![alt](./pics/photo2.png)"
	conv := NewConverter(nil).(*converter)
	images := conv.ExtractImages(markdown)

	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	if images[0].Type != ImageTypeLocal {
		t.Errorf("images[0].Type = %q, want %q", images[0].Type, ImageTypeLocal)
	}
	if images[0].Original != "./images/photo1.jpg" {
		t.Errorf("images[0].Original = %q", images[0].Original)
	}
	if images[1].Original != "./pics/photo2.png" {
		t.Errorf("images[1].Original = %q", images[1].Original)
	}
}

func TestExtractImages_OnlineImages(t *testing.T) {
	markdown := "![alt](https://example.com/img.jpg)\ntext\n![pic](http://cdn.example.com/pic.png)"
	conv := NewConverter(nil).(*converter)
	images := conv.ExtractImages(markdown)

	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	if images[0].Type != ImageTypeOnline {
		t.Errorf("images[0].Type = %q, want %q", images[0].Type, ImageTypeOnline)
	}
	if images[0].Original != "https://example.com/img.jpg" {
		t.Errorf("images[0].Original = %q", images[0].Original)
	}
}

func TestExtractImages_AIImages(t *testing.T) {
	markdown := "![cover](__generate:a beautiful sunset__)\ntext"
	conv := NewConverter(nil).(*converter)
	images := conv.ExtractImages(markdown)

	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].Type != ImageTypeAI {
		t.Errorf("images[0].Type = %q, want %q", images[0].Type, ImageTypeAI)
	}
	if images[0].AIPrompt != "a beautiful sunset" {
		t.Errorf("images[0].AIPrompt = %q", images[0].AIPrompt)
	}
}

func TestExtractImages_MixedTypes(t *testing.T) {
	markdown := "![local](./a.jpg)\n![online](https://x.com/b.png)\n![ai](__generate:prompt__)"
	conv := NewConverter(nil).(*converter)
	images := conv.ExtractImages(markdown)

	if len(images) != 3 {
		t.Fatalf("expected 3 images, got %d", len(images))
	}
	// Indices should be globally sequential
	for i, img := range images {
		if img.Index != i {
			t.Errorf("images[%d].Index = %d, want %d", i, img.Index, i)
		}
	}
	if images[0].Type != ImageTypeLocal {
		t.Errorf("images[0].Type = %q", images[0].Type)
	}
	if images[1].Type != ImageTypeOnline {
		t.Errorf("images[1].Type = %q", images[1].Type)
	}
	if images[2].Type != ImageTypeAI {
		t.Errorf("images[2].Type = %q", images[2].Type)
	}
}

func TestExtractImages_NoImages(t *testing.T) {
	conv := NewConverter(nil).(*converter)
	images := conv.ExtractImages("just plain text\nno images here")
	if len(images) != 0 {
		t.Errorf("expected 0 images, got %d", len(images))
	}
}

func TestReplaceImagePlaceholders(t *testing.T) {
	html := "<p>Hello</p><!-- IMG:0 --><p>World</p><!-- IMG:1 -->"
	images := []ImageRef{
		{Index: 0, Placeholder: "<!-- IMG:0 -->", WechatURL: "https://cdn.example.com/a.jpg"},
		{Index: 1, Placeholder: "<!-- IMG:1 -->", WechatURL: "https://cdn.example.com/b.jpg"},
	}

	result := ReplaceImagePlaceholders(html, images)
	if result == html {
		t.Error("expected HTML to be modified")
	}
	if !strings.Contains(result, "https://cdn.example.com/a.jpg") {
		t.Error("expected URL a.jpg in result")
	}
	if !strings.Contains(result, "https://cdn.example.com/b.jpg") {
		t.Error("expected URL b.jpg in result")
	}
	if strings.Contains(result, "<!-- IMG:0 -->") {
		t.Error("placeholder 0 should be replaced")
	}
}

func TestReplaceImagePlaceholders_PartialURLs(t *testing.T) {
	html := "<!-- IMG:0 --><!-- IMG:1 -->"
	images := []ImageRef{
		{Index: 0, Placeholder: "<!-- IMG:0 -->", WechatURL: "https://cdn.example.com/a.jpg"},
		{Index: 1, Placeholder: "<!-- IMG:1 -->", WechatURL: ""}, // No URL yet
	}

	result := ReplaceImagePlaceholders(html, images)
	if strings.Contains(result, "<!-- IMG:0 -->") {
		t.Error("placeholder 0 should be replaced")
	}
	if !strings.Contains(result, "<!-- IMG:1 -->") {
		t.Error("placeholder 1 should remain (no URL)")
	}
}

func TestImageProcessor_ExtractPlaceholders(t *testing.T) {
	p := NewImageProcessor()
	html := "<p><!-- IMG:0 --></p><p><!-- IMG:5 --></p>"

	placeholders := p.ExtractPlaceholders(html)
	if len(placeholders) != 2 {
		t.Fatalf("expected 2 placeholders, got %d", len(placeholders))
	}
	if placeholders[0].Index != 0 {
		t.Errorf("placeholders[0].Index = %d, want 0", placeholders[0].Index)
	}
	if placeholders[1].Index != 5 {
		t.Errorf("placeholders[1].Index = %d, want 5", placeholders[1].Index)
	}
}

func TestImageProcessor_CountImages(t *testing.T) {
	p := NewImageProcessor()
	markdown := "![a](./x.jpg)\ntext\n![b](./y.jpg)\n![c](./z.jpg)"

	count := p.CountImages(markdown)
	if count != 3 {
		t.Errorf("CountImages() = %d, want 3", count)
	}
}

func TestImageProcessor_ParseImageSyntax(t *testing.T) {
	p := NewImageProcessor()
	markdown := "![local](./a.jpg)\n![online](https://x.com/b.png)\n![ai](__generate:prompt__)"

	refs := p.ParseImageSyntax(markdown)
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}

	if refs[0].Type != ImageTypeLocal {
		t.Errorf("refs[0].Type = %q", refs[0].Type)
	}
	if refs[1].Type != ImageTypeOnline {
		t.Errorf("refs[1].Type = %q", refs[1].Type)
	}
	if refs[2].Type != ImageTypeAI {
		t.Errorf("refs[2].Type = %q", refs[2].Type)
	}
}
