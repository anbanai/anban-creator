package service

import "github.com/royalrick/anbanwriter/server/model"

// firstNonEmpty returns the first non-empty string in args, or "" if all empty.
// Used to resolve each style dimension through its precedence chain
// (task → template → plan → channel).
func firstNonEmpty(args ...string) string {
	for _, s := range args {
		if s != "" {
			return s
		}
	}
	return ""
}

// templateVisual returns the template's 图片视觉 (StylePrompt), nil-safe.
func templateVisual(t *model.Template) string {
	if t == nil {
		return ""
	}
	return t.StylePrompt
}

// templateWritingStyle returns the template's 写作风格, nil-safe.
func templateWritingStyle(t *model.Template) string {
	if t == nil {
		return ""
	}
	return t.WritingStyle
}

// templateTheme returns the template's 排版样式, nil-safe.
func templateTheme(t *model.Template) string {
	if t == nil {
		return ""
	}
	return t.Theme
}

// templateAuthorName returns the template's 作者（署名 byline）, nil-safe.
func templateAuthorName(t *model.Template) string {
	if t == nil {
		return ""
	}
	return t.AuthorName
}

// templateAuthorStyleIntro returns the template's 写作风格 (free-text writing
// imitation), nil-safe.
func templateAuthorStyleIntro(t *model.Template) string {
	if t == nil {
		return ""
	}
	return t.AuthorStyleIntro
}

// templateAuthorAvatar returns the template's optional 写作风格 persona avatar,
// nil-safe.
func templateAuthorAvatar(t *model.Template) string {
	if t == nil {
		return ""
	}
	return t.AuthorAvatarURL
}
