package model

import "time"

// Template represents a reusable content template (poster, seednote, article).
//
// Field surface differs by type:
//   - 小红书 (seednote): only 图片视觉 (StylePrompt + ThumbnailURL).
//   - 公众号 (article):  图片视觉 (StylePrompt) + 作者 (AuthorName, the published
//     byline — overrides project.Author via the resolved `author`) + 写作风格
//     (AuthorStyleIntro free text + optional AuthorAvatarURL persona avatar,
//     drives writing imitation) + 排版 (Theme).
//   - 海报 (poster):    legacy content scaffold (WritingStyle/Structure/Example/...).
//
// TWO concepts on the article template are kept STRICTLY SEPARATE:
//
//   - 作者 (AuthorName) = the published WeChat byline (署名). JUST a name.
//     Surfaced to the agent via get_project_profile as the resolved top-level
//     `author` (precedence template > project); the agent passes it to
//     publish_draft. Empty = no override (project.Author used).
//   - 写作风格 (AuthorStyleIntro + AuthorAvatarURL) = the writing imitation
//     (模仿内容的框架/写作方式/笔迹), defined inline. NOT the byline, does NOT
//     derive from 作者. Surfaced as template_writing_style (drives the writing
//     voice) and template_author_avatar (optional persona avatar, 入人设不入署名).
//
// WritingStyle (writer 资源 key) is retained for poster and project resolution;
// the article form does NOT set it, so it never pollutes Task.WritingStyle
// (which config_builder treats as a writer key).
//
// The content scaffold — Structure (内容结构, {"text": markdown}), ExampleContent
// (示例, {"text": markdown}) — plus Category + Tags are retained for poster /
// legacy data and the template-library filters.
type Template struct {
	ID           string         `gorm:"type:char(36);primaryKey" json:"id"`
	UserID       string         `gorm:"type:char(36);index" json:"user_id,omitempty"`
	Visibility   string         `gorm:"type:varchar(20);default:'public'" json:"visibility"` // public | private
	Type         string         `gorm:"type:varchar(20);not null;index" json:"type"`
	Name         string         `gorm:"type:varchar(100);not null" json:"name"`
	Category     string         `gorm:"type:varchar(50);index" json:"category"`
	ThumbnailURL string         `gorm:"type:varchar(500)" json:"thumbnail_url"`
	Structure    map[string]any `gorm:"type:json;serializer:json" json:"structure"`
	StylePrompt  string         `gorm:"type:text" json:"style_prompt"`
	// WritingStyle is the writer 资源 key (e.g. "dan-koe") used by poster projects
	// and project resolution. The article form does NOT set it (it uses the inline
	// writing-style fields below instead) so it never leaks into Task.WritingStyle.
	WritingStyle string `gorm:"type:text" json:"writing_style"`
	// Theme is the template's 排版样式 (theme resource key, e.g. "autumn-warm"),
	// the layout/typesetting dimension. Surfaced to the agent as template_theme
	// and resolved into Task.Theme at creation. Empty = no override (project theme used).
	Theme string `gorm:"type:varchar(50);default:''" json:"theme"`
	// 作者（署名 byline）— the published WeChat author name. Surfaced to the agent
	// via get_project_profile as the resolved top-level `author` (precedence
	// template > project); the agent passes it to publish_draft. JUST a name for
	// 署名 — independent of the 写作风格 fields below. Empty = project.Author used.
	AuthorName string `gorm:"type:varchar(100)" json:"author_name"`
	// 写作风格（模仿写作）— defined inline on the article template (NOT a writer
	// 资源 key, NOT the byline). AuthorStyleIntro is the free-text writing
	// direction (模仿框架/写作方式/笔迹); AuthorAvatarURL is an optional persona
	// avatar (人设视觉，不入署名). Surfaced as template_writing_style and
	// template_author_avatar. Empty = no template-level writing direction.
	AuthorAvatarURL  string         `gorm:"type:varchar(500)" json:"author_avatar_url"`
	AuthorStyleIntro string         `gorm:"type:text" json:"author_style_intro"`
	ExampleContent   map[string]any `gorm:"type:json;serializer:json" json:"example_content"`
	Tags             []string       `gorm:"type:json;serializer:json" json:"tags"`
	SortOrder        int            `gorm:"default:0" json:"sort_order"`
	IsActive         bool           `gorm:"default:true" json:"is_active"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

func (Template) TableName() string { return "templates" }
