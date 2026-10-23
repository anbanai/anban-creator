package service

import (
	"context"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/resources"
)

// ---------------------------------------------------------------------------
// ParseLayoutPlan unit tests
// ---------------------------------------------------------------------------

func TestParseLayoutPlan_Nil(t *testing.T) {
	_, err := ParseLayoutPlan(nil)
	if err == nil {
		t.Fatal("[FAIL] Expected error for nil layout_plan")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("[FAIL] Error should mention required, got: %v", err)
	}
}

func TestParseLayoutPlan_EmptySlots(t *testing.T) {
	_, err := ParseLayoutPlan(map[string]any{
		"article_type": "long-form-essay",
		"slots":        []any{},
	})
	if err == nil {
		t.Fatal("[FAIL] Expected error for empty slots")
	}
	if !strings.Contains(err.Error(), "at least one slot") {
		t.Errorf("[FAIL] Error should mention 'at least one slot', got: %v", err)
	}
}

func TestParseLayoutPlan_MissingSlotID(t *testing.T) {
	_, err := ParseLayoutPlan(map[string]any{
		"article_type": "long-form-essay",
		"slots": []any{
			map[string]any{
				"section_index": 0,
				// slot_id missing
			},
		},
	})
	if err == nil {
		t.Fatal("[FAIL] Expected error for missing slot_id")
	}
	if !strings.Contains(err.Error(), "slot_id") {
		t.Errorf("[FAIL] Error should mention slot_id, got: %v", err)
	}
}

func TestParseLayoutPlan_InvalidSlotID(t *testing.T) {
	// Unknown slot_id (e.g. a typo) should be rejected so it doesn't silently
	// get dropped during annotation.
	_, err := ParseLayoutPlan(map[string]any{
		"article_type": "long-form-essay",
		"slots": []any{
			map[string]any{
				"slot_id":       "section_openr", // typo
				"section_index": 1,
			},
		},
	})
	if err == nil {
		t.Fatal("[FAIL] Expected error for unknown slot_id")
	}
	if !strings.Contains(err.Error(), "section_openr") {
		t.Errorf("[FAIL] Error should echo the bad slot_id, got: %v", err)
	}
	if !strings.Contains(err.Error(), "hero/section_opener") {
		t.Errorf("[FAIL] Error should list valid slot_ids, got: %v", err)
	}
}

func TestParseLayoutPlan_Valid(t *testing.T) {
	plan, err := ParseLayoutPlan(map[string]any{
		"article_type":  "listicle",
		"template_name": "listicle",
		"slots": []any{
			map[string]any{
				"slot_id":       "hero",
				"section_index": 0,
				"image_url":     "https://cdn.example.com/hero.png",
				"image_size":    "full-bleed",
				"module":        "hero",
			},
			map[string]any{
				"slot_id":       "section_opener",
				"section_index": 1,
				"image_url":     "https://cdn.example.com/01.png",
				"image_size":    "full-width",
			},
		},
		"footer": map[string]any{
			"module": "cta",
		},
	})
	if err != nil {
		t.Fatalf("[FAIL] Unexpected error: %v", err)
	}
	if plan.ArticleType != "listicle" {
		t.Errorf("[FAIL] ArticleType = %q, want listicle", plan.ArticleType)
	}
	if len(plan.Slots) != 2 {
		t.Fatalf("[FAIL] Slots len = %d, want 2", len(plan.Slots))
	}
	if plan.Slots[0].SlotID != "hero" {
		t.Errorf("[FAIL] Slots[0].SlotID = %q, want hero", plan.Slots[0].SlotID)
	}
	if plan.Slots[0].ImageURL != "https://cdn.example.com/hero.png" {
		t.Errorf("[FAIL] Slots[0].ImageURL wrong: %q", plan.Slots[0].ImageURL)
	}
	if plan.Footer == nil || plan.Footer.Module == nil || *plan.Footer.Module != "cta" {
		t.Errorf("[FAIL] Footer module wrong: %v", plan.Footer)
	}
	if plan.Footer.SlotID != "footer" || plan.Footer.SectionIndex != -1 {
		t.Errorf("[FAIL] Footer position = %q/%d, want footer/-1", plan.Footer.SlotID, plan.Footer.SectionIndex)
	}
}

func TestParseLayoutPlanRejectsMisclassifiedFooter(t *testing.T) {
	_, err := ParseLayoutPlan(map[string]any{
		"article_type": "listicle",
		"slots": []any{
			map[string]any{"slot_id": "hero", "section_index": 0},
		},
		"footer": map[string]any{"slot_id": "hero", "module": "cta"},
	})
	if err == nil || !strings.Contains(err.Error(), "must be footer") {
		t.Fatalf("ParseLayoutPlan error = %v, want misclassified footer rejection", err)
	}
}

func TestRenderTemplateRendersFooterShorthandModule(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-footer-shorthand-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	plan, err := ParseLayoutPlan(map[string]any{
		"article_type": "listicle",
		"slots": []any{
			map[string]any{"slot_id": "hero", "section_index": 0},
		},
		"footer": map[string]any{
			"module":      "cta",
			"module_vars": map[string]string{"title": "下一步"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.RenderTemplate(t.Context(), userID, projectID, "# 标题\n\n## 正文\n\n内容。", plan, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.HTML, "下一步") {
		t.Fatalf("footer module was not rendered:\n%s", result.HTML)
	}
	if len(result.SlotsRendered) != 1 || result.SlotsRendered[0].SlotID != "footer" || result.SlotsRendered[0].Status != "rendered" {
		t.Fatalf("footer audit = %#v", result.SlotsRendered)
	}
}

// ---------------------------------------------------------------------------
// applySlotsToMarkdown unit tests (pure function, no LLM/DB)
// ---------------------------------------------------------------------------

func TestApplySlotsToMarkdown_HeroAndSectionOpeners(t *testing.T) {
	markdown := `# Title

Intro paragraph.

## Section 1

Content of section 1.

## Section 2

Content of section 2.
`
	plan := &LayoutPlan{
		ArticleType:  "long-form-essay",
		TemplateName: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/hero.png", ImageSize: "full-bleed", Module: strPtr("hero")},
			{SlotID: "section_opener", SectionIndex: 1, ImageURL: "https://cdn/s1.png", ImageSize: "full-width"},
			{SlotID: "section_opener", SectionIndex: 2, ImageURL: "https://cdn/s2.png", ImageSize: "full-width"},
		},
	}

	augmented := applySlotsToMarkdown(markdown, plan)
	// Hero image must be at the very top.
	if !strings.HasPrefix(augmented, "![hero](https://cdn/hero.png)") {
		t.Errorf("[FAIL] hero image not at top: %q", augmented[:min(len(augmented), 40)])
	}
	for _, url := range []string{"https://cdn/hero.png", "https://cdn/s1.png", "https://cdn/s2.png"} {
		if !strings.Contains(augmented, url) {
			t.Errorf("[FAIL] Missing image URL %s", url)
		}
	}
}

func TestApplySlotsToMarkdown_Footer(t *testing.T) {
	markdown := "## Section\n\nBody."
	plan := &LayoutPlan{
		ArticleType:  "tutorial",
		TemplateName: "tutorial",
		Slots: []LayoutPlanSlot{
			{SlotID: "section_opener", SectionIndex: 0, ImageURL: "https://cdn/a.png"},
		},
		Footer: &LayoutPlanSlot{SlotID: "footer", ImageURL: "https://cdn/footer.png"},
	}
	augmented := applySlotsToMarkdown(markdown, plan)
	// Footer image lands at the end of the document.
	if !strings.HasSuffix(strings.TrimSpace(augmented), "![footer](https://cdn/footer.png)") {
		t.Errorf("[FAIL] footer image not at end: %q", augmented)
	}
}

// Regression: section_opener images must appear BEFORE the section body so they
// anchor to the heading (earlier impls emitted them at the section tail).
func TestApplySlotsToMarkdown_SectionOpenerBeforeContent(t *testing.T) {
	markdown := "## First Section\n\nFirst body.\n\n## Second Section\n\nSecond body.\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "section_opener", SectionIndex: 0, ImageURL: "https://cdn/before-first.png"},
			{SlotID: "section_opener", SectionIndex: 1, ImageURL: "https://cdn/before-second.png"},
		},
	}

	augmented := applySlotsToMarkdown(markdown, plan)

	markerIdx := strings.Index(augmented, "https://cdn/before-second.png")
	bodyIdx := strings.Index(augmented, "Second body.")
	if markerIdx < 0 {
		t.Fatal("[FAIL] section 2 image URL missing")
	}
	if bodyIdx < 0 {
		t.Fatal("[FAIL] section 2 body missing")
	}
	if markerIdx > bodyIdx {
		t.Errorf("[FAIL] section_opener image (idx=%d) appears AFTER section body (idx=%d) — should be before",
			markerIdx, bodyIdx)
	}
}

// inline_detail images land after the target paragraph within the section.
func TestApplySlotsToMarkdown_InlineDetailAfterParagraph(t *testing.T) {
	markdown := "## Section\n\nParagraph one.\n\nParagraph two.\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "inline_detail", SectionIndex: 0, AfterParagraphIndex: 0, ImageURL: "https://cdn/inline.png"},
		},
	}
	augmented := applySlotsToMarkdown(markdown, plan)
	firstParaIdx := strings.Index(augmented, "Paragraph one.")
	secondParaIdx := strings.Index(augmented, "Paragraph two.")
	inlineIdx := strings.Index(augmented, "https://cdn/inline.png")
	if inlineIdx < 0 {
		t.Fatal("[FAIL] inline image missing")
	}
	if secondParaIdx < 0 {
		t.Fatal("[FAIL] second paragraph missing")
	}
	// Inline image (after paragraph 0) must come after "Paragraph one." and
	// before "Paragraph two.".
	if inlineIdx < firstParaIdx || inlineIdx > secondParaIdx {
		t.Errorf("[FAIL] inline image (idx=%d) not between para1 (idx=%d) and para2 (idx=%d)",
			inlineIdx, firstParaIdx, secondParaIdx)
	}
}

