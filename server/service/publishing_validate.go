package service

import (
	"fmt"
	"regexp"
)

// imgSrcRegexp matches the src attribute of <img ...> tags. It is
// case-insensitive and tolerates attribute ordering, single/double quotes, and
// whitespace around `=`. The HTML fed to create_draft is machine-generated
// (render_template / convert_markdown), so a regexp is sufficient and avoids
// pulling an HTML parser into the service package. Each match stops at the
// first `>`, so it cannot bleed across tags.
var imgSrcRegexp = regexp.MustCompile(`(?i)<img\b[^>]*\bsrc\s*=\s*["']([^"']+)["']`)

// extractImageSrcs returns every <img src> URL found in html, preserving
// duplicates and order. URLs are returned verbatim (no normalization).
func extractImageSrcs(html string) []string {
	matches := imgSrcRegexp.FindAllStringSubmatch(html, -1)
	srcs := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) >= 2 {
			srcs = append(srcs, m[1])
		}
	}
	return srcs
}

// validateContentImageDiversity rejects body HTML whose images all collapse to a
// single URL. This is the mechanical backstop for the "all content images
// identical yet still published" failure: when image generation fails partway,
// agents historically fell back to reusing one image for every slot, producing a
// draft where N <img> tags all point at the same URL. Such a draft must never
// reach WeChat.
//
// 0 or 1 image passes through; 2+ images sharing exactly one unique URL is
// rejected; 2+ distinct URLs pass through.
func validateContentImageDiversity(content string) error {
	srcs := extractImageSrcs(content)
	if len(srcs) < 2 {
		return nil
	}
	seen := make(map[string]struct{}, len(srcs))
	for _, s := range srcs {
		seen[s] = struct{}{}
	}
	if len(seen) == 1 {
		return fmt.Errorf("内容配图重复：正文含 %d 张图片但全部指向同一 URL，需重新生成配图后再发布", len(srcs))
	}
	return nil
}
