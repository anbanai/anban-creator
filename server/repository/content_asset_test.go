package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestContentReuseStageCASAndTransactionalAudit(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	stage := model.NewContentReuseStageExecution(run.ID, model.ContentReuseStageResearch)
	if err := repo.ContentAssets().CreateReuseStage(ctx, stage); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.ContentAssets().FindReuseStage(ctx, run.ID, model.ContentReuseStageResearch)
	if err != nil {
		t.Fatal(err)
	}
	stage.Status = model.ContentStageRunning
	event := &model.ContentReuseAuditEvent{UserID: run.UserID, RunID: run.ID, StageExecutionID: stage.ID, EntityKind: "stage", EntityID: stage.ID, FromState: model.ContentStageQueued, ToState: model.ContentStageRunning, ReasonCode: "admitted"}
	if err := repo.ContentAssets().TransitionReuseStage(ctx, stage, event); err != nil {
		t.Fatal(err)
	}
	stale.Status = model.ContentStageCompleted
	if err := repo.ContentAssets().TransitionReuseStage(ctx, stale, &model.ContentReuseAuditEvent{UserID: run.UserID, RunID: run.ID, EntityKind: "stage", EntityID: stage.ID, FromState: model.ContentStageRunning, ToState: model.ContentStageCompleted}); !errors.Is(err, ErrContentReuseRunConflict) {
		t.Fatalf("stale transition error = %v", err)
	}
	events, err := repo.ContentAssets().ListReuseAudit(ctx, run.UserID, run.ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("audit events=%d err=%v", len(events), err)
	}
	if events[0].ReasonCode != "stage_created" || events[1].PreviousVersion != 0 || events[1].NewVersion != 1 || events[1].ToState != model.ContentStageRunning {
		t.Fatalf("audit events = %#v", events)
	}
}

func TestContentResearchJobFingerprintIsUserScopedAndFrozen(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	frozen := []byte(`{"project_id":"owned-project","profile_revision":7}`)
	job := model.NewContentAssetResearchJob("owner", "owned-project", "fingerprint-1", frozen)
	job.PolicyRevision = 4
	if err := repo.ContentAssets().CreateResearchJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ContentAssets().FindResearchJobByFingerprint(ctx, "owner", job.Fingerprint)
	if err != nil || got.ID != job.ID || string(got.FrozenContext) != string(frozen) {
		t.Fatalf("job=%#v err=%v", got, err)
	}
	if _, err := repo.ContentAssets().FindResearchJobByFingerprint(ctx, "other", job.Fingerprint); err == nil {
		t.Fatal("foreign user saw research job")
	}
	stale, _ := repo.ContentAssets().FindResearchJobByFingerprint(ctx, "owner", job.Fingerprint)
	job.Status = model.ContentStageRunning
	if err := repo.ContentAssets().UpdateResearchJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	stale.Status = model.ContentStageFailed
	if err := repo.ContentAssets().UpdateResearchJob(ctx, stale); !errors.Is(err, ErrContentReuseRunConflict) {
		t.Fatalf("stale update=%v", err)
	}
}

func TestContentReuseStageTaskBindingIsAtomicAndScoped(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	stage := model.NewContentReuseStageExecution(run.ID, model.ContentReuseStageResearch)
	if err := repo.ContentAssets().CreateReuseStage(ctx, stage); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "stage-task", UserID: run.UserID, ProjectID: run.TargetProjectID, ContentAssetID: run.AssetID, ReuseRunID: run.ID, ReuseStageID: stage.ID, Type: model.TaskTypeContentResearch, Status: model.TaskStatusPending}
	if err := repo.ContentAssets().BindReuseStageTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	bound, err := repo.Tasks().FindByReuseStage(ctx, run.UserID, stage.ID)
	if err != nil || bound.ID != task.ID {
		t.Fatalf("bound task=%#v err=%v", bound, err)
	}
	got, err := repo.ContentAssets().FindReuseStageByID(ctx, stage.ID)
	if err != nil || got.TaskID == nil || *got.TaskID != task.ID || got.Status != model.ContentStageRunning {
		t.Fatalf("stage=%#v err=%v", got, err)
	}
	if err := repo.ContentAssets().BindReuseStageTask(ctx, &model.Task{ID: "second", UserID: run.UserID, ProjectID: run.TargetProjectID, ContentAssetID: run.AssetID, ReuseRunID: run.ID, ReuseStageID: stage.ID}); !errors.Is(err, ErrContentReuseRunConflict) {
		t.Fatalf("duplicate binding=%v", err)
	}
}

