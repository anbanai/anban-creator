// Package converter — deterministic renderer.
//
// render.go is the deterministic Markdown→WeChat-HTML renderer. It replaces the
// former LLM conversion path (convertViaAI): instead of asking a model to emit
// styled HTML (which timed out, dropped themes, and conflated style dimensions),
// it parses the Markdown with goldmark and emits WeChat-safe HTML with fully
// inline CSS driven entirely by the structured Theme spec.
//
// Design rules (see CLAUDE.md constraint #1 — WeChat HTML):
//   - Safe tags only: section, p, span, strong, em, a, h1-h6, ul, ol, li,
//     blockquote, pre, code, table, img, br, hr. No script/iframe/form/input/style.
//   - All CSS is inline (style="..."). No <style> blocks, no external resources.
//   - Images become <!-- IMG:N --> placeholders so the existing image-extraction
//     → CDN-upload → ReplacePlaceholders pipeline still works unchanged.
//   - Determinism: identical (markdown, theme) always yields identical HTML.
//   - Errors are surfaced, never silently degraded (no hand-written HTML fallback).

package converter

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// renderDeterministic is the deterministic Convert entry point. It parses the
// Markdown, applies the theme spec, and returns WeChat HTML with IMG
// placeholders plus the ordered image reference list. An empty theme name
// resolves to the default ("autumn-warm"); an unknown theme name is a hard
// error (errors must be exposed, not papered over).
func (c *converter) renderDeterministic(req *ConvertRequest) *ConvertResult {
	result := &ConvertResult{}

	if strings.TrimSpace(req.Markdown) == "" {
		result.Theme = req.Theme
		result.Error = ErrEmptyMarkdown.Error()
		return result
	}
	if req.Theme == "" {
		req.Theme = "autumn-warm"
	}
	result.Theme = req.Theme

	theme, err := c.theme.GetTheme(req.Theme)
	if err != nil {
		// Missing theme is a hard error — surface it rather than render unstyled.
		result.Error = fmt.Sprintf("theme %q not available: %s", req.Theme, err.Error())
		return result
	}

	r := &detRenderer{theme: theme, source: []byte(req.Markdown)}
	md := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.TaskList))
	doc := md.Parser().Parse(text.NewReader(r.source))

	var body strings.Builder
	r.renderBlocks(doc, &body)

	result.HTML = r.wrapContainer(body.String())
	result.Images = r.images
	result.Success = true

	c.log.Info().
		Str("theme", theme.Name).
		Int("image_count", len(r.images)).
		Int("html_length", len(result.HTML)).
		Msg("deterministic render complete")

	return result
}

// detRenderer holds theme + parse state for one render pass.
type detRenderer struct {
	theme  *Theme
	source []byte
	images []ImageRef
}

