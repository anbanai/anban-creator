package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestEnsureResearchJobFreezesOwnedContextAndIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "research-project", UserID: "research-user", Platform: model.PlatformWechat, Name: "owned"}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	input := map[string]any{"profile_revision": 7, "candidate_titles": []string{"one"}}
	first, err := svc.EnsureResearchJob(ctx, "research-user", "research-project", "", "research-fingerprint", 12, 4, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.EnsureResearchJob(ctx, "research-user", "research-project", "", "research-fingerprint", 12, 4, input)
	if err != nil || second.ID != first.ID {
		t.Fatalf("idempotent job=%#v err=%v", second, err)
	}
	if first.FrozenContextSHA256 == "" || first.SourceRevision != 12 || first.PolicyRevision != 4 {
		t.Fatalf("frozen job=%#v", first)
	}
	if _, err := svc.EnsureResearchJob(ctx, "research-user", "research-project", "", "research-fingerprint", 13, 4, input); err == nil {
		t.Fatal("revision change reused frozen job")
	}
	if _, err := svc.EnsureResearchJob(ctx, "foreign", "research-project", "", "foreign-fingerprint", 12, 4, input); err == nil {
		t.Fatal("foreign project accepted")
	}
}

func TestFinalizeResearchTaskPersistsTerminalJobFromCurrentExecution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	if err := repo.Projects().Create(ctx, &model.Project{ID: "research-finalizer-project", UserID: "research-finalizer-user", Platform: model.PlatformWechat, Name: "owned"}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	job, err := svc.EnsureResearchJob(ctx, "research-finalizer-user", "research-finalizer-project", "", "research-finalizer-fingerprint", 1, 1, map[string]any{"source_revision": 1})
	if err != nil {
		t.Fatal(err)
	}
	executionID := "research-finalizer-execution"
	task := &model.Task{ID: "research-finalizer-task", UserID: job.UserID, ProjectID: job.ProjectID, Type: model.TaskTypeContentResearch, TaskKind: model.TaskTypeContentResearch, Status: model.TaskStatusFailed, ResearchJobID: job.ID, CurrentExecutionID: &executionID, ErrorMessage: "provider unavailable"}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.ContentAssets().BindResearchJobTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: task.ID, Attempt: 1, Status: model.TaskExecutionFailed, ManifestSealed: true}); err != nil {
		t.Fatal(err)
	}
	svc.SetTaskService(NewTaskService(repo, nil, nil, nil, "", nil, nil))
	if err := svc.FinalizeResearchTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	updated, err := repo.ContentAssets().FindResearchJobByID(ctx, job.UserID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != model.ContentStageFailed || updated.AcceptedExecutionID != executionID || updated.ErrorMessage != "provider unavailable" {
		t.Fatalf("finalized research job=%#v", updated)
	}
}

