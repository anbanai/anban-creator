package service

import (
	"context"
	"gorm.io/datatypes"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func freezeTestTopicRefs(t *testing.T, repo repository.Repository, digest *model.TopicDigest) {
	t.Helper()
	snapshot := map[string]any{"source_refs": []model.TopicSourceRef{{Type: "search", RefID: "trend-1", Platform: "seednote"}, {Type: "search", RefID: "s1"}, {Type: "trend", RefID: "1"}, {Type: "trend", RefID: "hot-1", Platform: "douyin"}}}
	var err error
	digest.InputSnapshot, digest.InputHash, err = model.NewTopicDigestInput(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.TopicDigests().Update(context.Background(), digest); err != nil {
		t.Fatal(err)
	}
}

func TestTopicRadarRejectsUnfrozenOrMutatedSource(t *testing.T) {
	for _, test := range []struct{ name, snapshot, reference string }{
		{"empty", `{"source_refs":[]}`, `{"type":"search","ref_id":"fake"}`},
		{"type changed", `{"source_refs":[{"type":"search","ref_id":"allowed"}]}`, `{"type":"trend","ref_id":"allowed"}`},
		{"platform changed", `{"source_refs":[{"type":"trend","platform":"douyin","ref_id":"allowed"}]}`, `{"type":"trend","platform":"weibo","ref_id":"allowed"}`},
		{"url injected", `{"source_refs":[{"type":"search","ref_id":"allowed","url":"https://example.com/real"}]}`, `{"type":"search","ref_id":"allowed","url":"https://example.com/fake"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := topicRadarTestRepository(t)
			sub := seedTopicRadarSubscription(t, repo)
			svc := NewTopicRadarService(repo, nil, nil)
			ctx := context.Background()
			digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
			if err != nil {
				t.Fatal(err)
			}
			digest.InputSnapshot = datatypes.JSON(test.snapshot)
			if err = repo.TopicDigests().Update(ctx, digest); err != nil {
				t.Fatal(err)
			}
			artifact := []byte(`{"schema_version":"1.0","suggestions":[{"title":"test","introduction":"test","source_summary":"test","source_refs":[` + test.reference + `],"scores":{},"video_fit":"test"}]}`)
			if err = svc.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, artifact)); err == nil {
				t.Fatal("accepted source not identical to frozen evidence")
			}
			items, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
			if err != nil || len(items) != 0 {
				t.Fatalf("persisted invalid suggestions: %v %v", items, err)
			}
		})
	}
}

func TestTopicRadarReclaimsExpiredAdoption(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	item := &model.TopicSuggestionAdoption{ID: "expired", SuggestionID: "suggestion", Status: model.TopicAdoptionProcessing, LeaseToken: "old", LeaseUntil: &past}
	if err := repo.TopicSuggestionAdoptions().CreateBatch(ctx, []*model.TopicSuggestionAdoption{item}); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.TopicSuggestionAdoptions().Claim(ctx, item.ID, "new", time.Now().Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("expired processing lease cannot be reclaimed: claimed=%v err=%v", claimed, err)
	}
}