// wrapContainer wraps the rendered blocks in the theme's "主容器 + 卡片"
// layout. Fallbacks are mobile-first for WeChat reading: no artificial desktop
// max-width, lean outer padding, and readable inner gutters.
func (r *detRenderer) wrapContainer(inner string) string {
	bg := r.colorOf("background", "#faf9f5")
	cardStyle := fmt.Sprintf(
		`max-width:%s;margin:0 auto;padding:%s;background-color:%s;border-radius:%s;border:%s;box-shadow:%s;color:%s;`,
		r.firstNonEmpty(r.theme.Layout.MaxWidth, "none"),
		r.firstNonEmpty(r.theme.Layout.CardPadding, "20px 14px"),
		r.firstNonEmpty(r.theme.Layout.CardBackgroundColor, "#ffffff"),
		r.firstNonEmpty(r.theme.Layout.BorderRadius, "12px"),
		r.firstNonEmpty(r.theme.Layout.CardBorder, "1px solid rgba(0,0,0,0.05)"),
		r.firstNonEmpty(r.theme.Layout.CardBoxShadow, "0 4px 16px rgba(0,0,0,0.04)"),
		r.textColor(),
	)
	if img := r.theme.Layout.CardBackgroundImage; img != "" {
		cardStyle += fmt.Sprintf("background-image:%s;", img)
		if sz := r.theme.Layout.CardBackgroundSize; sz != "" {
			cardStyle += fmt.Sprintf("background-size:%s;", sz)
		}
	}

	var sb strings.Builder
	// All inherited body typography (font-family, font-size, line-height,
	// letter-spacing) is set ONCE on the outer wrapper so the whole document
	// (headings, paragraphs, list items, quotes — none of which repeat these)
	// inherits it. Code blocks override with monospace; headings override
	// line-height/font-size. Color lives on the card below (also inherited by
	// <p>/<li>). Hoisting these avoids repeating a ~70-char style block on every
	// <p>, which otherwise inflates long articles past WeChat's 20,000-char draft
	// limit. Serif/mono themes (classic-serif, geek-terminal, …) still render
	// their font; sans themes are unaffected ('Inter' → system sans).
	fmt.Fprintf(&sb, `<section style="background-color:%s;padding:%s;font-family:%s;font-size:%s;line-height:%s;letter-spacing:%s;">`,
		bg,
		r.firstNonEmpty(r.theme.Layout.ContainerPadding, "16px 0"),
		r.firstNonEmpty(r.theme.Typography.FontFamily, "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', sans-serif"),
		r.firstNonEmpty(r.theme.Typography.FontSize, "16px"),
		r.lineHeight(), r.letterSpacing())
	sb.WriteString("\n")
	fmt.Fprintf(&sb, `<section style="%s">`, cardStyle)
	sb.WriteString("\n")
	sb.WriteString(inner)
	sb.WriteString("</section>\n")
	sb.WriteString("</section>\n")
	return sb.String()
}

// renderBlocks walks a container node's block children.
func (r *detRenderer) renderBlocks(node ast.Node, sb *strings.Builder) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		r.renderBlock(child, sb)
	}
}

// renderBlock emits HTML for one block-level node.
func (r *detRenderer) renderBlock(node ast.Node, sb *strings.Builder) {
	switch n := node.(type) {
	case *ast.Heading:
		r.renderHeading(n, sb)
	case *ast.Paragraph:
		sb.WriteString(`<p style="`)
		sb.WriteString(r.bodyMargin())
		sb.WriteString(`">`)
		sb.WriteString(r.renderInlines(n))
		sb.WriteString("</p>\n")
	case *ast.TextBlock:
		// Loose text block (e.g. inside a list item): treat as a paragraph.
		sb.WriteString(`<p style="`)
		sb.WriteString(r.bodyMargin())
		sb.WriteString(`">`)
		sb.WriteString(r.renderInlines(n))
		sb.WriteString("</p>\n")
	case *ast.Blockquote:
		r.renderBlockquote(n, sb)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		r.renderCodeBlock(node, sb)
	case *ast.ThematicBreak:
		sb.WriteString(r.hrTag())
		sb.WriteString("\n")
	case *ast.HTMLBlock:
		// Raw HTML is NOT passed through. The renderer owns the HTML surface and
		// must emit only WeChat-safe tags (CLAUDE.md constraint #1), so an HTML
		// block in the source (<script>/<style>/<iframe>/…) is escaped to visible
		// literal text: content preserved, but inert — never executed, never
		// dropped silently.
		sb.WriteString(html.EscapeString(string(n.Text(r.source))))
		sb.WriteString("\n")
	case *ast.List:
		r.renderList(n, sb)
	case *ast.ListItem:
		// Rendered by renderList; bare items fall back to their blocks.
		r.renderBlocks(n, sb)
	case *extast.Table:
		r.renderTable(n, sb)
	default:
		// Unknown block: render its children defensively so content is never lost.
		r.renderBlocks(node, sb)
	}
}

