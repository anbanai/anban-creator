package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type InspectArticleRequest struct {
	ProjectID        string      `json:"project_id"`
	Markdown         string      `json:"markdown"`
	ArticleImageMode string      `json:"article_image_mode,omitempty"`
	LayoutPlan       *LayoutPlan `json:"layout_plan,omitempty"`
	CoverMediaID     string      `json:"cover_media_id,omitempty"`
}

type InspectArticleResult struct {
	Structure InspectArticleStructure `json:"structure"`
	Readiness InspectArticleReadiness `json:"readiness"`
	Checks    []InspectArticleCheck   `json:"checks"`
}

type InspectArticleStructure struct {
	H1Count      int `json:"h1_count"`
	H2Count      int `json:"h2_count"`
	ImageCount   int `json:"image_count"`
	RemoteImages int `json:"remote_images"`
}

type InspectArticleReadiness struct {
	Targets  map[string]string       `json:"targets"`
	Blockers []InspectArticleBlocker `json:"blockers"`
}

type InspectArticleBlocker struct {
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	Blocks       []string `json:"blocks"`
	SuggestedFix string   `json:"suggested_fix,omitempty"`
}

type InspectArticleCheck struct {
	Level        string `json:"level"`
	Code         string `json:"code"`
	Message      string `json:"message"`
	SuggestedFix string `json:"suggested_fix,omitempty"`
}

const (
	inspectReady   = "ready"
	inspectBlocked = "blocked"
	inspectWarn    = "warn"
	inspectError   = "error"
)

func (s *WritingService) InspectArticle(ctx context.Context, userID string, req InspectArticleRequest) (*InspectArticleResult, error) {
	if strings.TrimSpace(req.ProjectID) == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if strings.TrimSpace(req.Markdown) == "" {
		return nil, fmt.Errorf("markdown is required")
	}
	ch, err := s.repo.Projects().FindByID(ctx, req.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	if ch.UserID != userID {
		return nil, fmt.Errorf("project not owned by user")
	}

	imageMode := normalizeArticleImageMode(req.ArticleImageMode)
	result := &InspectArticleResult{
		Structure: inspectArticleStructure(req.Markdown),
		Readiness: InspectArticleReadiness{Targets: map[string]string{
			"convert": inspectReady,
			"draft":   inspectReady,
		}},
	}

	if req.LayoutPlan == nil {
		result.addCheck(inspectWarn, "LAYOUT_PLAN_MISSING", "layout_plan missing; render_template cannot audit planned slots", "Create visual-rhythm-plan.md and pass its layout_plan JSON.")
	} else {
		result.Checks = append(result.Checks, inspectLayoutPlanChecks(req.LayoutPlan, imageMode)...)
	}

	if coverRequired(imageMode) && strings.TrimSpace(req.CoverMediaID) == "" {
		result.block("MISSING_COVER_MEDIA_ID", "cover media_id is required before creating a WeChat draft in this image mode", []string{"draft"}, "Generate/upload the cover with upload_to_cdn=true and pass the returned media_id.")
	}

	for _, check := range result.Checks {
		if check.Level == inspectError {
			result.Readiness.Targets["convert"] = inspectBlocked
		}
	}
	if len(result.Readiness.Blockers) > 0 {
		for _, blocker := range result.Readiness.Blockers {
			for _, target := range blocker.Blocks {
				result.Readiness.Targets[target] = inspectBlocked
			}
		}
	}
	return result, nil
}

func (r *InspectArticleResult) addCheck(level, code, message, fix string) {
	r.Checks = append(r.Checks, InspectArticleCheck{Level: level, Code: code, Message: message, SuggestedFix: fix})
}

func (r *InspectArticleResult) block(code, message string, blocks []string, fix string) {
	r.Readiness.Blockers = append(r.Readiness.Blockers, InspectArticleBlocker{
		Code:         code,
		Message:      message,
		Blocks:       blocks,
		SuggestedFix: fix,
	})
}

func normalizeArticleImageMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "cover_only", "content_only", "text_only":
		return strings.TrimSpace(mode)
	default:
		return "cover_and_content"
	}
}

func coverRequired(mode string) bool {
	return mode == "cover_and_content" || mode == "cover_only"
}

func contentImagesRequired(mode string) bool {
	return mode == "cover_and_content" || mode == "content_only"
}

func inspectLayoutPlanChecks(plan *LayoutPlan, imageMode string) []InspectArticleCheck {
	var checks []InspectArticleCheck
	if plan == nil {
		return checks
	}
	for i, slot := range plan.Slots {
		if contentImagesRequired(imageMode) && slot.SlotID != "footer" && strings.TrimSpace(slot.ImageURL) == "" {
			checks = append(checks, InspectArticleCheck{
				Level:        inspectWarn,
				Code:         "LAYOUT_SLOT_IMAGE_MISSING",
				Message:      fmt.Sprintf("layout_plan.slots[%d] %s has no image_url", i, slot.SlotID),
				SuggestedFix: "Backfill image_url with the generated WeChat CDN URL, or set it to null only when the image mode disables that slot.",
			})
		}
		if imageMode == "text_only" && strings.TrimSpace(slot.ImageURL) != "" {
			checks = append(checks, InspectArticleCheck{
				Level:        inspectWarn,
				Code:         "TEXT_ONLY_SLOT_HAS_IMAGE",
				Message:      fmt.Sprintf("layout_plan.slots[%d] carries image_url while article_image_mode=text_only", i),
				SuggestedFix: "Set image_url to null for text_only article runs.",
			})
		}
	}
	return checks
}

var (
	h1LineRe        = regexp.MustCompile(`(?m)^# [^\n]+`)
	h2LineRe        = regexp.MustCompile(`(?m)^## [^\n]+`)
	markdownImageRe = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)[^)]*\)`)
)

func inspectArticleStructure(markdown string) InspectArticleStructure {
	structure := InspectArticleStructure{
		H1Count: len(h1LineRe.FindAllString(markdown, -1)),
		H2Count: len(h2LineRe.FindAllString(markdown, -1)),
	}
	for _, match := range markdownImageRe.FindAllStringSubmatch(markdown, -1) {
		structure.ImageCount++
		if len(match) > 1 && strings.HasPrefix(match[1], "http") {
			structure.RemoteImages++
		}
	}
	return structure
}
