package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func setupTaskLifecycleTest(t *testing.T, taskType string) (*TaskService, repository.Repository, *model.Task, *model.TaskExecution) {
	t.Helper()
	ctx := context.Background()
	repo := repository.New(setupTaskTestDB(t))
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, taskType)
	executionID := uuid.NewString()
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: taskType,
		Status: model.TaskStatusRunning, CurrentExecutionID: &executionID,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	execution := &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Target: "test",
		Status: model.TaskExecutionRunning, Started: true,
	}
	if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	logger := zerolog.New(io.Discard)
	return newTestTaskService(repo, nil, nil, &logger, "", nil, nil), repo, task, execution
}

func validLifecyclePlan() []model.TaskLifecyclePlanStage {
	return []model.TaskLifecyclePlanStage{
		{ID: "research", Title: "研究素材", Goal: "确认事实与角度"},
		{ID: "writing", Title: "撰写内容", Goal: "形成完整初稿"},
		{ID: "review", Title: "质量复核", Goal: "完成最终交付"},
	}
}

func TestSetTaskProgressPlanValidatesAgentStages(t *testing.T) {
	tests := []struct {
		name   string
		stages []model.TaskLifecyclePlanStage
	}{
		{name: "too few", stages: validLifecyclePlan()[:1]},
		{name: "too many", stages: append(validLifecyclePlan(),
			model.TaskLifecyclePlanStage{ID: "four", Title: "Four"},
			model.TaskLifecyclePlanStage{ID: "five", Title: "Five"},
			model.TaskLifecyclePlanStage{ID: "six", Title: "Six"},
			model.TaskLifecyclePlanStage{ID: "seven", Title: "Seven"},
			model.TaskLifecyclePlanStage{ID: "eight", Title: "Eight"},
		)},
		{name: "non snake case", stages: []model.TaskLifecyclePlanStage{{ID: "research-stage", Title: "Research"}, {ID: "write", Title: "Write"}}},
		{name: "reserved id", stages: []model.TaskLifecyclePlanStage{{ID: "system_prepare", Title: "Prepare"}, {ID: "write", Title: "Write"}}},
		{name: "duplicate id", stages: []model.TaskLifecyclePlanStage{{ID: "write", Title: "Write"}, {ID: "write", Title: "Review"}}},
		{name: "blank title", stages: []model.TaskLifecyclePlanStage{{ID: "write", Title: " "}, {ID: "review", Title: "Review"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, task, execution := setupTaskLifecycleTest(t, model.PlatformMoments)
			if _, err := svc.SetTaskProgressPlan(context.Background(), task.ID, execution.ID, tt.stages); !errors.Is(err, ErrInvalidTaskLifecyclePlan) {
				t.Fatalf("SetTaskProgressPlan error = %v, want ErrInvalidTaskLifecyclePlan", err)
			}
			persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Lifecycle.Data().Revision != 0 {
				t.Fatalf("invalid plan mutated lifecycle: %#v", persisted.Lifecycle.Data())
			}
		})
	}
}

func TestTaskLifecycleAdvancesSequentiallyAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupTaskLifecycleTest(t, model.PlatformMoments)

	created, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan())
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.Revision != 1 || created.ExecutionID != execution.ID || len(created.Stages) != 3 {
		t.Fatalf("created lifecycle = %#v", created)
	}
	for _, stage := range created.Stages {
		if stage.Source != model.TaskLifecycleSourceAgent || stage.Kind != model.TaskLifecycleKindWork || stage.State != model.TaskLifecycleStatePending {
			t.Fatalf("created stage = %#v", stage)
		}
	}

	active, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateActive, "正在核验来源")
	if err != nil {
		t.Fatal(err)
	}
	if active.Revision != 2 || active.Stages[0].State != model.TaskLifecycleStateActive || active.Stages[0].StartedAt == nil || active.Stages[0].LatestUpdate != "正在核验来源" {
		t.Fatalf("active lifecycle = %#v", active)
	}
	duplicate, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateActive, "正在核验来源")
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Revision != active.Revision || !duplicate.UpdatedAt.Equal(active.UpdatedAt) {
		t.Fatalf("duplicate changed lifecycle: before=%#v after=%#v", active, duplicate)
	}
	if _, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "writing", model.TaskLifecycleStateActive, "越级"); !errors.Is(err, ErrTaskLifecycleOutOfOrder) {
		t.Fatalf("out-of-order error = %v", err)
	}

	completed, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateComplete, "研究完成")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Revision != 3 || completed.Stages[0].State != model.TaskLifecycleStateComplete || completed.Stages[0].CompletedAt == nil {
		t.Fatalf("completed lifecycle = %#v", completed)
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Lifecycle.Data().Revision != completed.Revision {
		t.Fatalf("persisted lifecycle = %#v", persisted.Lifecycle.Data())
	}
}