// renderHeading emits h1/h2/h3+ with theme element styling.
func (r *detRenderer) renderHeading(n *ast.Heading, sb *strings.Builder) {
	inner := r.renderInlines(n)
	switch n.Level {
	case 1:
		fmt.Fprintf(sb,
			`<h1 style="color:%s;font-size:24px;font-weight:bold;text-align:center;line-height:1.4;margin:0 0 18px;letter-spacing:%s;">%s</h1>`+"\n",
			r.textColor(), r.letterSpacing(), inner)
	case 2:
		mod := r.theme.Modules.H2
		icon := mod.Icon
		if icon == "" {
			icon = "▶"
		}
		fmt.Fprintf(sb,
			`<h2 style="border-bottom:%s;padding-bottom:8px;margin:26px 0 14px;line-height:1.4;">`+
				`<span style="color:%s;text-shadow:%s;margin-right:8px;">%s</span>`+
				`<span style="color:%s;font-weight:bold;">%s</span></h2>`+"\n",
			r.firstNonEmpty(mod.BorderBottom, "1px dashed rgba(0,0,0,0.15)"),
			r.firstNonEmpty(mod.IconColor, r.primaryColor()), r.firstNonEmpty(mod.IconTextShadow, "none"), icon,
			r.firstNonEmpty(mod.TextColor, r.primaryColor()), inner)
	default: // h3..h6 collapse to h3 styling keyed on level font size.
		mod := r.theme.Modules.H3
		size := 18
		if n.Level >= 4 {
			size = 16
		}
		fmt.Fprintf(sb,
			`<h%d style="display:inline-block;color:%s;border-bottom:%s;font-size:%dpx;font-weight:bold;padding-bottom:4px;margin:%dpx 0 12px;line-height:1.4;">%s</h%d>`+"\n",
			n.Level,
			r.firstNonEmpty(mod.TextColor, r.secondaryColor()),
			r.firstNonEmpty(mod.BorderBottom, "2px solid "+r.primaryColor()),
			size, 24, inner, n.Level)
	}
}

// renderBlockquote handles normal quotes plus GFM-style alerts ([!info]/[!tip]/...).
func (r *detRenderer) renderBlockquote(n *ast.Blockquote, sb *strings.Builder) {
	full := string(n.Text(r.source))
	if alertType, ok := matchAlert(full); ok {
		r.renderAlert(n, alertType, sb)
		return
	}
	mod := r.theme.Modules.Blockquote
	fmt.Fprintf(sb,
		`<blockquote style="margin:14px 0;padding:12px 14px;background-color:%s;border-left:%s;border-radius:4px;box-shadow:%s;">`,
		r.firstNonEmpty(mod.BackgroundColor, r.firstNonEmpty(r.theme.Colors["quote_background"], "#f5f5f5")),
		r.firstNonEmpty(mod.BorderLeft, "5px solid "+r.primaryColor()),
		r.firstNonEmpty(mod.BoxShadow, "none"))
	sb.WriteString("\n")
	r.renderBlocks(n, sb)
	sb.WriteString("</blockquote>\n")
}

// renderAlert renders a GFM alert ([!info]/[!tip]/[!warning]/[!danger]/[!success]/[!note]).
func (r *detRenderer) renderAlert(n *ast.Blockquote, alertType string, sb *strings.Builder) {
	spec := alertStyle(alertType, r)
	fmt.Fprintf(sb,
		`<section style="margin:14px 0;padding:12px 14px;border-radius:8px;background-color:%s;border:1px solid %s;">`+
			`<p style="margin:0 0 6px;font-weight:bold;color:%s;">%s</p>`,
		spec.bg, spec.border, spec.accent, spec.label)
	sb.WriteString("\n")
	// The first paragraph carries the "[!type]" marker. In the common single-
	// paragraph form the body shares that paragraph ("> [!tip]\n> body"), so we
	// strip just the leading marker rather than dropping the whole paragraph;
	// in the two-paragraph form the marker paragraph is empty after stripping
	// and is skipped. Remaining children render normally.
	first := true
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if first {
			first = false
			if p, ok := child.(*ast.Paragraph); ok {
				body := strings.TrimSpace(alertLeadingMarker.ReplaceAllString(r.renderInlines(p), ""))
				if body != "" {
					fmt.Fprintf(sb, `<p style="%s">%s</p>`+"\n", r.bodyMargin(), body)
				}
				continue
			}
		}
		r.renderBlock(child, sb)
	}
	sb.WriteString("</section>\n")
}

