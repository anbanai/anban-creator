package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type managedCompletionFixture struct {
	svc       *TaskService
	repo      repository.Repository
	store     *fakeTaskStorage
	task      *model.Task
	execution *model.TaskExecution
}

func newManagedCompletionFixture(t *testing.T, taskType string) *managedCompletionFixture {
	t.Helper()
	db := setupTaskTestDB(t)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	repo := repository.New(db)
	ctx := context.Background()
	task := &model.Task{
		ID: uuid.NewString(), UserID: uuid.NewString(), Type: taskType,
		Status: model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	execution := attachManagedExecution(t, repo, task)
	store := &fakeTaskStorage{name: "oss", files: map[string][]byte{}}
	logger := zerolog.New(io.Discard)
	return &managedCompletionFixture{
		svc:       newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil),
		repo:      repo,
		store:     store,
		task:      task,
		execution: execution,
	}
}

func attachManagedExecution(t *testing.T, repo repository.Repository, task *model.Task) *model.TaskExecution {
	t.Helper()
	ctx := context.Background()
	if task.Status == model.TaskStatusPending {
		won, err := repo.Tasks().CompareAndSwapStatusAndStartedAt(ctx, task.ID, model.TaskStatusPending, model.TaskStatusRunning)
		if err != nil || !won {
			t.Fatalf("start task: won=%v err=%v", won, err)
		}
		task.Status = model.TaskStatusRunning
	}
	execution := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 1, Target: "managed-test",
		Status: model.TaskExecutionRunning, Started: true,
		RuntimeProfile: task.Type, RuntimeImage: "registry/runtime@sha256:test",
	}
	if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, execution.ID); err != nil || !won {
		t.Fatalf("set current execution: won=%v err=%v", won, err)
	}
	task.CurrentExecutionID = &execution.ID
	return execution
}

func (f *managedCompletionFixture) addPendingFile(t *testing.T, name string, size int64, body string) *model.TaskFile {
	t.Helper()
	relPath := filepath.ToSlash(filepath.Join("output", name))
	key := "managed-tests/" + f.execution.ID + "/" + relPath
	if body != "" {
		f.store.files[key] = []byte(body)
	}
	file, err := f.repo.TaskFiles().UpsertPendingCurrentExecution(context.Background(), f.task.ID, f.execution.ID, &model.TaskFile{
		Role: DetermineTaskFileRole(name, ""), FilePath: relPath, FileName: filepath.Base(name),
		FileSize: size, OSSKey: key, OSSURL: f.store.GetURL(key), StorageProvider: f.store.Name(),
	})
	if err != nil {
		t.Fatalf("add pending managed file %s: %v", relPath, err)
	}
	return file
}

