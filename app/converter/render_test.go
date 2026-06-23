package converter

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// testThemeYAML is a small structured theme used to exercise the deterministic
// renderer without depending on the server's embedded theme files.
const testThemeYAML = `name: test-theme
type: deterministic
description: "test theme"
version: "1.0"
colors:
  background: "#fffbeb"
  text: "#333333"
  primary: "#d97758"
  secondary: "#c06b4d"
  quote_background: "#fef4e7"
typography:
  font_family: "Arial, sans-serif"
  font_size: "16px"
  line_height: "1.75"
  letter_spacing: "0.5px"
layout:
  container_padding: "20px 10px"
  max_width: "700px"
  card_padding: "18px"
  border_radius: "12px"
  card_background_color: "#ffffff"
  card_border: "1px solid #eee"
  card_box_shadow: "0 2px 8px rgba(0,0,0,0.05)"
modules:
  h2:
    icon: "▶"
    icon_color: "#d97758"
    text_color: "#d97758"
    border_bottom: "1px dashed #ccc"
  strong:
    color: "#c06b4d"
  blockquote:
    background_color: "#fef4e7"
    border_left: "5px solid #d97758"
  hr:
    border: "none"
    height: "1px"
    background: "#ddd"
`

// newTestConverter builds a Converter wired with the test theme.
func newTestConverter(t *testing.T) Converter {
	t.Helper()
	log := zerolog.Nop()
	return NewConverterWithThemes(&log, map[string][]byte{"test-theme": []byte(testThemeYAML)})
}

// TestRender_BasicMarkdown_InlinesThemeColors renders a representative markdown
// sample and asserts the theme colors are inlined and the structure is sound.
func TestRender_BasicMarkdown_InlinesThemeColors(t *testing.T) {
	cvt := newTestConverter(t)
	md := `# 标题

正文段落，含**加粗**。

## 二级标题

- 列表项一
- 列表项二

> 这是一段引用。

---

收尾。
`
	res := cvt.Convert(&ConvertRequest{Markdown: md, Theme: "test-theme"})
	if !res.Success {
		t.Fatalf("render failed: %s", res.Error)
	}
	html := res.HTML

	// Container background + text color from the theme must be inlined.
	for _, want := range []string{"#fffbeb", "#333333", "#d97758"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing theme color %s", want)
		}
	}
	// Structural elements render to safe tags.
	for _, want := range []string{"<h1", "<h2", "<p ", "<ul", "<li", "<blockquote", "<hr"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing element %q", want)
		}
	}
	// H2 carries the theme icon span.
	if !strings.Contains(html, "▶") {
		t.Error("H2 icon ▶ missing")
	}
}

// TestRender_ImagePlaceholders: markdown images become <!-- IMG:N --> placeholders
// in document order, with matching ImageRef entries.
func TestRender_ImagePlaceholders(t *testing.T) {
	cvt := newTestConverter(t)
	md := `# T

![first](./a.jpg)

text

![second](https://x.com/b.png)
`
	res := cvt.Convert(&ConvertRequest{Markdown: md, Theme: "test-theme"})
	if !res.Success {
		t.Fatalf("render failed: %s", res.Error)
	}
	if len(res.Images) != 2 {
		t.Fatalf("Images len = %d, want 2", len(res.Images))
	}
	if res.Images[0].Original != "./a.jpg" || res.Images[1].Original != "https://x.com/b.png" {
		t.Errorf("image order wrong: %+v", res.Images)
	}
	for i, img := range res.Images {
		want := placeholderFor(i)
		if img.Placeholder != want {
			t.Errorf("Images[%d].Placeholder = %q, want %q", i, img.Placeholder, want)
		}
		if !strings.Contains(res.HTML, want) {
			t.Errorf("HTML missing placeholder %s", want)
		}
	}
}

