package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	AttributionDirectionHigh = "high"
	AttributionDirectionLow  = "low"
	attributionMinimumSample = 10
	// AttributionOutlierFraction is the fixed first-version decile threshold.
	AttributionOutlierFraction = 0.10
)

type AttributionPerformanceSample struct {
	ContentID string
	Score     float64
}

type AttributionOutlier struct {
	ContentID  string
	Score      float64
	Percentile float64
	Direction  string
}

func buildAttributionCandidateJobs(ctx context.Context, repo repository.Repository, scope feedbackProjectScope, at time.Time, state *model.AnalyticsState) ([]model.FeedbackJob, error) {
	if repo == nil || repo.Analytics() == nil || scope.Project == nil || state == nil {
		return nil, errors.New("attribution candidate context unavailable")
	}
	cutoff := feedbackMaturityCutoffForProject(at, scope.Timezone, 7*24*time.Hour)
	start := feedbackMaturityCutoffForProject(at, scope.Timezone, 97*24*time.Hour)
	db := repo.Analytics().DB().WithContext(ctx)
	var contents []model.AnalyticsContent
	contentQuery := scopeFeedbackContents(db.Model(&model.AnalyticsContent{}), "", scope.Channel).
		Where("project_id = ? AND date IS NOT NULL AND date(date) BETWEEN ? AND ?", scope.ID, start, cutoff).
		Order("date asc, id asc")
	switch scope.Channel {
	case model.ChannelArticle, model.ChannelWechatPicture:
		contentQuery = contentQuery.Where(`EXISTS (SELECT 1 FROM wechat_publications wp WHERE wp.project_id = analytics_contents.project_id AND (wp.id = analytics_contents.publication_id OR (analytics_contents.task_id <> '' AND wp.task_id = analytics_contents.task_id)) AND wp.status = ?)`, model.WechatPublicationStatusPublished)
	case model.ChannelSeednote:
		contentQuery = contentQuery.Where(`analytics_contents.post_id <> '' AND EXISTS (SELECT 1 FROM seednote_posts sp WHERE sp.id = analytics_contents.post_id AND sp.project_id = analytics_contents.project_id AND sp.first_published_at IS NOT NULL AND (sp.first_published_at_batch_id IS NULL OR sp.first_published_at_batch_id = '' OR EXISTS (SELECT 1 FROM seednote_import_batches sb WHERE sb.id = sp.first_published_at_batch_id AND sb.revoked_at IS NULL)))`)
	}
	if err := contentQuery.Find(&contents).Error; err != nil {
		return nil, err
	}
	contentByID := make(map[string]model.AnalyticsContent, len(contents))
	ids := make([]string, 0, len(contents))
	for _, content := range contents {
		contentByID[content.ID] = content
		ids = append(ids, content.ID)
	}
	var observations []model.AnalyticsObservation
	if len(ids) > 0 {
		if err := db.Where("project_id = ? AND content_id IN ? AND revoked_at IS NULL AND metric_basis = ? AND stat_date <= ?", scope.ID, ids, "cumulative", at.Format("2006-01-02")).
			Order("content_id asc, metric_basis asc, stat_date desc, source_priority desc, effective_at desc, received_at desc, sequence desc").Find(&observations).Error; err != nil {
			return nil, err
		}
	}
	latest := map[string]model.AnalyticsObservation{}
	for _, observation := range observations {
		key := observation.ContentID + "\x00" + observation.MetricBasis
		if _, ok := latest[key]; !ok {
			latest[key] = observation
		}
	}
	baselineItems := make([]model.FeedbackBaselineItem, 0, len(latest))
	samples := make([]AttributionPerformanceSample, 0, len(latest))
	for _, observation := range latest {
		content, ok := contentByID[observation.ContentID]
		if !ok || content.Date == nil || !analyticsObservationMatured(observation, content.Date, scope.Timezone, 7*24*time.Hour) {
			continue
		}
		score, ok := attributionEngagementRate(observation, scope.Channel)
		if !ok {
			continue
		}
		baselineItems = append(baselineItems, model.FeedbackBaselineItem{Content: content, Observation: observation, Score: score})
		samples = append(samples, AttributionPerformanceSample{ContentID: observation.ContentID, Score: score})
	}
	basis := "cumulative"
	sort.Slice(baselineItems, func(i, j int) bool { return baselineItems[i].Content.ID < baselineItems[j].Content.ID })
	idsForDigest := make([]string, 0, len(samples))
	for _, sample := range samples {
		idsForDigest = append(idsForDigest, sample.ContentID)
	}
	sort.Strings(idsForDigest)
	digestSum := sha256.Sum256([]byte(strings.Join(idsForDigest, "\x00")))
	digest := hex.EncodeToString(digestSum[:])
	periodStart := start
	periodEnd := at.Format("2006-01-02")
	base := model.FeedbackJob{
		ID: uuid.NewString(), UserID: scope.UserID, ProjectID: scope.ID, Platform: scope.Channel, AccountID: scope.AccountID,
		Operation: "content_postmortem", Cadence: FeedbackCadenceWeekly, PeriodStart: periodStart, PeriodEnd: periodEnd,
		MaturityCutoff: cutoff, AnalyticsRevision: state.Revision, ContentSetDigest: digest,
		ProfileRevision: projectProfileFor(scope.Project).Version, PerformanceMetric: "engagement_rate", PerformanceMetricBasis: basis,
		SampleCount: len(samples), Coverage: "partial", Trigger: FeedbackTriggerAutoAttribution,
		Status: model.FeedbackJobQueued,
	}
	if len(samples) < attributionMinimumSample {
		base.Status = model.FeedbackJobSkipped
		base.SkipReason = model.FeedbackSkipSampleTooSmall
		if len(samples) == 0 {
			immature, err := countPublishedImmatureAttributionContents(ctx, db, scope, cutoff, at)
			if err != nil {
				return nil, err
			}
			if immature > 0 {
				base.SkipReason = model.FeedbackSkipNoMatureContent
			}
		}
		base.Trigger = FeedbackCadenceWeekly
		base.Fingerprint = FeedbackJobFingerprint(FeedbackEligibilityInput{ProjectID: base.ProjectID, Platform: base.Platform, AccountID: base.AccountID, Operation: base.Operation, Cadence: base.Cadence, PeriodStart: base.PeriodStart, PeriodEnd: base.PeriodEnd, AnalyticsRevision: base.AnalyticsRevision, ContentSetDigest: base.ContentSetDigest, StrategyRevision: base.StrategyRevision})
		return []model.FeedbackJob{base}, nil
	}
	outliers, eligible := selectAttributionOutliers(samples)
	if !eligible || len(outliers) == 0 {
		base.Status = model.FeedbackJobSkipped
		base.SkipReason = model.FeedbackSkipNoPerformanceOutlier
		base.Trigger = FeedbackCadenceWeekly
		base.Fingerprint = FeedbackJobFingerprint(FeedbackEligibilityInput{ProjectID: base.ProjectID, Platform: base.Platform, AccountID: base.AccountID, Operation: base.Operation, Cadence: base.Cadence, PeriodStart: base.PeriodStart, PeriodEnd: base.PeriodEnd, AnalyticsRevision: base.AnalyticsRevision, ContentSetDigest: base.ContentSetDigest, StrategyRevision: base.StrategyRevision})
		return []model.FeedbackJob{base}, nil
	}
	if active, err := repo.FeedbackLoop().FindActiveStrategy(ctx, scope.ID, scope.Channel); err != nil {
		return nil, err
	} else if active != nil {
		base.StrategyRevision = active.Revision
	}
	jobs := make([]model.FeedbackJob, 0, len(outliers))
	for _, outlier := range outliers {
		job := base
		job.ID = uuid.NewString()
		job.TargetContentID = outlier.ContentID
		job.AttributionDirection = outlier.Direction
		job.PerformanceScore = outlier.Score
		job.PerformancePercentile = outlier.Percentile
		job.Trigger = FeedbackTriggerAutoAttribution
		baseline := make([]model.FeedbackBaselineItem, 0, len(baselineItems)-1)
		for _, item := range baselineItems {
			if item.Content.ID != outlier.ContentID {
				baseline = append(baseline, item)
			}
		}
		frozen := model.FeedbackFrozenContext{SchemaVersion: "1.0", AnalyticsRevision: state.Revision, ProfileRevision: base.ProfileRevision, TargetContent: contentByID[outlier.ContentID], TargetObservations: maturedTargetObservations(observations, contentByID[outlier.ContentID], scope.Timezone, at), Baseline: baseline, ProjectProfile: projectProfileFor(scope.Project)}
		frozenJSON, err := json.Marshal(frozen)
		if err != nil {
			return nil, fmt.Errorf("marshal frozen attribution context: %w", err)
		}
		job.FrozenContextJSON = model.NullableJSON(string(frozenJSON))
		job.Fingerprint = FeedbackJobFingerprint(FeedbackEligibilityInput{ProjectID: job.ProjectID, Platform: job.Platform, AccountID: job.AccountID, Operation: job.Operation, Cadence: job.Cadence, TargetContentID: job.TargetContentID, AttributionDirection: job.AttributionDirection, PeriodStart: job.PeriodStart, PeriodEnd: job.PeriodEnd, AnalyticsRevision: job.AnalyticsRevision, ContentSetDigest: job.ContentSetDigest, StrategyRevision: job.StrategyRevision})
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func countPublishedImmatureAttributionContents(ctx context.Context, db *gorm.DB, scope feedbackProjectScope, cutoff string, at time.Time) (int64, error) {
	if db == nil {
		return 0, nil
	}
	q := scopeFeedbackContents(db.WithContext(ctx).Model(&model.AnalyticsContent{}), "", scope.Channel).
		Where("project_id = ? AND date IS NOT NULL AND date(date) > ? AND date(date) <= ?", scope.ID, cutoff, at.Format("2006-01-02"))
	switch scope.Channel {
	case model.ChannelArticle, model.ChannelWechatPicture:
		q = q.Where(`EXISTS (SELECT 1 FROM wechat_publications wp WHERE wp.project_id = analytics_contents.project_id AND (wp.id = analytics_contents.publication_id OR (analytics_contents.task_id <> '' AND wp.task_id = analytics_contents.task_id)) AND wp.status = ?)`, model.WechatPublicationStatusPublished)
	case model.ChannelSeednote:
		q = q.Where(`analytics_contents.post_id <> '' AND EXISTS (SELECT 1 FROM seednote_posts sp WHERE sp.id = analytics_contents.post_id AND sp.project_id = analytics_contents.project_id AND sp.first_published_at IS NOT NULL AND (sp.first_published_at_batch_id IS NULL OR sp.first_published_at_batch_id = '' OR EXISTS (SELECT 1 FROM seednote_import_batches sb WHERE sb.id = sp.first_published_at_batch_id AND sb.revoked_at IS NULL)))`)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func analyticsObservationMatured(observation model.AnalyticsObservation, publishedAt *time.Time, timezone string, age time.Duration) bool {
	if publishedAt == nil {
		return false
	}
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return observation.StatDate >= publishedAt.In(location).Add(age).Format("2006-01-02")
}

func maturedTargetObservations(observations []model.AnalyticsObservation, content model.AnalyticsContent, timezone string, at time.Time) []model.AnalyticsObservation {
	result := make([]model.AnalyticsObservation, 0)
	for _, observation := range observations {
		if observation.ContentID == content.ID && observation.StatDate <= at.Format("2006-01-02") && analyticsObservationMatured(observation, content.Date, timezone, 7*24*time.Hour) {
			result = append(result, observation)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StatDate < result[j].StatDate })
	return result
}

func attributionEngagementRate(row model.AnalyticsObservation, channel string) (float64, bool) {
	if channel == model.ChannelArticle || channel == model.ChannelWechatPicture {
		if row.ReadUsers == nil || *row.ReadUsers <= 0 || (row.ShareUsers == nil && row.CollectionUsers == nil && row.CommentCount == nil && row.LikeUsers == nil) {
			return 0, false
		}
		var interactions int64
		for _, value := range []*int64{row.ShareUsers, row.CollectionUsers, row.CommentCount, row.LikeUsers} {
			if value != nil {
				interactions += *value
			}
		}
		return float64(interactions) / float64(*row.ReadUsers), true
	}
	if row.ViewCount == nil || *row.ViewCount <= 0 || (row.LikeCount == nil && row.CollectCount == nil && row.CommentCount == nil && row.ShareCount == nil) {
		return 0, false
	}
	var interactions int64
	for _, value := range []*int64{row.LikeCount, row.CollectCount, row.CommentCount, row.ShareCount} {
		if value != nil {
			interactions += *value
		}
	}
	return float64(interactions) / float64(*row.ViewCount), true
}

// selectAttributionOutliers selects at most one high and one low content from
// a project/platform cohort. Scores must already be normalized to the same
// platform metric. Equal boundary scores are omitted to avoid manufacturing a
// causal signal from an arbitrary tie.
func selectAttributionOutliers(samples []AttributionPerformanceSample) ([]AttributionOutlier, bool) {
	if len(samples) < attributionMinimumSample {
		return nil, false
	}
	ordered := append([]AttributionPerformanceSample(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Score == ordered[j].Score {
			return ordered[i].ContentID < ordered[j].ContentID
		}
		return ordered[i].Score < ordered[j].Score
	})
	lowIndex := int(float64(len(ordered)-1) * AttributionOutlierFraction)
	highIndex := len(ordered) - 1 - lowIndex
	if ordered[lowIndex].Score == ordered[highIndex].Score {
		return nil, true
	}
	low := ordered[lowIndex]
	high := ordered[highIndex]
	return []AttributionOutlier{
		{ContentID: high.ContentID, Score: high.Score, Percentile: float64(highIndex) / float64(len(ordered)-1), Direction: AttributionDirectionHigh},
		{ContentID: low.ContentID, Score: low.Score, Percentile: float64(lowIndex) / float64(len(ordered)-1), Direction: AttributionDirectionLow},
	}, true
}
