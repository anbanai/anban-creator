package service

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
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

func TestCreditService_RefundForGoalTask_FullRefund(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newTestCreditService(repo)
	ctx := context.Background()

	userID := createCreditTestUser(t, repo, 0)
	taskID := uuid.New().String()
	seedDeduction(t, repo, userID, taskID, 900) // 3x of 300

	if err := svc.RefundForGoalTask(ctx, taskID, 0, 3, "取消"); err != nil {
		t.Fatalf("RefundForGoalTask: %v", err)
	}

	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 900 {
		t.Errorf("balance = %d, want 900 (full refund, 0 attempts used)", user.CreditsBalance)
	}
}

func TestCreditService_RefundForGoalTask_ProportionalTwoThirds(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newTestCreditService(repo)
	ctx := context.Background()

	userID := createCreditTestUser(t, repo, 0)
	taskID := uuid.New().String()
	seedDeduction(t, repo, userID, taskID, 900)

	// 1 attempt used out of 3 → refund 2/3 = 600.
	if err := svc.RefundForGoalTask(ctx, taskID, 1, 3, "取消"); err != nil {
		t.Fatalf("RefundForGoalTask: %v", err)
	}

	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 600 {
		t.Errorf("balance = %d, want 600 (2/3 of 900)", user.CreditsBalance)
	}
}

func TestCreditService_RefundForGoalTask_OneThirdRemaining(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newTestCreditService(repo)
	ctx := context.Background()

	userID := createCreditTestUser(t, repo, 0)
	taskID := uuid.New().String()
	seedDeduction(t, repo, userID, taskID, 900)

	// 2 attempts used out of 3 → refund 1/3 = 300.
	if err := svc.RefundForGoalTask(ctx, taskID, 2, 3, "未达目标"); err != nil {
		t.Fatalf("RefundForGoalTask: %v", err)
	}

	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 300 {
		t.Errorf("balance = %d, want 300 (1/3 of 900)", user.CreditsBalance)
	}
}

func TestCreditService_RefundForGoalTask_AllAttemptsConsumed(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newTestCreditService(repo)
	ctx := context.Background()

	userID := createCreditTestUser(t, repo, 0)
	taskID := uuid.New().String()
	seedDeduction(t, repo, userID, taskID, 900)

	// 3 attempts used out of 3 → refund 0.
	if err := svc.RefundForGoalTask(ctx, taskID, 3, 3, "未达目标"); err != nil {
		t.Fatalf("RefundForGoalTask: %v", err)
	}

	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 0 {
		t.Errorf("balance = %d, want 0 (no refund when all attempts consumed)", user.CreditsBalance)
	}
}

func TestCreditService_RefundForGoalTask_ClampsAttemptsAboveMax(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newTestCreditService(repo)
	ctx := context.Background()

	userID := createCreditTestUser(t, repo, 0)
	taskID := uuid.New().String()
	seedDeduction(t, repo, userID, taskID, 900)

	// attemptsUsed > maxAttempts must not over-refund (no negative refund).
	if err := svc.RefundForGoalTask(ctx, taskID, 10, 3, "未达目标"); err != nil {
		t.Fatalf("RefundForGoalTask: %v", err)
	}
	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 0 {
		t.Errorf("balance = %d, want 0 (attempts above max clamped)", user.CreditsBalance)
	}
}

func TestCreditService_RefundForGoalTask_ClampsNegativeAttempts(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newTestCreditService(repo)
	ctx := context.Background()

	userID := createCreditTestUser(t, repo, 0)
	taskID := uuid.New().String()
	seedDeduction(t, repo, userID, taskID, 900)

	// Negative attemptsUsed must be clamped to 0 → full refund (not > 100%).
	if err := svc.RefundForGoalTask(ctx, taskID, -5, 3, "未达目标"); err != nil {
		t.Fatalf("RefundForGoalTask: %v", err)
	}
	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 900 {
		t.Errorf("balance = %d, want 900 (negative attempts clamped to 0)", user.CreditsBalance)
	}
}

func TestCreditService_RefundForGoalTask_ClampsZeroMaxAttempts(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newTestCreditService(repo)
	ctx := context.Background()

	userID := createCreditTestUser(t, repo, 0)
	taskID := uuid.New().String()
	seedDeduction(t, repo, userID, taskID, 900)

	// maxAttempts<=0 should be treated as 1 to avoid divide-by-zero. With
	// attemptsUsed=0, refund should be 900 (the full deduction).
	if err := svc.RefundForGoalTask(ctx, taskID, 0, 0, "未达目标"); err != nil {
		t.Fatalf("RefundForGoalTask: %v", err)
	}
	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 900 {
		t.Errorf("balance = %d, want 900 (maxAttempts=0 clamped to 1)", user.CreditsBalance)
	}
}

func TestCreditService_RefundForGoalTask_Idempotent(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newTestCreditService(repo)
	ctx := context.Background()

	userID := createCreditTestUser(t, repo, 0)
	taskID := uuid.New().String()
	seedDeduction(t, repo, userID, taskID, 900)

	// First call: refund 600.
	if err := svc.RefundForGoalTask(ctx, taskID, 1, 3, "取消"); err != nil {
		t.Fatalf("first RefundForGoalTask: %v", err)
	}
	// Second call: must be a no-op.
	if err := svc.RefundForGoalTask(ctx, taskID, 1, 3, "取消"); err != nil {
		t.Fatalf("second RefundForGoalTask: %v", err)
	}

	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 600 {
		t.Errorf("balance = %d, want 600 (double refund must be prevented)", user.CreditsBalance)
	}
}

func TestCreditService_RefundForGoalTask_NoDeductionIsNoop(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newTestCreditService(repo)
	ctx := context.Background()

	userID := createCreditTestUser(t, repo, 0)
	taskID := uuid.New().String()
	// No seedDeduction — task has no deduction record.

	if err := svc.RefundForGoalTask(ctx, taskID, 0, 3, "取消"); err != nil {
		t.Fatalf("RefundForGoalTask: %v", err)
	}

	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.CreditsBalance != 0 {
		t.Errorf("balance = %d, want 0 (refund without deduction must be a no-op)", user.CreditsBalance)
	}
}
