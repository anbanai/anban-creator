package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestRequestTopicScriptRetriesFailedFingerprint(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	enqueuer := &topicRadarTestEnqueuer{}
	svc := NewTopicRadarService(repo, enqueuer, nil)
	digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "script-retry"})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	if err := svc.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"脚本重试","introduction":"介绍","source_summary":"趋势","source_refs":[{"type":"search","ref_id":"s1"}],"scores":{},"video_fit":"口播"}]}`))); err != nil {
		t.Fatal(err)
	}
	suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, err := svc.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: subscription.UserID, SuggestionID: suggestions[0].ID, TargetPlatform: "douyin"})
	if err != nil {
		t.Fatal(err)
	}
	job.Status = model.TopicScriptJobFailed
	if err := repo.TopicScriptJobs().Update(ctx, job); err != nil {
		t.Fatal(err)
	}
	retried, err := svc.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: subscription.UserID, SuggestionID: suggestions[0].ID, TargetPlatform: "douyin"})
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID != job.ID || retried.Status != model.TopicScriptJobQueued {
		t.Fatalf("retried job = %#v, want same queued job", retried)
	}
	if enqueuer.taskType != model.TaskTypeTopicScript {
		t.Fatalf("task type = %q, want %q", enqueuer.taskType, model.TaskTypeTopicScript)
	}
}
