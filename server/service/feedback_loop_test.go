package service

import (
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestFeedbackRecommendationsReflectEngagementBand(t *testing.T) {
	cases := []struct {
		name string
		avg  float64
		want string
	}{
		{name: "strong", avg: 0.08, want: "increase"},
		{name: "weak", avg: 0.005, want: "reduce"},
		{name: "steady", avg: 0.025, want: "keep"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := feedbackRecommendations(feedbackMetricSummary{SampleCount: 10, AverageEngagement: tc.avg})
			if !strings.Contains(got[tc.want].(string), tc.name[:1]) {
				t.Fatalf("recommendations[%q] = %#v, want explanation for %s", tc.want, got[tc.want], tc.name)
			}
			if _, ok := got["test"].(string); !ok {
				t.Fatal("recommendations must always include a test suggestion")
			}
		})
	}
}

func TestFeedbackStrategyUsableRequiresActiveMatchingUnexpiredSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 29, 4, 0, 0, 0, time.UTC)
	base := &model.StrategySnapshot{
		Platform:        model.PlatformWechat,
		Status:          "active",
		ApplicableTasks: `["wechat-article"]`,
	}
	cases := []struct {
		name   string
		mutate func(*model.StrategySnapshot)
		want   bool
	}{
		{name: "matching active strategy", want: true},
		{name: "retired strategy", mutate: func(v *model.StrategySnapshot) { v.Status = "retired" }},
		{name: "wrong platform", mutate: func(v *model.StrategySnapshot) { v.Platform = model.PlatformSeednote }},
		{name: "wrong task", mutate: func(v *model.StrategySnapshot) { v.ApplicableTasks = `["seednote"]` }},
		{name: "expired strategy", mutate: func(v *model.StrategySnapshot) { expiry := now.Add(-time.Second); v.ExpiresAt = &expiry }},
		{name: "invalid task payload", mutate: func(v *model.StrategySnapshot) { v.ApplicableTasks = "not-json" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := *base
			if tc.mutate != nil {
				tc.mutate(&candidate)
			}
			if got := feedbackStrategyUsable(&candidate, model.TaskTypeWechatArticle, model.ChannelArticle, now); got != tc.want {
				t.Fatalf("feedbackStrategyUsable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFeedbackEligibilitySkipsEmptyAndMatureWindows(t *testing.T) {
	base := FeedbackEligibilityInput{
		HasNewRevision:       true,
		HasMatureContent:     true,
		HasValidObservations: true,
		MeetsSampleThreshold: true,
		ProjectActive:        true,
		AnalyticsReady:       true,
		Operation:            "publish_analytics",
		ProjectID:            "p1",
		Platform:             "article",
		PeriodStart:          "2026-09-01",
		PeriodEnd:            "2026-09-07",
		AnalyticsRevision:    4,
		ContentSetDigest:     "abc",
	}
	if got := EvaluateFeedbackEligibility(base); got.Status != FeedbackJobEligible {
		t.Fatalf("expected eligible, got %#v", got)
	}

	cases := []struct {
		name   string
		mutate func(*FeedbackEligibilityInput)
		reason string
	}{
		{"no revision", func(v *FeedbackEligibilityInput) { v.HasNewRevision = false }, FeedbackSkipNoNewRevision},
		{"no mature content", func(v *FeedbackEligibilityInput) { v.HasMatureContent = false }, FeedbackSkipNoMatureContent},
		{"no observations", func(v *FeedbackEligibilityInput) { v.HasValidObservations = false }, FeedbackSkipNoValidObservations},
		{"sample too small", func(v *FeedbackEligibilityInput) { v.MeetsSampleThreshold = false }, FeedbackSkipSampleTooSmall},
		{"paused", func(v *FeedbackEligibilityInput) { v.ProjectActive = false }, FeedbackSkipProjectPaused},
		{"rebuilding", func(v *FeedbackEligibilityInput) { v.AnalyticsReady = false }, FeedbackSkipAnalyticsRebuilding},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mutate(&in)
			if got := EvaluateFeedbackEligibility(in); got.Status != FeedbackJobSkipped || got.SkipReason != tc.reason {
				t.Fatalf("got %#v, want skipped %q", got, tc.reason)
			}
		})
	}
}

func TestFeedbackJobFingerprintIsStableAndIncludesRevision(t *testing.T) {
	in := FeedbackEligibilityInput{ProjectID: "p1", Platform: "article", Operation: "monthly_review", PeriodStart: "2026-09-01", PeriodEnd: "2026-09-30", AnalyticsRevision: 7, ContentSetDigest: "digest", StrategyRevision: 2}
	a := FeedbackJobFingerprint(in)
	b := FeedbackJobFingerprint(in)
	if a == "" || a != b {
		t.Fatalf("fingerprint is not stable: %q %q", a, b)
	}
	in.AnalyticsRevision++
	if FeedbackJobFingerprint(in) == a {
		t.Fatal("analytics revision must affect fingerprint")
	}
}

func TestFeedbackSampleThresholdUsesOperationSpecificMinimums(t *testing.T) {
	tests := []struct {
		operation string
		want      int64
	}{
		{operation: "data_tracker", want: 1},
		{operation: "content_postmortem", want: 1},
		{operation: "publish_analytics", want: 5},
		{operation: "performance_review", want: 1},
		{operation: "strategy_advisor", want: 10},
	}
	for _, tc := range tests {
		if got := feedbackSampleThreshold(tc.operation); got != tc.want {
			t.Fatalf("feedbackSampleThreshold(%q) = %d, want %d", tc.operation, got, tc.want)
		}
	}
}

func TestFeedbackOperationsKeepContentPostmortemManual(t *testing.T) {
	if got := feedbackOperations(FeedbackCadenceWeekly); len(got) != 1 || got[0] != "publish_analytics" {
		t.Fatalf("weekly feedback operations = %#v, want only publish_analytics", got)
	}
}

func TestFeedbackJitterIsStableAndBounded(t *testing.T) {
	fingerprint := "abcdef0123456789"
	if first, second := feedbackJitter(fingerprint), feedbackJitter(fingerprint); first != second {
		t.Fatalf("feedbackJitter is not stable: first=%s second=%s", first, second)
	}
	if got := feedbackJitter(fingerprint); got < 0 || got >= 31*time.Second {
		t.Fatalf("feedbackJitter(%q) = %s, want [0, 31s)", fingerprint, got)
	}
}

func TestFeedbackNextCadenceUsesProjectTimezoneAndJitter(t *testing.T) {
	now := time.Date(2026, 9, 29, 2, 30, 0, 0, time.UTC)
	got, err := NextFeedbackRun(now, "Asia/Shanghai", FeedbackCadenceDaily)
	if err != nil {
		t.Fatal(err)
	}
	if !got.After(now) {
		t.Fatalf("next run must be in the future: %s", got)
	}
}

func TestFeedbackWindowOpenUsesCadenceSpecificLocalWindows(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		cadence string
		at      time.Time
		want    bool
	}{
		{"daily before window", FeedbackCadenceDaily, time.Date(2026, 9, 29, 2, 59, 0, 0, loc), false},
		{"daily in window", FeedbackCadenceDaily, time.Date(2026, 9, 29, 3, 0, 0, 0, loc), true},
		{"daily at end", FeedbackCadenceDaily, time.Date(2026, 9, 29, 5, 0, 0, 0, loc), false},
		{"weekly before window", FeedbackCadenceWeekly, time.Date(2026, 9, 28, 3, 59, 0, 0, loc), false},
		{"weekly in window", FeedbackCadenceWeekly, time.Date(2026, 9, 28, 5, 30, 0, 0, loc), true},
		{"weekly at end", FeedbackCadenceWeekly, time.Date(2026, 9, 28, 6, 0, 0, 0, loc), false},
		{"monthly extended window", FeedbackCadenceMonthly, time.Date(2026, 9, 7, 6, 30, 0, 0, loc), true},
		{"monthly at end", FeedbackCadenceMonthly, time.Date(2026, 9, 7, 7, 0, 0, 0, loc), false},
		{"monthly weekend", FeedbackCadenceMonthly, time.Date(2026, 9, 6, 6, 30, 0, 0, loc), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := feedbackWindowOpen(tc.cadence, tc.at, "Asia/Shanghai"); got != tc.want {
				t.Fatalf("feedbackWindowOpen(%q, %s) = %v, want %v", tc.cadence, tc.at, got, tc.want)
			}
		})
	}
}

