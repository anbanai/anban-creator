package service

import (
	"context"
	"testing"
)

func TestInspectArticleReportsImageModeAndDraftBlockers(t *testing.T) {
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-inspect-article-001"
	projectID := createProjectWithTheme(t, repo, userID, "article", "", "autumn-warm")

	result, err := svc.InspectArticle(context.Background(), userID, InspectArticleRequest{
		ProjectID:        projectID,
		Markdown:         "# 标题\n\n## 第一节\n\n正文。\n",
		ArticleImageMode: "cover_and_content",
		LayoutPlan: &LayoutPlan{
			ArticleType:  "long-form-essay",
			TemplateName: "long-form-essay",
			Slots: []LayoutPlanSlot{
				{SlotID: "hero", SectionIndex: 0, ImageSize: "full-bleed"},
				{SlotID: "section_opener", SectionIndex: 0, ImageURL: "https://cdn.example.com/s1.png"},
			},
		},
	})
	if err != nil {
		t.Fatalf("InspectArticle error: %v", err)
	}

	if result.Readiness.Targets["convert"] != "ready" {
		t.Fatalf("convert target = %q, want ready", result.Readiness.Targets["convert"])
	}
	if result.Readiness.Targets["draft"] != "blocked" {
		t.Fatalf("draft target = %q, want blocked", result.Readiness.Targets["draft"])
	}
	if !hasInspectBlocker(result.Readiness.Blockers, "MISSING_COVER_MEDIA_ID") {
		t.Fatalf("expected missing cover media blocker, got %#v", result.Readiness.Blockers)
	}
	if !hasInspectCheck(result.Checks, "LAYOUT_SLOT_IMAGE_MISSING") {
		t.Fatalf("expected layout slot image missing check, got %#v", result.Checks)
	}
	if result.Structure.H2Count != 1 {
		t.Fatalf("h2_count = %d, want 1", result.Structure.H2Count)
	}
}

func TestInspectArticleTextOnlyDoesNotRequireImagesOrCover(t *testing.T) {
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-inspect-article-002"
	projectID := createProjectWithTheme(t, repo, userID, "article", "", "autumn-warm")

	result, err := svc.InspectArticle(context.Background(), userID, InspectArticleRequest{
		ProjectID:        projectID,
		Markdown:         "# 标题\n\n## 第一节\n\n正文。\n",
		ArticleImageMode: "text_only",
		LayoutPlan: &LayoutPlan{
			ArticleType:  "long-form-essay",
			TemplateName: "long-form-essay",
			Slots: []LayoutPlanSlot{
				{SlotID: "hero", SectionIndex: 0},
			},
		},
	})
	if err != nil {
		t.Fatalf("InspectArticle error: %v", err)
	}
	if result.Readiness.Targets["draft"] != "ready" {
		t.Fatalf("draft target = %q, want ready; blockers=%#v", result.Readiness.Targets["draft"], result.Readiness.Blockers)
	}
	if hasInspectBlocker(result.Readiness.Blockers, "MISSING_COVER_MEDIA_ID") {
		t.Fatalf("text_only should not require cover media, got %#v", result.Readiness.Blockers)
	}
}

func hasInspectBlocker(blockers []InspectArticleBlocker, code string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

func hasInspectCheck(checks []InspectArticleCheck, code string) bool {
	for _, check := range checks {
		if check.Code == code {
			return true
		}
	}
	return false
}