func TestArticleLifecycleAppendsServerOwnedPublicationStages(t *testing.T) {
	ctx := context.Background()
	svc, _, task, execution := setupTaskLifecycleTest(t, model.PlatformArticle)
	lifecycle, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan())
	if err != nil {
		t.Fatal(err)
	}
	if len(lifecycle.Stages) != 5 {
		t.Fatalf("article lifecycle stages = %#v", lifecycle.Stages)
	}
	draft := lifecycle.Stages[3]
	publication := lifecycle.Stages[4]
	if draft.ID != "system_draft" || draft.Title != "创建公众号草稿" || draft.Source != model.TaskLifecycleSourceServer || draft.Kind != model.TaskLifecycleKindDraft || draft.State != model.TaskLifecycleStatePending {
		t.Fatalf("draft stage = %#v", draft)
	}
	if publication.ID != "system_publication" || publication.Title != "正式发布" || publication.Source != model.TaskLifecycleSourceServer || publication.Kind != model.TaskLifecycleKindPublication || publication.State != model.TaskLifecycleStatePending {
		t.Fatalf("publication stage = %#v", publication)
	}
	replayed, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan())
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Revision != lifecycle.Revision {
		t.Fatalf("idempotent article plan revision = %d, want %d", replayed.Revision, lifecycle.Revision)
	}
}

func TestWechatPublicationLifecycleMapping(t *testing.T) {
	now := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		deliveryStatus  string
		publication     *model.WechatPublication
		wantDraft       string
		wantPublication string
	}{
		{name: "creating draft", deliveryStatus: model.TaskExecutionDraftDeliveryInFlight, publication: &model.WechatPublication{Status: model.WechatPublicationStatusDrafting}, wantDraft: model.TaskLifecycleStateActive, wantPublication: model.TaskLifecycleStatePending},
		{name: "draft created", deliveryStatus: model.TaskExecutionDraftDeliverySucceeded, publication: &model.WechatPublication{Status: model.WechatPublicationStatusDrafted, DraftMediaID: "draft-1"}, wantDraft: model.TaskLifecycleStateComplete, wantPublication: model.TaskLifecycleStatePending},
		{name: "recoverable preflight block", deliveryStatus: model.TaskExecutionDraftDeliveryBlocked, wantDraft: model.TaskLifecycleStateBlocked, wantPublication: model.TaskLifecycleStatePending},
		{name: "ambiguous draft result", deliveryStatus: model.TaskExecutionDraftDeliveryAmbiguous, publication: &model.WechatPublication{Status: model.WechatPublicationStatusDrafting, DraftAddAttemptedAt: &now}, wantDraft: model.TaskLifecycleStateBlocked, wantPublication: model.TaskLifecycleStatePending},
		{name: "formal publish processing", deliveryStatus: model.TaskExecutionDraftDeliverySucceeded, publication: &model.WechatPublication{Status: model.WechatPublicationStatusPublishing, DraftMediaID: "draft-1", SubmitAttemptedAt: &now}, wantDraft: model.TaskLifecycleStateComplete, wantPublication: model.TaskLifecycleStateActive},
		{name: "unsupported before submission", deliveryStatus: model.TaskExecutionDraftDeliverySucceeded, publication: &model.WechatPublication{Status: model.WechatPublicationStatusUnsupported, DraftMediaID: "draft-1"}, wantDraft: model.TaskLifecycleStateComplete, wantPublication: model.TaskLifecycleStateBlocked},
		{name: "unsupported after submission evidence", deliveryStatus: model.TaskExecutionDraftDeliverySucceeded, publication: &model.WechatPublication{Status: model.WechatPublicationStatusUnsupported, DraftMediaID: "draft-1", SubmitAttemptedAt: &now}, wantDraft: model.TaskLifecycleStateComplete, wantPublication: model.TaskLifecycleStateBlocked},
		{name: "article selection needed", deliveryStatus: model.TaskExecutionDraftDeliverySucceeded, publication: &model.WechatPublication{Status: model.WechatPublicationStatusNeedsSelection, DraftMediaID: "draft-1"}, wantDraft: model.TaskLifecycleStateComplete, wantPublication: model.TaskLifecycleStateBlocked},
		{name: "publish failed", deliveryStatus: model.TaskExecutionDraftDeliverySucceeded, publication: &model.WechatPublication{Status: model.WechatPublicationStatusPublishFailed, DraftMediaID: "draft-1", SubmitAttemptedAt: &now}, wantDraft: model.TaskLifecycleStateComplete, wantPublication: model.TaskLifecycleStateFailed},
		{name: "published", deliveryStatus: model.TaskExecutionDraftDeliverySucceeded, publication: &model.WechatPublication{Status: model.WechatPublicationStatusPublished, DraftMediaID: "draft-1", ArticleURL: "https://mp.weixin.qq.com/s/example"}, wantDraft: model.TaskLifecycleStateComplete, wantPublication: model.TaskLifecycleStateComplete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := deriveWechatPublicationLifecycle(tt.deliveryStatus, tt.publication)
			if view.Draft.State != tt.wantDraft || view.Publication.State != tt.wantPublication {
				t.Fatalf("lifecycle view = draft:%q publication:%q, want draft:%q publication:%q", view.Draft.State, view.Publication.State, tt.wantDraft, tt.wantPublication)
			}
		})
	}
}