func TestFeedbackAccountIDUsesPlatformIdentityAndProjectFallback(t *testing.T) {
	wechatA := &model.Project{ID: "project-a", Platform: model.PlatformWechat}
	wechatB := &model.Project{ID: "project-b", Platform: model.PlatformWechat}
	if gotA, gotB := feedbackAccountID(wechatA), feedbackAccountID(wechatB); gotA == gotB || gotA == "" || gotB == "" {
		t.Fatalf("wechat account identity = %q, %q", gotA, gotB)
	}
	seednote := &model.Project{ID: "project-c", Platform: model.PlatformSeednote, ProfileURL: "https://example.test/u/c"}
	if got := feedbackAccountID(seednote); got != "seednote:https://example.test/u/c" {
		t.Fatalf("seednote account identity = %q", got)
	}
	fallback := &model.Project{ID: "project-d", Platform: model.PlatformSeednote}
	if got := feedbackAccountID(fallback); got != "seednote:project:project-d" {
		t.Fatalf("fallback account identity = %q", got)
	}
}

func TestFairProjectOrderRoundRobinsUsers(t *testing.T) {
	ordered := fairProjectOrder([]*model.Project{
		{ID: "a-1", UserID: "user-a"}, {ID: "a-2", UserID: "user-a"},
		{ID: "b-1", UserID: "user-b"},
	})
	if len(ordered) != 3 || ordered[0].UserID == ordered[1].UserID {
		t.Fatalf("fair order = %#v, expected first two projects from different users", ordered)
	}
}
