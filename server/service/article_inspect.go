package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/anbanai/anban-creator/server/app/articlecheck"
	"github.com/anbanai/anban-creator/server/resources"
)

type InspectArticleRequest struct {
	ProjectID     string
	TaskID        string
	Markdown      string
	LayoutPlan    *LayoutPlan
	Target        string
	HTML          string
	Title         string
	Author        string
	Digest        string
	EvidencePaths []string
}

// InspectArticle performs deterministic article checks without persisting files
// or changing task state. Rendered and delivery checks compare submitted HTML
// with a fresh render using the same frozen task theme.
func (s *ContentRenderService) InspectArticle(ctx context.Context, userID string, req InspectArticleRequest) (*articlecheck.Result, error) {
	modules := articleModuleUsages(req.LayoutPlan, s.articleResourceMode)
	templateName := ""
	if req.LayoutPlan != nil {
		templateName = req.LayoutPlan.TemplateName
	}
	base := articlecheck.Request{
		Target:        "source",
		Markdown:      req.Markdown,
		Template:      templateName,
		Modules:       modules,
		ImageSlots:    articleImageSlotUsages(req.LayoutPlan),
		EvidencePaths: req.EvidencePaths,
	}
	if hasUnavailableArticleModule(req.LayoutPlan, s.articleResourceMode) {
		base.Target = req.Target
		if base.Target == "" {
			base.Target = "source"
		}
		result := articlecheck.Inspect(base)
		appendArticleBlocker(&result, "resource.lifecycle_mode_disabled")
		return &result, nil
	}
	if req.Target == "source" {
		result := articlecheck.Inspect(base)
		return &result, nil
	}
	if req.Target != "rendered" && req.Target != "delivery" {
		base.Target = req.Target
		result := articlecheck.Inspect(base)
		return &result, nil
	}
	if req.LayoutPlan == nil {
		base.Target = req.Target
		result := articlecheck.Inspect(base)
		appendArticleBlocker(&result, "layout_plan.required")
		return &result, nil
	}
	if source := articlecheck.Inspect(base); len(source.Readiness.Blockers) > 0 {
		base.Target = req.Target
		result := articlecheck.Inspect(base)
		return &result, nil
	}
	rendered, err := s.RenderTemplate(ctx, userID, req.ProjectID, req.Markdown, req.LayoutPlan, "", req.TaskID)
	if err != nil {
		return nil, fmt.Errorf("server article render: %w", err)
	}
	expectedImages := collectArticleImageURLs(req.Markdown, req.LayoutPlan)
	verified := strings.TrimSpace(req.HTML) != "" && req.HTML == rendered.HTML
	checkedHTML := rendered.HTML
	if req.HTML != "" && !verified {
		checkedHTML = req.HTML
	}
	check := articlecheck.Request{
		Target:             req.Target,
		Markdown:           req.Markdown,
		HTML:               checkedHTML,
		Title:              req.Title,
		Author:             req.Author,
		Digest:             req.Digest,
		Template:           templateName,
		Modules:            modules,
		ExpectedImageURLs:  expectedImages,
		EvidencePaths:      req.EvidencePaths,
		HTMLServerVerified: verified,
	}
	result := articlecheck.Inspect(check)
	if strings.TrimSpace(req.HTML) == "" {
		appendArticleBlocker(&result, "html.rendered_response_required")
	}
	if req.HTML != "" && !verified {
		appendArticleBlocker(&result, "html.server_mismatch")
	}
	if req.Target == "delivery" && strings.TrimSpace(req.HTML) == "" {
		appendArticleBlocker(&result, "html.delivery_record_required")
	}
	for _, slot := range rendered.SlotsRendered {
		if slot.Status != "rendered" {
			appendArticleBlocker(&result, "layout.slot:"+slot.SlotID+":"+slot.Status)
		}
	}
	return &result, nil
}

func articleImageSlotUsages(plan *LayoutPlan) []articlecheck.ImageSlotUsage {
	if plan == nil {
		return nil
	}
	slots := append([]LayoutPlanSlot(nil), plan.Slots...)
	if plan.Footer != nil {
		slots = append(slots, *plan.Footer)
	}
	result := make([]articlecheck.ImageSlotUsage, 0, len(slots))
	for _, slot := range slots {
		if !slot.ImageRequired && strings.TrimSpace(slot.ImageURL) == "" {
			continue
		}
		result = append(result, articlecheck.ImageSlotUsage{ID: slot.SlotID, URL: slot.ImageURL, Required: slot.ImageRequired})
	}
	return result
}

func hasUnavailableArticleModule(plan *LayoutPlan, mode resources.LifecycleMode) bool {
	if plan == nil {
		return false
	}
	if plan.ContractVersion == "" {
		return false
	}
	if mode == resources.LifecycleModeLegacy {
		return true
	}
	for _, slot := range auditableSlots(plan) {
		if slot.Module == nil || strings.TrimSpace(*slot.Module) == "" {
			continue
		}
		entry := resources.Manager().Get(resources.CategoryLayout, strings.TrimSpace(*slot.Module))
		if entry != nil && entry.ModuleContract != nil && entry.ModuleContract.Version == plan.ContractVersion && !resources.LifecycleAllowed(entry.ModuleContract.Lifecycle, mode) {
			return true
		}
	}
	return false
}

func collectArticleImageURLs(markdown string, plan *LayoutPlan) []string {
	seen := collectInlineImageURLs(markdown)
	if plan != nil {
		for _, slot := range plan.Slots {
			if strings.TrimSpace(slot.ImageURL) != "" {
				seen[slot.ImageURL] = true
			}
		}
		if plan.Footer != nil && strings.TrimSpace(plan.Footer.ImageURL) != "" {
			seen[plan.Footer.ImageURL] = true
		}
	}
	urls := make([]string, 0, len(seen))
	for imageURL := range seen {
		urls = append(urls, imageURL)
	}
	sort.Strings(urls)
	return urls
}

func appendArticleBlocker(result *articlecheck.Result, blocker string) {
	if result == nil || strings.TrimSpace(blocker) == "" {
		return
	}
	for _, existing := range result.Readiness.Blockers {
		if existing == blocker {
			return
		}
	}
	result.Readiness.Blockers = append(result.Readiness.Blockers, blocker)
	sort.Strings(result.Readiness.Blockers)
	result.Readiness.Status = "blocked"
}
