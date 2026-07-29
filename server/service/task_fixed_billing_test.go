package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
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

func TestTaskFixedBillingBatchAdmissionChargesEachTaskOnce(t *testing.T) {
	ctx := context.Background()
	svc, f, enqueuer := newFixedTaskBillingFixture(t, 1_500, 0)
	projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformArticle)

	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "cost_effective",
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
			tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "cost_effective",
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

	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "cost_effective",
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
	plan := &model.Plan{ExecutionProfile: "cost_effective",
		ID: "plan-current-catalog", UserID: billingWalletUserID, ProjectID: projectID,
		Type: model.PlatformArticle, Status: model.PlanStatusActive, Prompt: "scheduled",
	}

	first, err := svc.CreateFromPlan(ctx, plan)
	if err != nil || first == nil || first.BillingPriceCredits != 500 {
		t.Fatalf("first scheduled task = %#v, %v", first, err)
	}

	bundle := testBillingBundle()
	bundle.Products.CatalogID = "retail-test-v2"
	bundle.Products.SKUs[0].ExecutionProfile = "cost_effective"
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
	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "cost_effective",
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

func TestTaskTerminalBillingReasonIsIdenticalForLocalAndCloudEvidence(t *testing.T) {
	tests := []struct {
		name      string
		execution model.TaskExecution
		result    *agent.ExecutionResult
		want      string
	}{
		{name: "success", execution: model.TaskExecution{Status: model.TaskExecutionSucceeded}, want: model.TaskBillingTerminalCompleted},
		{name: "provider", execution: model.TaskExecution{Status: model.TaskExecutionFailed}, result: &agent.ExecutionResult{TerminalReason: model.TaskBillingTerminalProviderError}, want: model.TaskBillingTerminalProviderError},
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

// seedHistoricalManagedLocalExecution models an execution that began locally
// before managed profiles were made cloud-only. Terminal settlement must remain
// able to finalize this durable historical state.
func seedHistoricalManagedLocalExecution(t *testing.T, f *billingWalletFixture, task *model.Task) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	task.Status = model.TaskStatusRunning
	task.ExecutionTarget = model.ExecutionTargetLocalClaimed
	task.LocalClaimDeadline = nil

	profiledExecution := model.NewTaskExecutionAgentProfile(task.AgentProfileSnapshot)
	execution := &profiledExecution
	execution.ID = uuid.NewString()
	execution.TaskID = task.ID
	execution.Attempt = 1
	execution.Target = model.ExecutionTargetLocalClaimed
	execution.Status = model.TaskExecutionRunning
	execution.Started = true
	execution.StartedAt = &now
	execution.RuntimeProfile = "local"
	if err := f.repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatalf("create historical local execution: %v", err)
	}
	task.CurrentExecutionID = &execution.ID
	if err := f.repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("seed historical local task: %v", err)
	}
}

func TestLocalTaskTerminalBillingEnqueuesAndAppliesOneReversal(t *testing.T) {
	ctx := context.Background()
	svc, f, _ := newFixedTaskBillingFixture(t, 1_000, 0)
	projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformArticle)
	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "cost_effective",
		UserID: billingWalletUserID, ProjectID: projectID, Prompt: "local failure", Quantity: 1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	task := tasks[0]
	seedHistoricalManagedLocalExecution(t, f, task)
	result := &agent.ExecutionResult{Success: false, Error: "ark unavailable", TerminalReason: model.TaskBillingTerminalProviderError}
	if err := svc.CompleteLocalTask(ctx, task.ID, result); err != nil {
		t.Fatalf("CompleteLocalTask: %v", err)
	}
	if err := svc.CompleteLocalTask(ctx, task.ID, result); err != nil {
		t.Fatalf("duplicate CompleteLocalTask: %v", err)
	}
	found, err := f.repo.Tasks().FindByID(ctx, task.ID)
	if err != nil || found.BillingTerminalReason != model.TaskBillingTerminalProviderError {
		t.Fatalf("terminal task = %#v, %v", found, err)
	}
	settlement, err := f.repo.Billing().FindSettlementByKey(ctx, "task-terminal-reversal", *task.BillingChargeID)
	if err != nil || settlement.Action != model.BillingSettlementActionReverseTask || settlement.Reason != model.TaskBillingTerminalProviderError {
		t.Fatalf("reversal settlement = %#v, %v", settlement, err)
	}
	if processed, err := f.wallet.ProcessSettlementOutbox(ctx, 10); err != nil || processed != 1 {
		t.Fatalf("ProcessSettlementOutbox=%d, %v", processed, err)
	}
	if processed, err := f.wallet.ProcessSettlementOutbox(ctx, 10); err != nil || processed != 0 {
		t.Fatalf("duplicate ProcessSettlementOutbox=%d, %v", processed, err)
	}
	if account := f.account(t, billingWalletUserID); account.PaidCredits != 1_000 || account.DebtCredits != 0 {
		t.Fatalf("account after reversal = %#v", account)
	}
}

func TestLocalTaskTerminalBillingKeepsChargeWhenDurableOutputExists(t *testing.T) {
	ctx := context.Background()
	svc, f, _ := newFixedTaskBillingFixture(t, 1_000, 0)
	projectID := createTestProject(t, f.repo, billingWalletUserID, model.PlatformArticle)
	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "cost_effective",
		UserID: billingWalletUserID, ProjectID: projectID, Prompt: "partial output", Quantity: 1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	task := tasks[0]
	seedHistoricalManagedLocalExecution(t, f, task)
	if err := f.repo.TaskFiles().Create(ctx, &model.TaskFile{
		TaskID: task.ID, State: model.TaskFileStatePublished, Role: model.FileRoleDraft,
		FileName: "partial.md", FilePath: "output/partial.md", FileSize: 12,
	}); err != nil {
		t.Fatalf("create durable partial output: %v", err)
	}
	if err := svc.CompleteLocalTask(ctx, task.ID, &agent.ExecutionResult{
		Success: false, Error: "ark unavailable", TerminalReason: model.TaskBillingTerminalProviderError,
	}); err != nil {
		t.Fatalf("CompleteLocalTask: %v", err)
	}
	if _, err := f.repo.Billing().FindSettlementByKey(ctx, "task-terminal-reversal", *task.BillingChargeID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("durable output reversal lookup error=%v, want not found", err)
	}
	if account := f.account(t, billingWalletUserID); account.PaidCredits != 500 {
		t.Fatalf("paid credits=%d, want original charge retained", account.PaidCredits)
	}
}
