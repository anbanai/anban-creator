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
// annotateMarkdownWithSlots unit tests (pure function, no LLM/DB)
// ---------------------------------------------------------------------------

func TestAnnotateMarkdownWithSlots_HeroAndSectionOpeners(t *testing.T) {
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

	annotated, withImages := annotateMarkdownWithSlots(markdown, plan)
	if len(withImages) != 3 {
		t.Errorf("[FAIL] slotsWithImages = %d, want 3", len(withImages))
	}
	if !strings.Contains(annotated, "[SLOT: hero") {
		t.Error("[FAIL] Missing [SLOT: hero] marker")
	}
	if !strings.Contains(annotated, "https://cdn/hero.png") {
		t.Error("[FAIL] Missing hero image URL")
	}
	if !strings.Contains(annotated, "https://cdn/s1.png") {
		t.Error("[FAIL] Missing section 1 image URL")
	}
	if !strings.Contains(annotated, "https://cdn/s2.png") {
		t.Error("[FAIL] Missing section 2 image URL")
	}
	if !strings.Contains(annotated, "RENDER_TEMPLATE_CONTRACT") {
		t.Error("[FAIL] Missing contract header")
	}
}

func TestAnnotateMarkdownWithSlots_Footer(t *testing.T) {
	markdown := "## Section\n\nBody."
	plan := &LayoutPlan{
		ArticleType:  "tutorial",
		TemplateName: "tutorial",
		Slots: []LayoutPlanSlot{
			{SlotID: "section_opener", SectionIndex: 0, ImageURL: "https://cdn/a.png"},
		},
		Footer: &LayoutPlanSlot{Module: strPtr("cta")},
	}
	annotated, _ := annotateMarkdownWithSlots(markdown, plan)
	if !strings.Contains(annotated, "FOOTER SLOT") {
		t.Error("[FAIL] Missing FOOTER SLOT marker")
	}
	if !strings.Contains(annotated, "module=cta") {
		t.Error("[FAIL] Missing footer module=cta in marker")
	}
}

// TestAnnotateMarkdownWithSlots_SectionOpenerBeforeContent is a regression
// test: section_opener markers MUST appear BEFORE the section body (so the
// LLM anchors them to the section title per contract rule 2). Earlier
// implementations emitted them at the section's tail, which would make the
// LLM place the image at the wrong position.
func TestAnnotateMarkdownWithSlots_SectionOpenerBeforeContent(t *testing.T) {
	markdown := "## First Section\n\nFirst body.\n\n## Second Section\n\nSecond body.\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "section_opener", SectionIndex: 0, ImageURL: "https://cdn/before-first.png"},
			{SlotID: "section_opener", SectionIndex: 1, ImageURL: "https://cdn/before-second.png"},
		},
	}

	annotated, _ := annotateMarkdownWithSlots(markdown, plan)

	// Marker for section 2 must come before the "Second body." text.
	markerIdx := strings.Index(annotated, "https://cdn/before-second.png")
	bodyIdx := strings.Index(annotated, "Second body.")
	if markerIdx < 0 {
		t.Fatal("[FAIL] section 2 image URL missing from annotation")
	}
	if bodyIdx < 0 {
		t.Fatal("[FAIL] section 2 body missing from annotation")
	}
	if markerIdx > bodyIdx {
		t.Errorf("[FAIL] section_opener marker (idx=%d) appears AFTER section body (idx=%d) — should be before",
			markerIdx, bodyIdx)
	}
}

// TestAnnotateMarkdownWithSlots_InlineDetailAfterSection verifies inline_detail
// directives are placed at the END of the section body (the LLM reads the
// paragraph index from the directive).
func TestAnnotateMarkdownWithSlots_InlineDetailAfterSection(t *testing.T) {
	markdown := "## Section\n\nParagraph one.\n\nParagraph two.\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "inline_detail", SectionIndex: 0, AfterParagraphIndex: 1, ImageURL: "https://cdn/inline.png"},
		},
	}
	annotated, _ := annotateMarkdownWithSlots(markdown, plan)
	bodyIdx := strings.Index(annotated, "Paragraph two.")
	directiveIdx := strings.Index(annotated, "INLINE_SLOT")
	if directiveIdx < 0 {
		t.Fatal("[FAIL] inline_detail directive missing")
	}
	if bodyIdx < 0 {
		t.Fatal("[FAIL] section body missing")
	}
	if directiveIdx < bodyIdx {
		t.Errorf("[FAIL] INLINE_SLOT directive (idx=%d) appears BEFORE section body (idx=%d) — should be after",
			directiveIdx, bodyIdx)
	}
}

// ---------------------------------------------------------------------------
// Integration: RenderTemplate with diagnostic LLM mock
// ---------------------------------------------------------------------------

