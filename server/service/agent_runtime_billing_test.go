package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
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
	for _, operationID := range []string{
		agentRuntimeReserveOperationID(taskID),
		agentRuntimeSettlementOperationID(taskID),
		agentRuntimeRefundOperationID(taskID),
	} {
		if _, err := repo.Credits().FindByOperationID(context.Background(), operationID); err == nil {
			t.Fatalf("runtime transaction %q exists, want none", operationID)
		} else if err != gorm.ErrRecordNotFound {
			t.Fatalf("find runtime transaction %q: %v", operationID, err)
		}
	}
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

func mustRuntimeMetadata(t *testing.T, tx *model.CreditTransaction) model.CreditTransactionMetadata {
	t.Helper()
	if tx == nil || len(tx.Metadata) == 0 {
		t.Fatal("runtime transaction metadata is empty")
	}
	var metadata model.CreditTransactionMetadata
	if err := json.Unmarshal(tx.Metadata, &metadata); err != nil {
		t.Fatalf("decode runtime metadata: %v", err)
	}
	return metadata
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

func TestSettleAgentRuntimeChargesReportedCostForEveryOutcome(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "success", false: "failure"}[success], func(t *testing.T) {
			ctx := context.Background()
			svc, fixture := newRuntimeBillingCreditService(t, 9000)
			taskID := map[bool]string{true: "task-runtime-success", false: "task-runtime-failure"}[success]
			task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
			if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
				t.Fatalf("deduct task creation: %v", err)
			}

			totalCostUSD := 1.00
			result := &serveragent.ExecutionResult{
				Success:       success,
				Model:         "claude-test",
				SessionID:     "sess-runtime",
				NumTurns:      7,
				DurationAPIMs: 12345,
				TotalCostUSD:  &totalCostUSD,
				TokenUsage: &serveragent.TokenUsage{
					InputTokens: 1000, OutputTokens: 500,
					CacheReadTokens: 300, CacheCreationTokens: 200,
				},
			}
			if err := svc.SettleAgentRuntime(ctx, task, result); err != nil {
				t.Fatalf("settle runtime: %v", err)
			}

			user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
			if err != nil {
				t.Fatalf("find user: %v", err)
			}
			if user.CreditsBalance != -2200 {
				t.Fatalf("balance = %d, want -2200 after fixed fee and runtime usage", user.CreditsBalance)
			}
			tx, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeSettlementOperationID(taskID))
			if err != nil {
				t.Fatalf("find runtime transaction: %v", err)
			}
			if tx.Type != model.CreditTypeAgentRuntime || tx.Amount != -7200 || tx.BalanceAfter != -2200 {
				t.Fatalf("runtime transaction = type %q amount %d balance %d", tx.Type, tx.Amount, tx.BalanceAfter)
			}
			metadata := mustRuntimeMetadata(t, tx)
			if metadata.Provider != "anthropic" || metadata.Model != "claude-test" || metadata.BillingSource != "claude_code_result" {
				t.Fatalf("runtime metadata identity = %#v", metadata)
			}
			if metadata.TotalCostUSD == nil || *metadata.TotalCostUSD != totalCostUSD || metadata.FinalCredits != 7200 {
				t.Fatalf("runtime metadata cost = %#v", metadata)
			}
			if metadata.InputTokens != 1000 || metadata.OutputTokens != 500 || metadata.CacheReadInputTokens != 300 || metadata.CacheCreationInputTokens != 200 {
				t.Fatalf("runtime metadata tokens = %#v", metadata)
			}
		})
	}
}

func TestSettleAgentRuntimeFallsBackToTokenUsage(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 10_000)
	taskID := "task-runtime-token-fallback"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}
	result := &serveragent.ExecutionResult{
		Success: true, Model: "claude-test", SessionID: "sess-token-fallback",
		TokenUsage: &serveragent.TokenUsage{
			InputTokens: 1000, OutputTokens: 1000,
			CacheReadTokens: 1000, CacheCreationTokens: 1000,
		},
	}
	if err := svc.SettleAgentRuntime(ctx, task, result); err != nil {
		t.Fatalf("settle runtime: %v", err)
	}
	tx, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeSettlementOperationID(taskID))
	if err != nil {
		t.Fatalf("find runtime transaction: %v", err)
	}
	if tx.Amount != -159 {
		t.Fatalf("runtime amount = %d, want -159", tx.Amount)
	}
	metadata := mustRuntimeMetadata(t, tx)
	if metadata.TotalCostUSD != nil || metadata.FinalCredits != 159 || metadata.PriceSnapshot["cache_creation_input"] != float64(3.75) {
		t.Fatalf("token fallback metadata = %#v", metadata)
	}
}

func TestSettleAgentRuntimeIsIdempotent(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 10_000)
	taskID := "task-runtime-idempotent"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}
	totalCostUSD := 1.00
	result := &serveragent.ExecutionResult{Model: "claude-test", TotalCostUSD: &totalCostUSD}
	for range 2 {
		if err := svc.SettleAgentRuntime(ctx, task, result); err != nil {
			t.Fatalf("settle runtime: %v", err)
		}
	}
	user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != -1200 {
		t.Fatalf("balance = %d, want one fixed and one runtime charge", user.CreditsBalance)
	}
	txs, err := fixture.repo.Credits().FindByTaskIDAndUserID(ctx, taskID, fixture.userID)
	if err != nil {
		t.Fatalf("find task transactions: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("transactions = %d, want fixed fee plus one runtime charge: %#v", len(txs), txs)
	}
}

func TestSettleAgentRuntimeConcurrentCallsChargeOnce(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 10_000)
	taskID := "task-runtime-concurrent-idempotent"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}
	totalCostUSD := 1.00
	result := &serveragent.ExecutionResult{Model: "claude-test", TotalCostUSD: &totalCostUSD}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- svc.SettleAgentRuntime(ctx, task, result)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent settlement: %v", err)
		}
	}

	user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	if user.CreditsBalance != -1200 {
		t.Fatalf("balance = %d, want one fixed and one runtime charge", user.CreditsBalance)
	}
	txs, err := fixture.repo.Credits().FindByTaskIDAndUserID(ctx, taskID, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 2 {
		t.Fatalf("transactions = %d, want fixed fee plus one runtime charge", len(txs))
	}
}

func TestSettleAgentRuntimeMissingUsageDoesNotFabricateCharge(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 10_000)
	taskID := "task-runtime-missing-usage"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}
	if err := svc.SettleAgentRuntime(ctx, task, &serveragent.ExecutionResult{Model: "claude-test"}); err != nil {
		t.Fatalf("settle runtime: %v", err)
	}
	if _, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeSettlementOperationID(taskID)); err != gorm.ErrRecordNotFound {
		t.Fatalf("runtime transaction error = %v, want record not found", err)
	}
	found, err := fixture.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.BillingStatus != model.TaskBillingStatusSettled || found.BillingShortfallCredits != 0 {
		t.Fatalf("billing = %q shortfall %d, want settled/0", found.BillingStatus, found.BillingShortfallCredits)
	}
}

func TestAdminGrantClearsLegacyPaymentRequiredWithoutChargingShortfall(t *testing.T) {
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
	if found.BillingStatus != model.TaskBillingStatusSettled || found.BillingShortfallCredits != 0 {
		t.Fatalf("billing = %q shortfall %d, want settled/0", found.BillingStatus, found.BillingShortfallCredits)
	}
	assertNoRuntimeTransactions(t, fixture.repo, taskID, fixture.userID)
}