// alertLeadingMarker matches a leading "[!type]" alert marker (plus surrounding
// whitespace) so it can be stripped from an alert's first paragraph.
var alertLeadingMarker = regexp.MustCompile(`^\s*\[!(?:info|tip|warning|danger|success|note|important|caution)\]\s*`)

// renderCodeBlock emits a <pre><code> block.
func (r *detRenderer) renderCodeBlock(node ast.Node, sb *strings.Builder) {
	var code strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		code.Write(child.Text(r.source))
	}
	fmt.Fprintf(sb,
		`<pre style="margin:16px 0;padding:16px;background-color:#f6f6f6;border-radius:6px;overflow-x:auto;"><code style="font-family:'SFMono-Regular',Consolas,'Liberation Mono',Menlo,monospace;font-size:14px;line-height:1.5;color:#333;">%s</code></pre>`+"\n",
		html.EscapeString(strings.TrimRight(code.String(), "\n")))
}

// renderList emits ul/ol with their list items.
func (r *detRenderer) renderList(n *ast.List, sb *strings.Builder) {
	tag := "ul"
	if n.IsOrdered() {
		tag = "ol"
	}
	fmt.Fprintf(sb, `<%s style="margin:16px 0;padding-left:22px;line-height:%s;">`, tag, r.lineHeight())
	sb.WriteString("\n")
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		li, ok := child.(*ast.ListItem)
		if !ok {
			r.renderBlock(child, sb)
			continue
		}
		sb.WriteString(`<li style="margin:6px 0;">`)
		// A task-list checkbox is the first inline child of the item.
		r.renderListItemChildren(li, sb)
		sb.WriteString("</li>\n")
	}
	fmt.Fprintf(sb, "</%s>\n", tag)
}

// renderListItemChildren renders a list item's contents, translating a leading
// TaskCheckBox into a safe box glyph (☑/☐) since WeChat disallows <input>.
func (r *detRenderer) renderListItemChildren(li *ast.ListItem, sb *strings.Builder) {
	for child := li.FirstChild(); child != nil; child = child.NextSibling() {
		if cb, ok := child.(*extast.TaskCheckBox); ok {
			if cb.IsChecked {
				sb.WriteString(`<span style="margin-right:6px;">☑</span>`)
			} else {
				sb.WriteString(`<span style="margin-right:6px;">☐</span>`)
			}
			continue
		}
		r.renderBlock(child, sb)
	}
}

// renderTable emits a styled <table>. GFM tables parse into TableHeader /
// TableRow containers holding TableCell nodes with inline content + alignment.
func (r *detRenderer) renderTable(n *extast.Table, sb *strings.Builder) {
	cellStyle := "border:1px solid #e0e0e0;padding:8px 12px;text-align:left;"
	headStyle := "background-color:" + r.firstNonEmpty(r.colorOf("quote_background", "#f5f5f5"), "#f5f5f5") + ";font-weight:bold;border:1px solid #e0e0e0;padding:8px 12px;"
	sb.WriteString(`<table style="width:100%;border-collapse:collapse;margin:16px 0;font-size:15px;color:` + r.textColor() + `;">` + "\n")
	// All body rows share a single <tbody> (one tbody per row is valid HTML but
	// needlessly fragments the table).
	tbodyOpen := false
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		isHeader := false
		switch child.(type) {
		case *extast.TableHeader:
			sb.WriteString("<thead>\n")
			isHeader = true
		case *extast.TableRow:
			if !tbodyOpen {
				sb.WriteString("<tbody>\n")
				tbodyOpen = true
			}
		default:
			continue
		}
		sb.WriteString("<tr>\n")
		for cell := child.FirstChild(); cell != nil; cell = cell.NextSibling() {
			tc, ok := cell.(*extast.TableCell)
			if !ok {
				continue
			}
			align := ""
			switch tc.Alignment {
			case extast.AlignLeft:
				align = "text-align:left;"
			case extast.AlignCenter:
				align = "text-align:center;"
			case extast.AlignRight:
				align = "text-align:right;"
			}
			tag := "td"
			style := cellStyle + align
			if isHeader {
				tag = "th"
				style = headStyle + align
			}
			fmt.Fprintf(sb, `<%s style="%s">%s</%s>`, tag, style, r.renderInlines(tc), tag)
		}
		sb.WriteString("\n</tr>\n")
		if isHeader {
			sb.WriteString("</thead>\n")
		}
	}
	if tbodyOpen {
		sb.WriteString("</tbody>\n")
	}
	sb.WriteString("</table>\n")
}

