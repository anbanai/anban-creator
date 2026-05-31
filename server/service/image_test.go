package service

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	appimage "github.com/royalrick/anbanwriter/app/image"
)

func TestBuildImageResultIncludesGenerationMetadata(t *testing.T) {
	raw := &appimage.GenerateRawResult{
		URL:             "data:image/png;base64,iVBORw0KGgo=",
		Size:            "3:4",
		Prompt:          "春日饮茶指南",
		Provider:        "Volcengine",
		Model:           "doubao-seedream",
		RevisedPrompt:   "optimized prompt",
		ResponseType:    "url",
		ResponsePreview: "https://example.com/image.png",
		OutputMIME:      "image/png",
	}

	got := buildImageResult(raw, "cover")

	if got.Prompt != "春日饮茶指南" {
		t.Fatalf("Prompt = %q, want 春日饮茶指南", got.Prompt)
	}
	if got.ImageType != "cover" {
		t.Fatalf("ImageType = %q, want cover", got.ImageType)
	}
	if got.Provider != "Volcengine" {
		t.Fatalf("Provider = %q, want Volcengine", got.Provider)
	}
	if got.Model != "doubao-seedream" {
		t.Fatalf("Model = %q, want doubao-seedream", got.Model)
	}
	if got.RevisedPrompt != "optimized prompt" {
		t.Fatalf("RevisedPrompt = %q, want optimized prompt", got.RevisedPrompt)
	}
	if got.ResponseType != "url" {
		t.Fatalf("ResponseType = %q, want url", got.ResponseType)
	}
	if got.OutputMIME != "image/png" {
		t.Fatalf("OutputMIME = %q, want image/png", got.OutputMIME)
	}
}

func TestSaveGeneratedImageBytesReencodesJPEGWhenOutputIsPNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 120, G: 80, B: 40, A: 255})

	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "cover.png")
	outputMIME, err := saveGeneratedImageBytes(outputPath, jpegBuf.Bytes())
	if err != nil {
		t.Fatalf("saveGeneratedImageBytes() error = %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if detected := http.DetectContentType(data); detected != "image/png" {
		t.Fatalf("output MIME = %q, want image/png", detected)
	}
	if outputMIME != "image/png" {
		t.Fatalf("returned MIME = %q, want image/png", outputMIME)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("png.Decode(output) error = %v", err)
	}
}

func TestDetectTaskFileMIMEUsesImageContentBeforeExtension(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})

	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	path := filepath.Join(t.TempDir(), "image_01.png")
	if err := os.WriteFile(path, jpegBuf.Bytes(), 0644); err != nil {
		t.Fatalf("write jpeg bytes: %v", err)
	}

	if got := DetectTaskFileMIME(path); got != "image/jpeg" {
		t.Fatalf("DetectTaskFileMIME() = %q, want image/jpeg", got)
	}
}