// Regression: a slot whose image URL is ALREADY inlined in the source markdown
// must be skipped, otherwise the renderer emits a duplicate <img> (the
// article workflow hit this and had to manually clean up repeated img).
func TestApplySlotsToMarkdown_DedupAlreadyInlinedURL(t *testing.T) {
	markdown := `# Title

![hero](https://cdn/hero.png)

## Section 1

Content.
`
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/hero.png"},         // already inlined → skip
			{SlotID: "section_opener", SectionIndex: 1, ImageURL: "https://cdn/s1.png"}, // fresh → inject
		},
	}
	augmented := applySlotsToMarkdown(markdown, plan)
	if got := strings.Count(augmented, "https://cdn/hero.png"); got != 1 {
		t.Errorf("[FAIL] inlined hero URL count = %d, want 1 (slot must not duplicate):\n%s", got, augmented)
	}
	if got := strings.Count(augmented, "https://cdn/s1.png"); got != 1 {
		t.Errorf("[FAIL] fresh section_opener URL count = %d, want 1:\n%s", got, augmented)
	}
}

// Footer dedup: a footer slot whose URL is already inlined must not be appended again.
func TestApplySlotsToMarkdown_DedupFooterAlreadyInlined(t *testing.T) {
	markdown := "## Section\n\n![footer](https://cdn/f.png)\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "section_opener", SectionIndex: 0, ImageURL: "https://cdn/s.png"},
		},
		Footer: &LayoutPlanSlot{SlotID: "footer", ImageURL: "https://cdn/f.png"}, // already inlined → skip
	}
	augmented := applySlotsToMarkdown(markdown, plan)
	if got := strings.Count(augmented, "https://cdn/f.png"); got != 1 {
		t.Errorf("[FAIL] inlined footer URL count = %d, want 1 (no duplicate):\n%s", got, augmented)
	}
}

// ---------------------------------------------------------------------------
// Integration: RenderTemplate (deterministic — no LLM)
// ---------------------------------------------------------------------------

func TestRenderTemplate_LongFormEssay(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-render-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")

	markdown := "# 标题\n\n引言。\n\n## 第一节\n\n正文一。\n\n## 第二节\n\n正文二。\n"

	plan := &LayoutPlan{
		ArticleType:  "long-form-essay",
		TemplateName: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/hero.png", ImageSize: "full-bleed", Module: strPtr("hero")},
			{SlotID: "section_opener", SectionIndex: 1, ImageURL: "https://cdn/s1.png", ImageSize: "full-width"},
			{SlotID: "section_opener", SectionIndex: 2, ImageURL: "https://cdn/s2.png", ImageSize: "full-width"},
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, projectID, markdown, plan, "", "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}

	if result.HTML == "" {
		t.Fatal("[FAIL] HTML empty")
	}

	// Deterministic placement renders all 3 slot images as real <img> tags.
	if len(result.SlotsRendered) != 3 {
		t.Errorf("[FAIL] SlotsRendered len = %d, want 3", len(result.SlotsRendered))
	}
	for _, audit := range result.SlotsRendered {
		if audit.Status != "rendered" {
			t.Errorf("[FAIL] Slot %s status = %s, want rendered", audit.SlotID, audit.Status)
		}
	}
	for _, url := range []string{"https://cdn/hero.png", "https://cdn/s1.png", "https://cdn/s2.png"} {
		if !strings.Contains(result.HTML, url) {
			t.Errorf("[FAIL] HTML missing image URL %s", url)
		}
	}

	if result.Theme != "autumn-warm" {
		t.Errorf("[FAIL] Theme = %q, want autumn-warm", result.Theme)
	}
}

func TestRenderTemplate_Listicle(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-listicle-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")

	markdown := "# 三个清单\n\n## 第一项\n\nA.\n\n## 第二项\n\nB.\n\n## 第三项\n\nC.\n"

	plan := &LayoutPlan{
		ArticleType:  "listicle",
		TemplateName: "listicle",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/lh.png"},
			{SlotID: "section_opener", SectionIndex: 1, ImageURL: "https://cdn/l1.png"},
			{SlotID: "section_opener", SectionIndex: 2, ImageURL: "https://cdn/l2.png"},
			{SlotID: "section_opener", SectionIndex: 3, ImageURL: "https://cdn/l3.png"},
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, projectID, markdown, plan, "", "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}

	if len(result.SlotsRendered) != 4 {
		t.Errorf("[FAIL] SlotsRendered len = %d, want 4", len(result.SlotsRendered))
	}
	for _, a := range result.SlotsRendered {
		if a.Status != "rendered" {
			t.Errorf("[FAIL] Slot %s status = %s", a.SlotID, a.Status)
		}
	}
}

// With deterministic rendering, every image-bearing slot is placed; the audit
// reflects that. (Previously this tested an LLM "forgetting" an image — no
// longer applicable without an LLM.)
func TestRenderTemplate_AllSlotsRendered(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-all-rendered-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")

	markdown := "# T\n\n## S1\n\nbody.\n\n## S2\n\nbody.\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/hero.png"},
			{SlotID: "section_opener", SectionIndex: 1, ImageURL: "https://cdn/s1.png"},
			{SlotID: "section_opener", SectionIndex: 2, ImageURL: "https://cdn/s2.png"},
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, projectID, markdown, plan, "", "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}

	if len(result.SlotsRendered) != 3 {
		t.Fatalf("[FAIL] SlotsRendered len = %d, want 3", len(result.SlotsRendered))
	}
	renderedCount := 0
	for _, a := range result.SlotsRendered {
		if a.Status == "rendered" {
			renderedCount++
		}
	}
	if renderedCount != 3 {
		t.Errorf("[FAIL] rendered slot count = %d, want 3", renderedCount)
	}
}

func TestAuditRenderedSlotsMarksMissingImageURL(t *testing.T) {
	slots := []LayoutPlanSlot{
		{SlotID: "hero", SectionIndex: 0, ImageRequired: true},
		{SlotID: "footer", SectionIndex: -1, Module: strPtr("cta")},
	}
	audits := auditRenderedSlots("<p>body</p>", slots)
	if len(audits) != 2 {
		t.Fatalf("audits len = %d, want 2", len(audits))
	}
	if audits[0].Status != "missing-image" {
		t.Fatalf("hero status = %q, want missing-image", audits[0].Status)
	}
	if audits[1].Status != "rendered" {
		t.Fatalf("module-only footer status = %q, want rendered", audits[1].Status)
	}
}

func TestAuditRenderedSlotsRequiresImageSourceAttribute(t *testing.T) {
	url := "https://example.com/image.png"
	audits := auditRenderedSlots(`<a href="`+url+`">`+url+`</a>`, []LayoutPlanSlot{
		{SlotID: "hero", SectionIndex: 0, ImageURL: url, ImageRequired: true},
	})
	if len(audits) != 1 || audits[0].Status != "missing-image" {
		t.Fatalf("audit = %#v, want missing-image when URL is not an img src", audits)
	}
}

// Regression: when the source markdown already inlines an image AND a slot
// carries the same URL, the rendered HTML must contain exactly ONE <img> for
// that URL — the slot is deduped against the inline image.
func TestRenderTemplate_DedupInlineAndSlotSameURL(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-dedup-int-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")

	markdown := "# 标题\n\n![hero](https://cdn/hero.png)\n\n## 第一节\n\n正文。\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			// Same URL as the inline hero above → must NOT render a 2nd <img>.
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/hero.png"},
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, projectID, markdown, plan, "", "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}
	if got := strings.Count(result.HTML, "https://cdn/hero.png"); got != 1 {
		t.Errorf("[FAIL] hero URL appears %d times in HTML, want exactly 1 (inline + slot dedup):\n%s", got, result.HTML)
	}
}

func TestRenderTemplate_UsesSlotImageSizeForInlineStyles(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-image-size-style-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")

	markdown := "# 标题\n\n## 第一节\n\n第一段。\n\n第二段。\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/hero.png", ImageSize: "full-bleed"},
			{SlotID: "section_opener", SectionIndex: 0, ImageURL: "https://cdn/section.png", ImageSize: "full-width"},
			{SlotID: "inline_detail", SectionIndex: 0, AfterParagraphIndex: 0, ImageURL: "https://cdn/inline.png", ImageSize: "inline"},
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, projectID, markdown, plan, "", "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}

	assertImageStyleContains(t, result.HTML, "https://cdn/hero.png", "max-width:100%")
	assertImageStyleContains(t, result.HTML, "https://cdn/section.png", "max-width:100%")
	assertImageStyleContains(t, result.HTML, "https://cdn/inline.png", "max-width:68%")
	assertImageStyleContains(t, result.HTML, "https://cdn/hero.png", "margin:16px auto")
}

