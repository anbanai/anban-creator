package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTopicRadarScheduleRetainsDueOccurrenceWhileAnotherRunIsActive(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	now := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	due := now.Add(-time.Hour)
	sub.NextRunAt = &due
	if err := repo.TopicSubscriptions().Update(ctx, sub); err != nil {
		t.Fatal(err)
	}
	svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, func() time.Time { return now })
	manual, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID, RequestKey: "overlap"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ScheduleTopicDigests(ctx, 10); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.TopicSubscriptions().FindByID(ctx, sub.ID)
	if err != nil || stored.NextRunAt == nil || !stored.NextRunAt.Equal(due) {
		t.Fatalf("due occurrence lost behind active manual run: next=%v err=%v", stored.NextRunAt, err)
	}
	_ = svc.failDigest(ctx, manual, errors.New("manual run finished"))
	if err := svc.ScheduleTopicDigests(ctx, 10); err != nil {
		t.Fatal(err)
	}
	runs, err := repo.TopicDigests().ListBySubscription(ctx, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, run := range runs {
		if run.OccurrenceKey == model.ScheduledTopicOccurrenceKey(sub.ID, due) {
			found = true
		}
	}
	if len(runs) != 2 || !found {
		t.Fatalf("due occurrence did not recover: %+v", runs)
	}
}

func TestTopicRankingNormalizesAllConfiguredWeightsBeforeMissingScores(t *testing.T) {
	candidates := []topicDigestSuggestion{
		{Title: "all", Scores: map[string]float64{"traffic_potential": 1, "account_fit": 1}},
		{Title: "missing", Scores: map[string]float64{"traffic_potential": 1}},
	}
	snapshot := []byte(`{"subscription":{"weights":{"traffic_potential":2,"account_fit":2}}}`)
	if err := rankTopicDigestSuggestions(snapshot, candidates); err != nil {
		t.Fatal(err)
	}
	for i, want := range []float64{1, 0.5} {
		if got := candidates[i].Scores["score_total"]; math.Abs(got-want) > 1e-12 {
			t.Fatalf("candidate %s total=%v want=%v", candidates[i].Title, got, want)
		}
	}
}

func TestTopicAdoptionMarksNormalizedExistingTitleAsDuplicate(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	original := &model.TopicPool{UserID: sub.UserID, ProjectID: sub.ProjectID, Topic: "  Reusable Topic  "}
	if err := repo.TopicPools().Create(ctx, original); err != nil {
		t.Fatal(err)
	}
	svc := NewTopicRadarService(repo, nil, nil)
	digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	artifact := topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"reusable topic","introduction":"intro","source_summary":"source","source_refs":[{"type":"trend","ref_id":"1"}],"scores":{},"video_fit":"good"}]}`))
	if err := svc.FinalizeDigest(ctx, digest.ID, artifact); err != nil {
		t.Fatal(err)
	}
	suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil || len(suggestions) != 1 {
		t.Fatalf("suggestions=%v err=%v", suggestions, err)
	}
	adoption, err := repo.TopicSuggestionAdoptions().FindBySuggestion(ctx, suggestions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AdoptSuggestion(ctx, adoption.ID); err != nil {
		t.Fatal(err)
	}
	adoption, err = repo.TopicSuggestionAdoptions().FindBySuggestion(ctx, suggestions[0].ID)
	if err != nil || adoption.Status != model.TopicAdoptionDuplicate || adoption.TopicPoolID == nil || *adoption.TopicPoolID != original.ID {
		t.Fatalf("normalized duplicate not linked correctly: %+v err=%v", adoption, err)
	}
}