func TestUnboundStageAndResearchJobsAllowMultipleRows(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
		if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
			t.Fatal(err)
		}
		stage := model.NewContentReuseStageExecution(run.ID, model.ContentReuseStageResearch)
		if err := repo.ContentAssets().CreateReuseStage(ctx, stage); err != nil {
			t.Fatalf("second unbound stage rejected: %v", err)
		}
		job := model.NewContentAssetResearchJob("owner", "project", fmt.Sprintf("fingerprint-%d", i), []byte(`{}`))
		if err := repo.ContentAssets().CreateResearchJob(ctx, job); err != nil {
			t.Fatalf("second unbound research job rejected: %v", err)
		}
	}
}

func TestContentAssetAndRunListsApplyOwnerScopedFiltersAndPagination(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	for _, item := range []struct{ user, title, status, platform, source, project string }{
		{"owner", "A topic", model.ContentAssetStatusCandidate, model.PlatformWechat, model.ContentAssetEvidenceExternal, "p1"},
		{"owner", "B topic", model.ContentAssetStatusValidated, model.PlatformSeednote, model.ContentAssetEvidenceImported, "p2"},
		{"other", "A topic foreign", model.ContentAssetStatusCandidate, model.PlatformWechat, model.ContentAssetEvidenceExternal, "p1"},
	} {
		asset := model.NewContentAsset(item.user, item.title)
		asset.Status = item.status
		if err := repo.ContentAssets().Create(ctx, asset); err != nil {
			t.Fatal(err)
		}
		if err := repo.ContentAssets().CreateEvidence(ctx, &model.ContentAssetEvidence{ID: fmt.Sprintf("e-%s", asset.ID), AssetID: asset.ID, UserID: item.user, SourceType: item.source, Platform: item.platform, ProjectID: item.project}); err != nil {
			t.Fatal(err)
		}
		if item.user == "owner" {
			run := model.NewContentReuseRun(item.user, asset.ID, item.project, item.platform)
			run.Status = model.ContentReuseRunStatusRunning
			if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
				t.Fatal(err)
			}
		}
	}
	assets, total, err := repo.ContentAssets().ListWithFilters(ctx, ContentAssetListFilter{UserID: "owner", Status: model.ContentAssetStatusCandidate, Platform: model.PlatformWechat, Source: model.ContentAssetEvidenceExternal, ProjectID: "p1", Offset: 0, Limit: 1})
	if err != nil || total != 1 || len(assets) != 1 || assets[0].Title != "A topic" {
		t.Fatalf("filtered assets=%#v total=%d err=%v", assets, total, err)
	}
	runs, total, err := repo.ContentAssets().ListReuseRuns(ctx, ContentReuseRunListFilter{UserID: "owner", TargetPlatform: model.PlatformSeednote, Status: model.ContentReuseRunStatusRunning, Offset: 0, Limit: 10})
	if err != nil || total != 1 || len(runs) != 1 || runs[0].TargetProjectID != "p2" {
		t.Fatalf("filtered runs=%#v total=%d err=%v", runs, total, err)
	}
	foreign, total, err := repo.ContentAssets().ListReuseRuns(ctx, ContentReuseRunListFilter{UserID: "other", Offset: 0, Limit: 10})
	if err != nil || total != 0 || len(foreign) != 0 {
		t.Fatalf("foreign runs=%#v total=%d err=%v", foreign, total, err)
	}
}

func TestStageTransitionRejectsStoppedRunAndUpdatesCannotRewriteFrozenInput(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	stage := model.NewContentReuseStageExecution(run.ID, model.ContentReuseStageResearch)
	stage.InputSnapshot = []byte(`{"frozen":true}`)
	stage.InputSHA256 = "frozen-hash"
	if err := repo.ContentAssets().CreateReuseStage(ctx, stage); err != nil {
		t.Fatal(err)
	}
	stage.InputSnapshot, stage.InputSHA256 = []byte(`{"frozen":false}`), "rewritten-hash"
	stage.Status = model.ContentStageRunning
	if err := repo.ContentAssets().UpdateReuseStage(ctx, stage); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ContentAssets().FindReuseStageByID(ctx, stage.ID)
	if err != nil || string(got.InputSnapshot) != `{"frozen":true}` || got.InputSHA256 != "frozen-hash" {
		t.Fatalf("frozen input changed: %#v err=%v", got, err)
	}
	run, err = repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	run.Status = model.ContentReuseRunStatusPaused
	if err := repo.ContentAssets().UpdateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	got.Status = model.ContentStageCompleted
	if err := repo.ContentAssets().TransitionReuseStage(ctx, got, &model.ContentReuseAuditEvent{UserID: run.UserID, RunID: run.ID, EntityKind: "stage", EntityID: got.ID, FromState: model.ContentStageRunning, ToState: model.ContentStageCompleted}); !errors.Is(err, ErrContentReuseRunConflict) {
		t.Fatalf("stopped run transition=%v", err)
	}
	got, err = repo.ContentAssets().FindReuseStageByID(ctx, stage.ID)
	if err != nil || got.Status != model.ContentStageRunning {
		t.Fatalf("stopped completion committed: %#v err=%v", got, err)
	}
}

func TestContentAssetsPersistAndReuseIdempotency(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	asset := model.NewContentAsset("user-1", "可复用选题")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	project := &model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformWechat, Name: "公众号"}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("user-1", asset.ID, project.ID, "wechat-article")
	run.IdempotencyKey = "reuse:key-1"
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ContentAssets().FindReuseRunByIdempotency(ctx, "user-1", "reuse:key-1")
	if err != nil || got.ID != run.ID {
		t.Fatalf("reuse lookup = %#v, %v", got, err)
	}
	if _, err := repo.ContentAssets().FindReuseRunByIdempotency(ctx, "user-2", "reuse:key-1"); err == nil {
		t.Fatal("foreign user must not see reuse run")
	}
}

func TestContentReuseBudgetReservationIsIdempotentAndEnforcesDailyLimit(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	asset := model.NewContentAsset("user-budget", "预算选题")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	first := model.NewContentReuseRun("user-budget", asset.ID, "project-budget", model.PlatformWechat)
	first.Status, first.CreatedAt = model.ContentReuseRunStatusRunning, now
	if err := repo.ContentAssets().CreateReuseRun(ctx, first); err != nil {
		t.Fatal(err)
	}
	reserved, err := repo.ContentAssets().ReserveReuseRunBudget(ctx, first.ID, 60, 100, now)
	if err != nil || !reserved {
		t.Fatalf("first reservation = %v, %v", reserved, err)
	}
	reserved, err = repo.ContentAssets().ReserveReuseRunBudget(ctx, first.ID, 60, 100, now)
	if err != nil || !reserved {
		t.Fatalf("idempotent reservation = %v, %v", reserved, err)
	}
	second := model.NewContentReuseRun("user-budget", asset.ID, "project-budget", model.PlatformWechat)
	second.Status, second.CreatedAt = model.ContentReuseRunStatusRunning, now
	if err := repo.ContentAssets().CreateReuseRun(ctx, second); err != nil {
		t.Fatal(err)
	}
	reserved, err = repo.ContentAssets().ReserveReuseRunBudget(ctx, second.ID, 50, 100, now)
	if err != nil || reserved {
		t.Fatalf("over-budget reservation = %v, %v", reserved, err)
	}
	first, err = repo.ContentAssets().FindReuseRunByID(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	first.BudgetReserved, first.CostCredits = 0, 60
	if err := repo.ContentAssets().UpdateReuseRun(ctx, first); err != nil {
		t.Fatal(err)
	}
	reserved, err = repo.ContentAssets().ReserveReuseRunBudget(ctx, second.ID, 40, 100, now)
	if err != nil || !reserved {
		t.Fatalf("remaining budget reservation = %v, %v", reserved, err)
	}
}

func TestContentReuseStaleWorkerCannotOverwriteUserPause(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	run.Status = model.ContentReuseRunStatusPaused
	if err := repo.ContentAssets().UpdateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	stale.Stage = model.ContentReuseStagePublish
	if err := repo.ContentAssets().UpdateReuseRun(ctx, stale); err == nil {
		t.Fatal("stale worker overwrote user pause")
	}
	current, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.ContentReuseRunStatusPaused {
		t.Fatalf("status=%s", current.Status)
	}
	events, err := repo.ContentAssets().ListReuseAudit(ctx, run.UserID, run.ID)
	if err != nil || len(events) != 1 || events[0].FromState != "running:research" || events[0].ToState != "paused:research" {
		t.Fatalf("run transition audit=%#v err=%v", events, err)
	}
}

func TestReuseBudgetCountsAdmissionDayInsteadOfRunCreationDay(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	first := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	first.Status, first.CreatedAt = model.ContentReuseRunStatusRunning, now.Add(-48*time.Hour)
	if err := repo.ContentAssets().CreateReuseRun(ctx, first); err != nil {
		t.Fatal(err)
	}
	ok, err := repo.ContentAssets().ReserveReuseRunBudget(ctx, first.ID, 60, 100, now)
	if err != nil || !ok {
		t.Fatalf("first reservation = %v, %v", ok, err)
	}
	second := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	second.Status, second.CreatedAt = model.ContentReuseRunStatusRunning, now
	if err := repo.ContentAssets().CreateReuseRun(ctx, second); err != nil {
		t.Fatal(err)
	}
	ok, err = repo.ContentAssets().ReserveReuseRunBudget(ctx, second.ID, 50, 100, now)
	if err != nil || ok {
		t.Fatalf("previously queued work escaped today's budget: %v, %v", ok, err)
	}
}

func TestBudgetReservationInvalidatesStaleRunUpdates(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	ok, err := repo.ContentAssets().ReserveReuseRunBudget(ctx, run.ID, 60, 100, time.Now())
	if err != nil || !ok {
		t.Fatalf("reservation = %v, %v", ok, err)
	}
	run.Status = model.ContentReuseRunStatusPaused
	if err := repo.ContentAssets().UpdateReuseRun(ctx, run); err != ErrContentReuseRunConflict {
		t.Fatalf("stale update erased reservation: %v", err)
	}
}

func TestReuseBudgetRechecksUnboundReservationOnAdmissionDay(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(map[bool]string{false: "not yet admitted", true: "already admitted"}[bound], func(t *testing.T) {
			repo := New(setupTestDB(t))
			ctx := context.Background()
			now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
			a := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
			a.Status = model.ContentReuseRunStatusRunning
			if err := repo.ContentAssets().CreateReuseRun(ctx, a); err != nil {
				t.Fatal(err)
			}
			if ok, err := repo.ContentAssets().ReserveReuseRunBudget(ctx, a.ID, 100, 100, now.Add(-24*time.Hour)); err != nil || !ok {
				t.Fatalf("yesterday reserve: %v %v", ok, err)
			}
			if bound {
				a, _ = repo.ContentAssets().FindReuseRunByID(ctx, a.ID)
				a.TaskID = "admitted-task"
				if err := repo.ContentAssets().UpdateReuseRun(ctx, a); err != nil {
					t.Fatal(err)
				}
			}
			b := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
			b.Status = model.ContentReuseRunStatusRunning
			if err := repo.ContentAssets().CreateReuseRun(ctx, b); err != nil {
				t.Fatal(err)
			}
			if ok, err := repo.ContentAssets().ReserveReuseRunBudget(ctx, b.ID, 100, 100, now); err != nil || !ok {
				t.Fatalf("today reserve: %v %v", ok, err)
			}
			if ok, err := repo.ContentAssets().ReserveReuseRunBudget(ctx, a.ID, 100, 100, now); err != nil || ok != bound {
				t.Fatalf("resume admitted=%v: allowed=%v err=%v", bound, ok, err)
			}
		})
	}
}
