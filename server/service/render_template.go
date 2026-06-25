package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/app/converter"
	"github.com/royalrick/anbanwriter/server/resources"
)

// ---------------------------------------------------------------------------
// Public types — exposed via MCP tool arguments and results
// ---------------------------------------------------------------------------

// LayoutPlanSlot describes one image/module slot in a rendered article.
type LayoutPlanSlot struct {
	SlotID              string            `json:"slot_id"`                         // hero / section_opener / inline_detail / footer
	SectionIndex        int               `json:"section_index"`                   // 0-based ## section index; -1 for footer
	SectionTitle        string            `json:"section_title,omitempty"`         // convenience for logging/audit
	AfterParagraphIndex int               `json:"after_paragraph_index,omitempty"` // for inline_detail: 0-based paragraph within section
	ImageURL            string            `json:"image_url,omitempty"`             // empty for module-only slots (e.g. footer CTA)
	ImageSize           string            `json:"image_size,omitempty"`            // full-bleed / full-width / inline
	Module              *string           `json:"module,omitempty"`                // layout module name, nil for none
	ModuleVars          map[string]string `json:"module_vars,omitempty"`           // module template variables
}

// LayoutPlan is the structured rendering plan passed to RenderTemplate.
type LayoutPlan struct {
	ArticleType  string           `json:"article_type"`
	TemplateName string           `json:"template_name"`
	Slots        []LayoutPlanSlot `json:"slots"`
	Footer       *LayoutPlanSlot  `json:"footer,omitempty"`
}

// RenderedSlotAudit records what the renderer actually rendered for each planned slot.
type RenderedSlotAudit struct {
	SlotID       string `json:"slot_id"`
	SectionIndex int    `json:"section_index"`
	ImageURL     string `json:"image_url,omitempty"`
	Module       string `json:"module,omitempty"`
	Status       string `json:"status"` // rendered / missing-image / missing-module
	Note         string `json:"note,omitempty"`
}

// RenderTemplateResult is returned by RenderTemplate.
type RenderTemplateResult struct {
	HTML          string              `json:"html"`
	SlotsRendered []RenderedSlotAudit `json:"slots_rendered"`
	Theme         string              `json:"theme"`
}

// ---------------------------------------------------------------------------
// ParseLayoutPlan decodes a generic MCP args map into a LayoutPlan.
// Returns an error if required fields are missing or slots are empty.
// ---------------------------------------------------------------------------

// validSlotIDs is the closed set of slot_id values recognized by the renderer.
// Unknown slot_ids are rejected at parse time so a typo (e.g. "section_openr")
// doesn't silently get dropped during annotation.
var validSlotIDs = map[string]bool{
	"hero":           true,
	"section_opener": true,
	"inline_detail":  true,
	"footer":         true,
}

func ParseLayoutPlan(raw any) (*LayoutPlan, error) {
	if raw == nil {
		return nil, fmt.Errorf("layout_plan is required")
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal layout_plan: %w", err)
	}
	var plan LayoutPlan
	if err := json.Unmarshal(bytes, &plan); err != nil {
		return nil, fmt.Errorf("unmarshal layout_plan: %w", err)
	}
	if len(plan.Slots) == 0 {
		return nil, fmt.Errorf("layout_plan.slots must contain at least one slot")
	}
	for i, slot := range plan.Slots {
		if slot.SlotID == "" {
			return nil, fmt.Errorf("slot[%d].slot_id is required", i)
		}
		if !validSlotIDs[slot.SlotID] {
			return nil, fmt.Errorf("slot[%d].slot_id %q is not a recognized slot (want one of hero/section_opener/inline_detail/footer)", i, slot.SlotID)
		}
	}
	if plan.Footer != nil && plan.Footer.SlotID != "" && !validSlotIDs[plan.Footer.SlotID] {
		return nil, fmt.Errorf("footer.slot_id %q is not a recognized slot", plan.Footer.SlotID)
	}
	return &plan, nil
}

// ---------------------------------------------------------------------------
// RenderTemplate converts Markdown to WeChat HTML deterministically. Slot
// images are placed at their planned positions (hero at top, section_opener
// after the matching heading, inline_detail after the Nth paragraph, footer at
// the end), then the whole document is rendered with the structured theme.
//
// Unlike ConvertMarkdown, slot images carry FINAL URLs and are emitted as real
// <img> tags (not upload placeholders) — matching the original "URLs must be
// used verbatim" contract. The theme (排版样式) is resolved from the task when
// task_id is given.
//
// Module wrapping (hero/quote/callout/steps/cta) is not applied by the
// deterministic renderer; slots that specify a module still place their image
// but the module shell is omitted (rendered status notes it).
// ---------------------------------------------------------------------------