// renderInlines walks inline children, returning their HTML.
func (r *detRenderer) renderInlines(node ast.Node) string {
	var sb strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		r.renderInline(child, &sb)
	}
	return sb.String()
}

// renderInline emits HTML for one inline node.
func (r *detRenderer) renderInline(node ast.Node, sb *strings.Builder) {
	switch n := node.(type) {
	case *ast.Text:
		sb.WriteString(r.escapeText(string(n.Text(r.source))))
	case *ast.String: // e.g. raw HTML entities within code spans are handled separately
		sb.WriteString(r.escapeText(string(n.Text(r.source))))
	case *ast.CodeSpan:
		sb.WriteString(`<code style="background-color:#f0f0f0;padding:2px 5px;border-radius:3px;font-family:'SFMono-Regular',Consolas,Menlo,monospace;font-size:0.9em;color:` + r.secondaryColor() + `;">`)
		sb.WriteString(r.escapeText(string(n.Text(r.source))))
		sb.WriteString("</code>")
	case *ast.Emphasis:
		if n.Level == 2 {
			sb.WriteString(`<strong style="color:` + r.strongColor() + `;font-weight:bold;">`)
		} else {
			sb.WriteString(`<em style="font-style:italic;">`)
		}
		sb.WriteString(r.renderInlines(n))
		if n.Level == 2 {
			sb.WriteString("</strong>")
		} else {
			sb.WriteString("</em>")
		}
	case *ast.Link:
		fmt.Fprintf(sb, `<a href="%s" style="color:%s;text-decoration:none;border-bottom:1px solid %s;">`,
			r.sanitizeURL(string(n.Destination)), r.primaryColor(), r.primaryColor())
		sb.WriteString(r.renderInlines(n))
		sb.WriteString("</a>")
	case *ast.Image:
		r.emitImage(n, sb)
	case *extast.TaskCheckBox:
		if n.IsChecked {
			sb.WriteString(`<span style="margin-right:6px;">☑</span>`)
		} else {
			sb.WriteString(`<span style="margin-right:6px;">☐</span>`)
		}
	case *ast.AutoLink:
		url := string(n.URL(r.source))
		fmt.Fprintf(sb, `<a href="%s" style="color:%s;text-decoration:none;">%s</a>`, r.sanitizeURL(url), r.primaryColor(), html.EscapeString(url))
	case *ast.RawHTML:
		// Inline raw HTML is escaped to inert literal text (see HTMLBlock above).
		sb.WriteString(r.escapeText(string(n.Text(r.source))))
	default:
		// Unknown inline: emit its text content rather than drop it.
		sb.WriteString(r.escapeText(string(node.Text(r.source))))
	}
}

// emitImage converts a markdown image into a <!-- IMG:N --> placeholder and
// records an ImageRef so the downstream upload+replace pipeline can fill it in.
func (r *detRenderer) emitImage(n *ast.Image, sb *strings.Builder) {
	idx := len(r.images)
	placeholder := fmt.Sprintf("<!-- IMG:%d -->", idx)
	url := string(n.Destination)
	imgType := ImageTypeOnline
	if strings.HasPrefix(url, "./") || strings.HasPrefix(url, "/") && !strings.HasPrefix(url, "//") {
		imgType = ImageTypeLocal
	}
	r.images = append(r.images, ImageRef{
		Index:       idx,
		Original:    url,
		Placeholder: placeholder,
		Type:        imgType,
	})
	fmt.Fprintf(sb, `<p style="text-align:center;margin:16px 0;">%s</p>`, placeholder)
}

// ---------------------------------------------------------------------------
// Theme-spec accessors with safe defaults so a partially-specified theme still
// renders (the YAML is authoritative but every value has a sane fallback).
// ---------------------------------------------------------------------------

