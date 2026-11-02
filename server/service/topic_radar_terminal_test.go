package service

import (
	"context"
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTopicRadarStaleFailureCannotOverwriteSuccess(t *testing.T) {
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	svc := NewTopicRadarService(repo, nil, nil)
	ctx := context.Background()
	digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	artifact := []byte(`{"schema_version":"1.0","suggestions":[{"title":"stable","introduction":"intro","source_summary":"search","source_refs":[{"type":"search","ref_id":"s1"}],"scores":{},"video_fit":"video"}]}`)
	if err := svc.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, artifact)); err != nil {
		t.Fatal(err)
	}
	_ = svc.failDigest(ctx, digest, errors.New("late failed worker"))
	actual, err := repo.TopicDigests().FindByID(ctx, digest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Status != model.TopicDigestStatusSucceeded || actual.CandidateCount != 1 {
		t.Fatalf("stale worker overwrote success: %+v", actual)
	}
}

func TestTopicRadarAdoptionUpdatesSuggestionReadModel(t *testing.T) {
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	svc := NewTopicRadarService(repo, nil, nil)
	ctx := context.Background()
	digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	if err := svc.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"adopt","introduction":"intro","source_summary":"search","source_refs":[{"type":"search","ref_id":"s1"}],"scores":{},"video_fit":"video"}]}`))); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileTopicAdoptions(ctx, 10); err != nil {
		t.Fatal(err)
	}
	items, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].AdoptionStatus != model.TopicAdoptionAdopted {
		t.Fatalf("suggestion read model was not updated: %+v", items)
	}
}
