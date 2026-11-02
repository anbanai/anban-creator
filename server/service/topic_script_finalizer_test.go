package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestFinalizeTopicScriptValidatesArtifactAndCompletesJob(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	rad := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	digest, err := rad.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "script-finalize"})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	if err := rad.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"脚本选题","introduction":"介绍","source_summary":"来源","source_refs":[{"type":"trend","ref_id":"1"}],"scores":{},"video_fit":"口播"}]}`))); err != nil {
		t.Fatal(err)
	}
	suggestions, _ := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	job, err := rad.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: subscription.UserID, SuggestionID: suggestions[0].ID, TargetPlatform: "douyin"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rad.FinalizeTopicScript(ctx, job.ID, topicScriptTestArtifact(t, repo, job, []byte(`{"schema_version":"1.0","title":"脚本选题","hook":"开场","scenes":[{"duration_seconds":60,"visual":"画面","voiceover":"旁白"}],"cta":"关注"}`)), "/tmp/script.json"); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.TopicScriptJobs().FindByID(ctx, job.ID)
	if got.Status != model.TopicScriptJobSucceeded || got.ArtifactPath != "/tmp/script.json" {
		t.Fatalf("job = %#v", got)
	}
	if err := rad.FinalizeTopicScript(ctx, job.ID, []byte(`invalid replay`), "/tmp/replayed.json"); err != nil {
		t.Fatalf("replaying a succeeded script should be idempotent: %v", err)
	}
	got, _ = repo.TopicScriptJobs().FindByID(ctx, job.ID)
	if got.Status != model.TopicScriptJobSucceeded || got.ArtifactPath != "/tmp/script.json" {
		t.Fatalf("replay changed terminal job: %#v", got)
	}

	failed := *job
	failed.ID = "failed-script"
	failed.Fingerprint = "failed-script-fingerprint"
	failed.Status = model.TopicScriptJobQueued
	if err := repo.TopicScriptJobs().Create(ctx, &failed); err != nil {
		t.Fatal(err)
	}
	if err := rad.FinalizeTopicScript(ctx, failed.ID, []byte(`invalid`), "/tmp/invalid.json"); err == nil {
		t.Fatal("invalid artifact should fail the script job")
	}
	if err := rad.FinalizeTopicScript(ctx, failed.ID, []byte(`{"schema_version":"1.0","title":"t","hook":"h","scenes":[{"duration_seconds":1,"visual":"v","voiceover":"a"}],"cta":"c"}`), "/tmp/replayed-valid.json"); err != nil {
		t.Fatalf("replaying a failed script should be idempotent: %v", err)
	}
	got, _ = repo.TopicScriptJobs().FindByID(ctx, failed.ID)
	if got.Status != model.TopicScriptJobFailed || got.ArtifactPath != "" {
		t.Fatalf("replay changed failed terminal job: %#v", got)
	}
}