func TestApplyResearchResultMergesAssetsAndDeduplicatesEvidence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	userID := "research-merge-user"
	primary := model.NewContentAsset(userID, "candidate title")
	merged := model.NewContentAsset(userID, "older title")
	if err := repo.ContentAssets().Create(ctx, primary); err != nil {
		t.Fatal(err)
	}
	if err := repo.ContentAssets().Create(ctx, merged); err != nil {
		t.Fatal(err)
	}
	if err := repo.ContentAssets().CreateLink(ctx, &model.ContentAssetLink{ID: "merge-origin", AssetID: merged.ID, UserID: userID, ProjectID: "research-project", ContentKind: "task", ContentID: "old-task", Relation: model.ContentAssetLinkOrigin}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []*model.ContentAssetEvidence{
		{ID: "primary-evidence", AssetID: primary.ID, UserID: userID, SourceType: model.ContentAssetEvidenceExternal, SourceID: "source-1", URL: "https://example.com/source"},
		{ID: "merged-evidence", AssetID: merged.ID, UserID: userID, SourceType: model.ContentAssetEvidenceExternal, SourceID: "source-1", URL: "https://example.com/source"},
	} {
		if err := repo.ContentAssets().CreateEvidence(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	result := model.PlanningResearchResult{PlanningIdentity: model.PlanningIdentity{SchemaVersion: "1.0", TaskID: "research-task", ExecutionID: "research-execution", TaskKind: model.TaskTypeContentResearch, Status: "ready"}, Candidates: []model.PlanningProposal{{ID: "proposal-1", Title: "Canonical title", SemanticKernel: "The durable question", MergeAssetIDs: []string{merged.ID}, TargetFit: model.PlanningAssessment{Score: 80, Rationale: "fit", SourceRefs: []string{"source-1"}}, Transferability: model.PlanningAssessment{Score: 70, Rationale: "transfer", SourceRefs: []string{"source-1"}}, Angles: []string{"angle"}, ProductionCost: "low", MaterialRisk: "low", ComplianceRisk: "check"}}}
	evidence := model.PlanningResearchEvidence{PlanningIdentity: result.PlanningIdentity, Sources: []model.PlanningSource{{ID: "source-1", URL: "https://example.com/source", FetchedAt: "2026-10-21T00:00:00Z", Facts: []string{"fact"}}}}
	apply := func() error {
		return repo.WithTx(ctx, func(tx repository.Repository) error {
			return applyResearchResult(ctx, tx, userID, primary.ID, result, evidence)
		})
	}
	if err := apply(); err != nil {
		t.Fatal(err)
	}
	if err := apply(); err != nil {
		t.Fatal(err)
	}
	gotPrimary, err := repo.ContentAssets().FindByID(ctx, userID, primary.ID)
	if err != nil || gotPrimary.Status != model.ContentAssetStatusResearched || gotPrimary.Title != "Canonical title" || gotPrimary.SemanticKernel != "The durable question" {
		t.Fatalf("primary asset = %#v, err=%v", gotPrimary, err)
	}
	gotMerged, err := repo.ContentAssets().FindByID(ctx, userID, merged.ID)
	if err != nil || gotMerged.Status != model.ContentAssetStatusArchived {
		t.Fatalf("merged asset = %#v, err=%v", gotMerged, err)
	}
	links, err := repo.ContentAssets().ListLinks(ctx, userID, primary.ID)
	if err != nil || len(links) != 1 || links[0].ContentID != "old-task" {
		t.Fatalf("merged links = %#v, err=%v", links, err)
	}
	evidenceRows, err := repo.ContentAssets().ListEvidence(ctx, userID, primary.ID)
	if err != nil || len(evidenceRows) != 1 {
		t.Fatalf("deduplicated evidence = %#v, err=%v", evidenceRows, err)
	}
}

func TestContentReuseStageSequencePromotesScriptToMaterialsAndRender(t *testing.T) {
	if got := nextContentReuseStage(model.ContentReuseStageProduce); got != model.ContentReuseStageMaterials {
		t.Fatalf("script stage next=%q, want materials", got)
	}
	if got := nextContentReuseStage(model.ContentReuseStageMaterials); got != model.ContentReuseStageRender {
		t.Fatalf("materials stage next=%q, want render", got)
	}
}

func TestValidGeneratedSRTRejectsMalformedOrEmptyInput(t *testing.T) {
	valid := "1\n00:00:00,000 --> 00:00:01,000\nhello\n"
	if !validGeneratedSRT(valid) {
		t.Fatal("valid SRT rejected")
	}
	for _, value := range []string{"", "1\n00:00:00,000 -> 00:00:01,000\nhello", "1\n00:00:00,000 --> 00:00:01,000\n"} {
		if validGeneratedSRT(value) {
			t.Fatalf("malformed SRT accepted: %q", value)
		}
	}
}

func TestAdvancePlanningStageRejectsForeignRunOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset := model.NewContentAsset("stage-owner", "asset")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("stage-owner", asset.ID, "stage-project", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	run.PolicySnapshot = datatypes.JSON(`{"enabled":true}`)
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	stage := model.NewContentReuseStageExecution(run.ID, model.ContentReuseStageResearch)
	stage.Status = model.ContentStageCompleted
	stage.ArtifactRefs = datatypes.JSON(`[ {"task_id":"task","execution_id":"execution","path":"output/content-research.json","content_sha256":"hash"} ]`)
	if err := repo.ContentAssets().CreateReuseStage(ctx, stage); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	if err := svc.advancePlanningStage(ctx, "foreign-user", stage.ID); err == nil {
		t.Fatal("foreign user advanced content planning stage")
	}
}

func TestContentAssetLineageReportsServerCostsAndUnsupportedAnalytics(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset := model.NewContentAsset("lineage-user", "shared topic")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: "lineage-video", UserID: "lineage-user", Platform: model.PlatformHypit, Name: "video"}); err != nil {
		t.Fatal(err)
	}
	link := &model.ContentAssetLink{ID: "lineage-link", AssetID: asset.ID, UserID: asset.UserID, ProjectID: "lineage-video", ContentKind: "video_task", ContentID: "video-task", Platform: model.PlatformHypit, Relation: model.ContentAssetLinkDerivative}
	if err := repo.ContentAssets().CreateLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun(asset.UserID, asset.ID, "lineage-video", model.PlatformHypit)
	run.Status, run.CostCredits, run.BudgetReserved = model.ContentReuseRunStatusPaused, 35, 10
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	lineage, err := NewContentAssetService(repo).GetLineage(ctx, asset.UserID, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lineage.Summary.RunCount != 1 || lineage.Summary.DerivativeCount != 1 || lineage.Summary.CostCredits != 35 || lineage.Summary.ReservedCredits != 10 {
		t.Fatalf("lineage summary=%#v", lineage.Summary)
	}
	if len(lineage.PlatformPerformance) != 1 || lineage.PlatformPerformance[0].Platform != model.PlatformHypit || len(lineage.PlatformPerformance[0].Missing) == 0 || len(lineage.PlatformPerformance[0].Metrics) != 0 {
		t.Fatalf("unsupported platform projection=%#v", lineage.PlatformPerformance)
	}
}

func TestContentAssetLineageByContentChecksOwnershipAndReturnsLinkedAssets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	project := &model.Project{ID: "by-content-project", UserID: "by-content-owner", Platform: model.PlatformWechat, Name: "owned"}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "by-content-task", UserID: project.UserID, ProjectID: project.ID, Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	asset := model.NewContentAsset(project.UserID, "linked topic")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if err := repo.ContentAssets().CreateLink(ctx, &model.ContentAssetLink{ID: "by-content-link", AssetID: asset.ID, UserID: project.UserID, ProjectID: project.ID, ContentKind: "task", ContentID: task.ID, Platform: project.Platform, Relation: model.ContentAssetLinkOrigin}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	items, err := svc.GetLineageByContent(ctx, project.UserID, project.ID, "task", task.ID)
	if err != nil || len(items) != 1 || items[0].Asset.ID != asset.ID {
		t.Fatalf("owned content lineage=%#v err=%v", items, err)
	}
	if _, err := svc.GetLineageByContent(ctx, "foreign-user", project.ID, "task", task.ID); err == nil {
		t.Fatal("foreign user read linked content lineage")
	}
}

func TestCompleteReuseStageAcceptsOnlyFrozenDeliveredArtifact(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset := model.NewContentAsset("stage-owner", "asset")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("stage-owner", asset.ID, "stage-project", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	stage := model.NewContentReuseStageExecution(run.ID, model.ContentReuseStageResearch)
	taskID := "stage-task"
	stage.Status, stage.TaskID = model.ContentStageRunning, &taskID
	if err := repo.ContentAssets().CreateReuseStage(ctx, stage); err != nil {
		t.Fatal(err)
	}
	executionID := "stage-execution"
	task := &model.Task{ID: taskID, UserID: run.UserID, ProjectID: run.TargetProjectID, Type: model.TaskTypeContentResearch, TaskKind: model.TaskTypeContentResearch, Status: model.TaskStatusCompleted, ContentAssetID: run.AssetID, ReuseRunID: run.ID, ReuseStageID: stage.ID, CurrentExecutionID: &executionID}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: task.ID, Attempt: 1, Status: model.TaskExecutionSucceeded, ManifestSealed: true}); err != nil {
		t.Fatal(err)
	}
	file := &model.TaskFile{TaskID: task.ID, ExecutionID: executionID, State: model.TaskFileStateDelivered, Role: model.FileRoleOther, FilePath: "output/content-research.json", FileName: "content-research.json", FileSize: 10, ContentHash: "sha256-good"}
	if err := repo.TaskFiles().Create(ctx, file); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	bad := []ContentStageArtifact{{TaskID: task.ID, ExecutionID: executionID, Path: file.FilePath, ContentSHA256: "sha256-forged"}}
	if err := svc.CompleteReuseStage(ctx, run.UserID, stage.ID, executionID, bad, model.ContentStageCompleted, ""); err == nil {
		t.Fatal("forged artifact hash accepted")
	}
	good := []ContentStageArtifact{{TaskID: task.ID, ExecutionID: executionID, Path: file.FilePath, ContentSHA256: file.ContentHash}}
	if err := svc.CompleteReuseStage(ctx, run.UserID, stage.ID, executionID, good, model.ContentStageCompleted, ""); err != nil {
		t.Fatal(err)
	}
	completed, err := repo.ContentAssets().FindReuseStageByID(ctx, stage.ID)
	if err != nil {
		t.Fatal(err)
	}
	var frozen []ContentStageArtifact
	if err := json.Unmarshal(completed.ArtifactRefs, &frozen); err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.ContentStageCompleted || completed.AcceptedExecutionID != executionID || len(frozen) != 1 || frozen[0].ContentSHA256 != file.ContentHash {
		t.Fatalf("accepted stage provenance = %#v artifacts=%#v", completed, frozen)
	}
	events, err := repo.ContentAssets().ListReuseAudit(ctx, run.UserID, run.ID)
	if err != nil || len(events) != 2 || events[1].ExecutionID != executionID || events[1].ToState != model.ContentStageCompleted {
		t.Fatalf("stage audit = %#v err=%v", events, err)
	}
}

func TestEnsureReuseStageDoesNotReplaceFrozenInput(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	asset := model.NewContentAsset("stage-user", "stage asset")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	run := model.NewContentReuseRun("stage-user", asset.ID, "stage-project", model.PlatformWechat)
	run.Status = model.ContentReuseRunStatusRunning
	if err := repo.ContentAssets().CreateReuseRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	first, err := svc.EnsureReuseStage(ctx, "stage-user", run.ID, model.ContentReuseStageResearch, map[string]any{"revision": 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.EnsureReuseStage(ctx, "stage-user", run.ID, model.ContentReuseStageResearch, map[string]any{"revision": 1})
	if err != nil || second.ID != first.ID {
		t.Fatalf("stage idempotency=%#v err=%v", second, err)
	}
	if _, err := svc.EnsureReuseStage(ctx, "stage-user", run.ID, model.ContentReuseStageResearch, map[string]any{"revision": 2}); err == nil {
		t.Fatal("stage input was replaced")
	}
}
