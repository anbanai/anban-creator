package service

import (
	"context"
	"encoding/json"
	"testing"

	serveragent "github.com/anbanai/anban-creator/server/agent"
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

func mustTxMetadata(t *testing.T, tx *model.CreditTransaction) model.CreditTransactionMetadata {
	t.Helper()
	var metadata model.CreditTransactionMetadata
	if len(tx.Metadata) == 0 {
		t.Fatal("transaction metadata is empty")
	}
	if err := json.Unmarshal(tx.Metadata, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	return metadata
}

func TestDeductForTaskCreationAlsoReservesAgentRuntime(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 10_000)
	taskID := "task-runtime-reserve"

	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}

	user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 2000 {
		t.Fatalf("balance = %d, want 2000 after base fee + runtime reserve", user.CreditsBalance)
	}
	txs, err := fixture.repo.Credits().FindByTaskIDAndUserID(ctx, taskID, fixture.userID)
	if err != nil {
		t.Fatalf("find task transactions: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("transaction count = %d, want 2: %#v", len(txs), txs)
	}
	if txs[0].Type != model.CreditTypeTaskDeduct || txs[0].Amount != -4000 {
		t.Fatalf("first tx = %s %d, want task_deduct -4000", txs[0].Type, txs[0].Amount)
	}
	if txs[1].Type != model.CreditTypeAgentRuntimeReserve || txs[1].Amount != -4000 {
		t.Fatalf("second tx = %s %d, want agent_runtime_reserve -4000", txs[1].Type, txs[1].Amount)
	}
}

func TestSettleAgentRuntimeUsesTotalCostUSDAndRefundsReserveDelta(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 10_000)
	taskID := "task-runtime-refund"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}

	totalCostUSD := 0.50 // ceil(0.50 * 7.2 * 1000) = 3600 credits.
	result := &serveragent.ExecutionResult{
		Success:       true,
		Model:         "claude-test",
		SessionID:     "sess-runtime-refund",
		NumTurns:      7,
		DurationAPIMs: 12345,
		TotalCostUSD:  &totalCostUSD,
		TokenUsage: &serveragent.TokenUsage{
			InputTokens:         1000,
			OutputTokens:        500,
			CacheReadTokens:     300,
			CacheCreationTokens: 200,
		},
	}

	if err := svc.SettleAgentRuntime(ctx, task, result); err != nil {
		t.Fatalf("settle runtime: %v", err)
	}

	actualTx, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeSettlementOperationID(taskID))
	if err != nil {
		t.Fatalf("find runtime settlement tx: %v", err)
	}
	if actualTx.Type != model.CreditTypeAgentRuntime || actualTx.Amount != 0 {
		t.Fatalf("actual tx = %s %d, want agent_runtime 0 marker", actualTx.Type, actualTx.Amount)
	}
	metadata := mustTxMetadata(t, actualTx)
	if metadata.BillingSource != "claude_code_result" || metadata.TotalCostUSD == nil || *metadata.TotalCostUSD != totalCostUSD {
		t.Fatalf("metadata billing source/cost = %q/%v, want claude_code_result/%v", metadata.BillingSource, metadata.TotalCostUSD, totalCostUSD)
	}
	if metadata.FinalCredits != 3600 || metadata.SessionID != "sess-runtime-refund" || metadata.NumTurns != 7 || metadata.DurationAPIMs != 12345 {
		t.Fatalf("metadata final/session/turns/duration = %d/%q/%d/%d", metadata.FinalCredits, metadata.SessionID, metadata.NumTurns, metadata.DurationAPIMs)
	}
	if metadata.CacheReadInputTokens != 300 || metadata.CacheCreationInputTokens != 200 {
		t.Fatalf("metadata cache tokens = read %d creation %d, want 300/200", metadata.CacheReadInputTokens, metadata.CacheCreationInputTokens)
	}

	refundTx, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeRefundOperationID(taskID))
	if err != nil {
		t.Fatalf("find runtime refund tx: %v", err)
	}
	if refundTx.Type != model.CreditTypeAgentRuntimeRefund || refundTx.Amount != 400 {
		t.Fatalf("refund tx = %s %d, want agent_runtime_refund +400", refundTx.Type, refundTx.Amount)
	}
	user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 2400 {
		t.Fatalf("balance = %d, want 2400 after 400 refund", user.CreditsBalance)
	}
}