func TestSyncWechatPublicationLifecyclePersistsOnlyVisibleChanges(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupTaskLifecycleTest(t, model.PlatformArticle)
	created, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, "", model.TaskExecutionDraftDeliverySucceeded, nil); err != nil {
		t.Fatal(err)
	}
	publication := &model.WechatPublication{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, UserID: task.UserID, ProjectID: task.ProjectID,
		Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted, DraftMediaID: "draft-1",
	}
	if err := repo.WechatPublications().Create(ctx, publication); err != nil {
		t.Fatal(err)
	}

	first, err := svc.SyncWechatPublicationLifecycle(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != created.Revision+1 || first.Stages[3].State != model.TaskLifecycleStateComplete || first.Stages[4].State != model.TaskLifecycleStatePending {
		t.Fatalf("synced lifecycle = %#v", first)
	}
	second, err := svc.SyncWechatPublicationLifecycle(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != first.Revision || second.UpdatedAt != first.UpdatedAt {
		t.Fatalf("idempotent sync changed lifecycle: first=%#v second=%#v", first, second)
	}
}

func TestSyncWechatPublicationLifecycleIgnoresPreviousExecutionPublication(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupTaskLifecycleTest(t, model.PlatformArticle)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	publication := &model.WechatPublication{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, UserID: task.UserID, ProjectID: task.ProjectID,
		Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusPublished,
		DraftMediaID: "old-draft", ArticleURL: "https://mp.weixin.qq.com/s/old",
	}
	if err := repo.WechatPublications().Create(ctx, publication); err != nil {
		t.Fatal(err)
	}

	newExecutionID := uuid.NewString()
	if updated, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, newExecutionID); err != nil || !updated {
		t.Fatalf("set resumed execution = %v, %v", updated, err)
	}
	if err := repo.Tasks().UpdateStatus(ctx, task.ID, model.TaskStatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: newExecutionID, TaskID: task.ID, Attempt: 2, Target: "test", Status: model.TaskExecutionRunning, Started: true,
	}); err != nil {
		t.Fatal(err)
	}
	planned, err := svc.SetTaskProgressPlan(ctx, task.ID, newExecutionID, validLifecyclePlan())
	if err != nil {
		t.Fatal(err)
	}

	synced, err := svc.SyncWechatPublicationLifecycle(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if synced.ExecutionID != newExecutionID || synced.Stages[3].State != model.TaskLifecycleStatePending || synced.Stages[4].State != model.TaskLifecycleStatePending {
		t.Fatalf("previous execution publication leaked into resumed lifecycle: planned=%#v synced=%#v", planned, synced)
	}
}

