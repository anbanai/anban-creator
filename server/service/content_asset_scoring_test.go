package service

import (
	"math"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestScoreContentAssetIsPlatformLocalAndClamped(t *testing.T) {
	got := ScoreContentAsset(ContentAssetScoreInput{PlatformPerformance: 120, AccountFit: 80, Transferability: 80, Freshness: 90, AngleSpace: 70, Cost: 20, Risk: 10})
	if got.PlatformPerformance != 100 || got.Score < 80 || got.Confidence != "high" {
		t.Fatalf("score = %#v", got)
	}
}

func TestScoreContentAssetMarksStaleEvidenceLowConfidence(t *testing.T) {
	got := ScoreContentAsset(ContentAssetScoreInput{PlatformPerformance: 90, AccountFit: 90, Transferability: 90, Freshness: 10, AngleSpace: 90})
	if got.Confidence != "low" {
		t.Fatalf("confidence = %q", got.Confidence)
	}
}

func TestScoreContentAssetRequiresEveryCoreFactor(t *testing.T) {
	base := ContentAssetScoreInput{PlatformPerformance: 90, AccountFit: 90, Transferability: 90, Freshness: 90, AngleSpace: 90, Cost: 10, Risk: 10}
	for name, mutate := range map[string]func(*ContentAssetScoreInput){
		"account fit":      func(in *ContentAssetScoreInput) { in.AccountFit = 0 },
		"transferability":  func(in *ContentAssetScoreInput) { in.Transferability = 0 },
		"expired evidence": func(in *ContentAssetScoreInput) { in.Freshness = 0 },
		"no angle space":   func(in *ContentAssetScoreInput) { in.AngleSpace = 0 },
		"prohibitive cost": func(in *ContentAssetScoreInput) { in.Cost = 100 },
		"maximum risk":     func(in *ContentAssetScoreInput) { in.Risk = 100 },
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			mutate(&input)
			got := ScoreContentAsset(input)
			if got.Score != 0 || got.Confidence != "low" {
				t.Fatalf("score must be ineligible for a zero core factor: %#v", got)
			}
		})
	}
}

func TestScoreContentAssetUsesBoundedMultiplicativeFactors(t *testing.T) {
	allStrong := ScoreContentAsset(ContentAssetScoreInput{PlatformPerformance: 80, AccountFit: 80, Transferability: 80, Freshness: 80, AngleSpace: 80, Cost: 20, Risk: 20})
	oneWeak := ScoreContentAsset(ContentAssetScoreInput{PlatformPerformance: 80, AccountFit: 80, Transferability: 80, Freshness: 80, AngleSpace: 20, Cost: 20, Risk: 20})
	if !(allStrong.Score > oneWeak.Score && oneWeak.Score > 0 && oneWeak.Score < allStrong.Score) {
		t.Fatalf("weak transfer factor should materially reduce the combined score: strong=%#v weak=%#v", allStrong, oneWeak)
	}
	invalid := ScoreContentAsset(ContentAssetScoreInput{PlatformPerformance: math.NaN(), AccountFit: math.Inf(1), Transferability: 80, Freshness: 80, AngleSpace: 80, Cost: -1, Risk: 20})
	if math.IsNaN(invalid.Score) || math.IsInf(invalid.Score, 0) || invalid.Score < 0 || invalid.Score > 100 {
		t.Fatalf("invalid factors must not escape score bounds: %#v", invalid)
	}
}

func TestAutopilotSnapshotEligibilityRejectsFutureEvidence(t *testing.T) {
	now := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	snapshot := &model.ContentAssetScoreSnapshot{
		Status: "scored", AnalyticsRevision: 4, Score: 90, Confidence: "high", DataAsOf: &future,
	}
	policy := model.NewContentAutopilotPolicy("user-1")
	policy.MinRecommendationScore = 60

	if autopilotSnapshotEligible(snapshot, policy, model.PlatformWechat, 4, now) {
		t.Fatal("future evidence timestamp must not qualify for autopilot")
	}

	current := now
	snapshot.DataAsOf = &current
	if !autopilotSnapshotEligible(snapshot, policy, model.PlatformWechat, 4, now) {
		t.Fatal("evidence current at scan time should qualify")
	}
}

func TestAutopilotSnapshotEligibilityAppliesVideoConfidenceToVideoGenerationPlatforms(t *testing.T) {
	now := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	asOf := now.Add(-time.Hour)
	snapshot := &model.ContentAssetScoreSnapshot{
		Status: "scored", AnalyticsRevision: 4, Score: 90, Confidence: "medium", DataAsOf: &asOf,
	}
	policy := model.NewContentAutopilotPolicy("user-1")
	policy.MinRecommendationScore = 60
	policy.MinVideoConfidence = 80

	for _, platform := range []string{model.PlatformMontage, model.PlatformWhiteboardAnimation, model.PlatformHypit} {
		if autopilotSnapshotEligible(snapshot, policy, platform, 4, now) {
			t.Fatalf("medium confidence must not qualify for %s video production", platform)
		}
	}
}
