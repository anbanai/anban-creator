package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProviderCostCalculatorUsesExactCategories(t *testing.T) {
	got, err := providerCostCalculator(t).TokenCost("volcengine_ark/doubao-seed-evolving", TokenUsage{
		Input: 4_807, CacheRead: 7_792, Output: 198,
	})
	if err != nil {
		t.Fatalf("TokenCost: %v", err)
	}
	if got.MicroCNY != 44_133 {
		t.Fatalf("microCNY = %d, want 44133", got.MicroCNY)
	}
	if got.CategoryNumerators.Input != "28842000000" || got.CategoryNumerators.CacheRead != "9350400000" || got.CategoryNumerators.Output != "5940000000" {
		t.Fatalf("category numerators = %#v", got.CategoryNumerators)
	}
}

func TestProviderCostCalculatorConvertsForeignCurrencyBeforeSingleCeiling(t *testing.T) {
	bundle := providerCostBundle()
	bundle.Costs.Models["moonshot/model"] = billing.ModelCostConfig{
		PricingType: "token", Currency: "USD", Unit: 1_000_000,
		Input: 950_000, CacheReadInput: 190_000, CacheCreationInput: 950_000, Output: 4_000_000,
		OperatorEvidence: "test-price-snapshot", EffectiveAt: time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC),
	}
	got, err := NewProviderCostCalculator(bundle.Costs).TokenCost("moonshot/model", TokenUsage{Input: 1})
	if err != nil {
		t.Fatalf("TokenCost: %v", err)
	}
	if got.MicroCNY != 7 || got.CNYConversionNumerator != "6840000000000" || got.CNYConversionDenominator != "1000000000000" {
		t.Fatalf("foreign currency calculation = %#v", got)
	}
}

func TestProviderCostCalculatorRejectsNegativeUsageAndCostOverflow(t *testing.T) {
	calculator := providerCostCalculator(t)
	if _, err := calculator.TokenCost("volcengine_ark/doubao-seed-evolving", TokenUsage{Input: -1}); err == nil {
		t.Fatal("negative token usage was accepted")
	}
	if _, err := calculator.TokenCost("volcengine_ark/doubao-seed-evolving", TokenUsage{Output: int64(^uint64(0) >> 1)}); err == nil {
		t.Fatal("overflowing micro-CNY cost was accepted")
	}
}

