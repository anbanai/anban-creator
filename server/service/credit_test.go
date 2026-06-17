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
