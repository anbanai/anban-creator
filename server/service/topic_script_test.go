package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestRequestTopicScriptCreatesFingerprintAndDedicatedTask(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	enqueuer := &topicRadarTestEnqueuer{}
	radar := NewTopicRadarService(repo, enqueuer, nil)
	digest, err := radar.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-script"})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	if err := radar.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"一分钟复盘","introduction":"介绍","source_summary":"来源","source_refs":[{"type":"search","ref_id":"trend-1"}],"scores":{},"video_fit":"适合口播"}]}`))); err != nil {
		t.Fatal(err)
	}
	suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, err := radar.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: subscription.UserID, SuggestionID: suggestions[0].ID, TargetPlatform: "douyin", DurationSeconds: 60, AspectRatio: "9:16"})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.TopicScriptJobQueued || job.Fingerprint == "" {
		t.Fatalf("job = status %q fingerprint %q", job.Status, job.Fingerprint)
	}
	if enqueuer.taskType != model.TaskTypeTopicScript || enqueuer.key != job.ID {
		t.Fatalf("enqueue = %q/%q", enqueuer.taskType, enqueuer.key)
	}
	second, err := radar.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: subscription.UserID, SuggestionID: suggestions[0].ID, TargetPlatform: "douyin", DurationSeconds: 60, AspectRatio: "9:16"})
	if err != nil || second.ID != job.ID {
		t.Fatalf("same request should reuse job: second=%#v err=%v", second, err)
	}
}

func TestRequestTopicScriptVariantsCreateDistinctJobs(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	radar := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := radar.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-script-variants"})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	if err := radar.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"多平台变体","introduction":"介绍","source_summary":"来源","source_refs":[{"type":"search","ref_id":"trend-1"}],"scores":{},"video_fit":"口播"}]}`))); err != nil {
		t.Fatal(err)
	}
	suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil || len(suggestions) != 1 {
		t.Fatalf("suggestions = %d, err=%v", len(suggestions), err)
	}
	first, err := radar.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: subscription.UserID, SuggestionID: suggestions[0].ID, TargetPlatform: "douyin", DurationSeconds: 60, AspectRatio: "9:16"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := radar.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: subscription.UserID, SuggestionID: suggestions[0].ID, TargetPlatform: "xiaohongshu", DurationSeconds: 30, AspectRatio: "16:9"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.Fingerprint == second.Fingerprint {
		t.Fatalf("variants reused job: first=%#v second=%#v", first, second)
	}
}
