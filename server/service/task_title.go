package service

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	h1Re     = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	titleRe  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	headingRe = regexp.MustCompile(`^#\s+(.+)$`)
	stripRe  = regexp.MustCompile(`<[^>]+>`)
)

// ExtractTitleFromWorkspace scans workspace output files for a content title.
// Priority: HTML <h1> > HTML <title> > Markdown # heading.
func ExtractTitleFromWorkspace(workDir string) string {
	scanDir := workDir
	if info, err := os.Stat(filepath.Join(workDir, "output")); err == nil && info.IsDir() {
		scanDir = filepath.Join(workDir, "output")
	}

	var htmlTitle, mdHeading string
	_ = filepath.WalkDir(scanDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if ShouldSkipTaskFileDir(d.Name()) || ShouldSkipTaskFile(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		switch ext {
		case ".html", ".htm":
			if htmlTitle == "" {
				htmlTitle = extractTitleFromHTMLFile(path)
			}
		case ".md", ".markdown":
			if mdHeading == "" {
				mdHeading = extractTitleFromMarkdownFile(path)
			}
		}
		return nil
	})

	if htmlTitle != "" {
		return htmlTitle
	}
	return mdHeading
}

func extractTitleFromHTMLFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)

	if m := h1Re.FindStringSubmatch(content); len(m) > 1 {
		if t := cleanTitle(m[1]); t != "" {
			return t
		}
	}
	if m := titleRe.FindStringSubmatch(content); len(m) > 1 {
		if t := cleanTitle(m[1]); t != "" {
			return t
		}
	}
	return ""
}

func extractTitleFromMarkdownFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if m := headingRe.FindStringSubmatch(line); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func cleanTitle(raw string) string {
	s := stripRe.ReplaceAllString(raw, "")
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > 200 {
		s = string([]rune(s)[:200])
	}
	return s
}
