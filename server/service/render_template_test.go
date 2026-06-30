package service

import (
	"context"
	"strings"
	"testing"

	"github.com/royalrick/anbanwriter/server/model"
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
// wechatarticle workflow hit this and had to manually clean up repeated img).
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
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-render-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

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
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-listicle-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

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
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-all-rendered-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

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

// Regression: when the source markdown already inlines an image AND a slot
// carries the same URL, the rendered HTML must contain exactly ONE <img> for
// that URL — the slot is deduped against the inline image.
func TestRenderTemplate_DedupInlineAndSlotSameURL(t *testing.T) {
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-dedup-int-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

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
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-image-size-style-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

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
}

func TestRenderTemplate_ThemeFallbackToProjectTheme(t *testing.T) {
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-theme-fallback-001"
	// Project has no theme set → should default to "autumn-warm".
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "")

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
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-theme-override-001"
	// Project has autumn-warm but we override to spring-fresh.
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

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
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-empty-md"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "")

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
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-nil-plan"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "")

	_, err := svc.RenderTemplate(context.Background(), userID, projectID, "# Hello", nil, "", "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for nil plan")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }
