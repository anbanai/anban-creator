package service

import (
	"context"
	"os"
	"strings"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func runtimeBillingTestConfig() *srvconfig.Config {
	return &srvconfig.Config{
		Credits: srvconfig.CreditsConfig{
			TaskCosts: map[string]int{
				model.PlatformArticle: 4000,
			},
			// Kept here intentionally: Claude Code runtime reserve is no longer
			// billed to users even if an older config still contains the setting.
			AgentRuntimeReserve: map[string]int{
				model.PlatformArticle: 4000,
			},
		},
		Billing: srvconfig.BillingConfig{
			CreditsPerCNY:         1000,
			DefaultUserMultiplier: 1,
			MinimumChargeCredits:  1,
			TierMultipliers: map[string]float64{
				string(model.TierFree): 1,
			},
		},
		ModelPrices: srvconfig.ModelPricesConfig{
			CurrencyRates: map[string]srvconfig.CurrencyRate{
				"USD": {ToCNY: srvconfig.FlexibleFloat(7.2)},
			},
			TokenModels: map[string]srvconfig.TokenModelPrice{
				"anthropic/claude-test": {
					Currency:           "USD",
					Unit:               1_000_000,
					Input:              srvconfig.FlexibleFloat(3),
					Output:             srvconfig.FlexibleFloat(15),
					CachedInput:        srvconfig.FlexibleFloat(0.30),
					CacheReadInput:     srvconfig.FlexibleFloat(0.30),
					CacheCreationInput: srvconfig.FlexibleFloat(3.75),
				},
			},
		},
		Claude: srvconfig.ClaudeConfig{Model: "claude-test"},
	}
}

func newRuntimeBillingCreditService(t *testing.T, balance int) (*CreditService, repositoryFixture) {
	t.Helper()
	repo := setupCreditTestRepo(t)
	userID := createCreditTestUser(t, repo, balance)
	cfg := runtimeBillingTestConfig()
	svc := newTestCreditService(repo)
	svc.SetFullConfig(cfg)
	return svc, repositoryFixture{repo: repo, userID: userID}
}

type repositoryFixture struct {
	repo   repository.Repository
	userID string
}

func seedRuntimeBillingTask(t *testing.T, repo repository.Repository, userID string, taskID string) *model.Task {
	t.Helper()
	task := &model.Task{
		ID:        taskID,
		UserID:    userID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
		ProjectID: "project-runtime",
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return task
}

func assertNoRuntimeTransactions(t *testing.T, repo repository.Repository, taskID, userID string) {
	t.Helper()
	txs, err := repo.Credits().FindByTaskIDAndUserID(context.Background(), taskID, userID)
	if err != nil {
		t.Fatalf("find task transactions: %v", err)
	}
	for _, tx := range txs {
		switch tx.Type {
		case model.CreditTypeAgentRuntimeReserve, model.CreditTypeAgentRuntime, model.CreditTypeAgentRuntimeRefund:
			t.Fatalf("runtime transaction type %q exists in task transactions: %#v", tx.Type, tx)
		}
	}
}

func TestDeductForTaskCreationDoesNotReserveAgentRuntime(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 10_000)
	taskID := "task-runtime-no-reserve"

	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}

	user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 6000 {
		t.Fatalf("balance = %d, want 6000 after base task fee only", user.CreditsBalance)
	}
	txs, err := fixture.repo.Credits().FindByTaskIDAndUserID(ctx, taskID, fixture.userID)
	if err != nil {
		t.Fatalf("find task transactions: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("transaction count = %d, want only task deduction: %#v", len(txs), txs)
	}
	if txs[0].Type != model.CreditTypeTaskDeduct || txs[0].Amount != -4000 {
		t.Fatalf("tx = %s %d, want task_deduct -4000", txs[0].Type, txs[0].Amount)
	}
	assertNoRuntimeTransactions(t, fixture.repo, taskID, fixture.userID)
}

func TestDeductBatchForTaskCreationDoesNotReserveAgentRuntime(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 20_000)
	taskIDs := []string{"task-runtime-batch-a", "task-runtime-batch-b"}

	if err := svc.DeductBatchForTaskCreation(ctx, fixture.userID, model.PlatformArticle, 8000, taskIDs, 1); err != nil {
		t.Fatalf("deduct batch task creation: %v", err)
	}

	user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 12_000 {
		t.Fatalf("balance = %d, want 12000 after two base task fees only", user.CreditsBalance)
	}
	for _, taskID := range taskIDs {
		txs, err := fixture.repo.Credits().FindByTaskIDAndUserID(ctx, taskID, fixture.userID)
		if err != nil {
			t.Fatalf("find task transactions: %v", err)
		}
		if len(txs) != 1 {
			t.Fatalf("transaction count for %s = %d, want only task deduction: %#v", taskID, len(txs), txs)
		}
		if txs[0].Type != model.CreditTypeTaskDeduct || txs[0].Amount != -4000 {
			t.Fatalf("tx for %s = %s %d, want task_deduct -4000", taskID, txs[0].Type, txs[0].Amount)
		}
		assertNoRuntimeTransactions(t, fixture.repo, taskID, fixture.userID)
	}
}

func TestAdminGrantDoesNotMutateLegacyPaymentRequiredTask(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 1000)
	taskID := "task-runtime-admin-clears"
	seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if err := fixture.repo.Tasks().UpdateBillingStatus(ctx, taskID, model.TaskBillingStatusPaymentRequired, 3200); err != nil {
		t.Fatalf("mark payment required: %v", err)
	}

	if err := svc.AdminGrant(ctx, fixture.userID, 4000, "充值"); err != nil {
		t.Fatalf("admin grant: %v", err)
	}

	user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 5000 {
		t.Fatalf("balance = %d, want grant retained without shortfall charge", user.CreditsBalance)
	}
	found, err := fixture.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.BillingStatus != model.TaskBillingStatusPaymentRequired || found.BillingShortfallCredits != 3200 {
		t.Fatalf("billing = %q shortfall %d, want unchanged payment_required/3200", found.BillingStatus, found.BillingShortfallCredits)
	}
	assertNoRuntimeTransactions(t, fixture.repo, taskID, fixture.userID)
}

func TestProductionHasNoLegacyRuntimeReserveOrPaymentRequiredSettlementMethods(t *testing.T) {
	files := map[string][]string{
		"credit.go": {
			"agentRuntimeReserveOperationID", "agentRuntimeSettlementOperationID", "agentRuntimeRefundOperationID",
			"refundAgentRuntimeReserve", "RefundAgentRuntimeReserve",
			"settlePaymentRequiredBestEffort", "SettlePaymentRequiredTasks", "settlePaymentRequiredTask",
			"CreditTypeAgentRuntimeReserve", "CreditTypeAgentRuntime", "CreditTypeAgentRuntimeRefund",
		},
		"task.go":                     {"RefundAgentRuntimeReserve"},
		"../repository/repository.go": {"FindPaymentRequiredByUser"},
		"../repository/task.go":       {"FindPaymentRequiredByUser"},
	}
	for path, forbidden := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, symbol := range forbidden {
			if strings.Contains(string(raw), symbol) {
				t.Errorf("%s still contains legacy runtime symbol %q", path, symbol)
			}
		}
	}
}
