package service

import (
	"strings"
	"testing"
)

func TestSeednoteExportParsesMarkdown(t *testing.T) {
	svc := NewSeednoteExportService()
	result, err := svc.Export(SeednoteExportRequest{
		Format:   "json",
		Markdown: "# 每天认识一种茶\n\n## 黄茶入门\n\n![封面](cover.png)\n\n正文 **重点** #黄茶 #新手入门",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "每天认识一种茶" {
		t.Fatalf("title = %q", result.Title)
	}
	if !strings.Contains(result.Content, "[图片:1]") || !strings.Contains(result.Content, "正文 重点") {
		t.Fatalf("content = %q", result.Content)
	}
	if len(result.Tags) != 2 || result.Tags[0] != "黄茶" || result.Tags[1] != "新手入门" {
		t.Fatalf("tags = %#v", result.Tags)
	}
	if len(result.Images) != 1 || result.Images[0].URL != "cover.png" {
		t.Fatalf("images = %#v", result.Images)
	}
}

func TestSeednoteExportFormatsMarkdown(t *testing.T) {
	svc := NewSeednoteExportService()
	result, err := svc.Export(SeednoteExportRequest{
		Format: "markdown", Title: "黄茶", Content: "正文", Tags: []string{"茶", "入门"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != "markdown" || !strings.Contains(result.Output, "# Seednote Publishing Content") {
		t.Fatalf("markdown result = %#v", result)
	}
}

func TestSeednoteExportDirectTagsPreserveOrderAndDuplicates(t *testing.T) {
	result, err := NewSeednoteExportService().Export(SeednoteExportRequest{
		Title: "黄茶", Content: "正文", Tags: []string{" 茶 ", "茶", ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tags) != 3 || result.Tags[0] != "茶" || result.Tags[1] != "茶" || result.Tags[2] != "" {
		t.Fatalf("tags = %#v", result.Tags)
	}
}