func TestRenderTemplate_LongFormEssay(t *testing.T) {
	llm := &diagnosticLLM{
		// LLM "renders" the HTML with all 3 image URLs present.
		response: `<section><img src="https://cdn/hero.png"><h1>标题</h1><p>引言。</p><h2>第一节</h2><img src="https://cdn/s1.png"><p>正文。</p><h2>第二节</h2><img src="https://cdn/s2.png"><p>正文。</p></section>`,
	}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-render-001"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

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

	result, err := svc.RenderTemplate(context.Background(), userID, channelID, markdown, plan, "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}

	if result.HTML == "" {
		t.Fatal("[FAIL] HTML empty")
	}

	// All 3 images should have been rendered (mock LLM included them).
	if len(result.SlotsRendered) != 3 {
		t.Errorf("[FAIL] SlotsRendered len = %d, want 3", len(result.SlotsRendered))
	}
	for _, audit := range result.SlotsRendered {
		if audit.Status != "rendered" {
			t.Errorf("[FAIL] Slot %s status = %s, want rendered", audit.SlotID, audit.Status)
		}
	}

	// Prompt must include contract + theme + markdown.
	call := llm.lastCall()
	if call == nil {
		t.Fatal("[FAIL] LLM not called")
	}
	if !strings.Contains(call.UserPrompt, "结构化渲染契约") {
		t.Error("[FAIL] Prompt missing 结构化渲染契约 contract")
	}
	if !strings.Contains(call.UserPrompt, "[SLOT: hero") {
		t.Error("[FAIL] Prompt missing [SLOT: hero] marker")
	}

	if result.Theme != "autumn-warm" {
		t.Errorf("[FAIL] Theme = %q, want autumn-warm", result.Theme)
	}
}

func TestRenderTemplate_Listicle(t *testing.T) {
	llm := &diagnosticLLM{
		response: `<section><img src="https://cdn/lh.png"><h1>三个清单</h1><h2>第一项</h2><img src="https://cdn/l1.png"><h2>第二项</h2><img src="https://cdn/l2.png"><h2>第三项</h2><img src="https://cdn/l3.png"></section>`,
	}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-listicle-001"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

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

	result, err := svc.RenderTemplate(context.Background(), userID, channelID, markdown, plan, "")
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

func TestRenderTemplate_MissingImage_MarkedInAudit(t *testing.T) {
	// LLM "forgets" to include the second image URL — audit should catch it.
	llm := &diagnosticLLM{
		response: `<section><img src="https://cdn/hero.png"><h1>T</h1><h2>S1</h2><p>body</p><h2>S2</h2><p>body</p></section>`,
	}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-missing-001"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

	markdown := "# T\n\n## S1\n\nbody.\n\n## S2\n\nbody.\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/hero.png"},
			{SlotID: "section_opener", SectionIndex: 1, ImageURL: "https://cdn/s1.png"}, // missing in HTML
			{SlotID: "section_opener", SectionIndex: 2, ImageURL: "https://cdn/s2.png"}, // missing in HTML
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, channelID, markdown, plan, "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}

	// Soft audit: still succeeds, but flags the missing images.
	if len(result.SlotsRendered) != 3 {
		t.Fatalf("[FAIL] SlotsRendered len = %d, want 3", len(result.SlotsRendered))
	}

	renderedCount, missingCount := 0, 0
	for _, a := range result.SlotsRendered {
		if a.Status == "rendered" {
			renderedCount++
		}
		if a.Status == "missing-image" {
			missingCount++
		}
	}
	if renderedCount != 1 {
		t.Errorf("[FAIL] rendered slot count = %d, want 1 (only hero)", renderedCount)
	}
	if missingCount != 2 {
		t.Errorf("[FAIL] missing-image count = %d, want 2", missingCount)
	}
}

func TestRenderTemplate_ThemeFallbackToChannelTheme(t *testing.T) {
	llm := &diagnosticLLM{response: "<p>ok</p>"}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-theme-fallback-001"
	// Channel has no theme set → should default to "autumn-warm".
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "")

	markdown := "# Title\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/h.png"},
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, channelID, markdown, plan, "")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}
	if result.Theme != "autumn-warm" {
		t.Errorf("[FAIL] Theme = %q, want autumn-warm (default)", result.Theme)
	}
}

func TestRenderTemplate_ThemeArgOverridesChannelTheme(t *testing.T) {
	llm := &diagnosticLLM{response: "<p>ok</p>"}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-theme-override-001"
	// Channel has autumn-warm but we override to spring-fresh.
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

	markdown := "# Title\n"
	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/h.png"},
		},
	}

	result, err := svc.RenderTemplate(context.Background(), userID, channelID, markdown, plan, "spring-fresh")
	if err != nil {
		t.Fatalf("[FAIL] RenderTemplate error: %v", err)
	}
	if result.Theme != "spring-fresh" {
		t.Errorf("[FAIL] Theme = %q, want spring-fresh (override)", result.Theme)
	}
}

func TestRenderTemplate_EmptyMarkdown(t *testing.T) {
	llm := &diagnosticLLM{response: "should not reach"}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-empty-md"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "")

	plan := &LayoutPlan{
		ArticleType: "long-form-essay",
		Slots: []LayoutPlanSlot{
			{SlotID: "hero", SectionIndex: 0, ImageURL: "https://cdn/h.png"},
		},
	}
	_, err := svc.RenderTemplate(context.Background(), userID, channelID, "", plan, "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for empty markdown")
	}
	if llm.callCount() > 0 {
		t.Error("[FAIL] LLM should not have been called for empty markdown")
	}
}

func TestRenderTemplate_NilPlan(t *testing.T) {
	llm := &diagnosticLLM{response: "should not reach"}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-nil-plan"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "")

	_, err := svc.RenderTemplate(context.Background(), userID, channelID, "# Hello", nil, "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for nil plan")
	}
	if llm.callCount() > 0 {
		t.Error("[FAIL] LLM should not have been called")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }
