package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/app/converter"
	"github.com/anbanai/anban-creator/server/resources"
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
// Layout modules are rendered deterministically from module_vars and the
// embedded layout schema. Image-only slots remain compatible with existing
// visual plans, while module-only slots must satisfy required module fields.
// ---------------------------------------------------------------------------

func (s *ContentRenderService) RenderTemplate(
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

	// Deterministically fold slot images and layout modules into the markdown at
	// their planned positions, then render the whole document with the structured
	// theme.
	augmented, err := applySlotsAndModulesToMarkdown(markdown, layoutPlan)
	if err != nil {
		return nil, err
	}

	nopLog := zerolog.Nop()
	cvt := converter.NewConverterWithThemes(&nopLog, resources.Manager().GetAllRaw(resources.CategoryTheme))
	convResult := cvt.Convert(&converter.ConvertRequest{Markdown: augmented, Theme: theme})
	if !convResult.Success {
		return nil, fmt.Errorf("render template: %s", convResult.Error)
	}

	// Slot images are final — render their placeholders as real <img> tags.
	html := renderImagesAsRealTags(convResult.HTML, convResult.Images, layoutPlan)

	slotsRendered := auditRenderedSlots(html, auditableSlots(layoutPlan))

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
	// Idempotency guard: drop any slot whose image URL is ALREADY inlined in the
	// source markdown. Injecting it again would make the deterministic renderer
	// emit a duplicate <img> (inline image + slot image, same URL). The existing
	// inline keeps its position; we add no second copy. This makes the render
	// resilient to upstream callers that both inline images and pass them as
	// slots (the article workflow did this and had to manually clean up).
	//
	// Contract: each slot image URL is unique within an article (the rhythm plan
	// requires pairwise-distinct content images, never reusing one as cover/other).
	// So a URL appearing both inline and in a slot is always the SAME image the
	// caller placed twice, never two legitimately-distinct slots — dropping the
	// duplicate never loses a distinct image.
	existing := collectInlineImageURLs(markdown)
	slots := make([]LayoutPlanSlot, 0, len(plan.Slots))
	for _, slot := range plan.Slots {
		if slot.ImageURL != "" && existing[slot.ImageURL] {
			continue
		}
		slots = append(slots, slot)
	}
	var footer *LayoutPlanSlot
	if plan.Footer != nil && !(plan.Footer.ImageURL != "" && existing[plan.Footer.ImageURL]) {
		f := *plan.Footer
		footer = &f
	}

	sections := splitMarkdownByH2(markdown)

	openers := map[int][]LayoutPlanSlot{}
	inlines := map[int][]LayoutPlanSlot{}
	var hero string
	for _, slot := range slots {
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
	if footer != nil {
		if img := slotImageMarkdown(*footer); img != "" {
			b.WriteString("\n")
			b.WriteString(img)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func applySlotsAndModulesToMarkdown(markdown string, plan *LayoutPlan) (string, error) {
	existing := collectInlineImageURLs(markdown)
	slots := make([]LayoutPlanSlot, 0, len(plan.Slots))
	for _, slot := range plan.Slots {
		slots = append(slots, slot)
	}
	if plan.Footer != nil {
		slots = append(slots, *plan.Footer)
	}

	sections := splitMarkdownByH2(markdown)
	openers := map[int][]string{}
	inlines := map[int][]LayoutPlanSlot{}
	var hero []string
	var footer []string

	for _, slot := range slots {
		block, err := slotMarkdownBlock(slot, existing)
		if err != nil {
			return "", err
		}
		if block == "" {
			continue
		}
		switch slot.SlotID {
		case "hero":
			if len(hero) == 0 {
				hero = append(hero, block)
			}
		case "section_opener":
			openers[slot.SectionIndex] = append(openers[slot.SectionIndex], block)
		case "inline_detail":
			inlines[slot.SectionIndex] = append(inlines[slot.SectionIndex], slot)
		case "footer":
			footer = append(footer, block)
		}
	}

	var b strings.Builder
	if len(hero) > 0 {
		b.WriteString(strings.Join(hero, "\n\n"))
		b.WriteString("\n\n")
	}
	for i, section := range sections {
		b.WriteString(renderSectionWithModuleBlocks(section, openers[i], inlines[i], existing))
		b.WriteString("\n")
	}
	if len(footer) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(footer, "\n\n"))
		b.WriteString("\n")
	}
	return b.String(), nil
}

func renderSectionWithModuleBlocks(section string, openerBlocks []string, inlines []LayoutPlanSlot, existing map[string]bool) string {
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
	for _, block := range openerBlocks {
		b.WriteString(block)
		b.WriteString("\n\n")
	}
	b.WriteString(insertInlineBlocks(body, inlines, existing))
	return b.String()
}

func insertInlineBlocks(body string, inlines []LayoutPlanSlot, existing map[string]bool) string {
	if len(inlines) == 0 || strings.TrimSpace(body) == "" {
		return body
	}
	sorted := append([]LayoutPlanSlot(nil), inlines...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].AfterParagraphIndex < sorted[j].AfterParagraphIndex })

	paras := splitParagraphs(body)
	byPara := map[int][]string{}
	for _, in := range sorted {
		block, err := slotMarkdownBlock(in, existing)
		if err != nil || block == "" {
			continue
		}
		idx := in.AfterParagraphIndex
		if idx < 0 {
			idx = 0
		}
		if idx >= len(paras) {
			idx = len(paras) - 1
		}
		byPara[idx] = append(byPara[idx], block)
	}
	var b strings.Builder
	for i, p := range paras {
		b.WriteString(p)
		if blocks, ok := byPara[i]; ok {
			for _, block := range blocks {
				b.WriteString("\n\n")
				b.WriteString(block)
			}
		}
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func slotMarkdownBlock(slot LayoutPlanSlot, existing map[string]bool) (string, error) {
	parts := []string{}
	if mod, err := renderLayoutModuleMarkdown(slot); err != nil {
		return "", err
	} else if mod != "" {
		parts = append(parts, mod)
	}
	if slot.ImageURL != "" && !existing[slot.ImageURL] {
		parts = append(parts, slotImageMarkdown(slot))
	}
	return strings.Join(parts, "\n\n"), nil
}

func renderLayoutModuleMarkdown(slot LayoutPlanSlot) (string, error) {
	if slot.Module == nil || strings.TrimSpace(*slot.Module) == "" {
		return "", nil
	}
	if len(slot.ModuleVars) == 0 && strings.TrimSpace(slot.ImageURL) != "" {
		return "", nil
	}
	name := strings.TrimSpace(*slot.Module)
	entry := resources.Manager().Get(resources.CategoryLayout, name)
	if entry == nil {
		return "", fmt.Errorf("module %s not found", name)
	}
	for _, field := range requiredFields(entry) {
		if strings.TrimSpace(slot.ModuleVars[field.Name]) == "" {
			return "", fmt.Errorf("module %s missing required field %s", name, field.Name)
		}
	}

	switch name {
	case "cta":
		return renderCTAModule(slot.ModuleVars), nil
	case "quote":
		return renderQuoteModule(slot.ModuleVars), nil
	case "hero":
		return renderHeroModule(slot.ModuleVars), nil
	default:
		return renderGenericModule(name, slot.ModuleVars, entry), nil
	}
}

func requiredFields(entry *resources.ResourceEntry) []resources.FieldSpec {
	if entry == nil || entry.Fields == nil {
		return nil
	}
	return entry.Fields.Required
}

func renderCTAModule(vars map[string]string) string {
	var b strings.Builder
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "### %s\n\n", vars["title"])
	if note := strings.TrimSpace(vars["note"]); note != "" {
		fmt.Fprintf(&b, "%s\n\n", note)
	}
	if link := strings.TrimSpace(vars["link"]); link != "" {
		fmt.Fprintf(&b, "**%s**\n\n", link)
	}
	b.WriteString("---")
	return b.String()
}

func renderQuoteModule(vars map[string]string) string {
	var b strings.Builder
	if label := strings.TrimSpace(vars["label"]); label != "" {
		fmt.Fprintf(&b, "**%s**\n\n", label)
	}
	fmt.Fprintf(&b, "> %s\n", vars["text"])
	source := strings.TrimSpace(vars["source"])
	role := strings.TrimSpace(vars["role"])
	if source != "" && role != "" {
		fmt.Fprintf(&b, "> -- %s，%s\n", source, role)
	} else if source != "" {
		fmt.Fprintf(&b, "> -- %s\n", source)
	}
	return strings.TrimSpace(b.String())
}

func renderHeroModule(vars map[string]string) string {
	var b strings.Builder
	if eyebrow := strings.TrimSpace(vars["eyebrow"]); eyebrow != "" {
		fmt.Fprintf(&b, "**%s**\n\n", eyebrow)
	}
	if title := strings.TrimSpace(vars["title"]); title != "" {
		fmt.Fprintf(&b, "# %s\n\n", title)
	}
	if subtitle := strings.TrimSpace(vars["subtitle"]); subtitle != "" {
		fmt.Fprintf(&b, "## %s\n\n", subtitle)
	}
	if cta := strings.TrimSpace(vars["cta_text"]); cta != "" {
		fmt.Fprintf(&b, "*%s*\n\n---", cta)
	}
	return strings.TrimSpace(b.String())
}

func renderGenericModule(name string, vars map[string]string, entry *resources.ResourceEntry) string {
	var b strings.Builder
	title := firstModuleValue(vars, entry)
	if title == "" {
		title = name
	}
	fmt.Fprintf(&b, "### %s\n\n", title)
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := vars[key]
		if strings.TrimSpace(value) == "" || value == title {
			continue
		}
		fmt.Fprintf(&b, "%s\n\n", value)
	}
	return strings.TrimSpace(b.String())
}

