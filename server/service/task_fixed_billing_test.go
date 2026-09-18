package service

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/rs/zerolog"
)

func newFixedTaskBillingFixture(t *testing.T, paid, debt int64) (*TaskService, *billingWalletFixture, *mockEnqueuer) {
	t.Helper()
	f := newBillingWalletFixture(t, paid, 0, debt)
	enqueuer := &mockEnqueuer{}
	logger := zerolog.New(io.Discard)
	svc := newTestTaskService(f.repo, enqueuer, nil, &logger, "", nil, nil)
	svc.SetBillingCatalogService(f.catalog)
	svc.SetBillingWalletService(f.wallet)
	svc.SetNASResumeEnabled(true)
	return svc, f, enqueuer
}

func addDurableArticleDelivery(t *testing.T, repo repository.Repository, store *fakeTaskStorage, task *model.Task, executionID string) {
	t.Helper()
	for _, spec := range []struct {
		path, mimeType, role string
	}{
		{path: "output/04-article-final.md", mimeType: "text/markdown", role: model.FileRoleFinalMarkdown},
		{path: "output/05-article.html", mimeType: "text/html", role: model.FileRoleHTML},
		{path: "output/draft.json", mimeType: "application/json", role: model.FileRoleDraftPackage},
		{path: "output/final-review.md", mimeType: "text/markdown", role: model.FileRoleReview},
	} {
		body := validTaskDeliveryFixtureBody(spec.path, spec.mimeType)
		objectKey := buildTaskMCPArtifactStoragePrefix(task, executionID) + spec.path
		store.files[objectKey] = body
		if err := repo.TaskFiles().Create(context.Background(), &model.TaskFile{
			TaskID: task.ID, ExecutionID: executionID, State: model.TaskFileStateDelivered, Role: spec.role,
			FileName: filepath.Base(spec.path), FilePath: spec.path, MimeType: spec.mimeType,
			FileSize: int64(len(body)), OSSKey: objectKey, StorageProvider: store.Name(),
		}); err != nil {
			t.Fatalf("create durable published delivery %s: %v", spec.path, err)
		}
	}
}

func TestTaskFixedBillingBatchAdmissionChargesEachTaskOnce(t *testing.T) {
	ctx := context.Background()
	svc, f, enqueuer := newFixedTaskBillingFixture(t, 1_500, 0)
	projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformArticle)

	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
		UserID: billingWalletUserID, ProjectID: projectID, Prompt: "batch", Quantity: 2,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 2 || len(enqueuer.enqueued) != 2 {
		t.Fatalf("created=%d enqueued=%d, want 2/2", len(tasks), len(enqueuer.enqueued))
	}
	for _, task := range tasks {
		if task.BillingQuoteID == "" || task.BillingCatalogID != "retail-test-v1" || task.BillingSKUID != "task.article.v1" ||
			task.BillingPricingTier != string(model.TierFree) || task.BillingChargeID == nil || *task.BillingChargeID == "" || task.BillingPriceCredits != 500 {
			t.Fatalf("task billing identity = %#v", task)
		}
		charge, err := f.repo.Billing().FindChargeByTask(ctx, task.ID)
		if err != nil || charge.ID != *task.BillingChargeID || charge.PriceCredits != 500 {
			t.Fatalf("task charge = %#v, %v", charge, err)
		}
		quote, err := f.repo.Billing().FindQuoteByKey(ctx, "task-admission-quote", task.ID)
		if err != nil || quote.ConsumedAt == nil || quote.ResourceType != "task" || quote.ResourceID != task.ID {
			t.Fatalf("consumed quote = %#v, %v", quote, err)
		}
	}
	account := f.account(t, billingWalletUserID)
	if account.PaidCredits != 500 || account.DebtCredits != 0 {
		t.Fatalf("account = %#v, want paid=500 debt=0", account)
	}
}

func TestTaskFixedBillingEnqueueCancellationStillFinalizesAndReverses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	svc, f, _ := newFixedTaskBillingFixture(t, 1_000, 0)
	svc.enqueuer = cancelingFailTaskEnqueuer{cancel: cancel, err: errors.New("redis unavailable")}
	projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformArticle)

	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
		UserID: billingWalletUserID, ProjectID: projectID, Prompt: "enqueue failure", Quantity: 1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("created=%d, want 1", len(tasks))
	}
	found, err := f.repo.Tasks().FindByID(context.Background(), tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	settlements, settlementErr := f.repo.Billing().ListSettlementsByTask(context.Background(), tasks[0].ID)
	if found.Status != model.TaskStatusFailed || found.CompletedAt == nil || settlementErr != nil || len(settlements) != 1 || settlements[0].Action != model.BillingSettlementActionReverseTask {
		t.Fatalf("enqueue failure state: task=%#v settlements=%#v settlementErr=%v", found, settlements, settlementErr)
	}
}

