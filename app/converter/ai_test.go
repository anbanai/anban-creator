package converter

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestCompleteAIConversion(t *testing.T) {
	html := "<p>Hello World</p>"
	images := []ImageRef{
		{Index: 0, Placeholder: "<!-- IMG:0 -->", WechatURL: "https://cdn.example.com/img.jpg"},
	}

	result := CompleteAIConversion(html, images, "autumn-warm")
	if !result.Success {
		t.Error("expected Success = true")
	}
	if result.HTML != html {
		t.Errorf("HTML = %q, want %q", result.HTML, html)
	}
	if result.Theme != "autumn-warm" {
		t.Errorf("Theme = %q, want %q", result.Theme, "autumn-warm")
	}
	if len(result.Images) != 1 {
		t.Errorf("len(Images) = %d, want 1", len(result.Images))
	}
}

func TestIsAIRequest_True(t *testing.T) {
	result := &ConvertResult{
		Success: false,
		Error:   "AI_MODE_REQUEST:Convert this markdown",
	}
	if !IsAIRequest(result) {
		t.Error("expected IsAIRequest = true")
	}
}

func TestIsAIRequest_False(t *testing.T) {
	result := &ConvertResult{
		Success: false,
		Error:   "some other error",
	}
	if IsAIRequest(result) {
		t.Error("expected IsAIRequest = false")
	}
}

func TestExtractAIRequest(t *testing.T) {
	result := &ConvertResult{
		Error: "AI_MODE_REQUEST:This is the prompt",
	}
	prompt := ExtractAIRequest(result)
	if prompt != "This is the prompt" {
		t.Errorf("got %q, want %q", prompt, "This is the prompt")
	}
}

func TestGetAIRequestInfo(t *testing.T) {
	images := []ImageRef{
		{Index: 0, Original: "./photo.jpg", Type: ImageTypeLocal},
	}
	result := &ConvertResult{
		Error:  "AI_MODE_REQUEST:The prompt text",
		Images: images,
	}

	prompt, imgs, ok := GetAIRequestInfo(result)
	if !ok {
		t.Error("expected ok = true")
	}
	if prompt != "The prompt text" {
		t.Errorf("prompt = %q", prompt)
	}
	if len(imgs) != 1 {
		t.Errorf("len(imgs) = %d, want 1", len(imgs))
	}
}

func TestGetAIRequestInfo_NotAIRequest(t *testing.T) {
	result := &ConvertResult{
		Error: "regular error",
	}
	_, _, ok := GetAIRequestInfo(result)
	if ok {
		t.Error("expected ok = false for non-AI request")
	}
}

func TestConvertValidation_EmptyMarkdown(t *testing.T) {
	conv := NewConverter(zerolog.Nop())
	result := conv.Convert(&ConvertRequest{Markdown: ""})
	if result.Success {
		t.Error("expected failure for empty markdown")
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestConvertValidation_DefaultTheme(t *testing.T) {
	log := zerolog.Nop()
	conv := NewConverter(log)
	result := conv.Convert(&ConvertRequest{Markdown: "# Test", Theme: ""})
	// Theme should be set to "default" even though we don't check further
	// (the conversion will attempt AI mode which is expected)
	if result.Theme != "default" {
		t.Errorf("Theme = %q, want %q", result.Theme, "default")
	}
}