func (s *WritingService) RenderTemplate(
	ctx context.Context,
	userID, projectID, markdown string,
	layoutPlan *LayoutPlan,
	theme, taskID string,
) (*RenderTemplateResult, error) {
	if strings.TrimSpace(markdown) == "" {
		return nil, fmt.Errorf("markdown content is required")
	}
	if layoutPlan == nil {
		return nil, fmt.Errorf("layout_plan is required")
	}

	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("project not owned by user")
	}

	// Resolve 排版样式: explicit caller theme wins, else the task's resolved
	// theme (task > project), else the platform default.
	if theme == "" {
		theme = s.resolveEffectiveTheme(ctx, taskID, ch)
	}

	// Deterministically fold the slot images into the markdown at their planned
	// positions, then render the whole document with the structured theme.
	augmented := applySlotsToMarkdown(markdown, layoutPlan)

	nopLog := zerolog.Nop()
	cvt := converter.NewConverterWithThemes(&nopLog, resources.Manager().GetAllRaw(resources.CategoryTheme))
	convResult := cvt.Convert(&converter.ConvertRequest{Markdown: augmented, Theme: theme})
	if !convResult.Success {
		return nil, fmt.Errorf("render template: %s", convResult.Error)
	}

	// Slot images are final — render their placeholders as real <img> tags.
	html := renderImagesAsRealTags(convResult.HTML, convResult.Images)

	slotsRendered := auditRenderedSlots(html, imageBearingSlots(layoutPlan))

	s.logger.Info().
		Str("user_id", userID).
		Str("project_id", projectID).
		Str("theme", theme).
		Str("template_name", layoutPlan.TemplateName).
		Int("slot_count", len(layoutPlan.Slots)).
		Msg("template rendered")

	return &RenderTemplateResult{
		HTML:          html,
		SlotsRendered: slotsRendered,
		Theme:         theme,
	}, nil
}

// ---------------------------------------------------------------------------
// applySlotsToMarkdown folds each planned slot's image into the markdown at a
// deterministic position. Returns markdown with ![alt](url) images inserted:
//   - hero        → very top
//   - section_opener  → immediately after the matching ## heading
//   - inline_detail   → after the Nth paragraph within the section
//   - footer      → very bottom
//
// Images without a URL are skipped (module-only slots). Unknown / out-of-range
// sections fall back to appending at the document end so no slot is silently
// dropped.
// ---------------------------------------------------------------------------

