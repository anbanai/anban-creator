package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTitleFromWorkspace_HTML_H1(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output", "article.html"),
		`<html><head><title>Page Title</title></head><body><h1>实际文章标题</h1><p>content</p></body></html>`)

	got := ExtractTitleFromWorkspace(dir)
	if got != "实际文章标题" {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want %q", got, "实际文章标题")
	}
}

func TestExtractTitleFromWorkspace_HTML_TitleOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output", "article.html"),
		`<html><head><title>Fallback Title</title></head><body><p>no h1 here</p></body></html>`)

	got := ExtractTitleFromWorkspace(dir)
	if got != "Fallback Title" {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want %q", got, "Fallback Title")
	}
}

func TestExtractTitleFromWorkspace_HTML_H1_WithNestedTags(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output", "article.html"),
		`<html><body><h1><span style="color:red">嵌套标签标题</span></h1></body></html>`)

	got := ExtractTitleFromWorkspace(dir)
	if got != "嵌套标签标题" {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want %q", got, "嵌套标签标题")
	}
}

func TestExtractTitleFromWorkspace_Markdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output", "content.md"),
		`# Markdown 标题

Some body text here.
## Subtitle
More content.`)

	got := ExtractTitleFromWorkspace(dir)
	if got != "Markdown 标题" {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want %q", got, "Markdown 标题")
	}
}

func TestExtractTitleFromWorkspace_HTML_Priority_Over_Markdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output", "article.html"),
		`<html><body><h1>HTML 标题</h1></body></html>`)
	writeFile(t, filepath.Join(dir, "output", "content.md"),
		`# Markdown 标题`)

	got := ExtractTitleFromWorkspace(dir)
	if got != "HTML 标题" {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want HTML title", got)
	}
}

func TestExtractTitleFromWorkspace_NoTitle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output", "article.html"),
		`<html><body><p>no title elements at all</p></body></html>`)

	got := ExtractTitleFromWorkspace(dir)
	if got != "" {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want empty string", got)
	}
}

func TestExtractTitleFromWorkspace_EmptyDir(t *testing.T) {
	got := ExtractTitleFromWorkspace(t.TempDir())
	if got != "" {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want empty string", got)
	}
}

func TestExtractTitleFromWorkspace_HTML_Entities(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output", "article.html"),
		`<html><body><h1>A &amp; B &lt; C &gt; D &quot;E&quot; F&#39;G</h1></body></html>`)

	got := ExtractTitleFromWorkspace(dir)
	want := `A & B < C > D "E" F'G`
	if got != want {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want %q", got, want)
	}
}

func TestExtractTitleFromWorkspace_SkipsDotfiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output", ".hidden.html"),
		`<html><body><h1>Hidden Title</h1></body></html>`)
	writeFile(t, filepath.Join(dir, "output", "visible.html"),
		`<html><body><h1>Visible Title</h1></body></html>`)

	got := ExtractTitleFromWorkspace(dir)
	if got != "Visible Title" {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want %q", got, "Visible Title")
	}
}

func TestExtractTitleFromWorkspace_SkipsDockerRuntimeHome(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".anban-runtime-home", "cached.html"),
		`<html><body><h1>Private Runtime Title</h1></body></html>`)
	writeFile(t, filepath.Join(dir, "article.md"), "# Visible Title")

	got := ExtractTitleFromWorkspace(dir)
	if got != "Visible Title" {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want %q", got, "Visible Title")
	}
}

func TestExtractTitleFromWorkspace_NoOutputDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "article.html"),
		`<html><body><h1>Root Level Title</h1></body></html>`)

	got := ExtractTitleFromWorkspace(dir)
	if got != "Root Level Title" {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want %q", got, "Root Level Title")
	}
}

func TestExtractTitleFromWorkspace_MultilineH1(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "output", "article.html"),
		`<html><body><h1>
Line 1
  Line 2
</h1></body></html>`)

	got := ExtractTitleFromWorkspace(dir)
	want := "Line 1 Line 2"
	if got != want {
		t.Errorf("ExtractTitleFromWorkspace() = %q, want %q", got, want)
	}
}

func TestCleanTitle_LongUnicode(t *testing.T) {
	longTitle := string(make([]rune, 300))
	for i := range longTitle {
		longTitle = longTitle[:i] + "字" + longTitle[i+1:]
	}
	got := cleanTitle(longTitle)
	if len([]rune(got)) != 200 {
		t.Errorf("cleanTitle() length = %d, want 200", len([]rune(got)))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
