package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestFinalizeDigestIsIdempotentAfterSuccess(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "idempotent-finalize"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := []byte(`{"schema_version":"1.0","suggestions":[{"title":"幂等选题","introduction":"介绍","source_summary":"搜索趋势","source_refs":[{"type":"search","ref_id":"trend-1"}],"scores":{"traffic_potential":0.8},"video_fit":"口播"}]}`)
	freezeTestTopicRefs(t, repo, digest)
	if err := svc.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, artifact)); err != nil {
		t.Fatal(err)
	}
	if err := svc.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, artifact)); err != nil {
		t.Fatalf("repeated terminal finalization: %v", err)
	}
	suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("suggestions = %d, want 1", len(suggestions))
	}
}

func TestFinalizeDigestRejectsSourceAbsentFromFrozenSnapshot(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "frozen-source"})
	if err != nil {
		t.Fatal(err)
	}
	digest.InputSnapshot = []byte(`{"source_refs":[{"type":"search","ref_id":"allowed-source"}]}`)
	if err := repo.TopicDigests().Update(ctx, digest); err != nil {
		t.Fatal(err)
	}
	artifact := []byte(`{"schema_version":"1.0","suggestions":[{"title":"伪造来源","introduction":"介绍","source_summary":"外部","source_refs":[{"type":"search","ref_id":"not-in-snapshot"}],"scores":{},"video_fit":"口播"}]}`)
	if err := svc.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, artifact)); err == nil {
		t.Fatal("expected frozen source validation error")
	}
	persisted, err := repo.TopicDigests().FindByID(ctx, digest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.TopicDigestStatusFailed {
		t.Fatalf("status = %q, want failed", persisted.Status)
	}
}
