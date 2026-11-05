package service

import (
	"context"
	"encoding/json"
	"github.com/anbanai/anban-creator/server/model"
	"testing"
)

func TestTopicDigestRanksUsingFrozenWeightsAndCanonicalEvidence(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID, RequestKey: "rank"})
	if err != nil {
		t.Fatal(err)
	}
	var frozen topicFrozenContext
	if err = json.Unmarshal(digest.InputSnapshot, &frozen); err != nil {
		t.Fatal(err)
	}
	frozen.Subscription.Weights = []byte(`{"account_fit":1}`)
	frozen.SourceRefs = []model.TopicSourceRef{{Type: "trend", RefID: "fact-1", Platform: "douyin", Title: "真实标题", Hot: 42}}
	digest.InputSnapshot, digest.InputHash, err = model.NewTopicDigestInput(frozen)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.TopicDigests().Update(ctx, digest); err != nil {
		t.Fatal(err)
	}
	artifact := []byte(`{"schema_version":"1.0","suggestions":[{"title":"流量高","introduction":"介绍","source_summary":"来源","source_refs":[{"type":"trend","ref_id":"fact-1","title":"伪造标题","hot":99999}],"scores":{"traffic_potential":1,"account_fit":0.1},"video_fit":"口播"},{"title":"定位准","introduction":"介绍","source_summary":"来源","source_refs":[{"type":"trend","ref_id":"fact-1"}],"scores":{"traffic_potential":0.1,"account_fit":0.9},"video_fit":"口播"}]}`)
	if err = svc.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, artifact)); err != nil {
		t.Fatal(err)
	}
	suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 2 || suggestions[0].Title != "定位准" || suggestions[0].Rank != 1 {
		t.Fatalf("server ranking = %+v", suggestions)
	}
	for _, suggestion := range suggestions {
		var refs []model.TopicSourceRef
		if err = json.Unmarshal(suggestion.SourceRefs, &refs); err != nil {
			t.Fatal(err)
		}
		if refs[0].Title != "真实标题" || refs[0].Hot != 42 || refs[0].Platform != "douyin" {
			t.Fatalf("invented source metadata persisted: %+v", refs)
		}
	}
}