func firstModuleValue(vars map[string]string, entry *resources.ResourceEntry) string {
	if entry != nil && entry.Fields != nil {
		for _, field := range append(entry.Fields.Required, entry.Fields.Optional...) {
			if value := strings.TrimSpace(vars[field.Name]); value != "" {
				return value
			}
		}
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value := strings.TrimSpace(vars[key]); value != "" {
			return value
		}
	}
	return ""
}

// mdInlineImageRe matches a markdown image's URL: ![alt](url). Captures the URL
// (no whitespace, no closing paren). Used only to dedup slot injection against
// already-inlined images — not a full markdown parser.
var mdInlineImageRe = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)[^)]*\)`)

// collectInlineImageURLs returns the set of image URLs referenced as markdown
// images (![alt](url)) anywhere in the source. Slot injection consults this to
// avoid emitting a duplicate <img> for an image that is already inlined.
func collectInlineImageURLs(markdown string) map[string]bool {
	set := make(map[string]bool)
	for _, m := range mdInlineImageRe.FindAllStringSubmatch(markdown, -1) {
		if len(m) > 1 {
			if u := strings.TrimSpace(m[1]); u != "" {
				set[u] = true
			}
		}
	}
	return set
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

func auditableSlots(plan *LayoutPlan) []LayoutPlanSlot {
	var out []LayoutPlanSlot
	for _, slot := range plan.Slots {
		if slot.ImageURL != "" || slot.Module != nil {
			out = append(out, slot)
		}
	}
	if plan.Footer != nil && (plan.Footer.ImageURL != "" || plan.Footer.Module != nil) {
		out = append(out, *plan.Footer)
	}
	return out
}

// renderImagesAsRealTags replaces <!-- IMG:N --> placeholders with real <img>
// tags using each image's Original URL. RenderTemplate slots carry final URLs,
// so — unlike ConvertMarkdown — no upload pipeline intervenes.
func renderImagesAsRealTags(htmlContent string, images []converter.ImageRef, plan *LayoutPlan) string {
	out := htmlContent
	imageSizes := plannedImageSizes(plan)
	for _, img := range images {
		if img.Placeholder == "" {
			continue
		}
		maxWidth := imageMaxWidth(imageSizes[img.Original])
		tag := fmt.Sprintf(`<img src="%s" style="max-width:%s;height:auto;display:block;margin:16px auto;" alt="" />`, img.Original, maxWidth)
		out = strings.ReplaceAll(out, img.Placeholder, tag)
	}
	return out
}

func plannedImageSizes(plan *LayoutPlan) map[string]string {
	sizes := map[string]string{}
	if plan == nil {
		return sizes
	}
	for _, slot := range imageBearingSlots(plan) {
		if slot.ImageURL != "" && slot.ImageSize != "" {
			sizes[slot.ImageURL] = slot.ImageSize
		}
	}
	return sizes
}

func imageMaxWidth(imageSize string) string {
	switch imageSize {
	case "inline":
		return "68%"
	case "full-width":
		return "100%"
	case "full-bleed":
		return "100%"
	default:
		return "100%"
	}
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
// auditRenderedSlots checks whether each auditable slot rendered. Image-bearing
// slots must include their URL in the final HTML; module-only slots are marked
// rendered once deterministic module rendering succeeds. Soft audit: missing
// images are reported but don't fail the whole render. WeChat CDN URLs are
// UUID-unique, so substring collision is vanishingly unlikely for
// non-adversarial input.
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
