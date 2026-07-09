package service

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// newPricedCreditService builds a CreditService wired with task base fees and
// e-commerce module prices. Module prices remain available for delivery-scale
// estimates, while task creation should deduct only TaskCosts.
func newPricedCreditService(repo repository.Repository) *CreditService {
	logger := zerolog.New(io.Discard)
	return NewCreditService(repo, &config.CreditsConfig{
		TaskCosts: map[string]int{
			model.PlatformArticle:      4000,
			model.PlatformSeednote:     3600,
			model.PlatformEcommerce:    3000,
			model.PlatformVideoCreator: 2000,
			model.PlatformVideoEditor:  2000,
			model.PlatformMontage:  2000,
			"viral_analysis":           1200,
		},
		EcommerceModulePrices: map[string]int{
			"main_images":  1500,
			"detail_page":  3000,
			"cover_banner": 600,
			"share_image":  400,
			"sku_images":   300,
		},
	}, &logger)
}

func TestCreditServiceIncludesMontageTaskCost(t *testing.T) {
	svc := NewCreditService(nil, nil, nil)
	cost, ok := svc.TaskCost(model.PlatformMontage)
	if !ok {
		t.Fatal("TaskCost(montage) ok = false")
	}
	if cost != 2000 {
		t.Fatalf("TaskCost(montage) = %d, want 2000", cost)
	}
}

func TestEcommercePackageCost(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newPricedCreditService(repo)

	tests := []struct {
		name     string
		selected map[string]int
		wantCost int
		wantOK   bool
	}{
		{"empty selection", map[string]int{}, 0, true},
		{"single module single qty", map[string]int{"main_images": 1}, 1500, true},
		{"main_images x5", map[string]int{"main_images": 5}, 7500, true},
		{"full package", map[string]int{
			"main_images":  5,
			"detail_page":  10,
			"cover_banner": 2,
			"share_image":  1,
			"sku_images":   4,
		}, 1500*5 + 3000*10 + 600*2 + 400*1 + 300*4, true},
		{"zero/negative quantity skipped", map[string]int{"main_images": 0, "detail_page": 0, "sku_images": -3}, 0, true},
		{"unknown module mixed", map[string]int{"main_images": 1, "bogus": 3}, 1500, false},
		{"only unknown modules", map[string]int{"bogus": 3}, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotCost, gotOK := svc.EcommercePackageCost(tc.selected)
			if gotCost != tc.wantCost {
				t.Errorf("cost = %d, want %d", gotCost, tc.wantCost)
			}
			if gotOK != tc.wantOK {
				t.Errorf("known = %v, want %v", gotOK, tc.wantOK)
			}
		})
	}
}

// When module prices are not configured at all, EcommercePackageCost reports
// every module as unknown rather than silently pricing it at 0. The helper is
// retained for delivery-scale estimates, not task-creation billing.
func TestEcommercePackageCost_UnconfiguredPrices(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newTestCreditService(repo) // no EcommerceModulePrices in config

	cost, ok := svc.EcommercePackageCost(map[string]int{"main_images": 5, "detail_page": 2})
	if ok {
		t.Errorf("expected known=false when module prices are unconfigured, got true (cost=%d)", cost)
	}
	if cost != 0 {
		t.Errorf("cost = %d, want 0 when no module is priced", cost)
	}
}

func TestDeductForTaskWithAmount_SuccessAndRefund(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newPricedCreditService(repo)
	const starting = 100_000
	userID := createCreditTestUser(t, repo, starting)

	const amount = 4500 // e.g. main_images(1500) + detail_page(3000)
	bal, err := svc.DeductForTaskWithAmount(context.Background(), userID, "ecommerce", "ecom-task-A", amount)
	if err != nil {
		t.Fatalf("deduct: %v", err)
	}
	if bal != starting-amount {
		t.Errorf("balance after deduct = %d, want %d", bal, starting-amount)
	}
	tx, err := repo.Credits().FindDeductionByTaskID(context.Background(), "ecom-task-A")
	if err != nil {
		t.Fatalf("find ecommerce deduction: %v", err)
	}
	if tx.Description != "生成电商出图扣除积分4500" {
		t.Fatalf("description = %q, want friendly ecommerce package deduction", tx.Description)
	}

	// RefundForTask is amount-agnostic — it looks up the deduction by task_id and
	// refunds -deduction.Amount, so the same code refunds a fixed-cost task and a
	// package-priced task without change. Verify it restores the exact amount.
	if err := svc.RefundForTask(context.Background(), "ecom-task-A"); err != nil {
		t.Fatalf("refund: %v", err)
	}
	gotBal, err := svc.GetBalance(context.Background(), userID)
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if gotBal != starting {
		t.Errorf("balance after refund = %d, want %d (full round-trip)", gotBal, starting)
	}

	// Idempotent refund: a second refund must NOT double-credit.
	if err := svc.RefundForTask(context.Background(), "ecom-task-A"); err != nil {
		t.Fatalf("second refund: %v", err)
	}
	gotBal2, _ := svc.GetBalance(context.Background(), userID)
	if gotBal2 != starting {
		t.Errorf("balance after double refund = %d, want %d (idempotent)", gotBal2, starting)
	}
}

func TestDeductForTaskWithAmount_InsufficientCredits(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newPricedCreditService(repo)
	userID := createCreditTestUser(t, repo, 100) // far less than the package

	_, err := svc.DeductForTaskWithAmount(context.Background(), userID, "ecommerce", "ecom-task-low", 5000)
	if !errors.Is(err, ErrInsufficientCredits) {
		t.Fatalf("expected ErrInsufficientCredits, got %v", err)
	}

	// Balance must be untouched on a failed deduction.
	got, _ := svc.GetBalance(context.Background(), userID)
	if got != 100 {
		t.Errorf("balance after failed deduct = %d, want 100", got)
	}
}

func TestDeductForTaskWithAmount_InvalidAmount(t *testing.T) {
	repo := setupCreditTestRepo(t)
	svc := newPricedCreditService(repo)
	userID := createCreditTestUser(t, repo, 1000)

	for _, amt := range []int{0, -5} {
		if _, err := svc.DeductForTaskWithAmount(context.Background(), userID, "ecommerce", "ecom-task-zero", amt); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("amount %d: expected ErrInvalidAmount, got %v", amt, err)
		}
	}
}
