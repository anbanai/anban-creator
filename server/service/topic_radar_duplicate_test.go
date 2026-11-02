package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestAdoptSuggestionMarksExistingTopicAsDuplicate(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	if err := repo.TopicPools().Create(ctx, &model.TopicPool{UserID: subscription.UserID, ProjectID: subscription.ProjectID, Topic: "可持续选题"}); err != nil {
		t.Fatal(err)
	}
	radar := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := radar.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-duplicate"})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	if err := radar.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"可持续选题","introduction":"介绍","source_summary":"来源","source_refs":[{"type":"search","ref_id":"trend-1"}],"scores":{},"video_fit":"口播"}]}`))); err != nil {
		t.Fatal(err)
	}
	suggestions, _ := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	adoption, err := repo.TopicSuggestionAdoptions().FindBySuggestion(ctx, suggestions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := radar.AdoptSuggestion(ctx, adoption.ID); err != nil {
		t.Fatal(err)
	}
	adoption, err = repo.TopicSuggestionAdoptions().FindBySuggestion(ctx, suggestions[0].ID)
	if err != nil || adoption.Status != model.TopicAdoptionDuplicate || adoption.TopicPoolID == nil {
		t.Fatalf("adoption = %#v, err=%v", adoption, err)
	}
}