func TestSetTaskProgressPlanReplansOnlyMutableTail(t *testing.T) {
	ctx := context.Background()
	svc, _, task, execution := setupTaskLifecycleTest(t, model.PlatformMoments)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateActive, "researching"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateComplete, "done"); err != nil {
		t.Fatal(err)
	}

	replanned, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, []model.TaskLifecyclePlanStage{
		{ID: "research", Title: "研究素材", Goal: "确认事实与角度"},
		{ID: "draft", Title: "完成初稿"},
		{ID: "polish", Title: "编辑定稿"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if replanned.Revision != 4 || len(replanned.Stages) != 3 || replanned.Stages[0].State != model.TaskLifecycleStateComplete || replanned.Stages[1].ID != "draft" || replanned.Stages[2].ID != "polish" {
		t.Fatalf("replanned lifecycle = %#v", replanned)
	}

	_, err = svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, []model.TaskLifecyclePlanStage{
		{ID: "changed", Title: "篡改已完成阶段"},
		{ID: "draft", Title: "完成初稿"},
	})
	if !errors.Is(err, ErrTaskLifecycleImmutablePrefix) {
		t.Fatalf("immutable prefix error = %v", err)
	}
}

func TestTaskLifecycleRejectsStaleExecutionAndNormalizesTerminalState(t *testing.T) {
	ctx := context.Background()
	svc, _, task, execution := setupTaskLifecycleTest(t, model.PlatformMoments)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateActive, "working"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTaskProgress(ctx, task.ID, uuid.NewString(), "research", model.TaskLifecycleStateComplete, "stale"); !errors.Is(err, ErrStaleTaskExecution) {
		t.Fatalf("stale execution error = %v", err)
	}

	failed, err := svc.FinalizeTaskLifecycle(ctx, task.ID, execution.ID, model.TaskStatusFailed, "执行失败", model.LifecycleTerminalWork)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Stages[0].State != model.TaskLifecycleStateFailed || failed.Stages[0].LatestUpdate != "执行失败" {
		t.Fatalf("failed active stage = %#v", failed.Stages[0])
	}
	for _, stage := range failed.Stages[1:] {
		if stage.State != model.TaskLifecycleStateSkipped {
			t.Fatalf("remaining stage not skipped: %#v", stage)
		}
	}
}

func TestTaskLifecycleResumePreservesCompletedPrefix(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupTaskLifecycleTest(t, model.PlatformMoments)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateActive, "working"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "research", model.TaskLifecycleStateComplete, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTaskProgress(ctx, task.ID, execution.ID, "writing", model.TaskLifecycleStateActive, "writing"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FinalizeTaskLifecycle(ctx, task.ID, execution.ID, model.TaskStatusCancelled, "已取消", model.LifecycleTerminalWork); err != nil {
		t.Fatal(err)
	}

	newExecutionID := uuid.NewString()
	if updated, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, newExecutionID); err != nil || !updated {
		t.Fatalf("set resumed execution = %v, %v", updated, err)
	}
	if err := repo.Tasks().UpdateStatus(ctx, task.ID, model.TaskStatusRunning); err != nil {
		t.Fatal(err)
	}
	newExecution := &model.TaskExecution{ID: newExecutionID, TaskID: task.ID, Attempt: 2, Target: "test", Status: model.TaskExecutionRunning, Started: true}
	if err := repo.TaskExecutions().Create(ctx, newExecution); err != nil {
		t.Fatal(err)
	}

	resumed, err := svc.SetTaskProgressPlan(ctx, task.ID, newExecutionID, []model.TaskLifecyclePlanStage{
		{ID: "research", Title: "研究素材", Goal: "确认事实与角度"},
		{ID: "rewrite", Title: "重新撰写"},
		{ID: "review", Title: "质量复核"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ExecutionID != newExecutionID || resumed.Stages[0].State != model.TaskLifecycleStateComplete || resumed.Stages[1].ID != "rewrite" || resumed.Stages[1].State != model.TaskLifecycleStatePending {
		t.Fatalf("resumed lifecycle = %#v", resumed)
	}
}
