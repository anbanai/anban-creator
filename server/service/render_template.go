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

// RenderedSlotAudit records what the LLM actually rendered for each planned slot.
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
// RenderTemplate converts Markdown to WeChat HTML using a structured layout
// plan. Unlike ConvertMarkdown (which lets the LLM freely decide image
// placement), RenderTemplate annotates the markdown with explicit slot
// markers so the LLM must place each image where the plan dictates and wrap
// each section in the specified layout module.
//
// The LLM is still responsible for prose styling (theme colors, typography,
// card layout), but structural decisions (image position, module boundaries)
// are deterministic.
// ---------------------------------------------------------------------------

func (s *WritingService) RenderTemplate(
	ctx context.Context,
	userID, channelID, markdown string,
	layoutPlan *LayoutPlan,
	theme string,
) (*RenderTemplateResult, error) {
	if markdown == "" {
		return nil, fmt.Errorf("markdown content is required")
	}
	if layoutPlan == nil {
		return nil, fmt.Errorf("layout_plan is required")
	}

	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("channel not owned by user")
	}

	if theme == "" {
		theme = ch.Theme
	}
	if theme == "" {
		theme = "autumn-warm"
	}

	annotatedMarkdown, slotsWithImages := annotateMarkdownWithSlots(markdown, layoutPlan)
	prompt := s.buildRenderTemplatePrompt(theme, layoutPlan, annotatedMarkdown)

	renderCtx := ctx
	if s.convertTimeout > 0 {
		var cancel context.CancelFunc
		renderCtx, cancel = context.WithTimeout(ctx, s.convertTimeout)
		defer cancel()
	}

	html, err := s.getLLMClient(renderCtx, userID).Complete(renderCtx, "", prompt)
	if err != nil {
		return nil, fmt.Errorf("llm render template: %w", err)
	}

	slotsRendered := auditRenderedSlots(html, slotsWithImages)

	s.logger.Info().
		Str("user_id", userID).
		Str("channel_id", channelID).
		Str("theme", theme).
		Str("template_name", layoutPlan.TemplateName).
		Int("slot_count", len(layoutPlan.Slots)).
		Int("slots_with_image", len(slotsWithImages)).
		Msg("template rendered")

	return &RenderTemplateResult{
		HTML:          html,
		SlotsRendered: slotsRendered,
		Theme:         theme,
	}, nil
}

// ---------------------------------------------------------------------------
// annotateMarkdownWithSlots rewrites the markdown so each planned slot is
// marked with an explicit, LLM-readable contract block. Returns the annotated
// markdown plus the subset of slots that carry an image URL (used for audit).
// ---------------------------------------------------------------------------

func annotateMarkdownWithSlots(markdown string, plan *LayoutPlan) (string, []LayoutPlanSlot) {
	sections := splitMarkdownByH2(markdown)
	var slotsWithImages []LayoutPlanSlot

	// Build a lookup from section_index → slot list (a section may have both
	// a section_opener and an inline_detail slot).
	slotsBySection := map[int][]LayoutPlanSlot{}
	for _, slot := range plan.Slots {
		if slot.SlotID == "footer" {
			continue
		}
		slotsBySection[slot.SectionIndex] = append(slotsBySection[slot.SectionIndex], slot)
		if slot.ImageURL != "" {
			slotsWithImages = append(slotsWithImages, slot)
		}
	}

	var out strings.Builder
	out.WriteString("<!-- RENDER_TEMPLATE_CONTRACT: ")
	out.WriteString("Each [SLOT ...] block below specifies an exact rendering contract. ")
	out.WriteString("Place the image at the URL exactly where the marker appears. ")
	out.WriteString("Wrap the section in the named layout module when one is specified. ")
	out.WriteString("Do not invent images. Do not move slots. -->\n\n")

	for i, section := range sections {
		// Section 0 is the preamble (text before the first ##). The hero slot
		// typically attaches here.
		slots := slotsBySection[i]
		// Sort slots so section_opener comes before inline_detail.
		sortSlotsForRendering(slots)

		// Hero + section_opener markers go BEFORE the section body so the LLM
		// anchors them to the section title (contract rule: section_opener
		// renders "该章节标题正下方的一张全幅图").
		for _, slot := range slots {
			switch slot.SlotID {
			case "hero":
				// Hero only renders once, at the very top of the article. Skip
				// for non-zero sections to avoid duplication.
				if i == 0 {
					writeSlotMarker(&out, slot)
				}
			case "section_opener":
				writeSlotMarker(&out, slot)
			}
		}

		out.WriteString(section)
		out.WriteString("\n\n")

		// inline_detail markers go AFTER the section body — the LLM reads the
		// paragraph index and inserts the image at the right offset within
		// the section.
		for _, slot := range slots {
			if slot.SlotID == "inline_detail" {
				fmt.Fprintf(&out, "<!-- INLINE_SLOT at paragraph %d: ", slot.AfterParagraphIndex)
				fmt.Fprintf(&out, "image_url=%s size=%s -->\n", slot.ImageURL, slot.ImageSize)
			}
		}
	}

	if plan.Footer != nil {
		out.WriteString("\n<!-- FOOTER SLOT: ")
		if plan.Footer.Module != nil {
			fmt.Fprintf(&out, "module=%s ", *plan.Footer.Module)
		}
		if plan.Footer.ImageURL != "" {
			fmt.Fprintf(&out, "image_url=%s ", plan.Footer.ImageURL)
		}
		out.WriteString("-->\n")
	}

	return out.String(), slotsWithImages
}

// writeSlotMarker emits a single LLM-readable slot contract block.
func writeSlotMarker(out *strings.Builder, slot LayoutPlanSlot) {
	out.WriteString("[SLOT: ")
	out.WriteString(slot.SlotID)
	if slot.SectionTitle != "" {
		out.WriteString(" | section: ")
		out.WriteString(slot.SectionTitle)
	}
	if slot.ImageURL != "" {
		fmt.Fprintf(out, " | image: %s", slot.ImageURL)
	}
	if slot.ImageSize != "" {
		fmt.Fprintf(out, " | size: %s", slot.ImageSize)
	}
	if slot.Module != nil && *slot.Module != "" {
		fmt.Fprintf(out, " | module: %s", *slot.Module)
		if len(slot.ModuleVars) > 0 {
			// Sort keys so the prompt is deterministic for the same plan.
			out.WriteString(" | vars: ")
			keys := make([]string, 0, len(slot.ModuleVars))
			for k := range slot.ModuleVars {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for i, k := range keys {
				if i > 0 {
					out.WriteString(", ")
				}
				fmt.Fprintf(out, "%s=%q", k, slot.ModuleVars[k])
			}
		}
	}
	out.WriteString("]\n\n")
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
// buildRenderTemplatePrompt assembles the LLM prompt: theme YAML prompt +
// slot contract instructions + annotated markdown.
// ---------------------------------------------------------------------------

func (s *WritingService) buildRenderTemplatePrompt(theme string, plan *LayoutPlan, annotatedMarkdown string) string {
	nopLog := zerolog.Nop()
	cvt := converter.NewConverterWithThemes(&nopLog, resources.Manager().GetAllRaw(resources.CategoryTheme))

	convReq := &converter.ConvertRequest{
		Markdown: annotatedMarkdown,
		Theme:    theme,
	}
	convResult := cvt.Convert(convReq)

	themePrompt, _, ok := converter.GetAIRequestInfo(convResult)
	if !ok {
		// Non-AI path returned (unlikely). Fall back to wrapping the HTML.
		themePrompt = convResult.HTML
	}

	contract := renderContractInstructions(plan)

	var b strings.Builder
	b.WriteString(themePrompt)
	b.WriteString("\n\n")
	b.WriteString(contract)
	b.WriteString("\n\n请按上述契约渲染以下 Markdown（已含 [SLOT ...] 标记）：\n\n")
	b.WriteString(annotatedMarkdown)
	return b.String()
}

// renderContractInstructions produces the explicit rules the LLM must follow.
func renderContractInstructions(plan *LayoutPlan) string {
	var b strings.Builder
	b.WriteString("【结构化渲染契约 — 必须严格遵守】\n\n")
	b.WriteString("本文使用 render_template 模式渲染，每个图片位置和 layout module 由 [SLOT: ...] 标记指定，不得自由决定。\n\n")
	b.WriteString("规则：\n")
	b.WriteString("1. 每个 [SLOT: hero | image: URL | size: full-bleed | module: hero | vars: ...] 必须渲染为一张全幅图（位于文章最顶部），并按 module 模板填充 vars。\n")
	b.WriteString("2. 每个 [SLOT: section_opener | image: URL | size: full-width | module: <name>] 必须渲染为该章节标题正下方的一张全幅图（16:9），module 字段非空时按指定 module（hero/quote/callout/steps 等）包裹标题与首段。\n")
	b.WriteString("3. 每个 <!-- INLINE_SLOT at paragraph N: image_url=URL size=inline --> 必须在该章节第 N 段（0-based）之后渲染一张内嵌图。\n")
	b.WriteString("4. <!-- FOOTER SLOT: module=cta --> 必须在文章最末渲染对应 module（CTA / checklist / summary 等）。\n")
	b.WriteString("5. 图片 URL 必须原样使用，不得替换为占位符或省略。所有 <img> 标签使用 style=\"max-width:100%;height:auto;display:block;margin:20px auto;\"。\n")
	b.WriteString("6. 不得自创新图片，不得移动 slot 位置，不得省略任何 slot。\n")
	b.WriteString("7. 除 slot 指定的图外，正文内已有的 ![alt](url) 图片也按原位置渲染。\n\n")

	b.WriteString("本次 slot 计划：\n")
	for _, slot := range plan.Slots {
		fmt.Fprintf(&b, "- slot_id=%s section_index=%d", slot.SlotID, slot.SectionIndex)
		if slot.ImageURL != "" {
			b.WriteString(" image=yes")
		}
		if slot.Module != nil {
			fmt.Fprintf(&b, " module=%s", *slot.Module)
		}
		b.WriteString("\n")
	}
	if plan.Footer != nil {
		fmt.Fprintf(&b, "- footer slot present (module=%v)\n", plan.Footer.Module)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// auditRenderedSlots checks whether each image-bearing slot actually appears
// in the rendered HTML. This is a soft audit — missing slots are reported but
// don't fail the whole render.
//
// Implementation note: uses strings.Contains for URL matching. WeChat CDN URLs
// are UUID-based and unique per asset, so cross-URL substring collision is
// vanishingly unlikely in practice. Don't use this for adversarial input.
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
