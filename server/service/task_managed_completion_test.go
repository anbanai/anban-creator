package service

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

type persistThenFailSettlementRepository struct {
	repository.Repository
	err error
}

type persistThenFailSettlementBillingRepository struct {
	repository.BillingRepository
	err error
}

func (r *persistThenFailSettlementRepository) Billing() repository.BillingRepository {
	return &persistThenFailSettlementBillingRepository{BillingRepository: r.Repository.Billing(), err: r.err}
}

func (r *persistThenFailSettlementRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&persistThenFailSettlementRepository{Repository: tx, err: r.err})
	})
}

func (r *persistThenFailSettlementBillingRepository) EnqueueSettlement(ctx context.Context, settlement *model.BillingSettlementOutbox) error {
	if err := r.BillingRepository.EnqueueSettlement(ctx, settlement); err != nil {
		return err
	}
	return r.err
}

func newManagedCompletionFixture(t *testing.T, taskType string) *managedCompletionFixture {
	t.Helper()
	db := setupTaskTestDB(t)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: taskType,
		Name: "Managed completion", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: taskType,
		Status: model.TaskStatusRunning, ImageCapabilityKey: "standard",
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
	if err := applyAgentPackIdentity(execution, task.Type); err != nil {
		t.Fatalf("freeze managed execution pack: %v", err)
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
	key := buildTaskMCPArtifactStoragePrefix(f.task, f.execution.ID) + relPath
	data := []byte(body)
	if strings.EqualFold(filepath.Ext(name), ".png") && body != "" {
		data = taskImageTinyPNG()
	}
	if body != "" {
		f.store.files[key] = data
		size = int64(len(data))
	}
	ext := strings.ToLower(filepath.Ext(name))
	mimeType := mimeTypes[ext]
	if mimeType == "" {
		mimeType = contentTypeForUploadExt(ext)
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	file, err := f.repo.TaskFiles().UpsertPendingCurrentExecution(context.Background(), f.task.ID, f.execution.ID, &model.TaskFile{
		Role: DetermineTaskFileRole(name, ""), FilePath: relPath, FileName: filepath.Base(name),
		MimeType: mimeType, FileSize: size, OSSKey: key, OSSURL: f.store.GetURL(key), StorageProvider: f.store.Name(),
	})
	if err != nil {
		t.Fatalf("add pending managed file %s: %v", relPath, err)
	}
	return file
}

func (f *managedCompletionFixture) complete(t *testing.T, result *agent.ExecutionResult) error {
	t.Helper()
	if !result.Success {
		return f.svc.CompleteCloudExecution(context.Background(), f.execution.ID, result)
	}
	if err := f.repo.TaskFiles().ReplacePendingCurrentExecutionPreservingMCPArtifacts(context.Background(), f.task.ID, f.execution.ID, nil); err != nil {
		t.Fatalf("seal managed execution manifest: %v", err)
	}
	return f.svc.CompleteCloudExecution(context.Background(), f.execution.ID, result)
}

func validTestMP4() string {
	return "\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isommp42"
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

	if err := f.complete(t, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	f.assertTerminal(t, model.TaskStatusCompleted, model.TaskExecutionSucceeded, model.TaskFileStatePublished)
}

func TestCompleteCloudExecutionRejectsDeliveryImageWithForgedMIME(t *testing.T) {
	f := newManagedCompletionFixture(t, model.PlatformSeednote)
	var cover *model.TaskFile
	for _, name := range seednoteCompletionArtifactNamesForTest(true, false) {
		file := f.addPendingFile(t, name, 1, "fixture")
		if name == "cover.png" {
			cover = file
		}
	}
	if cover == nil {
		t.Fatal("cover fixture was not created")
	}
	forged := []byte("<html>not an image</html>")
	f.store.files[cover.OSSKey] = forged
	cover.FileSize = int64(len(forged))
	cover.MimeType = "image/png"
	if _, err := f.repo.TaskFiles().UpsertPendingCurrentExecution(context.Background(), f.task.ID, f.execution.ID, cover); err != nil {
		t.Fatal(err)
	}

	if err := f.complete(t, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	f.assertTerminal(t, model.TaskStatusFailed, model.TaskExecutionFailed, model.TaskFileStateCollected)
}

func TestCompleteCloudExecutionFinalizesMontageDeliverables(t *testing.T) {
	f := newManagedCompletionFixture(t, model.PlatformMontage)
	f.addPendingFile(t, "final.mp4", 1024, validTestMP4())
	f.addPendingFile(t, "montage-project.json", 2, `{}`)
	f.addPendingFile(t, "cover.png", 1024, "image")
	f.addPendingFile(t, "delivery-manifest.json", 2, `{}`)

	if err := f.complete(t, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	f.assertTerminal(t, model.TaskStatusCompleted, model.TaskExecutionSucceeded, model.TaskFileStatePublished)
}

func TestManagedCompletionRejectsMissingMontageDeliverables(t *testing.T) {
	for _, test := range []struct {
		name    string
		missing string
	}{
		{name: "final video", missing: "output/final.mp4"},
		{name: "project", missing: "output/montage-project.json"},
		{name: "cover", missing: "output/cover.png"},
		{name: "manifest", missing: "output/delivery-manifest.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newManagedCompletionFixture(t, model.PlatformMontage)
			for _, file := range []struct {
				name string
				size int64
				body string
			}{
				{name: "final.mp4", size: 1024, body: validTestMP4()},
				{name: "montage-project.json", size: 2, body: `{}`},
				{name: "cover.png", size: 1024, body: "image"},
				{name: "delivery-manifest.json", size: 2, body: `{}`},
			} {
				if "output/"+file.name != test.missing {
					f.addPendingFile(t, file.name, file.size, file.body)
				}
			}

			if err := f.complete(t, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
				t.Fatal(err)
			}
			f.assertTerminal(t, model.TaskStatusFailed, model.TaskExecutionFailed, model.TaskFileStateCollected)
			found, err := f.repo.Tasks().FindByID(context.Background(), f.task.ID)
			if err != nil || !strings.Contains(found.ErrorMessage, test.missing) {
				t.Fatalf("failure task=%#v err=%v", found, err)
			}
		})
	}
}

func TestCompleteCloudExecutionRejectsForgedMontageMP4(t *testing.T) {
	f := newManagedCompletionFixture(t, model.PlatformMontage)
	f.addPendingFile(t, "final.mp4", 16, "not really an mp4")
	f.addPendingFile(t, "montage-project.json", 2, `{}`)
	f.addPendingFile(t, "cover.png", 1024, "image")
	f.addPendingFile(t, "delivery-manifest.json", 2, `{}`)

	if err := f.complete(t, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	f.assertTerminal(t, model.TaskStatusFailed, model.TaskExecutionFailed, model.TaskFileStateCollected)
}

func TestCompleteCloudExecutionRejectsInvalidMontageJSON(t *testing.T) {
	for _, invalidName := range []string{"montage-project.json", "delivery-manifest.json"} {
		t.Run(invalidName, func(t *testing.T) {
			f := newManagedCompletionFixture(t, model.PlatformMontage)
			f.addPendingFile(t, "final.mp4", 1024, validTestMP4())
			f.addPendingFile(t, "montage-project.json", 2, `{}`)
			f.addPendingFile(t, "cover.png", 1024, "image")
			f.addPendingFile(t, "delivery-manifest.json", 2, `{}`)
			files, err := f.repo.TaskFiles().FindByExecutionID(context.Background(), f.execution.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if file.FilePath != "output/"+invalidName {
					continue
				}
				f.store.files[file.OSSKey] = []byte("{")
				file.FileSize = 1
				if _, err := f.repo.TaskFiles().UpsertPendingCurrentExecution(context.Background(), f.task.ID, f.execution.ID, file); err != nil {
					t.Fatal(err)
				}
			}

			if err := f.complete(t, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
				t.Fatal(err)
			}
			f.assertTerminal(t, model.TaskStatusFailed, model.TaskExecutionFailed, model.TaskFileStateCollected)
		})
	}
}

func TestValidateMontageCompletionArtifactsRejectsStaleRequiredFiles(t *testing.T) {
	for _, staleState := range []string{model.TaskFileStateCollected, model.TaskFileStateSuperseded} {
		t.Run(staleState, func(t *testing.T) {
			files := []*model.TaskFile{
				{State: model.TaskFileStatePending, FilePath: "output/final.mp4", FileSize: 1},
				{State: model.TaskFileStatePublished, FilePath: "output/montage-project.json", FileSize: 1},
				{State: staleState, FilePath: "output/cover.png", FileSize: 1},
				{State: model.TaskFileStatePending, FilePath: "output/delivery-manifest.json", FileSize: 1},
			}

			validation := validateMontageCompletionArtifacts(files)
			if validation.Valid || !strings.Contains(validation.Reason, "output/cover.png") {
				t.Fatalf("validation = %#v, want stale cover reported missing", validation)
			}
		})
	}
}

func TestValidateMontageCompletionArtifactsRequiresCanonicalPathCase(t *testing.T) {
	files := []*model.TaskFile{
		{State: model.TaskFileStatePending, FilePath: "output/final.mp4", FileSize: 1},
		{State: model.TaskFileStatePending, FilePath: "output/montage-project.json", FileSize: 1},
		{State: model.TaskFileStatePending, FilePath: "OUTPUT/COVER.PNG", FileSize: 1},
		{State: model.TaskFileStatePending, FilePath: "output/delivery-manifest.json", FileSize: 1},
	}

	validation := validateMontageCompletionArtifacts(files)
	if validation.Valid || !strings.Contains(validation.Reason, "output/cover.png") {
		t.Fatalf("validation = %#v, want non-canonical cover path reported missing", validation)
	}
}

func TestCompleteCloudExecutionRejectsNestedAgentOnlyResult(t *testing.T) {
	f := newManagedCompletionFixture(t, model.PlatformArticle)
	result := &agent.ExecutionResult{
		Success: true, RemoteArtifacts: true,
		ToolUseSummary: map[string]int{"Agent": 1, "TaskUpdate": 2},
	}
	if err := f.complete(t, result); err != nil {
		t.Fatal(err)
	}
	f.assertTerminal(t, model.TaskStatusFailed, model.TaskExecutionFailed, model.TaskFileStateCollected)
	found, err := f.repo.Tasks().FindByID(context.Background(), f.task.ID)
	if err != nil || found.ErrorMessage != agent.NestedAgentDelegationError {
		t.Fatalf("nested-agent failure task=%#v err=%v", found, err)
	}
}

func TestCompleteCloudExecutionBillingUsesManagedDurableDelivery(t *testing.T) {
	for _, test := range []struct {
		name       string
		filePath   string
		mimeType   string
		wantRefund bool
	}{
		{name: "provider failure without output reverses", wantRefund: true},
		{name: "provider failure with process artifact reverses", filePath: "output/failure-state.json", mimeType: "application/json", wantRefund: true},
		{name: "provider failure with collected contract file reverses", filePath: "output/04-article-final.md", mimeType: "text/markdown", wantRefund: true},
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
			if test.filePath != "" {
				if _, err := billing.repo.TaskFiles().UpsertPendingCurrentExecution(ctx, task.ID, execution.ID, &model.TaskFile{
					Role: model.FileRoleDraft, FileName: filepath.Base(test.filePath), FilePath: test.filePath,
					MimeType: test.mimeType, FileSize: 12, OSSKey: "managed-tests/" + execution.ID + "/" + test.filePath,
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

func TestCompleteCloudExecutionRollsBackInsertedSettlementWithCoreTransaction(t *testing.T) {
	ctx := context.Background()
	svc, billing, _ := newFixedTaskBillingFixture(t, 1_000, 0)
	projectID := createTestProject(t, billing.repo, billingWalletUserID, model.PlatformArticle)
	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		ExecutionProfile: "effective", UserID: billingWalletUserID, ProjectID: projectID,
		Prompt: "atomic cloud reversal", Quantity: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	task := tasks[0]
	execution := attachManagedExecution(t, billing.repo, task)
	if _, err := billing.repo.TaskFiles().UpsertPendingCurrentExecution(ctx, task.ID, execution.ID, &model.TaskFile{
		Role: model.FileRoleOther, FileName: "failure-state.json", FilePath: "output/failure-state.json",
		MimeType: "application/json", FileSize: 12, OSSKey: "managed-tests/" + execution.ID + "/output/failure-state.json",
	}); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("commit settlement failed")
	svc.repo = &persistThenFailSettlementRepository{Repository: billing.repo, err: wantErr}
	result := &agent.ExecutionResult{
		Success: false, Error: "provider unavailable",
		TerminalReason: model.TaskBillingTerminalProviderError, RemoteArtifacts: true,
	}

	if err := svc.CompleteCloudExecution(ctx, execution.ID, result); !errors.Is(err, wantErr) {
		t.Fatalf("CompleteCloudExecution error = %v, want settlement failure", err)
	}
	persistedTask, _ := billing.repo.Tasks().FindByID(ctx, task.ID)
	persistedExecution, _ := billing.repo.TaskExecutions().FindByID(ctx, execution.ID)
	files, _ := billing.repo.TaskFiles().FindByExecutionID(ctx, execution.ID)
	if persistedTask.Status != model.TaskStatusRunning || persistedTask.Result != nil || persistedTask.CompletedAt != nil ||
		persistedTask.BillingTerminalReason != "" || persistedTask.CostStatus != "" || len(persistedTask.TerminalModelUsage.Data()) != 0 {
		t.Fatalf("failed settlement leaked terminal task state: %#v", persistedTask)
	}
	if persistedExecution.FinalizationStatus != model.TaskExecutionFinalizationTerminal || persistedExecution.ManifestStatus != model.TaskExecutionManifestPending ||
		len(files) != 1 || files[0].State != model.TaskFileStatePending {
		t.Fatalf("failed settlement leaked artifact state: execution=%#v files=%#v", persistedExecution, files)
	}
	if _, err := billing.repo.Billing().FindSettlementByKey(ctx, "task-terminal-reversal", *task.BillingChargeID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("failed transaction retained settlement outbox: %v", err)
	}

	svc.repo = billing.repo
	if err := svc.CompleteCloudExecution(ctx, execution.ID, result); err != nil {
		t.Fatalf("resume cloud completion: %v", err)
	}
	if _, err := billing.repo.Billing().FindSettlementByKey(ctx, "task-terminal-reversal", *task.BillingChargeID); err != nil {
		t.Fatalf("resumed completion did not persist settlement: %v", err)
	}
}

func TestReconcileResumedExecutionFailureKeepsChargeForPriorPublishedDelivery(t *testing.T) {
	ctx := context.Background()
	svc, billing, _ := newFixedTaskBillingFixture(t, 1_000, 0)
	projectID := createTestProject(t, billing.repo, billingWalletUserID, model.PlatformArticle)
	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
		UserID: billingWalletUserID, ProjectID: projectID, Prompt: "original request", Quantity: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	task := tasks[0]
	store := &fakeTaskStorage{name: "oss", files: map[string][]byte{}}
	svc.store = store
	priorExecution := &model.TaskExecution{
		ID: "prior-successful-execution", TaskID: task.ID, Attempt: 1,
		Target: "managed-test", Status: model.TaskExecutionSucceeded, Started: true,
		StartedAt: &time.Time{}, CompletedAt: &time.Time{}, ManifestSealed: true,
		ManifestStatus:     model.TaskExecutionManifestPublished,
		FinalizationStatus: model.TaskExecutionFinalizationDone,
	}
	if err := applyAgentPackIdentity(priorExecution, task.Type); err != nil {
		t.Fatal(err)
	}
	if err := billing.repo.TaskExecutions().Create(ctx, priorExecution); err != nil {
		t.Fatal(err)
	}
	addDurableArticleDelivery(t, billing.repo, store, task, priorExecution.ID)
	if won, err := billing.repo.Tasks().CompareAndSwapStatusAndStartedAt(ctx, task.ID, model.TaskStatusPending, model.TaskStatusRunning); err != nil || !won {
		t.Fatalf("start resumed task: won=%v err=%v", won, err)
	}
	execution := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: 2, ParentExecutionID: "prior-successful-execution",
		Target: "managed-test", Status: model.TaskExecutionStarting,
		RuntimeProfile: task.Type, RuntimeImage: "registry/runtime@sha256:test",
	}
	if err := applyAgentPackIdentity(execution, task.Type); err != nil {
		t.Fatal(err)
	}
	if err := billing.repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if won, err := billing.repo.Tasks().SetCurrentExecution(ctx, task.ID, execution.ID); err != nil || !won {
		t.Fatalf("set resumed execution current: won=%v err=%v", won, err)
	}

	if err := svc.ReconcileExecutionFailure(ctx, execution.ID, model.TaskExecutionFailed, "runtime_failed", nil); err != nil {
		t.Fatal(err)
	}

	found, err := billing.repo.Tasks().FindByID(ctx, task.ID)
	if err != nil || found.Status != model.TaskStatusFailed {
		t.Fatalf("resumed terminal task=%#v err=%v", found, err)
	}
	if _, err := billing.repo.Billing().FindSettlementByKey(ctx, "task-terminal-reversal", *task.BillingChargeID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("prior published delivery allowed task charge reversal: %v", err)
	}
	files, err := billing.repo.TaskFiles().FindByTaskID(ctx, task.ID)
	if err != nil || len(files) != 3 || files[0].State != model.TaskFileStatePublished {
		t.Fatalf("prior published delivery=%#v err=%v", files, err)
	}
}