func TestProviderCostRecordTokenUsageIsIdempotentAndRejectsDrift(t *testing.T) {
	fixture := newProviderCostFixture(t)
	req := RecordTokenCostRequest{
		ExecutionID: "exec-1", TaskID: "task-1", Provider: "volcengine_ark", Model: "doubao-seed-evolving",
		CatalogID: "cost-v1", IdempotencyKey: "exec-1/evolving", Source: string(model.BillingProviderCostSourceClaudeResult),
		Usage: TokenUsage{Input: 4_807, CacheRead: 7_792, Output: 198},
	}
	first, err := fixture.service.RecordTokenUsage(context.Background(), req)
	if err != nil {
		t.Fatalf("RecordTokenUsage first: %v", err)
	}
	second, err := fixture.service.RecordTokenUsage(context.Background(), req)
	if err != nil {
		t.Fatalf("RecordTokenUsage replay: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("replay ID = %q, want %q", second.ID, first.ID)
	}
	req.Usage.Output++
	if _, err := fixture.service.RecordTokenUsage(context.Background(), req); !errors.Is(err, repository.ErrProviderCostConflict) {
		t.Fatalf("RecordTokenUsage drift error = %v, want ErrProviderCostConflict", err)
	}
}

func TestMissingClaudeResultIsUnreconciledAndDoesNotTouchWallet(t *testing.T) {
	fixture := newProviderCostFixture(t)
	ctx := context.Background()
	before := fixture.walletSnapshot(t, "user-1")
	if err := fixture.service.MarkExecutionUnreconciled(ctx, "exec-1", "missing_terminal_model_usage"); err != nil {
		t.Fatalf("MarkExecutionUnreconciled: %v", err)
	}
	after := fixture.walletSnapshot(t, "user-1")
	if after != before {
		t.Fatalf("wallet changed: before=%#v after=%#v", before, after)
	}
	status, err := fixture.costRepo.FindExecutionCostStatus(ctx, "exec-1")
	if err != nil {
		t.Fatalf("FindExecutionCostStatus: %v", err)
	}
	if status.Status != model.BillingProviderCostStatusUnreconciled || status.Reason != "missing_terminal_model_usage" {
		t.Fatalf("execution cost status = %#v", status)
	}
}

func TestProviderCostExecutionCanReconcileAfterMissingUsage(t *testing.T) {
	fixture := newProviderCostFixture(t)
	ctx := context.Background()
	if err := fixture.service.MarkExecutionUnreconciled(ctx, "exec-1", "missing_terminal_model_usage"); err != nil {
		t.Fatalf("mark unreconciled: %v", err)
	}
	_, err := fixture.service.RecordTokenUsage(ctx, RecordTokenCostRequest{
		ExecutionID: "exec-1", TaskID: "task-1", Provider: "volcengine_ark", Model: "doubao-seed-evolving",
		CatalogID: "cost-v1", IdempotencyKey: "exec-1/evolving", Source: string(model.BillingProviderCostSourceManualReconciliation),
		Usage: TokenUsage{Input: 1},
	})
	if err != nil {
		t.Fatalf("manual reconciliation: %v", err)
	}
	status, err := fixture.costRepo.FindExecutionCostStatus(ctx, "exec-1")
	if err != nil {
		t.Fatalf("find status: %v", err)
	}
	if status.Status != model.BillingProviderCostStatusReconciled || status.Reason != "" {
		t.Fatalf("execution cost status = %#v, want reconciled", status)
	}
}

func TestProviderCostReconciledExecutionCannotBeDowngradedByLateMissingUsage(t *testing.T) {
	fixture := newProviderCostFixture(t)
	ctx := context.Background()
	_, err := fixture.service.RecordTokenUsage(ctx, RecordTokenCostRequest{
		ExecutionID: "exec-1", TaskID: "task-1", Provider: "volcengine_ark", Model: "doubao-seed-evolving",
		CatalogID: "cost-v1", IdempotencyKey: "exec-1/evolving", Source: string(model.BillingProviderCostSourceClaudeResult),
		Usage: TokenUsage{Input: 1},
	})
	if err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if err := fixture.service.MarkExecutionUnreconciled(ctx, "exec-1", "late_missing_terminal_model_usage"); err != nil {
		t.Fatalf("late MarkExecutionUnreconciled: %v", err)
	}
	status, err := fixture.costRepo.FindExecutionCostStatus(ctx, "exec-1")
	if err != nil {
		t.Fatalf("find status: %v", err)
	}
	if status.Status != model.BillingProviderCostStatusReconciled || status.Reason != "" {
		t.Fatalf("late missing usage downgraded status: %#v", status)
	}
}

func TestProviderCostAppendInvoiceAdjustmentIsAppendOnly(t *testing.T) {
	fixture := newProviderCostFixture(t)
	ctx := context.Background()
	base, err := fixture.service.RecordTokenUsage(ctx, RecordTokenCostRequest{
		ExecutionID: "exec-1", TaskID: "task-1", Provider: "volcengine_ark", Model: "doubao-seed-evolving",
		CatalogID: "cost-v1", IdempotencyKey: "exec-1/evolving", Source: string(model.BillingProviderCostSourceClaudeResult),
		Usage: TokenUsage{Input: 4_807, CacheRead: 7_792, Output: 198},
	})
	if err != nil {
		t.Fatalf("record base: %v", err)
	}
	adjustment, err := fixture.service.AppendInvoiceAdjustment(ctx, AdjustmentRequest{
		OriginalEventID: base.ID, IdempotencyKey: "invoice-2026-07/line-1", CostDeltaMicroCNY: -133,
		Reason: "provider_invoice_reconciliation",
	})
	if err != nil {
		t.Fatalf("AppendInvoiceAdjustment: %v", err)
	}
	if adjustment.OriginalEventID == nil || *adjustment.OriginalEventID != base.ID || adjustment.CostMicroCNY != -133 {
		t.Fatalf("adjustment = %#v", adjustment)
	}
	persistedBase, err := fixture.costRepo.FindEventByID(ctx, base.ID)
	if err != nil {
		t.Fatalf("find base: %v", err)
	}
	if persistedBase.CostMicroCNY != 44_133 || persistedBase.OriginalEventID != nil {
		t.Fatalf("base was mutated: %#v", persistedBase)
	}
}

type providerCostFixture struct {
	db       *gorm.DB
	costRepo repository.BillingCostRepository
	service  *ProviderCostService
}

func newProviderCostFixture(t *testing.T) providerCostFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	account := model.BillingWalletAccount{UserID: "user-1", PaidCredits: 7_000, PromotionalCredits: 800, DebtCredits: 300, Version: 9}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	costRepo := repository.NewBillingCostRepository(db)
	return providerCostFixture{db: db, costRepo: costRepo, service: NewProviderCostService(costRepo, providerCostBundle())}
}

type walletCostSnapshot struct {
	Paid, Promotional, Debt, Version int64
}

func (f providerCostFixture) walletSnapshot(t *testing.T, userID string) walletCostSnapshot {
	t.Helper()
	var account model.BillingWalletAccount
	if err := f.db.First(&account, "user_id = ?", userID).Error; err != nil {
		t.Fatalf("read wallet: %v", err)
	}
	return walletCostSnapshot{account.PaidCredits, account.PromotionalCredits, account.DebtCredits, account.Version}
}

func providerCostCalculator(t *testing.T) *ProviderCostCalculator {
	t.Helper()
	return NewProviderCostCalculator(providerCostBundle().Costs)
}

func providerCostBundle() *billing.Bundle {
	return &billing.Bundle{Costs: billing.CostCatalog{
		CatalogID:     "cost-v1",
		CurrencyRates: map[string]billing.MicroCNY{"CNY": 1_000_000, "USD": 7_200_000},
		Models: map[string]billing.ModelCostConfig{
			"volcengine_ark/doubao-seed-evolving": {
				PricingType: "token", Currency: "CNY", Unit: 1_000_000,
				Input: 6_000_000, CacheReadInput: 1_200_000, CacheCreationInput: 6_000_000, Output: 30_000_000,
				OperatorEvidence: "test-price-snapshot", EffectiveAt: time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC),
			},
		},
	}}
}
