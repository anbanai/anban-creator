package service

import (
	"context"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestAdoptSuggestionLinksNewTopicPoolRowAndIsIdempotent(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	service := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-adopt"})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	if err := service.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"可持续选题","introduction":"介绍","source_summary":"搜索趋势","source_refs":[{"type":"search","ref_id":"trend-1"}],"scores":{},"video_fit":"口播"}]}`))); err != nil {
		t.Fatal(err)
	}
	suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil || len(suggestions) != 1 {
		t.Fatalf("suggestions = %d, err=%v", len(suggestions), err)
	}
	adoption, err := repo.TopicSuggestionAdoptions().FindBySuggestion(ctx, suggestions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AdoptSuggestion(ctx, adoption.ID); err != nil {
		t.Fatal(err)
	}
	adoption, err = repo.TopicSuggestionAdoptions().FindBySuggestion(ctx, suggestions[0].ID)
	if err != nil || adoption.Status != model.TopicAdoptionAdopted || adoption.TopicPoolID == nil {
		t.Fatalf("adoption = %#v, err=%v", adoption, err)
	}
	if err := service.AdoptSuggestion(ctx, adoption.ID); err != nil {
		t.Fatal(err)
	}
	items, _, err := repo.TopicPools().FindByProject(ctx, subscription.ProjectID, "", 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("topic pool rows = %d, err=%v", len(items), err)
	}
}

func TestAdoptSuggestionBoundsSourceLabelForStrictMySQL(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	service := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-long-label"})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	summary := strings.Repeat("来源", 100)
	artifact := []byte(`{"schema_version":"1.0","suggestions":[{"title":"长来源选题","introduction":"介绍","source_summary":"` + summary + `","source_refs":[{"type":"search","ref_id":"trend-1"}],"scores":{},"video_fit":"口播"}]}`)
	if err := service.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, artifact)); err != nil {
		t.Fatal(err)
	}
	suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil || len(suggestions) != 1 {
		t.Fatalf("suggestions = %d, err=%v", len(suggestions), err)
	}
	adoption, err := repo.TopicSuggestionAdoptions().FindBySuggestion(ctx, suggestions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AdoptSuggestion(ctx, adoption.ID); err != nil {
		t.Fatal(err)
	}
	items, _, err := repo.TopicPools().FindByProject(ctx, subscription.ProjectID, "", 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("topic pool rows = %d, err=%v", len(items), err)
	}
	if got := len([]rune(items[0].SourceLabel)); got != 120 {
		t.Fatalf("source label length = %d, want 120", got)
	}
	if !strings.HasPrefix(items[0].SourceLabel, "来源来源") {
		t.Fatalf("source label = %q, want preserved prefix", items[0].SourceLabel)
	}
}