func TestRenderTemplate_RendersLayoutModuleFromSlotVars(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-module-render-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")

	markdown := "# 标题\n\n## 第一节\n\n正文。\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{
				SlotID:       "footer",
				SectionIndex: -1,
				Module:       strPtr("cta"),
				ModuleVars: map[string]string{
					"title": "觉得有用？",
					"note":  "收藏起来，下次写文章直接用。",
				},
			},
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, projectID, markdown, plan, "", "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}
	if !strings.Contains(result.HTML, "觉得有用？") {
		t.Fatalf("[FAIL] module title missing from HTML:\n%s", result.HTML)
	}
	if !strings.Contains(result.HTML, "收藏起来，下次写文章直接用。") {
		t.Fatalf("[FAIL] module note missing from HTML:\n%s", result.HTML)
	}
	if len(result.SlotsRendered) != 1 {
		t.Fatalf("[FAIL] SlotsRendered len = %d, want 1", len(result.SlotsRendered))
	}
	audit := result.SlotsRendered[0]
	if audit.Module != "cta" || audit.Status != "rendered" {
		t.Fatalf("[FAIL] module audit = %#v, want cta rendered", audit)
	}
}

func TestRenderTemplate_RejectsLayoutModuleMissingRequiredVars(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-module-missing-vars-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")

	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "footer", SectionIndex: -1, Module: strPtr("cta")},
		},
	}

	_, err := svc.RenderTemplate(context.Background(), userID, projectID, "# 标题\n", plan, "", "")
	if err == nil {
		t.Fatal("[FAIL] Expected missing required module var error")
	}
	if !strings.Contains(err.Error(), "module cta") || !strings.Contains(err.Error(), "title") {
		t.Fatalf("[FAIL] module error should name cta.title, got: %v", err)
	}
}

func TestRenderTemplateRejectsUnknownModuleWhenModuleContractIsUsed(t *testing.T) {
	svc, repo := setupConvertTest(t)
	svc.articleResourceMode = resources.LifecycleModeCompatibility
	userID := "user-module-unknown-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	plan := &LayoutPlan{
		ArticleType:     "long-form-essay",
		ContractVersion: "anban.layout.v1",
		Slots: []LayoutPlanSlot{{
			SlotID: "footer", SectionIndex: -1, Module: strPtr("unknown-module"),
			ModuleVars: map[string]string{"title": "标题"},
		}},
	}
	_, err := svc.RenderTemplate(context.Background(), userID, projectID, "# 标题\n\n## 内容\n\n正文。", plan, "", "")
	if err == nil || !strings.Contains(err.Error(), "module.unknown:unknown-module") {
		t.Fatalf("RenderTemplate error = %v, want unknown module blocker", err)
	}
}

func TestRenderTemplateRejectsUnsafeMarkdownURL(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-unsafe-url-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	plan := &LayoutPlan{ArticleType: "long-form-essay", Slots: []LayoutPlanSlot{{SlotID: "hero"}}}
	_, err := svc.RenderTemplate(context.Background(), userID, projectID, "# 标题\n\n## 内容\n\n[点击](javascript:alert(1))", plan, "", "")
	if err == nil || !strings.Contains(err.Error(), "url.unsafe_link") {
		t.Fatalf("RenderTemplate error = %v, want unsafe URL blocker", err)
	}
}

func TestInspectArticleSourceCanRunWithoutLayoutPlan(t *testing.T) {
	svc, _ := setupConvertTest(t)
	result, err := svc.InspectArticle(context.Background(), "user-inspect-source", InspectArticleRequest{
		Target: "source", Markdown: "# 标题\n\n## 内容\n\n正文。",
	})
	if err != nil {
		t.Fatalf("InspectArticle error: %v", err)
	}
	if result.Target != "source" || result.Readiness.Status != "ready" {
		t.Fatalf("source inspection = %#v", result)
	}
}

func TestInspectArticleSourceBlocksDuplicateInlineImageURLs(t *testing.T) {
	svc, _ := setupConvertTest(t)
	result, err := svc.InspectArticle(context.Background(), "user-inspect-source-duplicate", InspectArticleRequest{
		Target: "source",
		Markdown: "# 标题\n\n## 内容\n\n![图一](https://cdn.example.com/repeat.png)\n\n" +
			"![图二](https://cdn.example.com/repeat.png)",
	})
	if err != nil {
		t.Fatalf("InspectArticle error: %v", err)
	}
	if result.Readiness.Status != "blocked" || !strings.Contains(strings.Join(result.Readiness.Blockers, ","), "image.duplicate") {
		t.Fatalf("duplicate inline URL was not blocked: %#v", result.Readiness)
	}
}

