package service

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/agentpack"
	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
)

func TestTopicRadarManagedTasksUsePackContractAndRetainOwnership(t *testing.T) {
	for _, kind := range []string{model.TaskKindTopicDigest, model.TaskKindTopicScript} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			sub := seedTopicRadarSubscription(t, repo)
			if err := repo.Users().Create(ctx, &model.User{ID: sub.UserID, Email: "radar@example.test"}); err != nil {
				t.Fatal(err)
			}
			logger := zerolog.New(io.Discard)
			tasks := newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, t.TempDir(), nil, nil)
			svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
			svc.SetTaskService(tasks)
			digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
			if err != nil {
				t.Fatal(err)
			}
			ensure := func() (*model.Task, error) { return svc.EnsureTopicDigestManagedTask(ctx, digest.ID) }
			taskType := model.TaskTypeTopicDigest
			snapshotID := digest.ID
			if kind == model.TaskKindTopicScript {
				suggestion := &model.TopicSuggestion{ID: "suggestion-contract", DigestID: digest.ID, ProjectID: sub.ProjectID, Title: "选题", Introduction: "介绍", SourceSummary: "来源", SourceRefs: datatypes.JSON(`[{"type":"topic_pool","ref_id":"1"}]`), Scores: datatypes.JSON(`{"traffic_potential":0.8}`), VideoFit: "good"}
				if err := repo.TopicSuggestions().CreateBatch(ctx, []*model.TopicSuggestion{suggestion}); err != nil {
					t.Fatal(err)
				}
				job := &model.TopicScriptJob{ID: "script-contract", UserID: sub.UserID, ProjectID: sub.ProjectID, DigestID: digest.ID, SuggestionID: suggestion.ID, Status: model.TopicScriptJobQueued, TargetPlatform: "douyin", DurationSeconds: 60, AspectRatio: "9:16", Fingerprint: "contract"}
				if err := repo.TopicScriptJobs().Create(ctx, job); err != nil {
					t.Fatal(err)
				}
				ensure = func() (*model.Task, error) { return svc.EnsureTopicScriptManagedTask(ctx, job.ID) }
				taskType, snapshotID = model.TaskTypeTopicScript, job.ID
			}
			task, err := ensure()
			if err != nil {
				t.Fatalf("managed task must pass Pack admission: %v", err)
			}
			input := task.AgentInput.Data()
			frozen, ok := input["frozen_context"].(map[string]any)
			if !ok || frozen["project"] == nil || frozen["subscription"] == nil {
				t.Fatalf("missing complete frozen context: %#v", input)
			}
			pack, ok := agentpack.Default().ForTaskType(taskType)
			if !ok {
				t.Fatal("Pack missing")
			}
			if err := agentpack.ValidateTaskInput(pack, input); err != nil {
				t.Fatal(err)
			}
			if input["task_id"] != task.ID || input["execution_id"] != "" || input["task_kind"] != kind {
				t.Fatalf("incorrect admission identity: %#v", input)
			}
			snapshot, ok := input["snapshot"].(map[string]any)
			if !ok || snapshot["snapshot_id"] != snapshotID || snapshot["digest_id"] != digest.ID {
				t.Fatalf("incorrect snapshot: %#v", input["snapshot"])
			}
			if kind == model.TaskKindTopicScript && input["selected_topic"] == nil {
				t.Fatal("missing frozen selected topic")
			}
			tasks.SetRuntimeDispatcher(&scopedDispatchTestDispatcher{dispatchTestDispatcher: &dispatchTestDispatcher{runtimeSelection: serverconfig.RuntimeImageSelection{Profile: "topic-radar", Image: "radar:test"}}, scope: "docker"})
			execution, created, err := tasks.createCurrentExecution(ctx, task)
			if err != nil || !created {
				t.Fatalf("dispatch: execution=%#v created=%v err=%v", execution, created, err)
			}
			persisted, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.AgentInput.Data()["execution_id"] != execution.ID {
				t.Fatalf("runtime input execution identity missing: %#v", persisted.AgentInput.Data())
			}
			if kind == model.TaskKindTopicDigest {
				owner, err := repo.TopicDigests().FindByID(ctx, snapshotID)
				if err != nil || owner.AgentExecutionID == nil || *owner.AgentExecutionID != execution.ID {
					t.Fatalf("digest execution ownership: %#v err=%v", owner, err)
				}
			} else {
				owner, err := repo.TopicScriptJobs().FindByID(ctx, snapshotID)
				if err != nil || owner.ExecutionID == nil || *owner.ExecutionID != execution.ID {
					t.Fatalf("script execution ownership: %#v err=%v", owner, err)
				}
			}
			task = persisted
			task.Status = model.TaskStatusFailed
			if err := repo.Tasks().Update(ctx, task); err != nil {
				t.Fatal(err)
			}
			repeated, err := ensure()
			if err != nil {
				t.Fatal(err)
			}
			if repeated.ID != task.ID {
				t.Fatalf("redelivery replaced terminal owner %s with %s", task.ID, repeated.ID)
			}
		})
	}
}

func TestTopicDigestAdmissionSkipsBillingCatalog(t *testing.T) {
	ctx := context.Background()
	svc, fixture, _ := newFixedTaskBillingFixture(t, 0, 0)
	projectID := createTestProject(t, fixture.repo, billingWalletUserID, model.PlatformSeednote)
	digest := &model.TopicDigest{
		ID: "digest-free-admission", SubscriptionID: "subscription-free", UserID: billingWalletUserID, ProjectID: projectID,
		Status: model.TopicDigestStatusQueued, DispatchStatus: model.TopicDigestDispatchPending,
		OccurrenceKey: "manual:subscription-free:request-free", RequestKey: "request-free", PlannedAt: time.Now().UTC(),
		InputSnapshot: []byte(`{}`), InputHash: "hash",
	}
	if err := fixture.repo.TopicDigests().Create(ctx, digest); err != nil {
		t.Fatal(err)
	}
	radar := NewTopicRadarService(fixture.repo, &topicRadarTestEnqueuer{}, nil)
	radar.SetTaskService(svc)
	task, err := radar.EnsureTopicDigestManagedTask(ctx, digest.ID)
	if err != nil {
		t.Fatalf("topic admission must not require a billing SKU: %v", err)
	}
	if task.Type != model.TaskTypeTopicDigest {
		t.Fatalf("task = %#v", task)
	}
	if account := fixture.account(t, billingWalletUserID); account.PaidCredits != 0 || account.DebtCredits != 0 {
		t.Fatalf("account = %#v, want unchanged zero balance", account)
	}
}
