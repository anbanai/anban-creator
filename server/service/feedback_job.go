package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
)

const (
	feedbackLeaseTTL = 2 * time.Hour
)

// ProcessFeedbackJob executes the deterministic feedback stage. An eventual
// LLM interpreter consumes the persisted insight; it is never invoked for a
// skipped or data-free job.
func ProcessFeedbackJob(ctx context.Context, repo repository.Repository, jobID string) error {
	if repo == nil || jobID == "" {
		return errors.New("feedback job identity is required")
	}
	job, err := repo.FeedbackLoop().FindJobByIDOrFingerprint(ctx, jobID)
	if err != nil {
		return fmt.Errorf("feedback job lookup failed: %w", err)
	}
	claimed, won, err := repo.FeedbackLoop().ClaimQueuedJob(ctx, job.ID, time.Now().UTC())
	if err != nil {
		return err
	}
	if !won {
		return nil
	}
	job = claimed
	defer func() { _ = repo.FeedbackLoop().UpdateJob(context.Background(), job) }()
	leaseScope := ""
	if strings.TrimSpace(job.AccountID) != "" {
		leaseScope = "feedback:account:" + job.AccountID
		acquired, leaseErr := repo.FeedbackLoop().AcquireFeedbackLease(ctx, leaseScope, job.ID, time.Now().UTC(), feedbackLeaseTTL)
		if leaseErr != nil {
			markFeedbackFailure(job, fmt.Errorf("account lease: %w", leaseErr))
			return leaseErr
		}
		if !acquired {
			job.Status = model.FeedbackJobQueued
			job.LastError = "account lease busy"
			if job.Attempts > 0 {
				job.Attempts--
			}
			return errors.New("feedback account lease busy")
		}
		defer func() { _ = repo.FeedbackLoop().ReleaseFeedbackLease(context.Background(), leaseScope, job.ID) }()
	}

	summary, evidenceData, err := loadFeedbackMetricSummary(ctx, repo, job)
	if err != nil {
		markFeedbackFailure(job, err)
		return err
	}
	if summary.SampleCount > 0 {
		job.SampleCount = summary.SampleCount
	}
	if summary.SampleCount == 0 {
		completed := time.Now().UTC()
		job.Status = model.FeedbackJobSkipped
		job.SkipReason = model.FeedbackSkipNoValidObservations
		job.CompletedAt = &completed
		job.LastError = ""
		return nil
	}
	if job.Operation == "data_tracker" {
		completed := time.Now().UTC()
		job.SampleCount = summary.SampleCount
		job.Status = model.FeedbackJobSucceeded
		job.CompletedAt = &completed
		job.LastError = ""
		return nil
	}
	evidenceData["coverage"] = job.Coverage
	evidenceData["period_start"] = job.PeriodStart
	evidenceData["period_end"] = job.PeriodEnd
	evidenceData["analytics_revision"] = job.AnalyticsRevision
	evidence, _ := json.Marshal(evidenceData)
	promotionStatus := model.FeedbackPromotionNone
	if strings.TrimSpace(job.TargetContentID) != "" && job.Operation == "content_postmortem" {
		promotionStatus = model.FeedbackPromotionCandidate
	}
	insight := &model.FeedbackInsight{ID: uuid.NewString(), JobID: job.ID, ProjectID: job.ProjectID, AnalyticsRevision: job.AnalyticsRevision, TargetContentID: job.TargetContentID, BaselineScope: "account_platform", Trigger: job.Trigger, Kind: job.Operation, EvidenceJSON: string(evidence), Summary: feedbackInsightSummary(job.Operation, summary), Confidence: confidenceForSample(job.SampleCount), Limitations: "Correlation does not establish causation; coverage depends on imported platform observations.", PromotionStatus: promotionStatus}
	if err := repo.FeedbackLoop().CreateInsight(ctx, insight); err != nil {
		markFeedbackFailure(job, err)
		return err
	}
	if job.Operation == "strategy_advisor" && job.Cadence == FeedbackCadenceMonthly && summary.SampleCount >= 10 {
		recommendationMap := feedbackRecommendations(summary)
		recommendations, _ := json.Marshal(recommendationMap)
		digestBytes := sha256.Sum256(recommendations)
		revision, err := repo.FeedbackLoop().NextStrategyRevision(ctx, job.ProjectID, job.Platform)
		if err != nil {
			markFeedbackFailure(job, err)
			return err
		}
		applicableTasks := []string{model.TaskTypeWechatArticle, model.TaskTypeWechatPicture, model.PlatformSeednote}
		if strings.TrimSpace(job.Platform) == model.PlatformSeednote {
			applicableTasks = []string{model.PlatformSeednote}
		}
		applicableJSON, _ := json.Marshal(applicableTasks)
		snapshot := &model.StrategySnapshot{ID: uuid.NewString(), ProjectID: job.ProjectID, Platform: job.Platform, Revision: revision, SourceRevision: job.AnalyticsRevision, Digest: hex.EncodeToString(digestBytes[:]), Status: "active", ApplicableTasks: string(applicableJSON), Recommendations: string(recommendations), Evidence: string(evidence), Confidence: confidenceForSample(job.SampleCount), Limitations: "Advisory only; review sample coverage before applying."}
		if err := repo.FeedbackLoop().CreateStrategy(ctx, snapshot); err != nil {
			markFeedbackFailure(job, err)
			return err
		}
	}
	completed := time.Now().UTC()
	job.Status = model.FeedbackJobSucceeded
	job.CompletedAt = &completed
	job.LastError = ""
	return nil
}

func markFeedbackFailure(job *model.FeedbackJob, err error) {
	if job == nil {
		return
	}
	if job.Attempts >= model.FeedbackMaxAttempts {
		job.Status = model.FeedbackJobBlocked
	} else {
		job.Status = model.FeedbackJobFailed
	}
	if err != nil {
		job.LastError = err.Error()
	}
}

// MarkManagedFeedbackFailure reconciles a managed feedback Task that reached a
// terminal failure before the normal feedback artifact finalizer could run.
// It is intentionally a no-op for already terminal jobs so a late retry cannot
// overwrite a successful or explicitly skipped result.
func MarkManagedFeedbackFailure(ctx context.Context, repo repository.Repository, jobID string, reason error) error {
	if repo == nil || strings.TrimSpace(jobID) == "" {
		return errors.New("feedback job identity is required")
	}
	job, err := repo.FeedbackLoop().FindJobByIDOrFingerprint(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status != model.FeedbackJobQueued && job.Status != model.FeedbackJobRunning {
		return nil
	}
	job.Attempts++
	markFeedbackFailure(job, reason)
	now := time.Now().UTC()
	job.CompletedAt = &now
	return repo.FeedbackLoop().UpdateJob(ctx, job)
}

func loadFeedbackMetricSummary(ctx context.Context, repo repository.Repository, job *model.FeedbackJob) (feedbackMetricSummary, map[string]any, error) {
	if repo == nil || job == nil || repo.Analytics() == nil {
		return feedbackMetricSummary{}, nil, errors.New("feedback analytics repository unavailable")
	}
	var rows []model.AnalyticsObservation
	query := repo.Analytics().DB().WithContext(ctx).
		Model(&model.AnalyticsObservation{}).
		Joins("JOIN analytics_contents ac ON ac.id = analytics_observations.content_id AND ac.project_id = analytics_observations.project_id").
		Where("analytics_observations.project_id = ? AND analytics_observations.stat_date BETWEEN ? AND ? AND analytics_observations.metric_basis IN ? AND analytics_observations.revoked_at IS NULL AND analytics_observations.content_id <> ''", job.ProjectID, job.PeriodStart, job.PeriodEnd, []string{"daily", "cumulative"})
	if strings.TrimSpace(job.MaturityCutoff) != "" {
		query = query.Where("ac.date IS NOT NULL AND date(ac.date) <= ?", job.MaturityCutoff)
	}
	err := query.
		Order("analytics_observations.content_id, analytics_observations.stat_date, analytics_observations.sequence").
		Find(&rows).Error
	if err != nil {
		return feedbackMetricSummary{}, nil, err
	}
	contentIDs := map[string]struct{}{}
	engagementSum := 0.0
	engagementCount := 0
	for _, row := range rows {
		if !hasValidFeedbackMetric(row, job.Platform) {
			continue
		}
		contentIDs[row.ContentID] = struct{}{}
		if rate, ok := feedbackEngagementRate(row, job.Platform); ok {
			engagementSum += rate
			engagementCount++
		}
	}
	summary := feedbackMetricSummary{SampleCount: len(contentIDs)}
	if engagementCount > 0 {
		summary.AverageEngagement = engagementSum / float64(engagementCount)
	}
	evidence := map[string]any{"sample_count": summary.SampleCount, "valid_observation_count": len(rows), "engagement_observation_count": engagementCount, "average_engagement_rate": math.Round(summary.AverageEngagement*1_000_000) / 1_000_000}
	return summary, evidence, nil
}

func hasValidFeedbackMetric(row model.AnalyticsObservation, platform string) bool {
	// Scheduled jobs store the account platform; content-attribution jobs
	// store their output channel. Resolve each explicit vocabulary here.
	family, err := model.AnalyticsMetricFamilyForPlatform(platform)
	if platform == model.ChannelArticle || platform == model.ChannelWechatPicture {
		family, err = model.AnalyticsMetricsWechat, nil
	}
	if err != nil {
		return false
	}
	for _, column := range model.AnalyticsMetricColumns() {
		if family.Contains(column) && feedbackMetricPresent(row, column) {
			return true
		}
	}
	return false
}

func feedbackMetricPresent(row model.AnalyticsObservation, column string) bool {
	metrics := row.AnalyticsMetrics
	switch column {
	case "delivered_users":
		return metrics.DeliveredUsers != nil
	case "read_users":
		return metrics.ReadUsers != nil
	case "share_users":
		return metrics.ShareUsers != nil
	case "collection_users":
		return metrics.CollectionUsers != nil
	case "like_users":
		return metrics.LikeUsers != nil
	case "zaikan_users":
		return metrics.ZaikanUsers != nil
	case "comment_count":
		return metrics.CommentCount != nil
	case "read_to_follow_users":
		return metrics.ReadToFollowUsers != nil
	case "exposure_count":
		return metrics.ExposureCount != nil
	case "view_count":
		return metrics.ViewCount != nil
	case "like_count":
		return metrics.LikeCount != nil
	case "collect_count":
		return metrics.CollectCount != nil
	case "follower_gain_count":
		return metrics.FollowerGainCount != nil
	case "share_count":
		return metrics.ShareCount != nil
	case "barrage_count":
		return metrics.BarrageCount != nil
	case "read_completion_rate":
		return metrics.ReadCompletionRate != nil
	case "average_read_active_time":
		return metrics.AverageReadActiveTime != nil
	case "cover_click_rate":
		return metrics.CoverClickRate != nil
	case "avg_watch_duration":
		return metrics.AvgWatchDuration != nil
	case "delivery_completion_rate":
		return metrics.DeliveryCompletionRate != nil
	default:
		return false
	}
}

func feedbackEngagementRate(row model.AnalyticsObservation, platform string) (float64, bool) {
	var denominator, numerator int64
	if platform == model.PlatformWechat || platform == model.ScopeWechat {
		if row.ReadUsers == nil || *row.ReadUsers <= 0 {
			return 0, false
		}
		denominator = *row.ReadUsers
		if row.ShareUsers != nil {
			numerator += *row.ShareUsers
		}
		if row.CollectionUsers != nil {
			numerator += *row.CollectionUsers
		}
		if row.CommentCount != nil {
			numerator += *row.CommentCount
		}
	} else {
		if row.ViewCount == nil || *row.ViewCount <= 0 {
			return 0, false
		}
		denominator = *row.ViewCount
		if row.LikeCount != nil {
			numerator += *row.LikeCount
		}
		if row.CollectCount != nil {
			numerator += *row.CollectCount
		}
		if row.CommentCount != nil {
			numerator += *row.CommentCount
		}
	}
	return float64(numerator) / float64(denominator), true
}

func feedbackInsightSummary(operation string, summary feedbackMetricSummary) string {
	return fmt.Sprintf("%s produced %d valid content observations with %.2f%% average observed engagement.", strings.ReplaceAll(operation, "_", " "), summary.SampleCount, summary.AverageEngagement*100)
}

func confidenceForSample(sample int) string {
	if sample >= 30 {
		return "high"
	}
	if sample >= 10 {
		return "medium"
	}
	if sample > 0 {
		return "low"
	}
	return "unknown"
}