func TestInspectArticleSourceBlocksMissingRequiredImageAndUnsafeSlotURL(t *testing.T) {
	svc, _ := setupConvertTest(t)
	tests := []struct {
		name string
		slot LayoutPlanSlot
		want string
	}{
		{
			name: "required URL missing",
			slot: LayoutPlanSlot{SlotID: "section_opener", SectionIndex: 0, ImageRequired: true},
			want: "image.required_slot_missing",
		},
		{
			name: "unsafe slot URL",
			slot: LayoutPlanSlot{SlotID: "hero", SectionIndex: 0, ImageURL: "javascript:alert(1)"},
			want: "url.unsafe_slot",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := svc.InspectArticle(context.Background(), "user-inspect-source-slot", InspectArticleRequest{
				Target: "source", Markdown: "# 标题\n\n## 内容\n\n正文。",
				LayoutPlan: &LayoutPlan{TemplateName: "tutorial", Slots: []LayoutPlanSlot{tc.slot}},
			})
			if err != nil {
				t.Fatalf("InspectArticle error: %v", err)
			}
			if result.Readiness.Status != "blocked" || !strings.Contains(strings.Join(result.Readiness.Blockers, ","), tc.want) {
				t.Fatalf("source inspection did not block %q: %#v", tc.want, result.Readiness)
			}
		})
	}
}

func TestInspectArticleSourceBlocksRepeatedPlannedImageURLs(t *testing.T) {
	svc, _ := setupConvertTest(t)
	result, err := svc.InspectArticle(context.Background(), "user-inspect-source-slot-duplicate", InspectArticleRequest{
		Target: "source", Markdown: "# 标题\n\n## 内容\n\n正文。",
		LayoutPlan: &LayoutPlan{TemplateName: "tutorial", Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn.example.com/repeat.png"},
			{SlotID: "section_opener", SectionIndex: 0, ImageURL: "https://cdn.example.com/repeat.png"},
		}},
	})
	if err != nil {
		t.Fatalf("InspectArticle error: %v", err)
	}
	if result.Readiness.Status != "blocked" || !strings.Contains(strings.Join(result.Readiness.Blockers, ","), "image.duplicate") {
		t.Fatalf("repeated planned URL was not blocked: %#v", result.Readiness)
	}
}

func TestInspectArticleRenderedRequiresExactServerHTML(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-inspect-rendered"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	markdown := "# 标题\n\n## 内容\n\n正文。"
	plan := &LayoutPlan{ArticleType: "long-form-essay", Slots: []LayoutPlanSlot{{SlotID: "hero", SectionIndex: 0}}}
	rendered, err := svc.RenderTemplate(context.Background(), userID, projectID, markdown, plan, "", "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.InspectArticle(context.Background(), userID, InspectArticleRequest{
		ProjectID: projectID, Markdown: markdown, LayoutPlan: plan, Target: "rendered", HTML: rendered.HTML,
	})
	if err != nil {
		t.Fatalf("InspectArticle exact HTML error: %v", err)
	}
	if result.Readiness.Status != "ready" {
		t.Fatalf("exact server HTML blocked: %#v", result.Readiness)
	}

	result, err = svc.InspectArticle(context.Background(), userID, InspectArticleRequest{
		ProjectID: projectID, Markdown: markdown, LayoutPlan: plan, Target: "rendered", HTML: rendered.HTML + "<script>",
	})
	if err != nil {
		t.Fatalf("InspectArticle mismatched HTML error: %v", err)
	}
	if !strings.Contains(strings.Join(result.Readiness.Blockers, ","), "html.server_mismatch") {
		t.Fatalf("mismatched HTML was not blocked: %#v", result.Readiness)
	}
}