func (r *detRenderer) textColor() string      { return r.colorOf("text", "#333333") }
func (r *detRenderer) primaryColor() string   { return r.colorOf("primary", "#d97758") }
func (r *detRenderer) secondaryColor() string { return r.colorOf("secondary", "#c06b4d") }
func (r *detRenderer) strongColor() string {
	c := r.theme.Modules.Strong.Color
	return r.firstNonEmpty(c, r.secondaryColor())
}

func (r *detRenderer) colorOf(key, fallback string) string {
	if v, ok := r.theme.Colors[key]; ok && v != "" {
		return v
	}
	return fallback
}

func (r *detRenderer) lineHeight() string {
	return r.firstNonEmpty(r.theme.Typography.LineHeight, "1.75")
}
func (r *detRenderer) letterSpacing() string {
	return r.firstNonEmpty(r.theme.Typography.LetterSpacing, "0.5px")
}
func (r *detRenderer) bodyMargin() string {
	// Body color + typography are inherited from the container wrapper, so a <p>
	// only needs its own margin. Repeating the full style on every paragraph
	// bloats long articles past WeChat's 20,000-char draft limit.
	return "margin:" + r.firstNonEmpty(r.theme.Layout.ParagraphMargin, "0 0 16px") + ";"
}

func (r *detRenderer) hrTag() string {
	mod := r.theme.Modules.HR
	border := r.firstNonEmpty(mod.Border, "none")
	height := r.firstNonEmpty(mod.Height, "1px")
	bg := r.firstNonEmpty(mod.Background, "rgba(0,0,0,0.1)")
	return fmt.Sprintf(`<hr style="border:%s;height:%s;background:%s;margin:28px auto;width:80%%;" />`, border, height, bg)
}

func (r *detRenderer) firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (r *detRenderer) escapeText(s string) string {
	// Collapse intra-paragraph soft line breaks to spaces; hard breaks survive as-is.
	s = strings.ReplaceAll(s, "\n", " ")
	return html.EscapeString(s)
}

// sanitizeURL permits only safe URL schemes (http/https/mailto/tel) plus
// protocol-relative, root-relative, anchor, and query references. Dangerous
// schemes — javascript:, data:, vbscript: — collapse to an empty href so a
// hostile or mistaken link can never execute. ASCII control chars are stripped
// first because browsers ignore them when matching a scheme, which would let a
// "java\x09script:" URL slip past a naive prefix check.
func (r *detRenderer) sanitizeURL(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f {
			return -1
		}
		return c
	}, s)
	if s == "" {
		return ""
	}
	relative := strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "#") || strings.HasPrefix(s, "?")
	if !relative {
		if i := strings.Index(s, ":"); i > 0 {
			switch strings.ToLower(s[:i]) {
			case "http", "https", "mailto", "tel":
				// allowed absolute scheme
			default:
				return "" // javascript:, data:, vbscript:, etc.
			}
		}
	}
	s = strings.ReplaceAll(s, `"`, "%22")
	return html.EscapeString(s)
}

// ---------------------------------------------------------------------------
// GFM alert ([!type]) detection and per-type styling.
// ---------------------------------------------------------------------------

var alertMarker = regexp.MustCompile(`(?m)^\s*\[!(info|tip|warning|danger|success|note|important|caution)\]\s*$`)

func matchAlert(text string) (string, bool) {
	m := alertMarker.FindStringSubmatch(strings.ToLower(text))
	if m == nil {
		return "", false
	}
	return m[1], true
}

type alertSpec struct {
	label, accent, bg, border string
}

func alertStyle(t string, r *detRenderer) alertSpec {
	primary := r.primaryColor()
	switch t {
	case "tip", "success":
		return alertSpec{"提示", "#2e7d32", "#eaf6ea", "#cfe8cf"}
	case "warning":
		return alertSpec{"注意", "#b8860b", "#fff7e0", "#f0dfa0"}
	case "danger", "important", "caution":
		return alertSpec{"警告", "#c0392b", "#fdecea", "#f5c6c0"}
	case "note", "info":
		fallthrough
	default:
		return alertSpec{"说明", primary, "#f0f3f7", "#d3dde8"}
	}
}
