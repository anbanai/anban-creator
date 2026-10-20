package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func TestContentReusePublicationPolicyUsesFrozenRunSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset := model.NewContentAsset("user-policy", "选题")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("user-policy", asset.ID, "project-policy", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	run.PolicySnapshot, _ = json.Marshal(map[string]any{
		"draft_enabled":   true,
		"publish_enabled": false,
		"publish_windows": []map[string]string{{"start": "09:00", "end": "21:00", "timezone": "Asia/Shanghai"}},
	})
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	policy, err := contentReusePublicationPolicy(ctx, repo, &model.Task{UserID: "user-policy", ProjectID: "project-policy", ReuseRunID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.DraftEnabled || policy.PublishEnabled || len(policy.PublishWindows) != 1 {
		t.Fatalf("frozen policy = %#v", policy)
	}
}

func TestContentReusePublicationPolicyFailsClosedForMissingSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	policy, err := contentReusePublicationPolicy(ctx, repo, &model.Task{ReuseRunID: "missing"})
	if err == nil || policy.DraftEnabled || policy.PublishEnabled {
		t.Fatalf("missing run policy = %#v, err=%v", policy, err)
	}
}

func TestContentReuseTransientFailureUsesBoundedRetries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset := model.NewContentAsset("user-retry", "选题")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("user-retry", asset.ID, "project-retry", model.PlatformWechat)
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	for attempt := 1; attempt <= contentReuseMaxRetries; attempt++ {
		_ = svc.failReuseRun(ctx, run, "billing_unavailable", errors.New("temporary"))
		if attempt < contentReuseMaxRetries && run.Status != model.ContentReuseRunStatusQueued {
			t.Fatalf("attempt %d status = %q, want queued", attempt, run.Status)
		}
	}
	if run.Status != model.ContentReuseRunStatusFailed || run.RetryCount != contentReuseMaxRetries {
		t.Fatalf("final retry state = %#v", run)
	}
}

func TestContentReuseArtifactsRequireCanonicalNonemptyFiles(t *testing.T) {
	for _, tc := range []struct {
		name, state, prefix string
		size                int64
		want                bool
	}{
		{"pending before promotion", model.TaskFileStatePending, "output/", 10, true},
		{"delivered replay", model.TaskFileStateDelivered, "output/", 10, true},
		{"wrong directory", model.TaskFileStateDelivered, "private/", 10, false},
		{"empty files", model.TaskFileStatePending, "output/", 0, false},
		{"discarded files", "discarded", "output/", 10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{ReuseRunID: "run-1"}
			var files []*model.TaskFile
			for _, name := range []string{"content-reuse-brief.json", "topic-analysis.md", "content-lineage.json", "quality-report.json"} {
				files = append(files, &model.TaskFile{FilePath: tc.prefix + name, FileName: name, State: tc.state, FileSize: tc.size})
			}
			if got := validateContentReuseArtifacts(task, files); got.Valid != tc.want {
				t.Fatalf("validation = %#v, want valid %v", got, tc.want)
			}
		})
	}
}

func TestTopicPoolAddsReuseExistingAssetAcrossProjects(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset := model.NewContentAsset("user-shared", "跨项目选题")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	project := &model.Project{ID: "project-shared", UserID: "user-shared", Platform: model.PlatformWechat, Name: "公众号"}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	service := NewTopicPoolService(repo, &logger)
	items, err := service.Add(ctx, "user-shared", project.ID, []string{"跨项目选题"})
	if err != nil || len(items) != 1 {
		t.Fatalf("add topics = %#v, err=%v", items, err)
	}
	if items[0].ContentAssetID == nil || *items[0].ContentAssetID != asset.ID {
		t.Fatalf("asset link = %#v, want %s", items[0].ContentAssetID, asset.ID)
	}
}

type contentAssetTestEnqueuer struct {
	taskType string
	payload  []byte
	key      string
	calls    int
}

func (e *contentAssetTestEnqueuer) EnqueueUnique(taskType string, payload []byte, key string) (bool, error) {
	e.calls++
	e.taskType, e.payload, e.key = taskType, payload, key
	return true, nil
}

func TestContentAssetServiceCreatesIdempotentReuseRun(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(&contentAssetTestEnqueuer{})
	asset, err := svc.Create(ctx, CreateContentAssetParams{UserID: "user-1", Title: "选题"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: "user-1", AssetID: asset.ID, TargetProjectID: "project-1", TargetPlatform: "wechat", IdempotencyKey: "same"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: "user-1", AssetID: asset.ID, TargetProjectID: "project-1", TargetPlatform: "wechat", IdempotencyKey: "same"})
	if err != nil || first.ID != second.ID {
		t.Fatalf("idempotency = %q %q %v", first.ID, second.ID, err)
	}
	paused, err := svc.SetReuseRunStatus(ctx, "user-1", first.ID, model.ContentReuseRunStatusPaused)
	if err != nil || paused.Status != model.ContentReuseRunStatusPaused {
		t.Fatalf("pause = %#v %v", paused, err)
	}
	queued, err := svc.SetReuseRunStatus(ctx, "user-1", first.ID, model.ContentReuseRunStatusQueued)
	if err != nil || queued.Status != model.ContentReuseRunStatusQueued {
		t.Fatalf("resume = %#v %v", queued, err)
	}
	policy, err := svc.GetPolicy(ctx, "user-1")
	if err != nil || policy.Enabled || policy.PublishEnabled {
		t.Fatalf("default policy = %#v %v", policy, err)
	}
	policy.Enabled, policy.PublishEnabled, policy.DraftEnabled, policy.DailyBudgetCredits = true, true, true, 100
	policy.PublishWindows = []byte(`[{"start":"09:00","end":"21:00","timezone":"Asia/Shanghai"}]`)
	if _, err = svc.UpsertPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.GetPolicy(ctx, "user-1")
	if err != nil || !stored.Enabled || !stored.PublishEnabled || stored.DailyBudgetCredits != 100 {
		t.Fatalf("stored policy = %#v %v", stored, err)
	}
}

func TestContentAssetServiceResumeRequeuesRun(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	queue := &contentAssetTestEnqueuer{}
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(queue)
	asset, err := svc.Create(ctx, CreateContentAssetParams{UserID: "user-1", Title: "选题"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: "user-1", AssetID: asset.ID, TargetProjectID: "project-1", TargetPlatform: model.PlatformWechat, IdempotencyKey: "resume-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetReuseRunStatus(ctx, "user-1", run.ID, model.ContentReuseRunStatusPaused); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetReuseRunStatus(ctx, "user-1", run.ID, model.ContentReuseRunStatusQueued); err != nil {
		t.Fatal(err)
	}
	if queue.calls != 2 {
		t.Fatalf("enqueue calls = %d, want create plus resume", queue.calls)
	}
}

func TestContentAssetServiceCancelReleasesReservedBudget(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-budget-cancel", UserID: "user-budget-cancel", Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(&contentAssetTestEnqueuer{})
	asset, err := svc.Create(ctx, CreateContentAssetParams{UserID: "user-budget-cancel", Title: "预算回滚选题"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: "user-budget-cancel", AssetID: asset.ID, TargetProjectID: "project-budget-cancel", TargetPlatform: model.PlatformWechat, IdempotencyKey: "budget-cancel"})
	if err != nil {
		t.Fatal(err)
	}
	run.BudgetReserved = 40
	if err := repo.ContentAssets().UpdateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	cancelled, err := svc.SetReuseRunStatus(ctx, "user-budget-cancel", run.ID, model.ContentReuseRunStatusCancelled)
	if err != nil || cancelled.BudgetReserved != 0 {
		t.Fatalf("cancelled run = %#v, err=%v", cancelled, err)
	}
}

func TestContentAssetServiceRejectsProjectPlatformMismatch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(&contentAssetTestEnqueuer{})
	asset, err := svc.Create(ctx, CreateContentAssetParams{UserID: "user-1", Title: "选题"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: "user-1", AssetID: asset.ID, TargetProjectID: "project-1", TargetPlatform: model.PlatformSeednote, IdempotencyKey: "mismatch"}); err == nil {
		t.Fatal("target platform mismatch must be rejected")
	}
}

func TestContentAssetServiceEnqueuesReuseRunOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	queue := &contentAssetTestEnqueuer{}
	svc.SetEnqueuer(queue)
	asset, err := svc.Create(ctx, CreateContentAssetParams{UserID: "user-1", Title: "选题"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: "user-1", AssetID: asset.ID, TargetProjectID: "project-1", TargetPlatform: model.PlatformWechat, IdempotencyKey: "reuse-key"})
	if err != nil {
		t.Fatal(err)
	}
	if queue.taskType != "content:reuse" || queue.key != "content-reuse:"+run.ID || string(queue.payload) == "" {
		t.Fatalf("queue = %#v", queue)
	}
}

func TestContentAssetServiceProcessesReuseRunIntoTask(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	if err := db.AutoMigrate(&model.ContentAsset{}, &model.ContentAssetEvidence{}, &model.ContentAssetLink{}, &model.ContentReuseRun{}, &model.ContentReuseStageExecution{}, &model.ContentReuseAuditEvent{}, &model.ContentAutopilotPolicy{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	assetSvc := NewContentAssetService(repo)
	assetSvc.SetEnqueuer(&contentAssetTestEnqueuer{})
	assetSvc.SetTaskService(taskSvc)
	asset, err := assetSvc.Create(ctx, CreateContentAssetParams{UserID: userID, Title: "自动复用选题", CoreQuestion: "核心问题"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := assetSvc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: userID, AssetID: asset.ID, TargetProjectID: projectID, TargetPlatform: model.PlatformWechat, IdempotencyKey: "worker-run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := assetSvc.ProcessReuseRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	var task model.Task
	if err := db.Where("reuse_run_id = ?", run.ID).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.ContentAssetID != asset.ID || task.ProjectID != projectID {
		t.Fatalf("task provenance = %#v", task)
	}
}

func TestContentAssetServiceResumesRunningRunWithoutDuplicateTask(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	if err := db.AutoMigrate(&model.ContentAsset{}, &model.ContentAssetEvidence{}, &model.ContentAssetLink{}, &model.ContentReuseRun{}, &model.ContentReuseStageExecution{}, &model.ContentReuseAuditEvent{}, &model.ContentAutopilotPolicy{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	assetSvc := NewContentAssetService(repo)
	assetSvc.SetEnqueuer(&contentAssetTestEnqueuer{})
	assetSvc.SetTaskService(taskSvc)
	asset, err := assetSvc.Create(ctx, CreateContentAssetParams{UserID: userID, Title: "可恢复选题"})
	if err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun(userID, asset.ID, projectID, model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	run.Stage = model.ContentReuseStageBrief
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := assetSvc.ProcessReuseRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	var tasks []model.Task
	if err := db.Where("reuse_run_id = ?", run.ID).Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("resumed tasks = %d, want one", len(tasks))
	}
	if err := assetSvc.ProcessReuseRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("reuse_run_id = ?", run.ID).Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("replayed tasks = %d, want one", len(tasks))
	}
}

func TestContentReuseTransientFailureHonorsPausedFailureMode(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset := model.NewContentAsset("user-pause", "暂停选题")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("user-pause", asset.ID, "project-pause", model.PlatformWechat)
	run.PolicySnapshot = []byte(`{"failure_mode":"pause"}`)
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	for attempt := 1; attempt <= contentReuseMaxRetries; attempt++ {
		_ = svc.failReuseRun(ctx, run, "billing_unavailable", errors.New("temporary"))
	}
	if run.Status != model.ContentReuseRunStatusPaused {
		t.Fatalf("final retry state = %q, want paused", run.Status)
	}
	if run.Stage != model.ContentReuseStageResearch {
		t.Fatalf("paused stage = %q, want last durable stage research", run.Stage)
	}
}

func TestContentAssetServiceFinalizesReuseRunFromTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset, err := NewContentAssetService(repo).Create(ctx, CreateContentAssetParams{UserID: "user-1", Title: "选题"})
	if err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("user-1", asset.ID, "project-1", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	if err := svc.FinalizeReuseTask(ctx, &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", ReuseRunID: run.ID, Status: model.TaskStatusCompleted, Outcome: &model.TaskOutcome{Review: model.TaskReviewOutcome{Status: model.TaskReviewPassed}, Publication: model.TaskPublicationOutcome{Status: model.TaskPublicationNotRequested}}}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil || got.Status != model.ContentReuseRunStatusCompleted || got.Stage != model.ContentReuseStageObserve {
		t.Fatalf("run = %#v, err=%v", got, err)
	}
}

func TestContentAssetServicePausesReuseRunWhenReviewEvidenceIsUnavailable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset, err := NewContentAssetService(repo).Create(ctx, CreateContentAssetParams{UserID: "user-review", Title: "审核选题"})
	if err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("user-review", asset.ID, "project-review", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	err = svc.FinalizeReuseTask(ctx, &model.Task{ID: "task-review", UserID: "user-review", ProjectID: "project-review", ReuseRunID: run.ID, Status: model.TaskStatusCompleted, Outcome: &model.TaskOutcome{Review: model.TaskReviewOutcome{Status: model.TaskReviewUnavailable}, Publication: model.TaskPublicationOutcome{Status: model.TaskPublicationNotRequested}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.ContentReuseRunStatusPaused || got.Stage != model.ContentReuseStageReview {
		t.Fatalf("run = %#v, want paused review", got)
	}
}

func TestContentAssetServiceRejectsIdempotencyKeyReuseWithDifferentRequest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-2", UserID: "user-1", Platform: model.PlatformSeednote, Name: "小红书"}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(&contentAssetTestEnqueuer{})
	asset, err := svc.Create(ctx, CreateContentAssetParams{UserID: "user-1", Title: "选题"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: "user-1", AssetID: asset.ID, TargetProjectID: "project-1", TargetPlatform: "wechat", IdempotencyKey: "same"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: "user-1", AssetID: asset.ID, TargetProjectID: "project-2", TargetPlatform: "seednote", IdempotencyKey: "same"}); err != ErrContentReuseConflict {
		t.Fatalf("error = %v, want %v", err, ErrContentReuseConflict)
	}
}

func TestContentAssetServiceReturnsPersistedLinkOnDuplicate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-link", UserID: "user-1", Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-link", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	asset, err := svc.Create(ctx, CreateContentAssetParams{UserID: "user-1", Title: "选题"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.LinkContent(ctx, LinkContentAssetParams{UserID: "user-1", AssetID: asset.ID, ContentKind: "task", ContentID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.LinkContent(ctx, LinkContentAssetParams{UserID: "user-1", AssetID: asset.ID, ContentKind: "task", ContentID: "task-1"})
	if err != nil || first.ID != second.ID {
		t.Fatalf("links = %q %q, err=%v", first.ID, second.ID, err)
	}
}

func TestContentAssetServiceValidatesAutopilotPolicyBounds(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repository.New(db))
	policy := model.NewContentAutopilotPolicy("user-1")
	policy.DailyBudgetCredits = -1
	if _, err := svc.UpsertPolicy(context.Background(), policy); err == nil {
		t.Fatal("negative daily budget must be rejected")
	}
	policy.DailyBudgetCredits = 10
	policy.FailureMode = "continue"
	if _, err := svc.UpsertPolicy(context.Background(), policy); err == nil {
		t.Fatal("unknown failure mode must be rejected")
	}
}

func TestContentAssetServiceValidatesPlatformScoreOverrides(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	svc := NewContentAssetService(repo)
	policy := model.NewContentAutopilotPolicy("user-1")
	policy.PlatformMinRecommendationScores = []byte(`{"wechat":70,"seednote":55}`)
	policy.VideoFirst = true
	if _, err := svc.UpsertPolicy(context.Background(), policy); err != nil {
		t.Fatalf("valid platform overrides should save: %v", err)
	}
	stored, err := svc.GetPolicy(context.Background(), "user-1")
	if err != nil || !stored.VideoFirst || string(stored.PlatformMinRecommendationScores) != string(policy.PlatformMinRecommendationScores) {
		t.Fatalf("stored policy lost platform/video-first fields: %#v err=%v", stored, err)
	}
	policy.PlatformMinRecommendationScores = []byte(`{"unknown":50}`)
	if _, err := svc.UpsertPolicy(context.Background(), policy); err == nil {
		t.Fatal("unsupported platform override must be rejected")
	}
}

func TestContentAssetServiceDoesNotAllowClientStatusEscalation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repository.New(db))
	if _, err := svc.Create(context.Background(), CreateContentAssetParams{UserID: "user-1", Title: "非法状态", Status: model.ContentAssetStatusReady}); err == nil {
		t.Fatal("client must not create an asset as ready")
	}
	asset, err := svc.Create(context.Background(), CreateContentAssetParams{UserID: "user-1", Title: "选题"})
	if err != nil || asset.Status != model.ContentAssetStatusCandidate {
		t.Fatalf("candidate asset = %#v, err=%v", asset, err)
	}
	status := model.ContentAssetStatusValidated
	if _, err := svc.Update(context.Background(), UpdateContentAssetParams{UserID: "user-1", AssetID: asset.ID, Status: &status}); err == nil {
		t.Fatal("client must not promote an asset to validated")
	}
	lifecycle := "unknown"
	if _, err := svc.Update(context.Background(), UpdateContentAssetParams{UserID: "user-1", AssetID: asset.ID, Lifecycle: &lifecycle}); err == nil {
		t.Fatal("unknown lifecycle must be rejected")
	}
}

func TestContentAssetServiceValidatesLinkedContentOwnership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-2", UserID: "user-2", Platform: model.PlatformWechat, Name: "他人项目"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: "task-1", UserID: "user-2", ProjectID: "project-2", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	asset, err := NewContentAssetService(repo).Create(ctx, CreateContentAssetParams{UserID: "user-1", Title: "选题"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	if _, err := svc.LinkContent(ctx, LinkContentAssetParams{UserID: "user-1", AssetID: asset.ID, ProjectID: "project-1", ContentKind: "task", ContentID: "task-1"}); err == nil {
		t.Fatal("cross-user task link must be rejected")
	}
	if _, err := svc.LinkContent(ctx, LinkContentAssetParams{UserID: "user-1", AssetID: asset.ID, ContentKind: "unknown", ContentID: "arbitrary"}); err == nil {
		t.Fatal("unknown content kind must be rejected")
	}
}

func TestContentAssetScoringPersistsInsufficientEvidenceWithoutInventingScore(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-score", UserID: "user-score", Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: "task-score", UserID: "user-score", ProjectID: "project-score", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	asset, err := svc.Create(ctx, CreateContentAssetParams{UserID: "user-score", Title: "选题"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkContent(ctx, LinkContentAssetParams{UserID: "user-score", AssetID: asset.ID, ContentKind: "task", ContentID: "task-score"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.ScoreAssetForProject(ctx, "user-score", asset.ID, "project-score")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "data_insufficient" || snapshot.Confidence != "low" || snapshot.AnalyticsRevision != 0 || snapshot.Score != 0 {
		t.Fatalf("score snapshot = %#v", snapshot)
	}
	stored, err := repo.ContentAssets().ListScoreSnapshots(ctx, "user-score", asset.ID)
	if err != nil || len(stored) != 1 || stored[0].ID != snapshot.ID {
		t.Fatalf("stored snapshots = %#v, err=%v", stored, err)
	}
}

func TestContentAssetServiceListsServerRecommendationsWithLineageSummary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset, err := NewContentAssetService(repo).Create(ctx, CreateContentAssetParams{UserID: "user-recommend", Title: "可复用选题"})
	if err != nil {
		t.Fatal(err)
	}
	asset.Status = model.ContentAssetStatusReady
	if err := repo.ContentAssets().Update(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-recommend", UserID: asset.UserID, Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsState{ProjectID: "project-recommend", Revision: 9, ActiveGeneration: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	asOf := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	if err := repo.ContentAssets().CreateScoreSnapshot(ctx, &model.ContentAssetScoreSnapshot{
		ID: uuid.NewString(), AssetID: asset.ID, UserID: asset.UserID, ProjectID: "project-recommend", Platform: model.PlatformWechat,
		AnalyticsRevision: 9, Status: "scored", Score: 88, Confidence: "high", DataAsOf: &asOf,
		Snapshot: []byte(`{"recommendation_reason":"公众号历史表现位于账号前列"}`), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun(asset.UserID, asset.ID, "project-recommend", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	run.Stage = model.ContentReuseStageReview
	run.BudgetReserved = 120
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	recommendations, err := NewContentAssetService(repo).ListRecommendations(ctx, asset.UserID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recommendations) != 1 {
		t.Fatalf("recommendations = %d, want one", len(recommendations))
	}
	got := recommendations[0]
	if got.Asset.ID != asset.ID || got.Score == nil || got.Score.Score != 88 || got.RecommendedPlatform != model.PlatformWechat || got.RecommendationReason != "公众号历史表现位于账号前列" {
		t.Fatalf("recommendation = %#v", got)
	}
	if got.EstimatedCostCredits != 120 || got.AutomationStatus != model.ContentReuseRunStatusRunning || got.AutomationStage != model.ContentReuseStageReview || got.EvidenceAsOf == nil || !got.EvidenceAsOf.Equal(asOf) {
		t.Fatalf("automation summary = %#v", got)
	}
}

func TestContentAssetRecommendationsChooseBestPlatformSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset, err := NewContentAssetService(repo).Create(ctx, CreateContentAssetParams{UserID: "user-recommend-best", Title: "跨平台选题"})
	if err != nil {
		t.Fatal(err)
	}
	asset.Status = model.ContentAssetStatusReady
	if err := repo.ContentAssets().Update(ctx, asset); err != nil {
		t.Fatal(err)
	}
	for _, project := range []*model.Project{
		{ID: "wechat-project", UserID: asset.UserID, Platform: model.PlatformWechat, Name: "公众号"},
		{ID: "seednote-project", UserID: asset.UserID, Platform: model.PlatformSeednote, Name: "小红书"},
	} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatal(err)
		}
		revision := int64(2)
		if project.ID == "seednote-project" {
			revision = 3
		}
		if err := db.Create(&model.AnalyticsState{ProjectID: project.ID, Revision: revision, ActiveGeneration: 1, Status: "ready"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	older := now.Add(-time.Hour)
	newer := now
	for _, snapshot := range []*model.ContentAssetScoreSnapshot{
		{ID: uuid.NewString(), AssetID: asset.ID, UserID: asset.UserID, ProjectID: "wechat-project", Platform: model.PlatformWechat, AnalyticsRevision: 2, Status: "scored", Score: 92, Confidence: "high", DataAsOf: &older, Snapshot: []byte(`{"recommendation_reason":"公众号平台表现更强"}`), CreatedAt: older},
		{ID: uuid.NewString(), AssetID: asset.ID, UserID: asset.UserID, ProjectID: "seednote-project", Platform: model.PlatformSeednote, AnalyticsRevision: 3, Status: "scored", Score: 51, Confidence: "medium", DataAsOf: &newer, Snapshot: []byte(`{"recommendation_reason":"小红书数据较新"}`), CreatedAt: newer},
	} {
		if err := repo.ContentAssets().CreateScoreSnapshot(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	recommendations, err := NewContentAssetService(repo).ListRecommendations(ctx, asset.UserID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recommendations) != 1 || recommendations[0].RecommendedPlatform != model.PlatformWechat || recommendations[0].Score == nil || recommendations[0].Score.Score != 92 {
		t.Fatalf("recommendation = %#v, want best platform snapshot", recommendations)
	}
}

func TestContentReuseRunDoesNotRemainQueuedWithoutWorkerQueue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project-no-queue", UserID: "user-no-queue", Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	asset, err := NewContentAssetService(repo).Create(ctx, CreateContentAssetParams{UserID: "user-no-queue", Title: "选题"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewContentAssetService(repo).CreateReuseRun(ctx, CreateReuseRunParams{UserID: "user-no-queue", AssetID: asset.ID, TargetProjectID: "project-no-queue", TargetPlatform: model.PlatformWechat, IdempotencyKey: "no-queue"}); err == nil {
		t.Fatal("run creation must fail without a configured worker queue")
	}
	var count int64
	if err := db.Model(&model.ContentReuseRun{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("persisted runs = %d, err=%v", count, err)
	}
}

func TestContentAutopilotTickEnqueuesOnlyCurrentScoredAssetOncePerDay(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	userID, projectID := "user-autopilot", "project-autopilot"
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformWechat, Name: "公众号"}); err != nil {
		t.Fatal(err)
	}
	asset, err := NewContentAssetService(repo).Create(ctx, CreateContentAssetParams{UserID: userID, Title: "已验证选题"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewContentAssetService(repo).ApplyResearchStatus(ctx, userID, asset.ID, model.ContentAssetStatusResearched, "verified"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewContentAssetService(repo).ApplyResearchStatus(ctx, userID, asset.ID, model.ContentAssetStatusValidated, "verified"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsState{ProjectID: projectID, Revision: 4, ActiveGeneration: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	policy := model.NewContentAutopilotPolicy(userID)
	policy.Enabled = true
	policy.ProjectIDs = []byte(`["project-autopilot"]`)
	policy.Platforms = []byte(`["wechat"]`)
	policy.MinRecommendationScore = 60
	if err := repo.ContentAssets().UpsertPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	asOf := time.Now().UTC().Add(-time.Hour)
	score := &model.ContentAssetScoreSnapshot{ID: uuid.NewString(), AssetID: asset.ID, UserID: userID, ProjectID: projectID, Platform: model.PlatformWechat, AnalyticsRevision: 4, Status: "scored", Score: 75, Confidence: "medium", DataAsOf: &asOf, Snapshot: []byte(`{"schema_version":1,"platform":"wechat"}`), CreatedAt: time.Now().UTC()}
	if err := repo.ContentAssets().CreateScoreSnapshot(ctx, score); err != nil {
		t.Fatal(err)
	}
	queue := &contentAssetTestEnqueuer{}
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(queue)
	now := time.Now().UTC()
	created, err := svc.RunAutopilotTick(ctx, now)
	if err != nil || created != 1 {
		t.Fatalf("first tick = created %d, err=%v", created, err)
	}
	created, err = svc.RunAutopilotTick(ctx, now.Add(time.Minute))
	if err != nil || created != 0 || queue.calls != 1 {
		t.Fatalf("repeated tick = created %d, enqueues %d, err=%v", created, queue.calls, err)
	}
}

func TestContentReusePublicationRejectsStoppedOrUnboundTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	for _, tc := range []struct{ name, status, user, project, task string }{
		{"paused", model.ContentReuseRunStatusPaused, "owner", "project", "task"},
		{"cancelled", model.ContentReuseRunStatusCancelled, "owner", "project", "task"},
		{"other user", model.ContentReuseRunStatusRunning, "other", "project", "task"},
		{"other project", model.ContentReuseRunStatusRunning, "owner", "other", "task"},
		{"other task", model.ContentReuseRunStatusRunning, "owner", "project", "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
			run.Status, run.TaskID = tc.status, "task"
			run.PolicySnapshot = []byte(`{"draft_enabled":true,"publish_enabled":true}`)
			if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			policy, err := contentReusePublicationPolicy(ctx, repo, &model.Task{ID: tc.task, UserID: tc.user, ProjectID: tc.project, ReuseRunID: run.ID})
			if err == nil || policy.DraftEnabled || policy.PublishEnabled {
				t.Fatalf("unsafe policy = %#v, err=%v", policy, err)
			}
		})
	}
}

func TestContentReuseLateCompletionPreservesUserControl(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	for _, status := range []string{model.ContentReuseRunStatusCancelled, model.ContentReuseRunStatusPaused} {
		t.Run(status, func(t *testing.T) {
			run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
			run.Status, run.TaskID, run.Stage = status, "task", model.ContentReuseStageProduce
			if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			task := &model.Task{ID: "task", UserID: "owner", ProjectID: "project", ReuseRunID: run.ID, Status: model.TaskStatusCompleted, Outcome: &model.TaskOutcome{Review: model.TaskReviewOutcome{Status: model.TaskReviewPassed}, Publication: model.TaskPublicationOutcome{Status: model.TaskPublicationNotRequested}}}
			if err := NewContentAssetService(repo).FinalizeReuseTask(ctx, task); err != nil {
				t.Fatal(err)
			}
			got, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != status || got.Stage != model.ContentReuseStageProduce {
				t.Fatalf("late callback overrode control: %#v", got)
			}
		})
	}
}

func TestContentReuseContextIsBoundToTaskAndUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset := model.NewContentAsset("owner", "verified topic")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("owner", asset.ID, "target-project", model.PlatformWechat)
	run.TaskID, run.Status = "task", model.ContentReuseRunStatusRunning
	run.PolicySnapshot = []byte(`{"draft_enabled":false}`)
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task", UserID: "owner", ProjectID: "target-project", ReuseRunID: run.ID, ContentAssetID: asset.ID}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	for _, tc := range []struct {
		name, user, project, task string
		allowed                   bool
	}{
		{"bound cross-project asset", "owner", "target-project", "task", true},
		{"other user", "other", "target-project", "task", false},
		{"other project", "owner", "other-project", "task", false},
		{"missing task", "owner", "target-project", "missing", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.GetReuseContext(ctx, tc.user, tc.project, tc.task)
			if !tc.allowed {
				if err == nil {
					t.Fatalf("unauthorized context: %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Asset.ID != asset.ID || got.Run.ID != run.ID {
				t.Fatalf("wrong context: %#v", got)
			}
		})
	}
}

func TestContentReuseWaitsForDurableFormalPublication(t *testing.T) {
	for _, tc := range []struct {
		status   string
		complete bool
	}{
		{model.WechatPublicationStatusDrafted, false},
		{model.WechatPublicationStatusPublishing, false},
		{model.WechatPublicationStatusAmbiguous, false},
		{model.WechatPublicationStatusPublished, true},
	} {
		t.Run(tc.status, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := model.AutoMigrate(db); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			repo := repository.New(db)
			svc := NewContentAssetService(repo)
			if err := repo.Projects().Create(ctx, &model.Project{ID: "project", UserID: "owner", Platform: model.PlatformWechat, Name: "target"}); err != nil {
				t.Fatal(err)
			}
			asset := model.NewContentAsset("owner", "publication topic")
			if err := repo.ContentAssets().Create(ctx, asset); err != nil {
				t.Fatal(err)
			}
			run := model.NewContentReuseRun("owner", asset.ID, "project", model.PlatformWechat)
			run.Status, run.TaskID = model.ContentReuseRunStatusRunning, "task"
			run.PolicySnapshot = []byte(`{"draft_enabled":true,"publish_enabled":true}`)
			if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			publication := &model.WechatPublication{ID: "publication", TaskID: "task", UserID: "owner", ProjectID: "project", Source: model.WechatPublicationSourceAnbanAPI, Status: tc.status}
			if err := repo.WechatPublications().Create(ctx, publication); err != nil {
				t.Fatal(err)
			}
			task := &model.Task{ID: "task", UserID: "owner", ProjectID: "project", ContentAssetID: asset.ID, ReuseRunID: run.ID, Status: model.TaskStatusCompleted, Outcome: &model.TaskOutcome{Review: model.TaskReviewOutcome{Status: model.TaskReviewPassed}, Publication: model.TaskPublicationOutcome{Status: model.TaskPublicationSucceeded}}}
			if err := svc.FinalizeReuseTask(ctx, task); err != nil {
				t.Fatal(err)
			}
			got, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (got.Status == model.ContentReuseRunStatusCompleted) != tc.complete {
				t.Fatalf("status=%s for publication=%s", got.Status, tc.status)
			}
			if !tc.complete && got.Stage != model.ContentReuseStagePublish {
				t.Fatalf("stage=%s", got.Stage)
			}
			if tc.complete {
				links, err := repo.ContentAssets().ListLinks(ctx, "owner", asset.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(links) != 1 || links[0].ContentKind != "wechat_publication" || links[0].ContentID != "publication" {
					t.Fatalf("missing publication lineage: %#v", links)
				}
			}
		})
	}
}

func TestPublicationReconciliationCompletesReuseRun(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := db.AutoMigrate(&model.ContentAsset{}, &model.ContentAssetEvidence{}, &model.ContentAssetLink{}, &model.ContentReuseRun{}, &model.ContentReuseAuditEvent{}, &model.ContentAutopilotPolicy{}, &model.BillingCharge{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	svc := NewContentAssetService(repo)
	if err := repo.Projects().Create(ctx, &model.Project{ID: "project", UserID: "owner", Platform: model.PlatformWechat, Name: "target"}); err != nil {
		t.Fatal(err)
	}
	asset := model.NewContentAsset("owner", "reconciled")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("owner", asset.ID, "project", model.PlatformWechat)
	run.TaskID, run.Status, run.Stage = "task", model.ContentReuseRunStatusRunning, model.ContentReuseStagePublish
	run.PolicySnapshot = []byte(`{"draft_enabled":true,"publish_enabled":true}`)
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task", UserID: "owner", ProjectID: "project", ContentAssetID: asset.ID, ReuseRunID: run.ID, Status: model.TaskStatusCompleted, Outcome: &model.TaskOutcome{Review: model.TaskReviewOutcome{Status: model.TaskReviewPassed}, Publication: model.TaskPublicationOutcome{Status: model.TaskPublicationSucceeded}}}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	publication := &model.WechatPublication{ID: "publication", TaskID: "task", UserID: "owner", ProjectID: "project", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusPublished}
	if err := repo.WechatPublications().Create(ctx, publication); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	taskSvc.SetReuseRunFinalizer(svc.FinalizeReuseTask)
	if _, err := taskSvc.SyncWechatPublicationLifecycle(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	current, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.ContentReuseRunStatusCompleted {
		t.Fatalf("reconciliation left reuse run %s", current.Status)
	}
}

func TestCancelledProductionKeepsBudgetUntilTaskSettles(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := db.AutoMigrate(&model.ContentReuseRun{}, &model.ContentReuseStageExecution{}, &model.ContentReuseAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	svc := NewContentAssetService(repo)
	run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	run.Status, run.TaskID, run.BudgetReserved = model.ContentReuseRunStatusRunning, "task", 100
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task", UserID: "owner", ProjectID: "project", ReuseRunID: run.ID, Status: model.TaskStatusRunning, BillingPriceCredits: 100}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	cancelled, err := svc.SetReuseRunStatus(ctx, "owner", run.ID, model.ContentReuseRunStatusCancelled)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.BudgetReserved != 100 {
		t.Fatalf("running production released budget: %d", cancelled.BudgetReserved)
	}
	task.Status = model.TaskStatusCompleted
	if err := svc.FinalizeReuseTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.ContentReuseRunStatusCancelled || got.CostCredits != 100 || got.BudgetReserved != 0 {
		t.Fatalf("cancelled accounting=%#v", got)
	}
}

func TestResumeReuseRunReplaysCompletedTaskFinalization(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := db.AutoMigrate(&model.ContentReuseRun{}, &model.ContentReuseAuditEvent{}, &model.ContentAsset{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	svc := NewContentAssetService(repo)
	logger := zerolog.New(io.Discard)
	svc.SetTaskService(newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil))
	svc.SetEnqueuer(&contentAssetTestEnqueuer{})
	run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	run.Status, run.TaskID, run.Stage = model.ContentReuseRunStatusPaused, "task", model.ContentReuseStageProduce
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task", UserID: "owner", ProjectID: "project", ReuseRunID: run.ID, Status: model.TaskStatusCompleted, Outcome: &model.TaskOutcome{Review: model.TaskReviewOutcome{Status: model.TaskReviewPassed}, Publication: model.TaskPublicationOutcome{Status: model.TaskPublicationNotRequested}}}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetReuseRunStatus(ctx, "owner", run.ID, model.ContentReuseRunStatusQueued); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessReuseRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.ContentReuseRunStatusCompleted {
		t.Fatalf("resumed run=%s", got.Status)
	}
}

func TestReuseTaskAdmissionChecksRunInsideTransaction(t *testing.T) {
	for _, tc := range []struct {
		name, status                          string
		wrongAsset, alreadyBound, wantAllowed bool
	}{
		{name: "active", status: model.ContentReuseRunStatusRunning, wantAllowed: true},
		{name: "paused after worker preflight", status: model.ContentReuseRunStatusPaused},
		{name: "cancelled after worker preflight", status: model.ContentReuseRunStatusCancelled},
		{name: "wrong asset", status: model.ContentReuseRunStatusRunning, wrongAsset: true},
		{name: "second admission", status: model.ContentReuseRunStatusRunning, alreadyBound: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTaskTestDB(t)
			if err := db.AutoMigrate(&model.ContentReuseRun{}); err != nil {
				t.Fatal(err)
			}
			repo := repository.New(db)
			ctx := context.Background()
			userID := uuid.NewString()
			projectID := createTestProject(t, repo, userID, model.PlatformWechat)
			run := model.NewContentReuseRun(userID, uuid.NewString(), projectID, model.PlatformWechat)
			run.Status = tc.status
			if tc.alreadyBound {
				run.TaskID = "existing-task"
			}
			if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, AgentID: "article", Type: model.TaskTypeWechatArticle, ReuseRunID: run.ID, ContentAssetID: run.AssetID}
			if tc.wrongAsset {
				task.ContentAssetID = uuid.NewString()
			}
			svc := &TaskService{repo: repo}
			err := svc.persistTaskWithFixedAdmission(ctx, task)
			if (err == nil) != tc.wantAllowed {
				t.Fatalf("admission error = %v, allowed = %v", err, tc.wantAllowed)
			}
			stored, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&model.Task{}).Where("reuse_run_id = ?", run.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if tc.wantAllowed {
				if count != 1 || stored.TaskID != task.ID || stored.Version != run.Version+1 {
					t.Fatalf("admission not atomically bound: count=%d run=%+v", count, stored)
				}
			} else if count != 0 || stored.TaskID != run.TaskID || stored.Status != tc.status {
				t.Fatalf("rejected admission mutated state: count=%d run=%+v", count, stored)
			}
		})
	}
}

func TestReuseAdmissionConsumesFrozenQuoteAndRollsBackWhenStopped(t *testing.T) {
	for _, status := range []string{model.ContentReuseRunStatusRunning, model.ContentReuseRunStatusCancelled} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			svc, f, _ := newFixedTaskBillingFixture(t, 1500, 0)
			projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformWechat)
			quote := f.quote(t, billingWalletUserID, "task.wechat_article", "", "reuse-frozen")
			run := model.NewContentReuseRun(billingWalletUserID, uuid.NewString(), projectID, model.PlatformWechat)
			run.Status, run.BillingQuoteID, run.BillingFingerprint = status, quote.ID, quote.RequestFingerprint
			if err := f.repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			task := &model.Task{ID: uuid.NewString(), UserID: billingWalletUserID, ProjectID: projectID, AgentID: "article", Type: model.TaskTypeWechatArticle, ExecutionProfile: "effective", ReuseRunID: run.ID, ContentAssetID: run.AssetID}
			err := svc.persistTasksWithFixedAdmission(ctx, []*model.Task{task}, CreateManualParams{BillingQuoteID: quote.ID, BillingRequestFingerprint: quote.RequestFingerprint})
			if status == model.ContentReuseRunStatusRunning {
				if err != nil {
					t.Fatalf("frozen reuse quote rejected: %v", err)
				}
				stored, err := f.repo.Tasks().FindByID(ctx, task.ID)
				if err != nil || stored.BillingQuoteID != quote.ID || stored.BillingChargeID == nil || stored.BillingPriceCredits != quote.PriceCredits {
					t.Fatalf("task charge = %+v, %v", stored, err)
				}
			} else {
				if !errors.Is(err, repository.ErrContentReuseRunConflict) {
					t.Fatalf("expected admission conflict after quote validation, got %v", err)
				}
				charge, err := f.repo.Billing().FindChargeByTask(ctx, task.ID)
				if !errors.Is(err, gorm.ErrRecordNotFound) || charge != nil {
					t.Fatalf("rejected admission retained charge: %+v, %v", charge, err)
				}
			}
		})
	}
}

func TestAutopilotRefreshesMissingAndChangedRevisionScores(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	asset := model.NewContentAsset(userID, "自动重评分")
	asset.Status = model.ContentAssetStatusValidated
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsState{ProjectID: projectID, Revision: 1, ActiveGeneration: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	policy := model.NewContentAutopilotPolicy(userID)
	policy.Enabled = true
	policy.ProjectIDs, _ = json.Marshal([]string{projectID})
	policy.Platforms = []byte(`["wechat"]`)
	if err := repo.ContentAssets().UpsertPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(&contentAssetTestEnqueuer{})
	now := time.Now().UTC()
	for tick := 0; tick < 3; tick++ {
		if tick == 2 {
			if err := db.Model(&model.AnalyticsState{}).Where("project_id = ?", projectID).Update("revision", 2).Error; err != nil {
				t.Fatal(err)
			}
		}
		created, err := svc.RunAutopilotTick(ctx, now.Add(time.Duration(tick)*time.Minute))
		if err != nil || created != 0 {
			t.Fatalf("insufficient evidence must not produce: %d, %v", created, err)
		}
		scores, err := repo.ContentAssets().ListScoreSnapshots(ctx, userID, asset.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if tick == 2 {
			want = 2
		}
		if len(scores) != want {
			t.Fatalf("tick %d snapshots=%d, want %d", tick, len(scores), want)
		}
		if scores[0].Status != "data_insufficient" || scores[0].AnalyticsRevision != int64(want) {
			t.Fatalf("latest score=%+v", scores[0])
		}
	}
}

func TestContentAssetIngestPersistsMultipleEvidenceAtomically(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	svc := NewContentAssetService(repo)
	ctx := context.Background()
	detail, err := svc.Ingest(ctx, IngestContentAssetParams{CreateContentAssetParams: CreateContentAssetParams{UserID: "owner", Title: "two sources"}, Evidence: []model.ContentAssetEvidence{{Title: "first", URL: "https://example.com/first"}, {Title: "second", URL: "https://example.com/second"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Evidence) != 2 || detail.Evidence[0].ID == "" || detail.Evidence[0].ID == detail.Evidence[1].ID {
		t.Fatalf("evidence identities=%+v", detail.Evidence)
	}
}

func TestContentAssetIngestRejectsForeignEvidenceWithoutPartialAsset(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	svc := NewContentAssetService(repo)
	ctx := context.Background()
	foreignProject := createTestProject(t, repo, "foreign", model.PlatformWechat)
	if err := repo.Tasks().Create(ctx, &model.Task{ID: "foreign-task", UserID: "foreign", ProjectID: foreignProject}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Ingest(ctx, IngestContentAssetParams{CreateContentAssetParams: CreateContentAssetParams{UserID: "owner", Title: "foreign evidence"}, Evidence: []model.ContentAssetEvidence{{SourceType: model.ContentAssetEvidencePublished, SourceKind: "task", SourceID: "foreign-task"}}})
	if err == nil {
		t.Fatal("foreign evidence accepted")
	}
	_, total, err := repo.ContentAssets().List(ctx, "owner", "", "", 0, 10)
	if err != nil || total != 0 {
		t.Fatalf("failed ingest left partial assets=%d, %v", total, err)
	}
}

func TestAssetScoreFreshnessUsesLinkedEvidenceNotUnrelatedRecentContent(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	projectID := createTestProject(t, repo, "owner", model.PlatformWechat)
	asset := model.NewContentAsset("owner", "older proven content")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsState{ProjectID: projectID, Revision: 1, ActiveGeneration: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := now.Add(-45 * 24 * time.Hour)
	for i, id := range []string{"linked", "recent-a", "recent-b"} {
		date, reads := now, int64(i+1)
		if i == 0 {
			date, reads = old, 1000
		}
		if err := db.Create(&model.AnalyticsContent{ID: "task:" + id, ProjectID: projectID, TaskID: id, Channel: model.ChannelArticle}).Error; err != nil {
			t.Fatal(err)
		}
		for _, granularity := range []string{"day", "month"} {
			bucket := date.Format("2006-01-02")
			if granularity == "month" {
				bucket = date.Format("2006-01") + "-01"
			}
			if err := db.Create(&model.AnalyticsBucket{ProjectID: projectID, Generation: 1, ContentID: "task:" + id, MetricBasis: "cumulative", Granularity: granularity, BucketStart: bucket, LastStatDate: date.Format("2006-01-02"), AnalyticsMetrics: model.AnalyticsMetrics{ReadUsers: &reads}}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := repo.ContentAssets().CreateLink(ctx, &model.ContentAssetLink{ID: uuid.NewString(), UserID: "owner", AssetID: asset.ID, ProjectID: projectID, ContentKind: "task", ContentID: "linked", Relation: model.ContentAssetLinkOrigin}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ContentAssets().CreateEvidence(ctx, &model.ContentAssetEvidence{ID: uuid.NewString(), UserID: "owner", AssetID: asset.ID, SourceType: model.ContentAssetEvidencePublished, Platform: model.PlatformWechat, DataAsOf: &now, Metrics: []byte(`{"account_fit":90,"transferability":90,"angle_space":90,"cost":10,"risk":0}`)}); err != nil {
		t.Fatal(err)
	}
	score, err := NewContentAssetService(repo).ScoreAssetForProject(ctx, "owner", asset.ID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if score.DataAsOf == nil || score.DataAsOf.Format("2006-01-02") != old.Format("2006-01-02") || score.Confidence != "low" {
		t.Fatalf("freshness borrowed from unrelated content: %+v", score)
	}
}

func TestAssetScorePreservesSourcePlatformPercentileWithoutTargetMetrics(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	sourceProject := createTestProject(t, repo, "owner", model.PlatformWechat)
	targetProject := createTestProject(t, repo, "owner", model.PlatformSeednote)
	asset := model.NewContentAsset("owner", "source proven topic")
	asset.Status = model.ContentAssetStatusResearched
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Create(&model.AnalyticsState{ProjectID: sourceProject, Revision: 3, ActiveGeneration: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	for i, item := range []struct {
		id    string
		reads int64
	}{{"source-top", 100}, {"baseline-one", 10}, {"baseline-two", 20}, {"baseline-three", 30}} {
		contentID := "task:" + item.id
		if err := db.Create(&model.AnalyticsContent{ID: contentID, ProjectID: sourceProject, TaskID: item.id, Channel: model.ChannelArticle}).Error; err != nil {
			t.Fatal(err)
		}
		for _, granularity := range []string{"day", "month"} {
			bucket := now.Format("2006-01-02")
			if granularity == "month" {
				bucket = now.Format("2006-01") + "-01"
			}
			if err := db.Create(&model.AnalyticsBucket{ProjectID: sourceProject, Generation: 1, ContentID: contentID, MetricBasis: "cumulative", Granularity: granularity, BucketStart: bucket, LastStatDate: now.Format("2006-01-02"), AnalyticsMetrics: model.AnalyticsMetrics{ReadUsers: &item.reads}}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if i == 0 {
			if err := repo.Tasks().Create(ctx, &model.Task{ID: item.id, UserID: "owner", ProjectID: sourceProject, Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}); err != nil {
				t.Fatal(err)
			}
			if err := repo.ContentAssets().CreateLink(ctx, &model.ContentAssetLink{ID: "source-asset-link", UserID: "owner", AssetID: asset.ID, ProjectID: sourceProject, ContentKind: "task", ContentID: item.id, Platform: model.PlatformWechat, Relation: model.ContentAssetLinkOrigin}); err != nil {
				t.Fatal(err)
			}
		}
	}
	snapshot, err := NewContentAssetService(repo).ScoreAssetForProject(ctx, "owner", asset.ID, targetProject)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(snapshot.Snapshot, &body); err != nil {
		t.Fatal(err)
	}
	sources, ok := body["source_platform_performance"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("source platform evidence missing: %#v", body)
	}
	source := sources[0].(map[string]any)
	if source["platform"] != model.PlatformWechat || source["metric"] != "read_users" || source["percentile"].(float64) < 99 {
		t.Fatalf("source percentile not normalized within source platform: %#v", source)
	}
	missing, _ := body["missing"].([]any)
	if !containsAny(missing, "asset_has_no_target_platform_analytics") || snapshot.Status != "exploratory" || snapshot.Score <= 0 || snapshot.Score > 49 || snapshot.Confidence != "low" {
		t.Fatalf("source performance was not surfaced as bounded exploration: status=%s score=%v confidence=%s missing=%#v", snapshot.Status, snapshot.Score, snapshot.Confidence, missing)
	}
	recommendations, err := NewContentAssetService(repo).ListRecommendations(ctx, "owner", 5)
	if err != nil || len(recommendations) != 1 || recommendations[0].Score == nil || recommendations[0].Score.Status != "exploratory" {
		t.Fatalf("source-only opportunity was not surfaced as exploratory recommendation: %#v err=%v", recommendations, err)
	}
}

func TestDiscoverPublishedAnalyticsCreatesIdempotentCandidateAssetEvidence(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.WechatPublication{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	projectID := createTestProject(t, repo, "discover-owner", model.PlatformWechat)
	if err := db.Create(&model.AnalyticsState{ProjectID: projectID, Revision: 4, ActiveGeneration: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsContent{ID: "wechat-publication-content", ProjectID: projectID, PublicationID: "publication-1", Channel: model.ChannelArticle, Title: "已发布的高质量选题", Status: model.WechatPublicationStatusPublished}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.WechatPublication{ID: "publication-1", TaskID: "published-task", UserID: "discover-owner", ProjectID: projectID, Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusPublished}).Error; err != nil {
		t.Fatal(err)
	}
	date := time.Now().UTC().Add(-24 * time.Hour)
	reads := int64(321)
	for _, granularity := range []string{"day", "month"} {
		bucket := date.Format("2006-01-02")
		if granularity == "month" {
			bucket = date.Format("2006-01") + "-01"
		}
		if err := db.Create(&model.AnalyticsBucket{ProjectID: projectID, Generation: 1, ContentID: "wechat-publication-content", MetricBasis: "cumulative", Granularity: granularity, BucketStart: bucket, LastStatDate: date.Format("2006-01-02"), AnalyticsMetrics: model.AnalyticsMetrics{ReadUsers: &reads}}).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewContentAssetService(repo)
	created, err := svc.DiscoverPublishedContentAssets(ctx, "discover-owner", projectID, 4)
	if err != nil || created != 1 {
		t.Fatalf("first discovery created=%d err=%v", created, err)
	}
	created, err = svc.DiscoverPublishedContentAssets(ctx, "discover-owner", projectID, 4)
	if err != nil || created != 0 {
		t.Fatalf("second discovery created=%d err=%v", created, err)
	}
	asset, err := repo.ContentAssets().FindByTitle(ctx, "discover-owner", "已发布的高质量选题")
	if err != nil {
		t.Fatal(err)
	}
	links, err := repo.ContentAssets().ListLinks(ctx, "discover-owner", asset.ID)
	if err != nil || len(links) != 1 || links[0].ContentKind != "wechat_publication" || links[0].ContentID != "publication-1" {
		t.Fatalf("discovered links=%#v err=%v", links, err)
	}
	evidence, err := repo.ContentAssets().ListEvidence(ctx, "discover-owner", asset.ID)
	if err != nil || len(evidence) != 1 || evidence[0].SourceType != model.ContentAssetEvidencePublished || evidence[0].Quality != "verified" {
		t.Fatalf("discovered evidence=%#v err=%v", evidence, err)
	}
}

func containsAny(values []any, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type failingReuseEnqueuer struct{}

func (failingReuseEnqueuer) EnqueueUnique(string, []byte, string) (bool, error) {
	return false, errors.New("queue offline")
}

func TestReuseQueueFailureIsDurableAndRecoverable(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	projectID := createTestProject(t, repo, "owner", model.PlatformWechat)
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(failingReuseEnqueuer{})
	asset, err := svc.Create(ctx, CreateContentAssetParams{UserID: "owner", Title: "durable queue intent"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: "owner", AssetID: asset.ID, TargetProjectID: projectID, TargetPlatform: model.PlatformWechat, IdempotencyKey: "queue-recovery"})
	if err == nil {
		t.Fatal("queue failure hidden")
	}
	run, err := repo.ContentAssets().FindReuseRunByIdempotency(ctx, "owner", "queue-recovery")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != model.ContentReuseRunStatusQueued || run.NextRunAt == nil || run.RetryCount != 1 {
		t.Fatalf("queue failure is not recoverable: %+v", run)
	}
	queue := &contentAssetTestEnqueuer{}
	svc.SetEnqueuer(queue)
	if _, err := svc.RunAutopilotTick(ctx, time.Now().Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if queue.calls != 1 {
		t.Fatalf("recovery enqueues=%d, want 1", queue.calls)
	}
	if _, err := svc.SetReuseRunStatus(ctx, "owner", run.ID, model.ContentReuseRunStatusPaused); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunAutopilotTick(ctx, time.Now().Add(40*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if queue.calls != 1 {
		t.Fatalf("paused run recovered unexpectedly: %d", queue.calls)
	}

}

type immediateClaimReuseEnqueuer struct{ repo repository.Repository }

func (e immediateClaimReuseEnqueuer) EnqueueUnique(_ string, body []byte, _ string) (bool, error) {
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, err
	}
	return e.repo.ContentAssets().ClaimReuseRun(context.Background(), payload["run_id"])
}
func TestReuseResumeAllowsWorkerToClaimImmediately(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := db.AutoMigrate(&model.ContentReuseRun{}, &model.ContentReuseAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	delay := time.Now().Add(time.Hour)
	run.Status, run.NextRunAt, run.ErrorCode = model.ContentReuseRunStatusPaused, &delay, "queue_unavailable"
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(immediateClaimReuseEnqueuer{repo})
	if _, err := svc.SetReuseRunStatus(ctx, "owner", run.ID, model.ContentReuseRunStatusQueued); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.ContentReuseRunStatusRunning || got.NextRunAt != nil || got.ErrorCode != "" {
		t.Fatalf("resume retained stale retry state: %+v", got)
	}
}

func TestReuseRunRefreshesExpiredAdmissionQuote(t *testing.T) {
	ctx := context.Background()
	taskSvc, f, queue := newFixedTaskBillingFixture(t, 1500, 0)
	projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformWechat)
	asset := model.NewContentAsset(billingWalletUserID, "recover quotation")
	asset.Status = model.ContentAssetStatusValidated
	if err := f.repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	policy := model.NewContentAutopilotPolicy(billingWalletUserID)
	policy.Enabled, policy.DailyBudgetCredits, policy.MaxRunCostCredits = true, 2000, 1000
	if err := f.repo.ContentAssets().UpsertPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun(billingWalletUserID, asset.ID, projectID, model.PlatformWechat)
	run.PolicySnapshot, _ = json.Marshal(policy)
	fingerprint := billingFingerprint("content-reuse-task", run.ID, model.TaskTypeWechatArticle, "effective")
	old, err := f.catalog.CreateTaskQuote(ctx, TaskQuoteRequest{UserID: billingWalletUserID, TaskType: model.TaskTypeWechatArticle, ExecutionProfile: "effective", RequestFingerprint: fingerprint, IdempotencyScope: "content-reuse-quote", IdempotencyKey: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	run.BillingQuoteID, run.BillingFingerprint = old.ID, fingerprint
	if err := f.repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	later := f.now.Add(time.Hour)
	f.catalog.now, f.wallet.now = func() time.Time { return later }, func() time.Time { return later }
	svc := NewContentAssetService(f.repo)
	svc.SetTaskService(taskSvc)
	svc.SetBillingCatalogService(f.catalog)
	if err := svc.ProcessReuseRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	task, err := f.repo.Tasks().FindByReuseRun(ctx, billingWalletUserID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.BillingQuoteID == old.ID || task.BillingChargeID == nil || len(queue.enqueued) != 1 {
		t.Fatalf("expired quote did not recover: %+v, enqueued=%d", task, len(queue.enqueued))
	}
}

type committedAdmissionCrashRepository struct {
	repository.Repository
	crashed bool
}

func (r *committedAdmissionCrashRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	if err := r.Repository.WithTx(ctx, fn); err != nil {
		return err
	}
	if !r.crashed {
		r.crashed = true
		return errors.New("simulated crash after committed admission")
	}
	return nil
}
func TestReuseRecoveryDispatchesAlreadyAdmittedPendingTask(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	projectID := createTestProject(t, repo, "owner", model.PlatformWechat)
	run := model.NewContentReuseRun("owner", "asset", projectID, model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	queue := &mockEnqueuer{}
	logger := zerolog.New(io.Discard)
	taskSvc := newTestTaskService(&committedAdmissionCrashRepository{Repository: repo}, queue, nil, &logger, "", nil, nil)
	_, err := taskSvc.CreateManual(ctx, CreateManualParams{UserID: "owner", ProjectID: projectID, AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration, ExecutionProfile: "effective", Prompt: "recover pending", ContentAssetID: "asset", ReuseRunID: run.ID})
	if err == nil {
		t.Fatal("crash simulation did not interrupt dispatch")
	}
	existing, err := repo.Tasks().FindByReuseRun(ctx, "owner", run.ID)
	if err != nil || existing.Status != model.TaskStatusPending {
		t.Fatalf("pending task=%+v,%v", existing, err)
	}
	svc := NewContentAssetService(repo)
	svc.SetTaskService(taskSvc)
	if err := svc.ProcessReuseRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("admitted task never dispatched: %d", len(queue.enqueued))
	}
	recovered, err := repo.Tasks().FindByReuseRun(ctx, "owner", run.ID)
	if err != nil || recovered.ID != existing.ID {
		t.Fatalf("recovery replaced task: %+v,%v", recovered, err)
	}
}

type archivedReuseEnqueuer struct {
	archived   string
	dispatched int
}

func (e *archivedReuseEnqueuer) EnqueueUnique(_ string, _ []byte, key string) (bool, error) {
	if key == e.archived {
		return false, nil
	}
	e.dispatched++
	return true, nil
}
func TestReuseRecoveryDoesNotCollideWithArchivedDispatch(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	run := model.NewContentReuseRun("owner", "asset", "project", model.PlatformWechat)
	run.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	queue := &archivedReuseEnqueuer{archived: "content-reuse:" + run.ID}
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(queue)
	if _, err := svc.RunAutopilotTick(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if queue.dispatched != 1 {
		t.Fatal("archived dispatch permanently blocks persisted run")
	}
}
func TestAutopilotAdmissionRechecksDisabledPolicy(t *testing.T) {
	db := setupTaskTestDB(t)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	projectID := createTestProject(t, repo, "owner", model.PlatformWechat)
	asset := model.NewContentAsset("owner", "stale scan")
	asset.Status = model.ContentAssetStatusValidated
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	policy := model.NewContentAutopilotPolicy("owner")
	policy.Enabled = false
	if err := repo.ContentAssets().UpsertPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	queue := &contentAssetTestEnqueuer{}
	svc.SetEnqueuer(queue)
	_, _, err := svc.createAutopilotReuseRun(ctx, CreateReuseRunParams{UserID: "owner", AssetID: asset.ID, TargetProjectID: projectID, TargetPlatform: model.PlatformWechat, IdempotencyKey: "stale-scan", ScoreSnapshot: map[string]any{"score": 90}})
	if !errors.Is(err, ErrContentReuseConflict) {
		t.Fatalf("disabled policy admitted stale automatic candidate: %v", err)
	}
	if queue.calls != 0 {
		t.Fatalf("revoked automation queued %d times", queue.calls)
	}
}
