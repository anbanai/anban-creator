package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type topicRadarTestEnqueuer struct {
	taskType string
	payload  []byte
	key      string
}

func (e *topicRadarTestEnqueuer) EnqueueUnique(taskType string, payload []byte, key string) (bool, error) {
	e.taskType = taskType
	e.payload = payload
	e.key = key
	return true, nil
}

func topicRadarTestRepository(t *testing.T) repository.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return repository.New(db)
}

func seedTopicRadarSubscription(t *testing.T, repo repository.Repository) *model.TopicSubscription {
	t.Helper()
	ctx := context.Background()
	project := &model.Project{ID: "project-1", UserID: "user-1", Platform: "seednote", Name: "测试项目", Status: model.ProjectStatusActive, Timezone: "Asia/Shanghai"}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	subscription := &model.TopicSubscription{
		ID:           "subscription-1",
		UserID:       project.UserID,
		ProjectID:    project.ID,
		Name:         "每日选题",
		Enabled:      true,
		Timezone:     "Asia/Shanghai",
		Schedule:     datatypes.JSON(`{"hour":9}`),
		SourceConfig: datatypes.JSON(`{"platforms":["seednote"]}`),
		Weights:      datatypes.JSON(`{"traffic_potential":0.25}`),
		ScriptPolicy: datatypes.JSON(`{"enabled":true}`),
	}
	if err := repo.TopicSubscriptions().Create(ctx, subscription); err != nil {
		t.Fatal(err)
	}
	return subscription
}