func TestInspectArticleDeliveryRequiresRecordedHTMLAndMetadata(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-inspect-delivery"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	markdown := "# 标题\n\n## 内容\n\n正文。"
	plan := &LayoutPlan{ArticleType: "long-form-essay", Slots: []LayoutPlanSlot{{SlotID: "hero", SectionIndex: 0}}}
	rendered, err := svc.RenderTemplate(context.Background(), userID, projectID, markdown, plan, "", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.InspectArticle(context.Background(), userID, InspectArticleRequest{
		ProjectID: projectID, Markdown: markdown, LayoutPlan: plan, Target: "delivery",
		HTML: rendered.HTML, Title: "标题", Digest: "摘要", EvidencePaths: []string{"output/05-article.html"},
	})
	if err != nil {
		t.Fatalf("InspectArticle valid delivery error: %v", err)
	}
	if result.Readiness.Status != "ready" {
		t.Fatalf("complete delivery blocked: %#v", result.Readiness)
	}
	result, err = svc.InspectArticle(context.Background(), userID, InspectArticleRequest{
		ProjectID: projectID, Markdown: markdown, LayoutPlan: plan, Target: "delivery", HTML: rendered.HTML,
	})
	if err != nil {
		t.Fatalf("InspectArticle incomplete delivery error: %v", err)
	}
	if result.Readiness.Status != "blocked" || len(result.Missing) == 0 {
		t.Fatalf("incomplete delivery did not block: %#v", result)
	}
	result, err = svc.InspectArticle(context.Background(), userID, InspectArticleRequest{
		ProjectID: projectID, Markdown: markdown, LayoutPlan: plan, Target: "rendered",
	})
	if err != nil {
		t.Fatalf("InspectArticle missing HTML error: %v", err)
	}
	if result.Readiness.Status != "blocked" || !strings.Contains(strings.Join(result.Readiness.Blockers, ","), "html.rendered_response_required") {
		t.Fatalf("rendered inspection without submitted HTML did not block: %#v", result.Readiness)
	}
}

func TestRenderTemplateV1UsesDedicatedArticleModules(t *testing.T) {
	svc, repo := setupConvertTest(t)
	svc.articleResourceMode = resources.LifecycleModeCompatibility
	userID := "user-module-v1-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	markdown := "# 标题\n\n## 正文\n\n文章主体内容。"
	cases := []struct {
		name string
		vars map[string]string
		want []string
	}{
		{name: "quote", vars: map[string]string{"text": "核心观点", "source": "作者"}, want: []string{"<blockquote", "核心观点", "作者"}},
		{name: "callout", vars: map[string]string{"type": "warning", "body": "检查链接"}, want: []string{"注意", "检查链接", "#fff7e0"}},
		{name: "steps", vars: map[string]string{"title": "落地步骤", "steps": "01 | 明确目标 | 先写出目标\n02 | 检查结果 | 对照目标复核"}, want: []string{"落地步骤", "01 明确目标", "02 检查结果"}},
		{name: "checklist", vars: map[string]string{"title": "检查清单", "items": "done | 资料完整 | 来源已核对\npending | 复核链接 | 确认可以访问"}, want: []string{"检查清单", "☑", "☐", "复核链接"}},
		{name: "summary", vars: map[string]string{"variant": "three", "title": "带走三点", "items": "结构清楚 | 图片合规 | 结论具体"}, want: []string{"带走三点", "结构清楚", "图片合规", "结论具体"}},
		{name: "comparison-table", vars: map[string]string{"title": "方案对比", "left_title": "方案 A", "left_items": "部署快 | 成本低", "right_title": "方案 B", "right_items": "扩展强 | 运维较多"}, want: []string{"<table", "方案 A", "方案 B", "部署快", "扩展强"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := tc.name
			plan := &LayoutPlan{
				ArticleType: "tutorial", ContractVersion: "anban.layout.v1",
				Slots: []LayoutPlanSlot{{SlotID: "footer", SectionIndex: -1, Module: &name, ModuleVars: tc.vars}},
			}
			result, err := svc.RenderTemplate(t.Context(), userID, projectID, markdown, plan, "", "")
			if err != nil {
				t.Fatalf("RenderTemplate error: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(result.HTML, want) {
					t.Errorf("HTML missing %q:\n%s", want, result.HTML)
				}
			}
		})
	}
}

func TestRenderTemplateV1IsBlockedByLegacyServerMode(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-module-v1-disabled-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	name := "quote"
	plan := &LayoutPlan{
		ArticleType: "tutorial", ContractVersion: "anban.layout.v1",
		Slots: []LayoutPlanSlot{{SlotID: "footer", SectionIndex: -1, Module: &name, ModuleVars: map[string]string{"text": "核心观点"}}},
	}
	_, err := svc.RenderTemplate(t.Context(), userID, projectID, "# 标题\n\n## 正文\n\n内容。", plan, "", "")
	if err == nil || !strings.Contains(err.Error(), "article resource lifecycle mode") {
		t.Fatalf("legacy server RenderTemplate error = %v, want lifecycle mode blocker", err)
	}
}

func TestRenderTemplateV1RejectsInvalidVariantAndOverlongFields(t *testing.T) {
	svc, repo := setupConvertTest(t)
	svc.articleResourceMode = resources.LifecycleModeCompatibility
	userID := "user-module-v1-invalid-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	makePlan := func(name string, vars map[string]string) *LayoutPlan {
		return &LayoutPlan{ArticleType: "tutorial", ContractVersion: "anban.layout.v1", Slots: []LayoutPlanSlot{{
			SlotID: "footer", SectionIndex: -1, Module: &name, ModuleVars: vars,
		}}}
	}
	_, err := svc.RenderTemplate(t.Context(), userID, projectID, "# 标题\n\n## 正文\n\n内容。", makePlan("summary", map[string]string{"variant": "metrics", "items": "A | B"}), "", "")
	if err == nil || !strings.Contains(err.Error(), "module.summary.variant:invalid_variant") {
		t.Fatalf("invalid variant error = %v", err)
	}
	_, err = svc.RenderTemplate(t.Context(), userID, projectID, "# 标题\n\n## 正文\n\n内容。", makePlan("callout", map[string]string{"body": strings.Repeat("长", 2001)}), "", "")
	if err == nil || !strings.Contains(err.Error(), "exceeds 2000 characters") {
		t.Fatalf("overlong field error = %v", err)
	}
}