func applySlotsToMarkdown(markdown string, plan *LayoutPlan) string {
	sections := splitMarkdownByH2(markdown)

	openers := map[int][]LayoutPlanSlot{}
	inlines := map[int][]LayoutPlanSlot{}
	var hero string
	for _, slot := range plan.Slots {
		switch slot.SlotID {
		case "hero":
			if hero == "" {
				hero = slotImageMarkdown(slot)
			}
		case "section_opener":
			openers[slot.SectionIndex] = append(openers[slot.SectionIndex], slot)
		case "inline_detail":
			inlines[slot.SectionIndex] = append(inlines[slot.SectionIndex], slot)
		}
	}

	var b strings.Builder
	if hero != "" {
		b.WriteString(hero)
		b.WriteString("\n\n")
	}
	for i, section := range sections {
		b.WriteString(renderSection(section, openers[i], inlines[i]))
		b.WriteString("\n")
	}
	if plan.Footer != nil {
		if img := slotImageMarkdown(*plan.Footer); img != "" {
			b.WriteString("\n")
			b.WriteString(img)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderSection emits one section: its heading line, then section_opener images
// directly under the heading, then the body with inline_detail images inserted
// after the Nth paragraph.
func renderSection(section string, openers, inlines []LayoutPlanSlot) string {
	sortSlotsForRendering(openers)
	head := ""
	body := section
	if strings.HasPrefix(section, "## ") {
		nl := strings.IndexByte(section, '\n')
		if nl == -1 {
			head = section
			body = ""
		} else {
			head = section[:nl+1]
			body = section[nl+1:]
		}
	}
	var b strings.Builder
	b.WriteString(head)
	for _, op := range openers {
		if img := slotImageMarkdown(op); img != "" {
			b.WriteString(img)
			b.WriteString("\n\n")
		}
	}
	b.WriteString(insertInlines(body, inlines))
	return b.String()
}

// insertInlines splits a section body into paragraphs (blank-line separated)
// and inserts each inline_detail image after its target paragraph index,
// clamped to the last paragraph when out of range.
func insertInlines(body string, inlines []LayoutPlanSlot) string {
	if len(inlines) == 0 || strings.TrimSpace(body) == "" {
		return body
	}
	sorted := append([]LayoutPlanSlot(nil), inlines...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].AfterParagraphIndex < sorted[j].AfterParagraphIndex })

	paras := splitParagraphs(body)
	byPara := map[int][]string{}
	for _, in := range sorted {
		img := slotImageMarkdown(in)
		if img == "" {
			continue
		}
		idx := in.AfterParagraphIndex
		if idx < 0 {
			idx = 0
		}
		if idx >= len(paras) {
			idx = len(paras) - 1
		}
		byPara[idx] = append(byPara[idx], img)
	}
	var b strings.Builder
	for i, p := range paras {
		b.WriteString(p)
		if imgs, ok := byPara[i]; ok {
			for _, img := range imgs {
				b.WriteString("\n\n")
				b.WriteString(img)
			}
		}
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// splitParagraphs splits text into non-empty paragraphs separated by blank lines.
func splitParagraphs(body string) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	var paras []string
	for _, p := range strings.Split(body, "\n\n") {
		if t := strings.TrimSpace(p); t != "" {
			paras = append(paras, t)
		}
	}
	if len(paras) == 0 {
		paras = append(paras, body)
	}
	return paras
}

// slotImageMarkdown renders a slot's image as markdown, or "" if it has no URL.
func slotImageMarkdown(slot LayoutPlanSlot) string {
	if slot.ImageURL == "" {
		return ""
	}
	alt := slot.SlotID
	if slot.SectionTitle != "" {
		alt = slot.SectionTitle
	}
	return fmt.Sprintf("![%s](%s)", alt, slot.ImageURL)
}

// imageBearingSlots returns the slots (including footer) that carry an image URL.
func imageBearingSlots(plan *LayoutPlan) []LayoutPlanSlot {
	var out []LayoutPlanSlot
	for _, slot := range plan.Slots {
		if slot.ImageURL != "" {
			out = append(out, slot)
		}
	}
	if plan.Footer != nil && plan.Footer.ImageURL != "" {
		out = append(out, *plan.Footer)
	}
	return out
}

// renderImagesAsRealTags replaces <!-- IMG:N --> placeholders with real <img>
// tags using each image's Original URL. RenderTemplate slots carry final URLs,
// so — unlike ConvertMarkdown — no upload pipeline intervenes.
func renderImagesAsRealTags(htmlContent string, images []converter.ImageRef) string {
	out := htmlContent
	for _, img := range images {
		if img.Placeholder == "" {
			continue
		}
		tag := fmt.Sprintf(`<img src="%s" style="max-width:100%%;height:auto;display:block;margin:20px auto;" alt="" />`, img.Original)
		out = strings.ReplaceAll(out, img.Placeholder, tag)
	}
	return out
}

// sortSlotsForRendering orders slots so section_opener precedes inline_detail.
func sortSlotsForRendering(slots []LayoutPlanSlot) {
	for i := 0; i < len(slots); i++ {
		for j := i + 1; j < len(slots); j++ {
			if slotRank(slots[j]) < slotRank(slots[i]) {
				slots[i], slots[j] = slots[j], slots[i]
			}
		}
	}
}

func slotRank(slot LayoutPlanSlot) int {
	switch slot.SlotID {
	case "hero":
		return 0
	case "section_opener":
		return 1
	case "inline_detail":
		return 2
	case "footer":
		return 3
	}
	return 9
}

// splitMarkdownByH2 splits markdown into sections at each `## ` heading. The
// first section (index 0) is the preamble before any `## `.
func splitMarkdownByH2(markdown string) []string {
	lines := strings.Split(markdown, "\n")
	var sections []string
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			sections = append(sections, current.String())
			current.Reset()
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	flush()

	if len(sections) == 0 {
		sections = append(sections, markdown)
	}
	return sections
}

// ---------------------------------------------------------------------------
// auditRenderedSlots checks whether each image-bearing slot's URL actually
// appears in the rendered HTML. Soft audit: missing slots are reported but
// don't fail the whole render. WeChat CDN URLs are UUID-unique, so substring
// collision is vanishingly unlikely for non-adversarial input.
// ---------------------------------------------------------------------------

func auditRenderedSlots(html string, slots []LayoutPlanSlot) []RenderedSlotAudit {
	out := make([]RenderedSlotAudit, 0, len(slots))
	for _, slot := range slots {
		audit := RenderedSlotAudit{
			SlotID:       slot.SlotID,
			SectionIndex: slot.SectionIndex,
			ImageURL:     slot.ImageURL,
		}
		if slot.Module != nil {
			audit.Module = *slot.Module
		}
		if slot.ImageURL != "" && !strings.Contains(html, slot.ImageURL) {
			audit.Status = "missing-image"
			audit.Note = "image URL not found in rendered HTML"
		} else {
			audit.Status = "rendered"
		}
		out = append(out, audit)
	}
	return out
}
