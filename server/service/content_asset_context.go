package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// GetLineage returns a bounded Server projection for the asset detail view.
// Analytics are grouped by their own project/platform family; this method never
// combines unlike platform metrics into one score or total.
func (s *ContentAssetService) GetLineage(ctx context.Context, userID, assetID string) (*ContentAssetLineage, error) {
	detail, err := s.Get(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}
	lineage := &ContentAssetLineage{ContentAssetDetail: detail, Links: detail.Links, PlatformPerformance: []ContentAssetPlatformPerformance{}, LatestRuns: []*model.ContentReuseRun{}}
	for _, link := range detail.Links {
		if link == nil {
			continue
		}
		switch link.Relation {
		case model.ContentAssetLinkOrigin:
			lineage.Summary.OriginCount++
		case model.ContentAssetLinkDerivative:
			lineage.Summary.DerivativeCount++
		}
	}
	for offset := 0; ; offset += 100 {
		runs, total, listErr := s.repo.ContentAssets().ListReuseRuns(ctx, repository.ContentReuseRunListFilter{UserID: userID, AssetID: assetID, Offset: offset, Limit: 100})
		if listErr != nil {
			return nil, listErr
		}
		lineage.Summary.RunCount += len(runs)
		for _, run := range runs {
			if run == nil {
				continue
			}
			lineage.Summary.CostCredits += run.CostCredits
			lineage.Summary.ReservedCredits += run.BudgetReserved
			if len(lineage.LatestRuns) < 20 {
				lineage.LatestRuns = append(lineage.LatestRuns, run)
			}
		}
		if offset+len(runs) >= int(total) || len(runs) == 0 {
			break
		}
	}
	type performanceKey struct{ projectID, platform string }
	groups := map[performanceKey]*ContentAssetPlatformPerformance{}
	for _, link := range detail.Links {
		if link == nil || strings.TrimSpace(link.ProjectID) == "" {
			continue
		}
		key := performanceKey{projectID: link.ProjectID, platform: link.Platform}
		perf := groups[key]
		if perf == nil {
			perf = &ContentAssetPlatformPerformance{ProjectID: link.ProjectID, Platform: link.Platform, Metrics: map[string]any{}, Contents: []ContentAssetPlatformContent{}, Missing: []string{}}
			groups[key] = perf
		}
		perf.ContentCount++
		published := false
		if link.ContentKind == "wechat_publication" {
			publication, publicationErr := s.repo.WechatPublications().FindByID(ctx, link.ContentID)
			if publicationErr == nil && publication.UserID == userID && publication.ProjectID == link.ProjectID {
				published = publication.Status == model.WechatPublicationStatusPublished
			} else {
				perf.Missing = contentAssetAppendUniqueString(perf.Missing, "publication_status_unavailable")
			}
		} else if link.ContentKind == "seednote_post" {
			post, postErr := s.repo.SeednotePosts().FindByID(ctx, link.ProjectID, link.ContentID)
			published = postErr == nil && post != nil
		}
		if published {
			perf.PublishedCount++
		}
		perf.Contents = append(perf.Contents, ContentAssetPlatformContent{Kind: link.ContentKind, ID: link.ContentID, Published: published})
	}
	now := time.Now().UTC()
	for _, perf := range groups {
		family, familyErr := model.AnalyticsMetricFamilyForPlatform(perf.Platform)
		if familyErr != nil {
			perf.Missing = contentAssetAppendUniqueString(perf.Missing, "unsupported_platform_analytics")
			lineage.Summary.PublishedCount += perf.PublishedCount
			lineage.PlatformPerformance = append(lineage.PlatformPerformance, *perf)
			continue
		}
		state, stateErr := s.repo.Analytics().WithMetricFamily(family).State(ctx, perf.ProjectID)
		if stateErr != nil || state == nil || state.Status != "ready" || state.ActiveGeneration <= 0 {
			perf.Missing = contentAssetAppendUniqueString(perf.Missing, "analytics_projection_unavailable")
			lineage.Summary.PublishedCount += perf.PublishedCount
			lineage.PlatformPerformance = append(lineage.PlatformPerformance, *perf)
			continue
		}
		perf.AnalyticsRevision = state.Revision
		rows, _, rowsErr := s.repo.Analytics().WithMetricFamily(family).Contents(ctx, perf.ProjectID, state.ActiveGeneration, "cumulative", "1970-01-01", now.Format("2006-01-02"), repository.AnalyticsContentFilter{Limit: 100})
		if rowsErr != nil {
			perf.Missing = contentAssetAppendUniqueString(perf.Missing, "analytics_read_failed")
		} else {
			for _, link := range detail.Links {
				if link == nil || link.ProjectID != perf.ProjectID || link.Platform != perf.Platform {
					continue
				}
				for _, row := range rows {
					if row.ID == link.ContentID || row.TaskID == link.ContentID || row.PublicationID == link.ContentID || row.PostID == link.ContentID {
						metrics := row.AnalyticsMetrics.Map(family)
						if row.LastStatDate != "" {
							parsed, parseErr := time.Parse("2006-01-02", row.LastStatDate)
							if parseErr == nil && (perf.DataAsOf == nil || parsed.Before(*perf.DataAsOf)) {
								perf.DataAsOf = &parsed
							}
							for index := range perf.Contents {
								if perf.Contents[index].Kind == link.ContentKind && perf.Contents[index].ID == link.ContentID {
									perf.Contents[index].Metrics = metrics
									parsedCopy := parsed
									perf.Contents[index].DataAsOf = &parsedCopy
									break
								}
							}
						}
						for key, value := range metrics {
							if count, ok := numericAnalyticsValue(value); ok {
								perf.Metrics[key] = numericAnalyticsValueOrExisting(perf.Metrics[key], count)
							}
						}
						break
					}
				}
			}
		}
		if perf.DataAsOf == nil {
			perf.Missing = contentAssetAppendUniqueString(perf.Missing, "analytics_data_as_of_unavailable")
		}
		lineage.Summary.PublishedCount += perf.PublishedCount
		lineage.PlatformPerformance = append(lineage.PlatformPerformance, *perf)
	}
	return lineage, nil
}

func (s *ContentAssetService) GetLineageByContent(ctx context.Context, userID, projectID, contentKind, contentID string) ([]*ContentAssetLineage, error) {
	if strings.TrimSpace(contentKind) == "" || strings.TrimSpace(contentID) == "" {
		return nil, fmt.Errorf("content identity is required")
	}
	resolvedProject, _, err := s.validateLinkedContent(ctx, LinkContentAssetParams{UserID: userID, ProjectID: projectID, ContentKind: contentKind, ContentID: contentID})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) != "" && resolvedProject != projectID {
		return nil, fmt.Errorf("content project mismatch")
	}
	links, err := s.repo.ContentAssets().ListLinksByContent(ctx, userID, resolvedProject, contentKind, contentID)
	if err != nil {
		return nil, err
	}
	result := make([]*ContentAssetLineage, 0, len(links))
	seen := map[string]bool{}
	for _, link := range links {
		if link == nil || seen[link.AssetID] {
			continue
		}
		seen[link.AssetID] = true
		lineage, err := s.GetLineage(ctx, userID, link.AssetID)
		if err != nil {
			return nil, err
		}
		result = append(result, lineage)
	}
	return result, nil
}

func numericAnalyticsValue(value any) (float64, bool) {
	switch number := value.(type) {
	case int64:
		return float64(number), true
	case int:
		return float64(number), true
	case float64:
		return number, true
	default:
		return 0, false
	}
}

func numericAnalyticsValueOrExisting(existing any, value float64) any {
	if old, ok := numericAnalyticsValue(existing); ok {
		return old + value
	}
	return value
}

func contentAssetAppendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// ContentReuseContext is read-only input to one bound production task. Runtime
// callers cannot select a different asset or run through this capability.
type ContentReuseContext struct {
	*ContentAssetDetail
	Run    *model.ContentReuseRun `json:"run"`
	TaskID string                 `json:"task_id"`
}

func (s *ContentAssetService) GetReuseContext(ctx context.Context, userID, projectID, taskID string) (*ContentReuseContext, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("content reuse identity required")
	}
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID || task.ProjectID != projectID || task.ReuseRunID == "" {
		return nil, fmt.Errorf("content reuse context unavailable for task")
	}
	run, err := s.repo.ContentAssets().FindReuseRun(ctx, userID, task.ReuseRunID)
	if err != nil {
		return nil, err
	}
	if run.TargetProjectID != projectID || run.AssetID != task.ContentAssetID || (run.TaskID != "" && run.TaskID != taskID) {
		return nil, fmt.Errorf("content reuse task binding mismatch")
	}
	detail, err := s.Get(ctx, userID, run.AssetID)
	if err != nil {
		return nil, err
	}
	return &ContentReuseContext{ContentAssetDetail: detail, Run: run, TaskID: taskID}, nil
}