func (f *managedCompletionFixture) assertTerminal(t *testing.T, taskStatus, executionStatus, fileState string) {
	t.Helper()
	ctx := context.Background()
	task, err := f.repo.Tasks().FindByID(ctx, f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := f.repo.TaskExecutions().FindByID(ctx, f.execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != taskStatus || execution.Status != executionStatus || execution.FinalizationStatus != model.TaskExecutionFinalizationDone {
		t.Fatalf("terminal state task=%q execution=%q finalization=%q error=%q", task.Status, execution.Status, execution.FinalizationStatus, task.ErrorMessage)
	}
	files, err := f.repo.TaskFiles().FindByExecutionID(ctx, f.execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.State != fileState {
			t.Fatalf("file %s state=%q, want %q", file.FilePath, file.State, fileState)
		}
	}
}

func TestCompleteCloudExecutionPublishesSeednoteDeliverables(t *testing.T) {
	f := newManagedCompletionFixture(t, model.PlatformSeednote)
	for _, name := range seednoteCompletionArtifactNamesForTest(true, false) {
		f.addPendingFile(t, name, 1, "fixture")
	}

	if err := f.svc.CompleteCloudExecution(context.Background(), f.execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	f.assertTerminal(t, model.TaskStatusCompleted, model.TaskExecutionSucceeded, model.TaskFileStatePublished)
}

func TestCompleteCloudExecutionFinalizesMontageDeliverables(t *testing.T) {
	f := newManagedCompletionFixture(t, model.PlatformMontage)
	f.addPendingFile(t, "montage/final.mp4", 1024, "video")
	f.addPendingFile(t, "montage/delivery-manifest.json", 2, `{}`)

	if err := f.svc.CompleteCloudExecution(context.Background(), f.execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	f.assertTerminal(t, model.TaskStatusCompleted, model.TaskExecutionSucceeded, model.TaskFileStatePublished)
}

func TestCompleteCloudExecutionRejectsMissingOrInvalidMontageManifest(t *testing.T) {
	for _, test := range []struct {
		name         string
		manifestSize int64
		addManifest  bool
	}{
		{name: "missing"},
		{name: "empty", addManifest: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newManagedCompletionFixture(t, model.PlatformMontage)
			f.addPendingFile(t, "montage/final.mp4", 1024, "video")
			if test.addManifest {
				f.addPendingFile(t, "montage/delivery-manifest.json", test.manifestSize, "")
			}

			if err := f.svc.CompleteCloudExecution(context.Background(), f.execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
				t.Fatal(err)
			}
			f.assertTerminal(t, model.TaskStatusFailed, model.TaskExecutionFailed, model.TaskFileStateCollected)
			found, err := f.repo.Tasks().FindByID(context.Background(), f.task.ID)
			if err != nil || !strings.Contains(found.ErrorMessage, "delivery-manifest.json") {
				t.Fatalf("failure task=%#v err=%v", found, err)
			}
		})
	}
}

func TestCompleteCloudExecutionRejectsNestedAgentOnlyResult(t *testing.T) {
	f := newManagedCompletionFixture(t, model.PlatformArticle)
	result := &agent.ExecutionResult{
		Success: true, RemoteArtifacts: true,
		ToolUseSummary: map[string]int{"Agent": 1, "TaskUpdate": 2},
	}
	if err := f.svc.CompleteCloudExecution(context.Background(), f.execution.ID, result); err != nil {
		t.Fatal(err)
	}
	f.assertTerminal(t, model.TaskStatusFailed, model.TaskExecutionFailed, model.TaskFileStateCollected)
	found, err := f.repo.Tasks().FindByID(context.Background(), f.task.ID)
	if err != nil || found.ErrorMessage != agent.NestedAgentDelegationError {
		t.Fatalf("nested-agent failure task=%#v err=%v", found, err)
	}
}

func TestCompleteCloudExecutionExtractsPendingArticleDraftForApproval(t *testing.T) {
	for _, test := range []struct {
		name, fileName, body, wantTitle, wantContent string
	}{
		{
			name: "draft json", fileName: "draft.json",
			body:      `{"articles":[{"title":"Managed title","content":"<p>Managed body</p>"}]}`,
			wantTitle: "Managed title", wantContent: "<p>Managed body</p>",
		},
		{
			name: "persisted html title", fileName: "article.html",
			body:      `<html><h1><span>Persisted &amp; title</span></h1><p>Body</p></html>`,
			wantTitle: "Persisted & title", wantContent: `<html><h1><span>Persisted &amp; title</span></h1><p>Body</p></html>`,
		},
		{
			name: "persisted markdown title", fileName: "article.md",
			body:      "# Persisted Markdown\n\nBody",
			wantTitle: "Persisted Markdown", wantContent: "# Persisted Markdown\n\nBody",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newManagedCompletionFixture(t, model.PlatformArticle)
			project := &model.Project{
				ID: uuid.NewString(), UserID: f.task.UserID, Name: "managed approval",
				Platform: model.PlatformArticle, Status: model.ProjectStatusActive,
				Config: model.ProjectConfig{EnablePublishing: true, RequirePublishApproval: true},
			}
			if err := f.repo.Projects().Create(context.Background(), project); err != nil {
				t.Fatal(err)
			}
			f.task.ProjectID = project.ID
			if err := f.repo.Tasks().Update(context.Background(), f.task); err != nil {
				t.Fatal(err)
			}
			f.addPendingFile(t, test.fileName, int64(len(test.body)), test.body)
			logger := zerolog.New(io.Discard)
			f.svc.cloudPublisher = NewPublishingService(f.repo, &logger)

			if err := f.svc.CompleteCloudExecution(context.Background(), f.execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
				t.Fatal(err)
			}
			f.assertTerminal(t, model.TaskStatusCompleted, model.TaskExecutionSucceeded, model.TaskFileStatePublished)
			found, err := f.repo.Tasks().FindByID(context.Background(), f.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			var articles []DraftArticleInput
			if err := json.Unmarshal(found.PendingDraftArticles, &articles); err != nil {
				t.Fatal(err)
			}
			if found.PublishApprovalState != model.PublishApprovalStatePending || len(articles) != 1 || articles[0].Title != test.wantTitle || articles[0].Content != test.wantContent {
				t.Fatalf("approval state=%q articles=%#v", found.PublishApprovalState, articles)
			}
		})
	}
}

func TestCompleteCloudExecutionBillingUsesManagedDurableDelivery(t *testing.T) {
	for _, test := range []struct {
		name        string
		durableFile bool
		wantRefund  bool
	}{
		{name: "provider failure without output reverses", wantRefund: true},
		{name: "provider failure with collected output keeps charge", durableFile: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			svc, billing, _ := newFixedTaskBillingFixture(t, 1_000, 0)
			projectID := createTestProject(t, billing.repo, billingWalletUserID, model.PlatformArticle)
			tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
				UserID: billingWalletUserID, ProjectID: projectID, Prompt: test.name, Quantity: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			task := tasks[0]
			execution := attachManagedExecution(t, billing.repo, task)
			if test.durableFile {
				if _, err := billing.repo.TaskFiles().UpsertPendingCurrentExecution(ctx, task.ID, execution.ID, &model.TaskFile{
					Role: model.FileRoleDraft, FileName: "partial.md", FilePath: "output/partial.md", FileSize: 12,
				}); err != nil {
					t.Fatal(err)
				}
			}

			result := &agent.ExecutionResult{Success: false, Error: "provider unavailable", TerminalReason: model.TaskBillingTerminalProviderError, RemoteArtifacts: true}
			if err := svc.CompleteCloudExecution(ctx, execution.ID, result); err != nil {
				t.Fatal(err)
			}
			found, err := billing.repo.Tasks().FindByID(ctx, task.ID)
			if err != nil || found.Status != model.TaskStatusFailed || found.BillingTerminalReason != model.TaskBillingTerminalProviderError {
				t.Fatalf("terminal task=%#v err=%v", found, err)
			}
			settlement, settlementErr := billing.repo.Billing().FindSettlementByKey(ctx, "task-terminal-reversal", *task.BillingChargeID)
			if test.wantRefund {
				if settlementErr != nil || settlement.Action != model.BillingSettlementActionReverseTask || settlement.AttemptID == nil || *settlement.AttemptID != execution.ID {
					t.Fatalf("managed reversal=%#v err=%v", settlement, settlementErr)
				}
				if processed, err := billing.wallet.ProcessSettlementOutbox(ctx, 10); err != nil || processed != 1 {
					t.Fatalf("process managed reversal=%d, %v", processed, err)
				}
				if account := billing.account(t, billingWalletUserID); account.PaidCredits != 1_000 {
					t.Fatalf("refunded account=%#v", account)
				}
				return
			}
			if !errors.Is(settlementErr, gorm.ErrRecordNotFound) {
				t.Fatalf("durable output reversal=%#v err=%v, want not found", settlement, settlementErr)
			}
			files, err := billing.repo.TaskFiles().FindByExecutionID(ctx, execution.ID)
			if err != nil || len(files) != 1 || files[0].State != model.TaskFileStateCollected {
				t.Fatalf("managed durable files=%#v err=%v", files, err)
			}
			if account := billing.account(t, billingWalletUserID); account.PaidCredits != 500 {
				t.Fatalf("charged account=%#v", account)
			}
		})
	}
}