func TestTopicRadarDeleteSubscriptionTombstonesAndRejectsNewRuns(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	createdAt := time.Now()
	if err := repo.TopicDigests().Create(ctx, &model.TopicDigest{ID: "history-1", SubscriptionID: subscription.ID, UserID: subscription.UserID, ProjectID: subscription.ProjectID, Status: model.TopicDigestStatusSucceeded, DispatchStatus: model.TopicDigestDispatchClaimed, OccurrenceKey: "history-1", RequestKey: "history-1", PlannedAt: createdAt, InputSnapshot: []byte(`{}`), InputHash: "history"}); err != nil {
		t.Fatal(err)
	}
	service := NewTopicRadarService(repo, nil, func() time.Time { return createdAt })

	if err := service.DeleteSubscription(ctx, subscription.UserID, subscription.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteSubscription(ctx, subscription.UserID, subscription.ID); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
	if _, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "after-delete"}); !errors.Is(err, ErrTopicSubscriptionNotFound) {
		t.Fatalf("run after delete error = %v, want %v", err, ErrTopicSubscriptionNotFound)
	}
	items, err := service.ListSubscriptions(ctx, subscription.UserID, subscription.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("active subscriptions = %d, want 0", len(items))
	}
	history, err := service.ListDigests(ctx, subscription.UserID, subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ID != "history-1" {
		t.Fatalf("digest history = %#v, want preserved history", history)
	}
	stored, err := repo.TopicSubscriptions().FindByID(ctx, subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DeletedAt == nil || stored.Enabled || stored.NextRunAt != nil {
		t.Fatalf("tombstone state = %+v", stored)
	}
}

func TestTopicRadarDeleteSubscriptionRequiresOwnerAndProject(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	service := NewTopicRadarService(repo, nil, time.Now)

	if err := service.DeleteSubscription(ctx, "other-user", subscription.ID); !errors.Is(err, ErrTopicProjectAccessDenied) {
		t.Fatalf("wrong owner error = %v, want %v", err, ErrTopicProjectAccessDenied)
	}
	stored, err := repo.TopicSubscriptions().FindByID(ctx, subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DeletedAt != nil {
		t.Fatal("wrong owner deleted subscription")
	}
}

func TestTopicRadarRunCreatesFrozenDigestAndEnqueuesAfterPersistence(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	clock := time.Date(2026, 10, 31, 10, 45, 0, 0, time.FixedZone("CST", 8*60*60))
	enqueuer := &topicRadarTestEnqueuer{}
	service := NewTopicRadarService(repo, enqueuer, func() time.Time { return clock })

	got, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.TopicDigestStatusQueued {
		t.Fatalf("status = %q, want queued", got.Status)
	}
	if got.DispatchStatus != model.TopicDigestDispatchEnqueued {
		t.Fatalf("dispatch status = %q, want enqueued", got.DispatchStatus)
	}
	if got.OccurrenceKey != "manual:"+subscription.ID+":manual-1" {
		t.Fatalf("occurrence key = %q", got.OccurrenceKey)
	}
	if got.InputHash == "" || len(got.InputSnapshot) == 0 {
		t.Fatal("expected frozen input snapshot and hash")
	}
	if enqueuer.taskType != model.TaskTypeTopicDigest || enqueuer.key != got.ID {
		t.Fatalf("enqueue = type %q key %q, want %q %q", enqueuer.taskType, enqueuer.key, model.TaskTypeTopicDigest, got.ID)
	}
	persisted, err := repo.TopicDigests().FindByID(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ID != got.ID {
		t.Fatal("digest was not persisted before enqueue")
	}
}

func TestTopicRadarRunReturnsExistingActiveDigest(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	enqueuer := &topicRadarTestEnqueuer{}
	service := NewTopicRadarService(repo, enqueuer, time.Now)
	first, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-2"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("active guard returned %q, want %q", second.ID, first.ID)
	}
}

func TestTopicRadarRetryCreatesNewRunAfterSuccess(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	enqueuer := &topicRadarTestEnqueuer{}
	service := NewTopicRadarService(repo, enqueuer, time.Now)
	first, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "manual-1"})
	if err != nil {
		t.Fatal(err)
	}
	finished := time.Now()
	first.Status = model.TopicDigestStatusSucceeded
	first.FinishedAt = &finished
	first.ActiveKey = nil
	if err := repo.TopicDigests().Update(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := service.Retry(ctx, subscription.UserID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.RequestKey == first.RequestKey {
		t.Fatalf("retry reused original run: first=%q/%q second=%q/%q", first.ID, first.RequestKey, second.ID, second.RequestKey)
	}
	if got, err := repo.TopicDigests().ListBySubscription(ctx, subscription.ID, 10); err != nil || len(got) != 2 {
		t.Fatalf("digest history = %d, err=%v", len(got), err)
	}
}

func TestProcessTopicDigestFailsAdmissionAndReleasesActiveRun(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	service := NewTopicRadarService(repo, nil, nil)
	digest, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "admission-failure"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessTopicDigest(ctx, digest.ID); err == nil {
		t.Fatal("expected admission failure")
	}
	failed, err := repo.TopicDigests().FindByID(ctx, digest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.TopicDigestStatusFailed || failed.ActiveKey != nil {
		t.Fatalf("digest = status %q active=%v, want failed and released", failed.Status, failed.ActiveKey)
	}
	next, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "after-admission-failure"})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == digest.ID {
		t.Fatal("failed digest still blocks the subscription")
	}
}

func TestProcessTopicScriptFailsAdmissionWithoutTouchingDigest(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	subscription := seedTopicRadarSubscription(t, repo)
	service := NewTopicRadarService(repo, nil, nil)
	digest, err := service.Run(ctx, RunTopicDigestParams{UserID: subscription.UserID, SubscriptionID: subscription.ID, RequestKey: "script-admission-digest"})
	if err != nil {
		t.Fatal(err)
	}
	freezeTestTopicRefs(t, repo, digest)
	if err := service.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"脚本选题","introduction":"介绍","source_summary":"来源","source_refs":[{"type":"search","ref_id":"trend-1"}],"scores":{},"video_fit":"口播"}]}`))); err != nil {
		t.Fatal(err)
	}
	suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
	if err != nil || len(suggestions) != 1 {
		t.Fatalf("suggestions = %d, err=%v", len(suggestions), err)
	}
	job, err := service.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: subscription.UserID, AssetID: suggestions[0].ContentAssetID, ProjectID: suggestions[0].ProjectID, TargetPlatform: "douyin", DurationSeconds: 60, AspectRatio: "9:16"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessTopicScript(ctx, job.ID); err == nil {
		t.Fatal("expected script admission failure")
	}
	failed, err := repo.TopicScriptJobs().FindByID(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.TopicScriptJobFailed {
		t.Fatalf("script status = %q, want failed", failed.Status)
	}
	stored, err := repo.TopicDigests().FindByID(ctx, digest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TopicDigestStatusSucceeded {
		t.Fatalf("digest status = %q, want unchanged success", stored.Status)
	}
}

func TestTopicAdmissionErrorClassification(t *testing.T) {
	if topicAdmissionErrorIsTransient(ErrTopicManagedRuntimeUnavailable) {
		t.Fatal("runtime configuration errors must terminalize the durable row")
	}
	if topicAdmissionErrorIsTransient(errors.New("invalid frozen digest input")) {
		t.Fatal("frozen input errors must terminalize the durable row")
	}
	for _, transient := range []error{context.DeadlineExceeded, context.Canceled, driver.ErrBadConn, gorm.ErrInvalidDB} {
		if !topicAdmissionErrorIsTransient(transient) {
			t.Fatalf("%v must stay retryable", transient)
		}
	}
}
