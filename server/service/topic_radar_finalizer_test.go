package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestFinalizeTopicDigestPersistsRankedSuggestionsAndAdoptionOutbox(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	enqueuer := &topicRadarTestEnqueuer{}
	service := NewTopicRadarService(repo, enqueuer, nil)
	digest, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-finalize"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := []byte(`{"schema_version":"1.0","generated_at":"2026-10-31T03:00:00Z","suggestions":[{"title":"低成本直播复盘","introduction":"用一个真实案例拆解低成本直播的关键动作。","source_summary":"平台热点与历史表现","source_refs":[{"type":"search","label":"搜索趋势","platform":"seednote","ref_id":"trend-1","title":"低成本直播"}],"scores":{"traffic_potential":0.9,"account_fit":0.8},"video_fit":"适合60秒口播"}]}`)
	freezeTestTopicRefs(t, repo, digest)
	if err := service.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, artifact)); err != nil {
		t.Fatal(err)
	}
	got, err := repo.TopicDigests().FindByID(ctx, digest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.TopicDigestStatusSucceeded || got.CandidateCount != 1 || got.ActiveKey != nil {
		t.Fatalf("digest = status %q candidates %d active=%v", got.Status, got.CandidateCount, got.ActiveKey)
	}
	suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil || len(suggestions) != 1 {
		t.Fatalf("suggestions = %d, err=%v", len(suggestions), err)
	}
	adoption, err := repo.TopicSuggestionAdoptions().FindBySuggestion(ctx, suggestions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if adoption.Status != model.TopicAdoptionPending {
		t.Fatalf("adoption status = %q, want pending", adoption.Status)
	}
}

func TestFinalizeTopicDigestRejectsSuggestionWithoutResolvableSource(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	service := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-invalid-source"})
	if err != nil {
		t.Fatal(err)
	}
	err = service.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"无来源选题","introduction":"介绍","source_summary":"无","source_refs":[{"type":"search","label":"搜索趋势"}],"scores":{},"video_fit":"适合"}]}`)))
	if err == nil {
		t.Fatal("expected source validation error")
	}
	got, findErr := repo.TopicDigests().FindByID(ctx, digest.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if got.Status != model.TopicDigestStatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}
