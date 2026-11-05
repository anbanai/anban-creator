package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTopicPoolAddCanonicalDedupeDoesNotCreateOrphanAssets(t *testing.T) {
	for _, tc := range []struct {
		name       string
		existing   bool
		radar      bool
		topics     []string
		wantAdded  int
		wantAssets int64
	}{
		{name: "equivalent titles in one request", topics: []string{"Hello World", "hello  world", " HELLO\tWORLD "}, wantAdded: 1, wantAssets: 1},
		{name: "equivalent existing manual topic", existing: true, topics: []string{"hello  world"}, wantAdded: 0, wantAssets: 1},
		{name: "equivalent adopted topic", existing: true, radar: true, topics: []string{"hello  world"}, wantAdded: 0, wantAssets: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := topicRadarTestRepository(t)
			subscription := seedTopicRadarSubscription(t, repo)
			ctx := context.Background()
			svc := NewTopicPoolService(repo, nil)
			if tc.existing {
				if tc.radar {
					source := "suggestion-1"
					if err := repo.TopicPools().Create(ctx, &model.TopicPool{UserID: subscription.UserID, ProjectID: subscription.ProjectID, Topic: "Hello World", SourceSuggestionID: &source, Status: model.TopicStatusUnused}); err != nil {
						t.Fatal(err)
					}
				} else if _, err := svc.Add(ctx, subscription.UserID, subscription.ProjectID, []string{"Hello World"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := MigrateTopicPoolToContentAssets(ctx, repo.Analytics().DB(), nil); err != nil {
				t.Fatal(err)
			}
			added, err := svc.Add(ctx, subscription.UserID, subscription.ProjectID, tc.topics)
			if err != nil {
				t.Fatal(err)
			}
			if len(added) != tc.wantAdded {
				t.Errorf("added topics = %d, want %d", len(added), tc.wantAdded)
			}
			assets, totalAssets, err := repo.ContentAssets().List(ctx, subscription.UserID, "", "", 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			if totalAssets != tc.wantAssets {
				t.Errorf("content assets = %d, want %d", totalAssets, tc.wantAssets)
			}
			topics, totalTopics, err := repo.TopicPools().FindByProject(ctx, subscription.ProjectID, "", 0, 100)
			if err != nil || totalTopics != 1 || len(topics) != 1 {
				t.Fatalf("canonical topic count = %d, error = %v", totalTopics, err)
			}
			if topics[0].Topic != "Hello World" {
				t.Errorf("canonical title changed to %q", topics[0].Topic)
			}
			for _, asset := range assets {
				if topics[0].ContentAssetID == nil || *topics[0].ContentAssetID != asset.ID {
					t.Errorf("asset %s is not linked to canonical topic %d", asset.ID, topics[0].ID)
				}
			}
			if tc.radar && (topics[0].SourceSuggestionID == nil || *topics[0].SourceSuggestionID != "suggestion-1") {
				t.Error("automatic adoption provenance was lost")
			}
		})
	}
}
