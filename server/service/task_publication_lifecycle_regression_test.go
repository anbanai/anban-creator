package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
)

func TestCompleteCloudExecutionProjectsPreflightPublicationBlock(t *testing.T) {
	for _, tc := range []struct {
		name           string
		cover          bool
		code           string
		action         string
		invalidPackage bool
	}{
		{"publication service unavailable", false, "publication_service_unavailable", "retry_draft", false},
		{"cover missing", true, "cover_media_missing", "retry_visuals", false},
		{"extra cover metadata in package", false, "publication_package_invalid", "review_content", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, repo, task, execution := setupCloudCompletionTest(t, true)
			if tc.invalidPackage {
				var pkg map[string]any
				if err := json.Unmarshal(validTaskDeliveryFixtureBody("output/draft.json", "application/json"), &pkg); err != nil {
					t.Fatal(err)
				}
				pkg["cover"] = map[string]string{"file_path": "output/cover.png"}
				body, err := json.Marshal(pkg)
				if err != nil {
					t.Fatal(err)
				}
				addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/draft.json", "application/json", body)
			}
			task.ArticleWithCover = &tc.cover
			if err := repo.Tasks().Update(ctx, task); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
				t.Fatal(err)
			}
			if err := svc.CompleteCloudExecution(ctx, execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
				t.Fatal(err)
			}
			stored, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Outcome == nil || stored.Outcome.Publication.Code != tc.code || stored.Outcome.Publication.Action != tc.action || stored.Outcome.Publication.Attempted {
				t.Fatalf("preflight outcome = %#v", stored.Outcome)
			}
			draft := stored.Lifecycle.Data().Stages[3]
			if draft.State != model.TaskLifecycleStateBlocked || draft.LatestUpdate != stored.Outcome.Publication.Message {
				t.Fatalf("draft stage = %#v, want blocked with actual preflight reason %q", draft, stored.Outcome.Publication.Message)
			}
		})
	}
}

func TestFinalizeCloudDraftDeliveryReplaysPersistedBlockIntoLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupTaskLifecycleTest(t, model.PlatformArticle)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	evidence := encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "publication_package_invalid", false, "review_content")
	if won, err := repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, "", model.TaskExecutionDraftDeliveryFailed, evidence); err != nil || !won {
		t.Fatalf("record block: %v %v", won, err)
	}
	if err := svc.finalizeCloudDraftDelivery(ctx, task, execution, nil); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Lifecycle.Data().Stages[3].State != model.TaskLifecycleStateBlocked {
		t.Fatal("persisted preflight result was not projected on replay")
	}
}

func TestCompleteFailedExecutionDoesNotRestoreSkippedPublicationStagesToPending(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteCloudExecution(ctx, execution.ID, &agent.ExecutionResult{Success: false, RemoteArtifacts: true, Error: "generation failed"}); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range stored.Lifecycle.Data().Stages[3:] {
		if stage.State != model.TaskLifecycleStateSkipped {
			t.Fatalf("publication stage after failed generation = %#v", stage)
		}
	}
}

func TestGetTaskRepairsStalePublicationLifecycleWithoutCreatingDraft(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupTaskLifecycleTest(t, model.PlatformArticle)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	evidence := encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryBlocked, "cover_media_missing", false, "retry_visuals")
	if won, err := repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, "", model.TaskExecutionDraftDeliveryBlocked, evidence); err != nil || !won {
		t.Fatalf("record preflight: %v, %v", won, err)
	}
	if err := repo.Tasks().UpdateStatus(ctx, task.ID, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	found, err := svc.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Lifecycle.Data().Stages[3].State != model.TaskLifecycleStatePending {
		t.Fatal("unauthorized task lookup mutated the lifecycle")
	}
	svc.RefreshTaskPublicationLifecycle(ctx, "another-user", found)
	stored, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil || stored.Lifecycle.Data().Stages[3].State != model.TaskLifecycleStatePending {
		t.Fatalf("cross-owner refresh changed lifecycle: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	svc.RefreshTaskPublicationLifecycle(cancelled, task.UserID, found)
	if found.Lifecycle.Data().Stages[3].State != model.TaskLifecycleStatePending {
		t.Fatal("failed best-effort refresh changed the readable task")
	}
	for range 2 {
		found, err := svc.GetByID(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		svc.RefreshTaskPublicationLifecycle(ctx, task.UserID, found)
		draft := found.Lifecycle.Data().Stages[3]
		if draft.State != model.TaskLifecycleStateBlocked || draft.LatestUpdate != "封面尚未生成或上传完成，微信尚未收到请求。" {
			t.Fatalf("read draft stage = %#v", draft)
		}
	}
	if _, err := repo.WechatPublications().FindByTaskID(ctx, task.ID); err == nil {
		t.Fatal("reading task created a publication")
	}
}