// placeholderFor mirrors the renderer's <!-- IMG:N --> contract.
func placeholderFor(i int) string {
	switch i {
	case 0:
		return "<!-- IMG:0 -->"
	case 1:
		return "<!-- IMG:1 -->"
	case 2:
		return "<!-- IMG:2 -->"
	default:
		return "<!-- IMG:" + itoa(i) + " -->"
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestRender_CalloutModule: a GFM alert blockquote (> [!info]) renders as a
// callout section rather than a plain blockquote.
func TestRender_CalloutModule(t *testing.T) {
	cvt := newTestConverter(t)
	md := "> [!tip]\n> Use this wisely.\n"
	res := cvt.Convert(&ConvertRequest{Markdown: md, Theme: "test-theme"})
	if !res.Success {
		t.Fatalf("render failed: %s", res.Error)
	}
	// A callout is emitted as a styled <section> (not a bare <blockquote>).
	if !strings.Contains(res.HTML, "<section ") {
		t.Error("callout did not render as a <section>")
	}
	if !strings.Contains(res.HTML, "Use this wisely.") {
		t.Error("callout body text missing")
	}
}

// TestRender_Table: a GFM table renders to <table> with header + body rows.
func TestRender_Table(t *testing.T) {
	cvt := newTestConverter(t)
	md := `| 名称 | 值 |
|------|----|
| a    | 1  |
| b    | 2  |
`
	res := cvt.Convert(&ConvertRequest{Markdown: md, Theme: "test-theme"})
	if !res.Success {
		t.Fatalf("render failed: %s", res.Error)
	}
	for _, want := range []string{"<table", "<th", "<td", "名称", "值"} {
		if !strings.Contains(res.HTML, want) {
			t.Errorf("table HTML missing %q", want)
		}
	}
}

// TestRender_TaskList: GFM task list items render with checkbox markers.
func TestRender_TaskList(t *testing.T) {
	cvt := newTestConverter(t)
	md := "- [x] done\n- [ ] todo\n"
	res := cvt.Convert(&ConvertRequest{Markdown: md, Theme: "test-theme"})
	if !res.Success {
		t.Fatalf("render failed: %s", res.Error)
	}
	if !strings.Contains(res.HTML, "☑") {
		t.Error("checked task item (☑) missing")
	}
	if !strings.Contains(res.HTML, "☐") {
		t.Error("unchecked task item (☐) missing")
	}
}

// TestRender_ContainerWrapper: the output is wrapped in an outer background
// <section> and an inner card <section> carrying the card styling.
func TestRender_ContainerWrapper(t *testing.T) {
	cvt := newTestConverter(t)
	res := cvt.Convert(&ConvertRequest{Markdown: "# Hi\n\nbody.", Theme: "test-theme"})
	if !res.Success {
		t.Fatalf("render failed: %s", res.Error)
	}
	html := res.HTML
	// Two nested sections: outer (background) + inner (card).
	if strings.Count(html, "<section ") < 2 {
		t.Errorf("expected ≥2 <section> elements (container + card), got %d", strings.Count(html, "<section "))
	}
	if !strings.HasPrefix(html, "<section") {
		t.Errorf("HTML should start with the container <section>, got: %q", firstChars(html, 40))
	}
	// Card background + border + shadow must be present.
	for _, want := range []string{"#ffffff", "1px solid #eee", "max-width:700px"} {
		if !strings.Contains(html, want) {
			t.Errorf("card style missing %q", want)
		}
	}
}

// TestRender_Determinism: identical (markdown, theme) yields identical HTML.
func TestRender_Determinism(t *testing.T) {
	cvt := newTestConverter(t)
	md := "## S\n\npara with **bold** and `code`.\n\n- a\n- b\n"
	r1 := cvt.Convert(&ConvertRequest{Markdown: md, Theme: "test-theme"})
	r2 := cvt.Convert(&ConvertRequest{Markdown: md, Theme: "test-theme"})
	if !r1.Success || !r2.Success {
		t.Fatalf("render failed")
	}
	if r1.HTML != r2.HTML {
		t.Error("deterministic renderer produced different HTML for identical input")
	}
}

// TestRender_EmptyMarkdown_Error: empty markdown is a hard error (no fallback).
func TestRender_EmptyMarkdown_Error(t *testing.T) {
	cvt := newTestConverter(t)
	for _, md := range []string{"", "   ", "\n\n"} {
		res := cvt.Convert(&ConvertRequest{Markdown: md, Theme: "test-theme"})
		if res.Success {
			t.Errorf("empty/blank markdown should error, got success for %q", md)
		}
		if res.HTML != "" {
			t.Errorf("empty markdown should produce no HTML, got %q", res.HTML)
		}
	}
}

// TestRender_MissingTheme_Error: an unknown theme is a hard error.
func TestRender_MissingTheme_Error(t *testing.T) {
	cvt := newTestConverter(t)
	res := cvt.Convert(&ConvertRequest{Markdown: "# Hi", Theme: "no-such-theme"})
	if res.Success {
		t.Fatal("missing theme should error, got success")
	}
	if !strings.Contains(res.Error, "no-such-theme") {
		t.Errorf("error should name the missing theme, got: %s", res.Error)
	}
}

// TestRender_DefaultThemeFallback: an empty theme name resolves to autumn-warm.
func TestRender_DefaultThemeFallback(t *testing.T) {
	// Build a converter that also registers autumn-warm so the fallback resolves.
	log := zerolog.Nop()
	themes := map[string][]byte{
		"test-theme": []byte(testThemeYAML),
		"autumn-warm": []byte(`name: autumn-warm
type: deterministic
description: "秋日暖光"
colors:
  background: "#faf9f5"
  text: "#4a413d"
  primary: "#d97758"
`),
	}
	cvt := NewConverterWithThemes(&log, themes)
	res := cvt.Convert(&ConvertRequest{Markdown: "# Hi"})
	if !res.Success {
		t.Fatalf("render failed: %s", res.Error)
	}
	if res.Theme != "autumn-warm" {
		t.Errorf("Theme = %q, want autumn-warm", res.Theme)
	}
}

// TestRender_RawHTML_EscapedNotPassedThrough feeds adversarial raw HTML
// (blocks + inline) and asserts the renderer escapes it to inert literal text —
// never emitting a live <script>/<style>/<iframe>/<form>/<input> tag. Raw HTML
// in source markdown must not survive into the WeChat HTML surface
// (CLAUDE.md constraint #1).
func TestRender_RawHTML_EscapedNotPassedThrough(t *testing.T) {
	cvt := newTestConverter(t)
	md := "# T\n\n" +
		"<script>alert(1)</script>\n\n" +
		"<style>body{color:red}</style>\n\n" +
		"<iframe src=\"https://evil/\"></iframe>\n\n" +
		"<form><input type=text></form>\n\n" +
		"Inline raw HTML: <strong>X</strong> <em>Y</em>.\n"
	res := cvt.Convert(&ConvertRequest{Markdown: md, Theme: "test-theme"})
	if !res.Success {
		t.Fatalf("render failed: %s", res.Error)
	}
	lower := strings.ToLower(res.HTML)
	// Live forbidden tags must never appear (escaped text starts with "&lt;").
	for _, bad := range []string{"<script", "<style", "<iframe", "<form", "<input"} {
		if strings.Contains(lower, bad) {
			t.Errorf("raw HTML passed through as live tag %q — must be escaped", bad)
		}
	}
	// Raw HTML is preserved as inert escaped literal text (content not lost).
	if !strings.Contains(res.HTML, "&lt;script&gt;") {
		t.Errorf("raw <script> should be escaped to &lt;script&gt;, got: %s", res.HTML)
	}
}

// TestRender_DangerousLinks_StrippedFromHref feeds markdown links with hostile
// URL schemes and asserts none reach the href attribute (where a browser would
// execute them). The legitimate https link must survive so sanitization is not
// over-broad. Input is pure markdown links (no raw HTML), so a surviving
// scheme string can only come from a leaky href.
func TestRender_DangerousLinks_StrippedFromHref(t *testing.T) {
	cvt := newTestConverter(t)
	md := "[bad](javascript:alert(1)) [data](data:text/html,x) [vbs](vbscript:alert(2)) [ok](https://example.com)\n"
	res := cvt.Convert(&ConvertRequest{Markdown: md, Theme: "test-theme"})
	if !res.Success {
		t.Fatalf("render failed: %s", res.Error)
	}
	lower := strings.ToLower(res.HTML)
	for _, bad := range []string{"javascript:", "data:text/html", "vbscript:"} {
		if strings.Contains(lower, bad) {
			t.Errorf("dangerous URL scheme %q survived into href", bad)
		}
	}
	if !strings.Contains(lower, "https://example.com") {
		t.Error("legitimate https link dropped by URL sanitizer")
	}
}

// TestRender_StrongUsesThemeColor: **bold** inlines the theme strong color.
func TestRender_StrongUsesThemeColor(t *testing.T) {
	cvt := newTestConverter(t)
	res := cvt.Convert(&ConvertRequest{Markdown: "a **b** c", Theme: "test-theme"})
	if !res.Success {
		t.Fatalf("render failed: %s", res.Error)
	}
	if !strings.Contains(res.HTML, "#c06b4d") {
		t.Error("strong should use theme strong color #c06b4d")
	}
	if !strings.Contains(res.HTML, "<strong") {
		t.Error("<strong> tag missing")
	}
}

func firstChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