func TestSettleAgentRuntimeDeductsOverageWhenBalanceIsEnough(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 20_000)
	taskID := "task-runtime-overage"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}

	totalCostUSD := 1.00 // 7200 credits, 3200 over the 4000 reserve.
	result := &serveragent.ExecutionResult{
		Success:      true,
		Model:        "claude-test",
		SessionID:    "sess-runtime-overage",
		TotalCostUSD: &totalCostUSD,
		TokenUsage:   &serveragent.TokenUsage{InputTokens: 100, OutputTokens: 50},
	}

	if err := svc.SettleAgentRuntime(ctx, task, result); err != nil {
		t.Fatalf("settle runtime: %v", err)
	}

	actualTx, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeSettlementOperationID(taskID))
	if err != nil {
		t.Fatalf("find runtime settlement tx: %v", err)
	}
	if actualTx.Type != model.CreditTypeAgentRuntime || actualTx.Amount != -3200 {
		t.Fatalf("actual tx = %s %d, want agent_runtime -3200", actualTx.Type, actualTx.Amount)
	}
	metadata := mustTxMetadata(t, actualTx)
	if metadata.FinalCredits != 7200 || metadata.BaseCredits != 7200 {
		t.Fatalf("metadata credits = base %d final %d, want 7200/7200", metadata.BaseCredits, metadata.FinalCredits)
	}
	found, err := fixture.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.BillingStatus != model.TaskBillingStatusSettled || found.BillingShortfallCredits != 0 {
		t.Fatalf("billing = %q shortfall %d, want settled/0", found.BillingStatus, found.BillingShortfallCredits)
	}
}

func TestSettleAgentRuntimeFallsBackToTokenUsageWhenTotalCostMissing(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 10_000)
	taskID := "task-runtime-token-fallback"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}

	result := &serveragent.ExecutionResult{
		Success:    true,
		Model:      "claude-test",
		SessionID:  "sess-token-fallback",
		TokenUsage: &serveragent.TokenUsage{InputTokens: 1000, OutputTokens: 1000, CacheReadTokens: 1000, CacheCreationTokens: 1000},
	}

	if err := svc.SettleAgentRuntime(ctx, task, result); err != nil {
		t.Fatalf("settle runtime: %v", err)
	}

	actualTx, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeSettlementOperationID(taskID))
	if err != nil {
		t.Fatalf("find runtime settlement tx: %v", err)
	}
	metadata := mustTxMetadata(t, actualTx)
	if metadata.FinalCredits != 159 || metadata.TotalCostUSD != nil {
		t.Fatalf("metadata final credits/cost = %d/%v, want 159/nil", metadata.FinalCredits, metadata.TotalCostUSD)
	}
	if metadata.CacheReadInputTokens != 1000 || metadata.CacheCreationInputTokens != 1000 {
		t.Fatalf("metadata cache tokens = read %d creation %d, want 1000/1000", metadata.CacheReadInputTokens, metadata.CacheCreationInputTokens)
	}
	if got := metadata.PriceSnapshot["cache_creation_input"]; got != float64(3.75) {
		t.Fatalf("cache creation price snapshot = %#v, want 3.75", got)
	}
	refundTx, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeRefundOperationID(taskID))
	if err != nil {
		t.Fatalf("find runtime refund tx: %v", err)
	}
	if refundTx.Amount != 3841 {
		t.Fatalf("refund = %d, want 3841", refundTx.Amount)
	}
}

func TestSettleAgentRuntimeLocksDeliveryWhenOverageBalanceIsInsufficient(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 9_000)
	taskID := "task-runtime-shortfall"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}

	totalCostUSD := 1.00 // 7200 credits, 3200 over reserve; only 1000 remains.
	result := &serveragent.ExecutionResult{
		Success:      true,
		Model:        "claude-test",
		SessionID:    "sess-runtime-shortfall",
		TotalCostUSD: &totalCostUSD,
		TokenUsage:   &serveragent.TokenUsage{InputTokens: 100, OutputTokens: 50},
	}

	if err := svc.SettleAgentRuntime(ctx, task, result); err != nil {
		t.Fatalf("settle runtime: %v", err)
	}

	if _, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeSettlementOperationID(taskID)); err == nil {
		t.Fatal("runtime settlement transaction exists despite unpaid shortfall")
	}
	found, err := fixture.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.BillingStatus != model.TaskBillingStatusPaymentRequired || found.BillingShortfallCredits != 3200 {
		t.Fatalf("billing = %q shortfall %d, want payment_required/3200", found.BillingStatus, found.BillingShortfallCredits)
	}
}

func TestSettleAgentRuntimeRefundsReserveForLocalClaudeRun(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 10_000)
	taskID := "task-runtime-local"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	task.ExecutionTarget = model.ExecutionTargetLocalClaimed
	if err := fixture.repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("mark local claimed: %v", err)
	}
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}
	totalCostUSD := 1.00

	if err := svc.SettleAgentRuntime(ctx, task, &serveragent.ExecutionResult{
		Success:      true,
		Model:        "claude-test",
		TotalCostUSD: &totalCostUSD,
		TokenUsage:   &serveragent.TokenUsage{InputTokens: 100, OutputTokens: 50},
	}); err != nil {
		t.Fatalf("settle local runtime: %v", err)
	}

	if _, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeSettlementOperationID(taskID)); err == nil {
		t.Fatal("local Claude run created platform runtime settlement transaction")
	}
	refundTx, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeRefundOperationID(taskID))
	if err != nil {
		t.Fatalf("find runtime refund tx: %v", err)
	}
	if refundTx.Amount != 4000 {
		t.Fatalf("refund = %d, want full reserve 4000", refundTx.Amount)
	}
	user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 6000 {
		t.Fatalf("balance = %d, want 6000 after keeping only base task fee", user.CreditsBalance)
	}
}

