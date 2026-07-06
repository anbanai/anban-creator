package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// setupCreditTestRepo spins up an in-memory SQLite DB with all the tables the
// credit-refund path touches, and returns a fresh Repository bound to it.
func setupCreditTestRepo(t *testing.T) repository.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.New().String()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, _ := db.DB(); sqlDB != nil {
			sqlDB.Close()
		}
	})
	return repository.New(db)
}

// createCreditTestUser inserts a user with the given starting balance and
// returns the userID.
func createCreditTestUser(t *testing.T, repo repository.Repository, balance int) string {
	t.Helper()
	userID := uuid.New().String()
	if err := repo.Users().Create(context.Background(), &model.User{
		ID:             userID,
		Email:          userID + "@example.com",
		Nickname:       "Credit Test User",
		Password:       "hashed",
		InviteCode:     strings.ReplaceAll(strings.ToUpper(userID[:8]), "-", "X"),
		CreditsBalance: balance,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return userID
}

// seedDeduction inserts a task_deduct CreditTransaction for the given task.
// Mirrors what DeductForTask / DeductBatchWithMultiplier produce.
func seedDeduction(t *testing.T, repo repository.Repository, userID, taskID string, amount int) {
	t.Helper()
	taskIDCopy := taskID
	tx := &model.CreditTransaction{
		UserID:       userID,
		Type:         model.CreditTypeTaskDeduct,
		Amount:       -amount, // expenses are negative
		BalanceAfter: 0,       // not validated in these tests
		TaskID:       &taskIDCopy,
		Description:  "task deduction seed",
	}
	if err := repo.Credits().CreateTransaction(context.Background(), tx); err != nil {
		t.Fatalf("seed deduction: %v", err)
	}
}

func newTestCreditService(repo repository.Repository) *CreditService {
	logger := zerolog.New(io.Discard)
	return NewCreditService(repo, &config.CreditsConfig{
		TaskCosts: map[string]int{
			"article":  4000,
			"seednote": 3200,
		},
	}, &logger)
}

func TestDeductForOperationStoresTaskIDAndLongOperationID(t *testing.T) {
	repo := setupCreditTestRepo(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 1000)
	taskID := uuid.New().String()
	operationID := "video_gen:" + taskID
	svc := newTestCreditService(repo)

	if _, err := svc.DeductForOperation(ctx, userID, model.CreditTypeImageGen, 120, operationID, taskID); err != nil {
		t.Fatalf("deduct operation: %v", err)
	}

	tx, err := repo.Credits().FindDeductionByOperationID(ctx, operationID)
	if err != nil {
		t.Fatalf("find deduction by operation id: %v", err)
	}
	if tx.TaskID == nil || *tx.TaskID != taskID {
		t.Fatalf("transaction task_id = %v, want %q", tx.TaskID, taskID)
	}
}

func TestGetUserBillingMultiplier(t *testing.T) {
	repo := setupCreditTestRepo(t)
	ctx := context.Background()
	userID := uuid.New().String()
	multiplier := 1.05
	if err := repo.Users().Create(ctx, &model.User{
		ID:                userID,
		Email:             userID + "@example.com",
		Nickname:          "Low Margin User",
		Password:          "hashed",
		InviteCode:        "lowmargin",
		CreditsBalance:    1000,
		BillingMultiplier: &multiplier,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	svc := newTestCreditService(repo)

	got, err := svc.GetUserBillingMultiplier(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserBillingMultiplier: %v", err)
	}
	if got != multiplier {
		t.Fatalf("billing multiplier = %v, want %v", got, multiplier)
	}
}

func TestGetUserBillingMultiplierDefaultsToOne(t *testing.T) {
	repo := setupCreditTestRepo(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 1000)
	svc := newTestCreditService(repo)

	got, err := svc.GetUserBillingMultiplier(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserBillingMultiplier: %v", err)
	}
	if got != 1 {
		t.Fatalf("billing multiplier = %v, want 1", got)
	}
}

func TestDeductForTaskUsesFriendlyDescription(t *testing.T) {
	repo := setupCreditTestRepo(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 10_000)
	taskID := uuid.New().String()
	svc := newTestCreditService(repo)

	if _, err := svc.DeductForTask(ctx, userID, "seednote", taskID); err != nil {
		t.Fatalf("deduct task: %v", err)
	}

	tx, err := repo.Credits().FindDeductionByTaskID(ctx, taskID)
	if err != nil {
		t.Fatalf("find deduction by task id: %v", err)
	}
	if tx.Description != "生成种草笔记扣除积分3200" {
		t.Fatalf("description = %q, want friendly seednote deduction", tx.Description)
	}
}

func TestDeductForTaskWithMultiplierUsesFriendlyDescription(t *testing.T) {
	repo := setupCreditTestRepo(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 20_000)
	taskID := uuid.New().String()
	svc := newTestCreditService(repo)

	if _, err := svc.DeductForTask(ctx, userID, "seednote", taskID, 3); err != nil {
		t.Fatalf("deduct goal task: %v", err)
	}

	tx, err := repo.Credits().FindDeductionByTaskID(ctx, taskID)
	if err != nil {
		t.Fatalf("find deduction by task id: %v", err)
	}
	if tx.Description != "生成种草笔记（强目标 x3）扣除积分9600" {
		t.Fatalf("description = %q, want friendly goal-mode deduction", tx.Description)
	}
}

func TestDeductBatchUsesFriendlyDescriptionPerTask(t *testing.T) {
	repo := setupCreditTestRepo(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 20_000)
	taskIDs := []string{uuid.New().String(), uuid.New().String()}
	svc := newTestCreditService(repo)

	if err := svc.DeductBatch(ctx, userID, "article", 8000, taskIDs); err != nil {
		t.Fatalf("deduct batch: %v", err)
	}

	for _, taskID := range taskIDs {
		txs, err := repo.Credits().FindByTaskIDAndUserID(ctx, taskID, userID)
		if err != nil {
			t.Fatalf("find transactions for task %s: %v", taskID, err)
		}
		if len(txs) != 1 {
			t.Fatalf("transactions len for task %s = %d, want 1", taskID, len(txs))
		}
		if txs[0].Description != "生成公众号文章扣除积分4000" {
			t.Fatalf("description = %q, want friendly article deduction", txs[0].Description)
		}
	}
}

func TestDeductForOperationUsesFriendlyDescription(t *testing.T) {
	repo := setupCreditTestRepo(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 1000)
	taskID := uuid.New().String()
	operationID := "image_gen:" + taskID
	svc := newTestCreditService(repo)

	if _, err := svc.DeductForOperation(ctx, userID, model.CreditTypeImageGen, 80, operationID, taskID); err != nil {
		t.Fatalf("deduct operation: %v", err)
	}

	tx, err := repo.Credits().FindDeductionByOperationID(ctx, operationID)
	if err != nil {
		t.Fatalf("find deduction by operation id: %v", err)
	}
	if tx.Description != "AI 生图扣除积分80" {
		t.Fatalf("description = %q, want friendly image generation deduction", tx.Description)
	}
}

func TestDeductForOperationWithMetadataStoresTokenCostSnapshot(t *testing.T) {
	repo := setupCreditTestRepo(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 1000)
	taskID := uuid.New().String()
	operationID := "video_understanding:" + taskID
	svc := newTestCreditService(repo)

	metadata := model.CreditTransactionMetadata{
		Provider:          "moonshot",
		Model:             "kimi-k2.7-code-highspeed",
		Route:             model.CreditTypeVideoUnderstanding,
		InputTokens:       10_000,
		CachedInputTokens: 2_000,
		OutputTokens:      1_000,
		TotalTokens:       11_000,
		BaseCredits:       173,
		TierMultiplier:    1.30,
		UserMultiplier:    1.00,
		FinalCredits:      225,
		PriceSnapshot:     map[string]any{"currency": "USD", "input": 1.90, "output": 8.00},
	}
	if _, err := svc.DeductForOperationWithMetadata(ctx, userID, model.CreditTypeVideoUnderstanding, 225, metadata, operationID, taskID); err != nil {
		t.Fatalf("deduct operation with metadata: %v", err)
	}

	tx, err := repo.Credits().FindDeductionByOperationID(ctx, operationID)
	if err != nil {
		t.Fatalf("find deduction by operation id: %v", err)
	}
	var got model.CreditTransactionMetadata
	if err := json.Unmarshal(tx.Metadata, &got); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if got.Route != model.CreditTypeVideoUnderstanding || got.FinalCredits != 225 || got.TotalTokens != 11_000 {
		t.Fatalf("metadata = %#v, want video understanding token cost snapshot", got)
	}
}

func TestRefundForOperationByIDKeepsTaskID(t *testing.T) {
	repo := setupCreditTestRepo(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 1000)
	taskID := uuid.New().String()
	operationID := "video_gen:" + taskID
	svc := newTestCreditService(repo)

	if _, err := svc.DeductForOperation(ctx, userID, model.CreditTypeVideoGen, 120, operationID, taskID); err != nil {
		t.Fatalf("deduct operation: %v", err)
	}
	if err := svc.RefundForOperationByID(ctx, operationID, "视频生成提交失败退还"); err != nil {
		t.Fatalf("refund operation: %v", err)
	}

	txs, err := repo.Credits().FindByTaskIDAndUserID(ctx, taskID, userID)
	if err != nil {
		t.Fatalf("find task transactions: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("task transactions len = %d, want deduction and refund", len(txs))
	}
	refund := txs[1]
	if refund.Amount != 120 {
		t.Fatalf("refund amount = %d, want 120", refund.Amount)
	}
	if refund.TaskID == nil || *refund.TaskID != taskID {
		t.Fatalf("refund task_id = %v, want %q", refund.TaskID, taskID)
	}
}