func TestRenderTemplateV1RejectsMalformedModuleData(t *testing.T) {
	svc, repo := setupConvertTest(t)
	svc.articleResourceMode = resources.LifecycleModeCompatibility
	userID := "user-module-v1-malformed-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")
	markdown := "# 标题\n\n## 正文\n\n内容。"
	tests := []struct {
		name   string
		module string
		vars   map[string]string
		want   string
	}{
		{name: "callout enum", module: "callout", vars: map[string]string{"type": "loud", "body": "提醒"}, want: "module.callout.type:invalid_variant"},
		{name: "steps row columns", module: "steps", vars: map[string]string{"title": "步骤", "steps": "01 | 目标"}, want: "at least 3 pipe-separated columns"},
		{name: "steps row limit", module: "steps", vars: map[string]string{"title": "步骤", "steps": "01 | A | a\n02 | B | b\n03 | C | c\n04 | D | d\n05 | E | e\n06 | F | f\n07 | G | g\n08 | H | h\n09 | I | i"}, want: "at most 8 rows"},
		{name: "checklist status", module: "checklist", vars: map[string]string{"title": "清单", "items": "complete | 已完成"}, want: "invalid status"},
		{name: "summary exact count", module: "summary", vars: map[string]string{"variant": "three", "items": "A | B"}, want: "exactly 3 items"},
		{name: "comparison paired rows", module: "comparison-table", vars: map[string]string{"title": "对比", "left_title": "A", "left_items": "快 | 便宜", "right_title": "B", "right_items": "灵活"}, want: "same number of rows"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := &LayoutPlan{ArticleType: "tutorial", ContractVersion: "anban.layout.v1", Slots: []LayoutPlanSlot{{
				SlotID: "footer", SectionIndex: -1, Module: &tc.module, ModuleVars: tc.vars,
			}}}
			_, err := svc.RenderTemplate(t.Context(), userID, projectID, markdown, plan, "", "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("RenderTemplate error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRenderTemplate_ThemeFallbackToProjectTheme(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-theme-fallback-001"
	// Project has no theme set → should default to "autumn-warm".
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "")

	markdown := "# Title\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/h.png"},
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, projectID, markdown, plan, "", "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}
	if result.Theme != "autumn-warm" {
		t.Errorf("[FAIL] Theme = %q, want autumn-warm (default)", result.Theme)
	}
}

func assertImageStyleContains(t *testing.T, html, url, wantStyle string) {
	t.Helper()
	idx := strings.Index(html, `src="`+url+`"`)
	if idx < 0 {
		t.Fatalf("[FAIL] HTML missing image URL %s:\n%s", url, html)
	}
	end := strings.Index(html[idx:], ">")
	if end < 0 {
		t.Fatalf("[FAIL] image tag for %s is not closed:\n%s", url, html[idx:])
	}
	tag := html[idx : idx+end]
	if !strings.Contains(tag, wantStyle) {
		t.Fatalf("[FAIL] image tag for %s missing style %q:\n%s", url, wantStyle, tag)
	}
}

func TestRenderTemplate_ThemeArgOverridesProjectTheme(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-theme-override-001"
	// Project has autumn-warm but we override to spring-fresh.
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "autumn-warm")

	markdown := "# Title\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/h.png"},
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, projectID, markdown, plan, "spring-fresh", "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}
	if result.Theme != "spring-fresh" {
		t.Errorf("[FAIL] Theme = %q, want spring-fresh (override)", result.Theme)
	}
}

func TestRenderTemplate_EmptyMarkdown(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-empty-md"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "")

	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/h.png"},
		},
	}
	_, err := svc.RenderTemplate(context.Background(), userID, projectID, "", plan, "", "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for empty markdown")
	}
}

func TestRenderTemplate_NilPlan(t *testing.T) {
	svc, repo := setupConvertTest(t)
	userID := "user-nil-plan"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformWechat, "", "")

	_, err := svc.RenderTemplate(context.Background(), userID, projectID, "# Hello", nil, "", "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for nil plan")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }
