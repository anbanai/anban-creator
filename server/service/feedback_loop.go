package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func feedbackStrategyUsable(snapshot *model.StrategySnapshot, taskType, platform string, now time.Time) bool {
	if snapshot == nil || snapshot.Status != "active" || strings.TrimSpace(taskType) == "" || strings.TrimSpace(platform) == "" || snapshot.Platform != platform {
		return false
	}
	if snapshot.ExpiresAt != nil && !snapshot.ExpiresAt.After(now) {
		return false
	}
	var applicable []string
	if err := json.Unmarshal([]byte(snapshot.ApplicableTasks), &applicable); err != nil {
		return false
	}
	for _, value := range applicable {
		if value == taskType {
			return true
		}
	}
	return false
}

const (
	FeedbackCadenceDaily            = "daily"
	FeedbackCadenceWeekly           = "weekly"
	FeedbackCadenceMonthly          = "monthly"
	FeedbackJobEligible             = "eligible"
	FeedbackJobSkipped              = "skipped"
	FeedbackSkipNoNewRevision       = "no_new_revision"
	FeedbackSkipNoMatureContent     = "no_mature_content"
	FeedbackSkipNoValidObservations = "no_valid_observations"
	FeedbackSkipSampleTooSmall      = "sample_too_small"
	FeedbackSkipProjectPaused       = "project_paused"
	FeedbackSkipAnalyticsRebuilding = "analytics_rebuilding"
)

type feedbackMetricSummary struct {
	SampleCount       int
	AverageEngagement float64
}

func feedbackRecommendations(summary feedbackMetricSummary) map[string]any {
	recommendations := map[string]any{
		"keep":     "",
		"increase": "",
		"reduce":   "",
		"test":     "test one controlled variation and compare it with the same observation window",
	}
	if summary.SampleCount < 10 {
		recommendations["keep"] = "keep the current mix while the sample is below the strategy threshold"
		return recommendations
	}
	switch {
	case summary.AverageEngagement >= 0.05:
		recommendations["increase"] = "strong observed engagement supports increasing this content pattern gradually"
	case summary.AverageEngagement <= 0.01:
		recommendations["reduce"] = "weak observed engagement supports reducing this content pattern gradually"
	default:
		recommendations["keep"] = "steady observed engagement supports keeping the current content pattern"
	}
	return recommendations
}

func feedbackSampleThreshold(operation string) int64 {
	switch operation {
	case "publish_analytics":
		return 5
	case "strategy_advisor":
		return 10
	default:
		return 1
	}
}

type FeedbackEligibilityInput struct {
	ProjectID, Platform, AccountID, Operation, Cadence                           string
	PeriodStart, PeriodEnd, ContentSetDigest                                     string
	AnalyticsRevision, StrategyRevision                                          int64
	HasNewRevision, HasMatureContent, HasValidObservations, MeetsSampleThreshold bool
	ProjectActive, AnalyticsReady                                                bool
	AlreadySucceeded, RunningDuplicate                                           bool
}

type FeedbackEligibilityResult struct{ Status, SkipReason string }

func EvaluateFeedbackEligibility(in FeedbackEligibilityInput) FeedbackEligibilityResult {
	checks := []struct {
		ok     bool
		reason string
	}{
		{in.HasNewRevision, FeedbackSkipNoNewRevision},
		{in.HasMatureContent, FeedbackSkipNoMatureContent},
		{in.HasValidObservations, FeedbackSkipNoValidObservations},
		{in.MeetsSampleThreshold, FeedbackSkipSampleTooSmall},
		{!in.AlreadySucceeded, "already_succeeded"},
		{!in.RunningDuplicate, "duplicate_running"},
		{in.ProjectActive, FeedbackSkipProjectPaused},
		{in.AnalyticsReady, FeedbackSkipAnalyticsRebuilding},
	}
	for _, c := range checks {
		if !c.ok {
			return FeedbackEligibilityResult{Status: FeedbackJobSkipped, SkipReason: c.reason}
		}
	}
	return FeedbackEligibilityResult{Status: FeedbackJobEligible}
}

func FeedbackJobFingerprint(in FeedbackEligibilityInput) string {
	raw := strings.Join([]string{in.ProjectID, in.Platform, in.AccountID, in.Operation, in.Cadence, in.PeriodStart, in.PeriodEnd, fmt.Sprint(in.AnalyticsRevision), in.ContentSetDigest, fmt.Sprint(in.StrategyRevision)}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func NextFeedbackRun(now time.Time, location, cadence string) (time.Time, error) {
	if strings.TrimSpace(location) == "" {
		location = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(location)
	if err != nil {
		return time.Time{}, err
	}
	local := now.In(loc)
	next := time.Date(local.Year(), local.Month(), local.Day(), 4, 0, 0, 0, loc)
	switch cadence {
	case FeedbackCadenceDaily:
		if !next.After(local) {
			next = next.AddDate(0, 0, 1)
		}
	case FeedbackCadenceWeekly:
		days := (int(time.Monday-local.Weekday()) + 7) % 7
		next = next.AddDate(0, 0, days)
		if !next.After(local) {
			next = next.AddDate(0, 0, 7)
		}
	case FeedbackCadenceMonthly:
		next = time.Date(local.Year(), local.Month(), 1, 4, 0, 0, 0, loc)
		for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
			next = next.AddDate(0, 0, 1)
		}
		if !next.After(local) {
			next = time.Date(local.Year(), local.Month()+1, 1, 4, 0, 0, 0, loc)
			for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
				next = next.AddDate(0, 0, 1)
			}
		}
	default:
		return time.Time{}, fmt.Errorf("unsupported feedback cadence %q", cadence)
	}
	return next.UTC(), nil
}