func TestAdminGrantAutoSettlesAgentRuntimeShortfall(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 9_000)
	taskID := "task-runtime-auto-settle"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}

	totalCostUSD := 1.00
	result := &serveragent.ExecutionResult{
		Success:      true,
		Model:        "claude-test",
		SessionID:    "sess-runtime-auto-settle",
		TotalCostUSD: &totalCostUSD,
		TokenUsage:   &serveragent.TokenUsage{InputTokens: 100, OutputTokens: 50},
	}
	rawResult, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if err := fixture.repo.Tasks().UpdateResult(ctx, taskID, string(rawResult)); err != nil {
		t.Fatalf("persist result: %v", err)
	}
	if err := svc.SettleAgentRuntime(ctx, task, result); err != nil {
		t.Fatalf("settle runtime: %v", err)
	}

	if err := svc.AdminGrant(ctx, fixture.userID, 4_000, "充值"); err != nil {
		t.Fatalf("admin grant: %v", err)
	}

	actualTx, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeSettlementOperationID(taskID))
	if err != nil {
		t.Fatalf("find runtime settlement tx: %v", err)
	}
	if actualTx.Type != model.CreditTypeAgentRuntime || actualTx.Amount != -3200 {
		t.Fatalf("actual tx = %s %d, want agent_runtime -3200", actualTx.Type, actualTx.Amount)
	}
	found, err := fixture.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.BillingStatus != model.TaskBillingStatusSettled || found.BillingShortfallCredits != 0 {
		t.Fatalf("billing = %q shortfall %d, want settled/0", found.BillingStatus, found.BillingShortfallCredits)
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
	totalCostUSD := 0.50
	result := &serveragent.ExecutionResult{
		Success:      true,
		Model:        "claude-test",
		SessionID:    "sess-runtime-idempotent",
		TotalCostUSD: &totalCostUSD,
		TokenUsage:   &serveragent.TokenUsage{InputTokens: 100, OutputTokens: 50},
	}

	if err := svc.SettleAgentRuntime(ctx, task, result); err != nil {
		t.Fatalf("first settle runtime: %v", err)
	}
	if err := svc.SettleAgentRuntime(ctx, task, result); err != nil {
		t.Fatalf("second settle runtime: %v", err)
	}

	txs, err := fixture.repo.Credits().FindByTaskIDAndUserID(ctx, taskID, fixture.userID)
	if err != nil {
		t.Fatalf("find task transactions: %v", err)
	}
	countByType := map[string]int{}
	for _, tx := range txs {
		countByType[tx.Type]++
	}
	if countByType[model.CreditTypeAgentRuntime] != 1 || countByType[model.CreditTypeAgentRuntimeRefund] != 1 {
		t.Fatalf("runtime tx counts = %#v, want one settlement and one refund", countByType)
	}
	user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 2400 {
		t.Fatalf("balance = %d, want 2400 after a single refund", user.CreditsBalance)
	}
}

func TestSettleAgentRuntimeRefundsReserveWhenUsageIsMissing(t *testing.T) {
	ctx := context.Background()
	svc, fixture := newRuntimeBillingCreditService(t, 10_000)
	taskID := "task-runtime-no-usage"
	task := seedRuntimeBillingTask(t, fixture.repo, fixture.userID, taskID)
	if _, err := svc.DeductForTaskCreation(ctx, fixture.userID, model.PlatformArticle, taskID); err != nil {
		t.Fatalf("deduct task creation: %v", err)
	}

	if err := svc.SettleAgentRuntime(ctx, task, &serveragent.ExecutionResult{Success: false, Error: "cancelled before sdk result"}); err != nil {
		t.Fatalf("settle runtime: %v", err)
	}

	refundTx, err := fixture.repo.Credits().FindByOperationID(ctx, agentRuntimeRefundOperationID(taskID))
	if err != nil {
		t.Fatalf("find runtime refund tx: %v", err)
	}
	if refundTx.Type != model.CreditTypeAgentRuntimeRefund || refundTx.Amount != 4000 {
		t.Fatalf("refund tx = %s %d, want full runtime reserve refund +4000", refundTx.Type, refundTx.Amount)
	}
	user, err := fixture.repo.Users().FindByID(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 6000 {
		t.Fatalf("balance = %d, want only base task fee consumed", user.CreditsBalance)
	}
}