func TestTaskFixedBillingRejectsDebtAndInsufficientBalanceBeforeEnqueue(t *testing.T) {
	tests := []struct {
		name string
		paid int64
		debt int64
		want error
	}{
		{name: "debt", paid: 1_000, debt: 1, want: ErrBillingDebtOutstanding},
		{name: "insufficient", paid: 499, want: ErrBillingInsufficientForTask},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			svc, f, enqueuer := newFixedTaskBillingFixture(t, tt.paid, tt.debt)
			projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformArticle)
			tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
				UserID: billingWalletUserID, ProjectID: projectID, Prompt: tt.name, Quantity: 1,
			})
			if !errors.Is(err, tt.want) || tasks != nil {
				t.Fatalf("CreateManual = %#v, %v; want nil/%v", tasks, err, tt.want)
			}
			if len(enqueuer.enqueued) != 0 {
				t.Fatalf("enqueued=%d, want zero", len(enqueuer.enqueued))
			}
			count, countErr := f.repo.Tasks().CountByUserID(ctx, billingWalletUserID, projectID, "")
			if countErr != nil || count != 0 {
				t.Fatalf("task count=%d, %v; want zero", count, countErr)
			}
		})
	}
}

func TestTaskFixedBillingBatchAdmissionIsAtomicWhenTotalBalanceIsInsufficient(t *testing.T) {
	ctx := context.Background()
	svc, f, enqueuer := newFixedTaskBillingFixture(t, 700, 0)
	projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformArticle)

	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
		UserID: billingWalletUserID, ProjectID: projectID, Prompt: "atomic batch", Quantity: 2,
	})
	if !errors.Is(err, ErrBillingInsufficientForTask) || tasks != nil {
		t.Fatalf("CreateManual = %#v, %v; want nil/ErrBillingInsufficientForTask", tasks, err)
	}
	if len(enqueuer.enqueued) != 0 {
		t.Fatalf("enqueued=%d, want zero", len(enqueuer.enqueued))
	}
	count, countErr := f.repo.Tasks().CountByUserID(ctx, billingWalletUserID, projectID, "")
	if countErr != nil || count != 0 {
		t.Fatalf("task count=%d, %v; want zero", count, countErr)
	}
	if account := f.account(t, billingWalletUserID); account.PaidCredits != 700 || account.DebtCredits != 0 {
		t.Fatalf("wallet mutated by failed batch: %#v", account)
	}
}

func TestTaskFixedBillingScheduledRunsResolveCurrentCatalog(t *testing.T) {
	ctx := context.Background()
	svc, f, _ := newFixedTaskBillingFixture(t, 2_000, 0)
	projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformArticle)
	plan := &model.Plan{ExecutionProfile: "effective",
		ID: "plan-current-catalog", UserID: billingWalletUserID, ProjectID: projectID,
		Type: model.PlatformArticle, Status: model.PlanStatusActive, Prompt: "scheduled",
	}

	first, err := svc.CreateFromPlan(ctx, plan)
	if err != nil || first == nil || first.BillingPriceCredits != 500 {
		t.Fatalf("first scheduled task = %#v, %v", first, err)
	}

	bundle := testBillingBundle()
	bundle.Products.CatalogID = "retail-test-v2"
	bundle.Products.SKUs[0].ExecutionProfile = "effective"
	bundle.Products.SKUs[0].PriceCredits = 700
	now := f.now.Add(time.Hour)
	catalog := NewBillingCatalogService(f.repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }})
	if _, err := catalog.Publish(ctx); err != nil {
		t.Fatalf("publish second catalog: %v", err)
	}
	svc.SetBillingCatalogService(catalog)
	svc.SetBillingWalletService(NewBillingWalletService(f.repo, &bundle, BillingWalletOptions{Now: func() time.Time { return now }}))

	second, err := svc.CreateFromPlan(ctx, plan)
	if err != nil || second == nil || second.BillingCatalogID != "retail-test-v2" || second.BillingPriceCredits != 700 {
		t.Fatalf("second scheduled task = %#v, %v", second, err)
	}
	if first.BillingCatalogID != "retail-test-v1" || first.BillingSKUID != "task.article.v1" {
		t.Fatalf("first task price identity changed: %#v", first)
	}
}

