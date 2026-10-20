package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
)

// ContentAssetScoreInput contains normalized, platform-local signals. Values
// are percentages from 0 to 100 and must never mix raw metrics from platforms.
type ContentAssetScoreInput struct {
	PlatformPerformance float64
	AccountFit          float64
	Transferability     float64
	Freshness           float64
	AngleSpace          float64
	Cost                float64
	Risk                float64
}

type ContentAssetScore struct {
	PlatformPerformance float64 `json:"platform_performance"`
	AccountFit          float64 `json:"account_fit"`
	Transferability     float64 `json:"transferability"`
	Freshness           float64 `json:"freshness"`
	AngleSpace          float64 `json:"angle_space"`
	Cost                float64 `json:"cost"`
	Risk                float64 `json:"risk"`
	Score               float64 `json:"score"`
	Confidence          string  `json:"confidence"`
}

func ScoreContentAsset(in ContentAssetScoreInput) ContentAssetScore {
	clamp := func(value float64) float64 {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return 0
		}
		if value > 100 {
			return 100
		}
		return value
	}
	in.PlatformPerformance, in.AccountFit, in.Transferability = clamp(in.PlatformPerformance), clamp(in.AccountFit), clamp(in.Transferability)
	in.Freshness, in.AngleSpace, in.Cost, in.Risk = clamp(in.Freshness), clamp(in.AngleSpace), clamp(in.Cost), clamp(in.Risk)
	// The score is platform-local: platform performance is an input already
	// normalized against that platform's own account history. A weighted
	// geometric mean preserves the interaction in the product requirement and
	// makes any zero core factor ineligible instead of allowing another signal
	// to hide it.
	factors := []float64{
		in.PlatformPerformance,
		in.AccountFit,
		in.Transferability,
		in.Freshness,
		in.AngleSpace,
		100 - in.Cost,
		100 - in.Risk,
	}
	weights := []float64{0.30, 0.20, 0.18, 0.12, 0.10, 0.06, 0.04}
	base := 0.0
	eligible := true
	for i, factor := range factors {
		if factor <= 0 {
			eligible = false
			break
		}
		base += weights[i] * math.Log(factor/100)
	}
	if eligible {
		base = 100 * math.Exp(base)
	} else {
		base = 0
	}
	confidence := "low"
	if in.Freshness >= 60 && in.PlatformPerformance >= 60 {
		confidence = "medium"
	}
	if in.Freshness >= 80 && in.PlatformPerformance >= 75 && in.AccountFit >= 70 {
		confidence = "high"
	}
	if !eligible {
		confidence = "low"
	}
	return ContentAssetScore{PlatformPerformance: in.PlatformPerformance, AccountFit: in.AccountFit, Transferability: in.Transferability, Freshness: in.Freshness, AngleSpace: in.AngleSpace, Cost: in.Cost, Risk: in.Risk, Score: base, Confidence: confidence}
}

type contentAssetScoreEvidence struct {
	ContentID  string  `json:"content_id"`
	Metric     string  `json:"metric"`
	Value      float64 `json:"value"`
	Percentile float64 `json:"percentile"`
}

// ScoreAssetForProject creates an immutable recommendation snapshot using only
// the target project's platform-local analytics projection. Dimensions without
// authoritative inputs remain explicitly missing and prevent a recommendation.
func (s *ContentAssetService) ScoreAssetForProject(ctx context.Context, userID, assetID, projectID string) (*model.ContentAssetScoreSnapshot, error) {
	asset, err := s.repo.ContentAssets().FindByID(ctx, userID, assetID)
	if err != nil {
		return nil, ErrContentAssetNotFound
	}
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil || project.UserID != userID {
		return nil, fmt.Errorf("project does not belong to user")
	}
	family, familyErr := model.AnalyticsMetricFamilyForPlatform(project.Platform)
	now := time.Now().UTC()
	missing := []string{}
	platformPerformance := 0.0
	var dataAsOf *time.Time
	var revision int64
	evidence := []contentAssetScoreEvidence{}
	metric := ""
	dimensions := map[string]float64{}
	assetEvidence, evidenceErr := s.repo.ContentAssets().ListEvidence(ctx, userID, assetID)
	if evidenceErr != nil {
		return nil, fmt.Errorf("read content asset evidence: %w", evidenceErr)
	}
	for _, item := range assetEvidence {
		if item == nil || item.Quality == "unverified" || (item.Platform != "" && item.Platform != project.Platform) {
			continue
		}
		var metrics map[string]any
		if len(item.Metrics) == 0 || json.Unmarshal(item.Metrics, &metrics) != nil {
			continue
		}
		used := false
		for _, key := range []string{"account_fit", "transferability", "angle_space", "production_cost", "cost", "compliance_risk", "risk"} {
			if _, exists := dimensions[key]; exists {
				continue
			}
			if value := assetAnalyticsNumber(metrics[key]); value != nil && *value <= 100 {
				dimensions[key] = *value
				used = true
			}
		}
		if used {
			if item.DataAsOf == nil || item.DataAsOf.After(now) {
				missing = append(missing, "dimension_evidence_date_unavailable")
			} else if dataAsOf == nil || item.DataAsOf.Before(*dataAsOf) {
				dataAsOf = item.DataAsOf
			}
		}
	}
	sourcePerformance, sourceErr := s.sourcePlatformPerformance(ctx, userID, assetID, projectID, now)
	if sourceErr != nil {
		return nil, fmt.Errorf("read source platform performance: %w", sourceErr)
	}
	if familyErr != nil {
		missing = append(missing, "unsupported_platform_analytics")
	} else {
		analytics := s.repo.Analytics().WithMetricFamily(family)
		state, stateErr := analytics.State(ctx, projectID)
		if stateErr != nil {
			return nil, fmt.Errorf("read analytics state: %w", stateErr)
		}
		revision = state.Revision
		if state.Status != "ready" || state.ActiveGeneration <= 0 {
			missing = append(missing, "analytics_projection_unavailable")
		} else {
			if family == model.AnalyticsMetricsWechat {
				metric = "read_users"
			} else {
				metric = "view_count"
			}
			from, to := now.AddDate(0, 0, -90).Format("2006-01-02"), now.Format("2006-01-02")
			allRows := []repository.AnalyticsContentRow{}
			for offset := 0; ; offset += 100 {
				page, total, queryErr := analytics.Contents(ctx, projectID, state.ActiveGeneration, "cumulative", from, to, repository.AnalyticsContentFilter{Sort: metric, Direction: "desc", Offset: offset, Limit: 100})
				if queryErr != nil {
					return nil, fmt.Errorf("read platform-local analytics: %w", queryErr)
				}
				allRows = append(allRows, page...)
				if len(allRows) >= int(total) || len(page) == 0 {
					break
				}
				if len(allRows) >= 1000 {
					missing = append(missing, "historical_baseline_over_limit")
					break
				}
			}
			assetLinks, linkErr := s.repo.ContentAssets().ListLinks(ctx, userID, assetID)
			if linkErr != nil {
				return nil, fmt.Errorf("read asset lineage for scoring: %w", linkErr)
			}
			linked := map[string]bool{}
			for _, link := range assetLinks {
				if link.ProjectID != projectID {
					continue
				}
				switch link.ContentKind {
				case "task", "video_task":
					linked["task:"+link.ContentID] = true
				case "seednote_post":
					linked["seednote_post:"+link.ContentID] = true
				case "wechat_publication":
					for _, row := range allRows {
						if row.PublicationID == link.ContentID {
							linked[row.ID] = true
						}
					}
				}
			}
			values := make([]float64, 0, len(allRows))
			assetValues := make([]repository.AnalyticsContentRow, 0)
			for _, row := range allRows {
				value := assetAnalyticsNumber(row.AnalyticsMetrics.Map(family)[metric])
				if value == nil {
					continue
				}
				values = append(values, *value)
				if linked[row.ID] || (row.TaskID != "" && linked["task:"+row.TaskID]) || (row.PostID != "" && linked["seednote_post:"+row.PostID]) {
					assetValues = append(assetValues, row)
				}

			}
			if len(values) < 3 {
				missing = append(missing, "platform_history_insufficient")
			}
			if len(assetValues) == 0 {
				missing = append(missing, "asset_has_no_target_platform_analytics")
			} else if len(values) >= 3 {
				bestPercentile := 0.0
				for _, row := range assetValues {
					value := assetAnalyticsNumber(row.AnalyticsMetrics.Map(family)[metric])
					if value == nil {
						continue
					}
					parsed, parseErr := time.Parse("2006-01-02", row.LastStatDate)
					if parseErr != nil || parsed.After(now) {
						missing = append(missing, "linked_analytics_date_unavailable")
					} else if dataAsOf == nil || parsed.Before(*dataAsOf) {
						dataAsOf = &parsed
					}
					below := 0
					for _, baseline := range values {
						if baseline <= *value {
							below++
						}
					}
					percentile := 100 * float64(below) / float64(len(values))
					if percentile > bestPercentile {
						bestPercentile = percentile
					}
					evidence = append(evidence, contentAssetScoreEvidence{ContentID: row.ID, Metric: metric, Value: *value, Percentile: percentile})
				}
				platformPerformance = bestPercentile
			}
		}
	}
	accountFit, accountFitOK := dimensions["account_fit"]
	transferability, transferabilityOK := dimensions["transferability"]
	angleSpace, angleSpaceOK := dimensions["angle_space"]
	cost, costOK := dimensions["production_cost"]
	if !costOK {
		cost, costOK = dimensions["cost"]
	}
	risk, riskOK := dimensions["compliance_risk"]
	if !riskOK {
		risk, riskOK = dimensions["risk"]
	}
	if !accountFitOK {
		missing = append(missing, "account_fit")
	}
	if !transferabilityOK {
		missing = append(missing, "transferability")
	}
	if !angleSpaceOK {
		missing = append(missing, "angle_space")
	}
	if !costOK {
		missing = append(missing, "production_cost")
	}
	if !riskOK {
		missing = append(missing, "compliance_risk")
	}
	score := ScoreContentAsset(ContentAssetScoreInput{PlatformPerformance: platformPerformance, AccountFit: accountFit, Transferability: transferability, Freshness: freshnessScore(dataAsOf, now), AngleSpace: angleSpace, Cost: cost, Risk: risk})
	status := "data_insufficient"
	if len(missing) == 0 {
		status = "scored"
	} else {
		score.Score, score.Confidence = 0, "low"
	}
	// A source-only signal can guide exploration, but it is not target-platform
	// performance. Keep it out of autopilot's "scored" admission contract and
	// cap its visible score below the normal recommendation threshold.
	if status == "data_insufficient" && len(sourcePerformance) > 0 {
		bestSourcePercentile := 0.0
		for _, source := range sourcePerformance {
			if source.Percentile > bestSourcePercentile {
				bestSourcePercentile = source.Percentile
			}
			if source.DataAsOf != nil && (dataAsOf == nil || source.DataAsOf.Before(*dataAsOf)) {
				dataAsOf = source.DataAsOf
			}
		}
		if bestSourcePercentile > 0 {
			status = "exploratory"
			score.Score = math.Min(49, bestSourcePercentile*0.49)
			score.Confidence = "low"
		}
	}
	encoded, err := json.Marshal(map[string]any{
		"schema_version": 1, "platform": project.Platform, "platform_performance": platformPerformance,
		"metric": metric, "evidence": evidence, "source_platform_performance": sourcePerformance, "missing": missing, "as_of": dataAsOf,
		"account_fit": accountFit, "transferability": transferability, "angle_space": angleSpace,
		"production_cost": cost, "compliance_risk": risk,
		"recommendation_reason": scoreReason(status, evidence, missing),
	})
	if err != nil {
		return nil, fmt.Errorf("encode score snapshot: %w", err)
	}
	snapshot := &model.ContentAssetScoreSnapshot{ID: uuid.NewString(), AssetID: asset.ID, UserID: userID, ProjectID: projectID, Platform: project.Platform, AnalyticsRevision: revision, Status: status, Score: score.Score, Confidence: score.Confidence, DataAsOf: dataAsOf, Snapshot: encoded, CreatedAt: now}
	if err := s.repo.ContentAssets().CreateScoreSnapshot(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("persist score snapshot: %w", err)
	}
	return snapshot, nil
}

type sourcePlatformPerformance struct {
	Platform          string     `json:"platform"`
	ProjectID         string     `json:"project_id"`
	AnalyticsRevision int64      `json:"analytics_revision"`
	Metric            string     `json:"metric"`
	Percentile        float64    `json:"percentile"`
	DataAsOf          *time.Time `json:"data_as_of,omitempty"`
	ContentIDs        []string   `json:"content_ids"`
}

func (s *ContentAssetService) sourcePlatformPerformance(ctx context.Context, userID, assetID, targetProjectID string, now time.Time) ([]sourcePlatformPerformance, error) {
	links, err := s.repo.ContentAssets().ListLinks(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}
	byProject := map[string][]*model.ContentAssetLink{}
	projectOrder := []string{}
	for _, link := range links {
		if link == nil || link.ProjectID == "" || link.ProjectID == targetProjectID {
			continue
		}
		if _, found := byProject[link.ProjectID]; !found {
			projectOrder = append(projectOrder, link.ProjectID)
		}
		byProject[link.ProjectID] = append(byProject[link.ProjectID], link)
	}
	results := make([]sourcePlatformPerformance, 0, len(projectOrder))
	for _, projectID := range projectOrder {
		project, err := s.repo.Projects().FindByID(ctx, projectID)
		if err != nil || project.UserID != userID {
			continue
		}
		family, err := model.AnalyticsMetricFamilyForPlatform(project.Platform)
		if err != nil {
			continue
		}
		analytics := s.repo.Analytics().WithMetricFamily(family)
		state, err := analytics.State(ctx, projectID)
		if err != nil || state == nil || state.Status != "ready" || state.ActiveGeneration <= 0 || state.Revision <= 0 {
			continue
		}
		metric := "read_users"
		if family == model.AnalyticsMetricsSeednote {
			metric = "view_count"
		}
		from, to := now.AddDate(0, 0, -90).Format("2006-01-02"), now.Format("2006-01-02")
		rows := []repository.AnalyticsContentRow{}
		for offset := 0; ; offset += 100 {
			page, total, err := analytics.Contents(ctx, projectID, state.ActiveGeneration, "cumulative", from, to, repository.AnalyticsContentFilter{Sort: metric, Direction: "desc", Offset: offset, Limit: 100})
			if err != nil {
				return nil, err
			}
			rows = append(rows, page...)
			if offset+len(page) >= int(total) || len(page) == 0 || len(rows) >= 1000 {
				break
			}
		}
		values := []float64{}
		for _, row := range rows {
			if value := assetAnalyticsNumber(row.AnalyticsMetrics.Map(family)[metric]); value != nil {
				values = append(values, *value)
			}
		}
		if len(values) < 3 {
			continue
		}
		linkedValues := []float64{}
		contentIDs := []string{}
		var dataAsOf *time.Time
		seenContent := map[string]bool{}
		for _, row := range rows {
			linked := false
			for _, link := range byProject[projectID] {
				if row.ID == link.ContentID || row.TaskID == link.ContentID || row.PublicationID == link.ContentID || row.PostID == link.ContentID {
					linked = true
					break
				}
			}
			value := assetAnalyticsNumber(row.AnalyticsMetrics.Map(family)[metric])
			if !linked || value == nil {
				continue
			}
			linkedValues = append(linkedValues, *value)
			if !seenContent[row.ID] {
				seenContent[row.ID] = true
				contentIDs = append(contentIDs, row.ID)
			}
			parsed, parseErr := time.Parse("2006-01-02", row.LastStatDate)
			if parseErr == nil && !parsed.After(now) && (dataAsOf == nil || parsed.Before(*dataAsOf)) {
				dataAsOf = &parsed
			}
		}
		if len(linkedValues) == 0 {
			continue
		}
		bestPercentile := 0.0
		for _, value := range linkedValues {
			below := 0
			for _, baseline := range values {
				if baseline <= value {
					below++
				}
			}
			if percentile := 100 * float64(below) / float64(len(values)); percentile > bestPercentile {
				bestPercentile = percentile
			}
		}
		results = append(results, sourcePlatformPerformance{Platform: project.Platform, ProjectID: projectID, AnalyticsRevision: state.Revision, Metric: metric, Percentile: bestPercentile, DataAsOf: dataAsOf, ContentIDs: contentIDs})
	}
	return results, nil
}

func freshnessScore(asOf *time.Time, now time.Time) float64 {
	if asOf == nil || asOf.After(now) {
		return 0
	}
	days := now.Sub(*asOf).Hours() / 24
	if days <= 1 {
		return 100
	}
	if days >= 90 {
		return 0
	}
	return 100 * (90 - days) / 89
}

func assetAnalyticsNumber(value any) *float64 {
	var number float64
	switch typed := value.(type) {
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case float64:
		number = typed
	case json.Number:
		parsed, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil {
			return nil
		}
		number = parsed
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return nil
		}
		number = parsed
	default:
		return nil
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return nil
	}
	return &number
}

func scoreReason(status string, evidence []contentAssetScoreEvidence, missing []string) string {
	if status == "scored" {
		return fmt.Sprintf("基于 %d 条目标平台分析记录计算平台内表现百分位", len(evidence))
	}
	return fmt.Sprintf("数据不足，缺少：%v；已获得 %d 条目标平台证据", missing, len(evidence))
}
