package service

import (
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/resources"
)

// ErrAuthorIsWriterName is returned when a publish author (作者署名) is set to a
// known 写作风格 writer persona name or key. The author is the real publishing
// author name/brand; a writer persona name (e.g. "Dan Koe") is the voice being
// imitated and has a completely different meaning. Conflating them publishes the
// article under an imitated identity — a regression we explicitly reject.
var ErrAuthorIsWriterName = fmt.Errorf("author must not be a writer persona name")

// RejectWriterNameAsAuthor returns ErrAuthorIsWriterName (wrapping a descriptive
// message) when the supplied author is a known 写作风格 writer persona name or
// resource key. An empty author is allowed (means "no explicit author; resolve
// from the precedence chain").
//
// Defense in depth: the Studio PersonaBlock no longer writes the writer name
// into the author field, and this guard rejects any other path (direct API,
// future UI, import) that tries the same. Comparison is trimmed + case-insensitive
// against both the persona display name (e.g. "Dan Koe") and the resource key
// (e.g. "dan-koe"), so neither form slips through.
func RejectWriterNameAsAuthor(author string) error {
	a := strings.TrimSpace(author)
	if a == "" {
		return nil
	}
	lower := strings.ToLower(a)
	for _, w := range resources.Manager().List(resources.CategoryWriter) {
		display := strings.TrimSpace(w.Name)
		key := strings.TrimSpace(w.EnglishName)
		if display == "" && key == "" {
			continue
		}
		if strings.ToLower(display) == lower || strings.ToLower(key) == lower {
			canonical := display
			if canonical == "" {
				canonical = key
			}
			return fmt.Errorf(
				"%w: %q 命中写作风格人设（%s，key=%s）。署名是真实发布者姓名/品牌，写作风格只是供 AI 模仿的口吻——"+
					"二者语义不同、不能混用。请改填真实发布署名（或留空由上层解析）",
				ErrAuthorIsWriterName, a, canonical, key,
			)
		}
	}
	return nil
}