func TestTaskFixedBillingResumeKeepsOriginalCharge(t *testing.T) {
	ctx := context.Background()
	svc, f, _ := newFixedTaskBillingFixture(t, 1_000, 0)
	projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformArticle)
	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
		UserID: billingWalletUserID, ProjectID: projectID, Prompt: "first", Quantity: 1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	task := tasks[0]
	if err := f.repo.Tasks().UpdateStatusAndError(ctx, task.ID, model.TaskStatusFailed, "provider failed"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Tasks().SetCompletedAt(ctx, task.ID); err != nil {
		t.Fatal(err)
	}

	resumed, err := svc.Resume(ctx, billingWalletUserID, task.ID, ResumeTaskParams{Prompt: "continue"})
	if err != nil || resumed.ID != task.ID || resumed.BillingChargeID == nil || *resumed.BillingChargeID != *task.BillingChargeID {
		t.Fatalf("Resume = %#v, %v", resumed, err)
	}
	account := f.account(t, billingWalletUserID)
	if account.PaidCredits != 500 {
		t.Fatalf("paid credits=%d, want one 500-credit charge", account.PaidCredits)
	}
	charge, err := f.repo.Billing().FindChargeByTask(ctx, task.ID)
	if err != nil || charge.ID != *task.BillingChargeID {
		t.Fatalf("original charge = %#v, %v", charge, err)
	}
}

func TestShouldReverseTaskChargeUsesExplicitReasonAndDurableDelivery(t *testing.T) {
	tests := []struct {
		reason  string
		durable bool
		want    bool
	}{
		{reason: model.TaskBillingTerminalPlatformError, want: true},
		{reason: model.TaskBillingTerminalProviderError, want: true},
		{reason: model.TaskBillingTerminalExecutionTimeout, want: true},
		{reason: model.TaskBillingTerminalInfrastructureCancelled, want: true},
		{reason: model.TaskBillingTerminalWorkflowError, want: false},
		{reason: model.TaskBillingTerminalUserCancelled, want: false},
		{reason: model.TaskBillingTerminalCompleted, want: false},
		{reason: "execution failed: provider_error", want: false},
		{reason: model.TaskBillingTerminalProviderError, durable: true, want: false},
	}
	for _, tt := range tests {
		if got := ShouldReverseTaskCharge(tt.reason, tt.durable); got != tt.want {
			t.Errorf("ShouldReverseTaskCharge(%q, %v)=%v, want %v", tt.reason, tt.durable, got, tt.want)
		}
	}
}

func TestTaskTerminalBillingReasonUsesExecutionEvidence(t *testing.T) {
	tests := []struct {
		name      string
		execution model.TaskExecution
		result    *agent.ExecutionResult
		want      string
	}{
		{name: "success", execution: model.TaskExecution{Status: model.TaskExecutionSucceeded}, want: model.TaskBillingTerminalCompleted},
		{name: "provider", execution: model.TaskExecution{Status: model.TaskExecutionFailed}, result: &agent.ExecutionResult{TerminalReason: model.TaskBillingTerminalProviderError}, want: model.TaskBillingTerminalProviderError},
		{name: "workflow", execution: model.TaskExecution{Status: model.TaskExecutionFailed}, result: &agent.ExecutionResult{TerminalReason: model.TaskBillingTerminalWorkflowError}, want: model.TaskBillingTerminalWorkflowError},
		{name: "timeout", execution: model.TaskExecution{Status: model.TaskExecutionTimedOut}, want: model.TaskBillingTerminalExecutionTimeout},
		{name: "infrastructure cancellation", execution: model.TaskExecution{Status: model.TaskExecutionCancelled}, want: model.TaskBillingTerminalInfrastructureCancelled},
		{name: "user cancellation", execution: model.TaskExecution{Status: model.TaskExecutionCancelled, TerminalReason: model.TaskBillingTerminalUserCancelled}, want: model.TaskBillingTerminalUserCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalBillingReason(&tt.execution, tt.result); got != tt.want {
				t.Fatalf("terminalBillingReason=%q, want %q", got, tt.want)
			}
		})
	}
}
