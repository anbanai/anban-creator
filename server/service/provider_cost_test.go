package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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

func TestProviderCostCalculatorOutputPixelTiersAreExact(t *testing.T) {
	calculator := NewProviderCostCalculator(providerCostBundleWithPixels().Costs)
	for _, test := range []struct {
		name          string
		width, height int64
		wantPixels    int64
		wantMicroCNY  int64
		wantTier      int
	}{
		{name: "at bounded tier", width: 2_360, height: 1_000, wantPixels: 2_360_000, wantMicroCNY: 300_000, wantTier: 0},
		{name: "above bounded tier", width: 2_361, height: 1_000, wantPixels: 2_361_000, wantMicroCNY: 600_000, wantTier: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := calculator.OutputPixelCost("volcengine_ark/doubao-seedream-5-0-pro-260628", OutputPixelUsage{Width: test.width, Height: test.height})
			if err != nil {
				t.Fatalf("OutputPixelCost: %v", err)
			}
			if got.Pixels != test.wantPixels || got.MicroCNY != test.wantMicroCNY || got.SelectedTier.Index != test.wantTier {
				t.Fatalf("calculation = %#v", got)
			}
		})
	}
}

func TestProviderCostCalculatorOutputPixelRejectsInvalidAndOverflow(t *testing.T) {
	calculator := NewProviderCostCalculator(providerCostBundleWithPixels().Costs)
	for _, usage := range []OutputPixelUsage{{Width: 0, Height: 1}, {Width: -1, Height: 1}, {Width: int64(^uint64(0) >> 1), Height: 2}} {
		if _, err := calculator.OutputPixelCost("volcengine_ark/doubao-seedream-5-0-pro-260628", usage); err == nil {
			t.Fatalf("invalid usage %#v was accepted", usage)
		}
	}
	if _, err := calculator.OutputPixelCost("volcengine_ark/missing", OutputPixelUsage{Width: 1, Height: 1}); err == nil {
		t.Fatal("missing pixel price was accepted")
	}
}

func TestProviderCostFinalizeExecutionPrecomputesAllModelsBeforePersistence(t *testing.T) {
	fixture := newProviderCostFixture(t)
	_, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), FinalizeExecutionTokenCostsRequest{
		ExecutionID: "exec-batch", TaskID: "task-1", CatalogID: "cost-v1",
		Entries: []ExecutionTokenCostEntry{
			{Provider: "volcengine_ark", Model: "doubao-seed-evolving", IdempotencyKey: "exec-batch/evolving", Source: string(model.BillingProviderCostSourceClaudeResult), Usage: TokenUsage{Input: 1}},
			{Provider: "volcengine_ark", Model: "missing-model", IdempotencyKey: "exec-batch/missing", Source: string(model.BillingProviderCostSourceClaudeResult), Usage: TokenUsage{Input: 1}},
		},
	})
	if err == nil {
		t.Fatal("batch with missing second model price succeeded")
	}
	assertProviderCostExecutionEmpty(t, fixture, "exec-batch")
}

func TestProviderCostFinalizeExecutionRejectsDuplicateProviderModel(t *testing.T) {
	fixture := newProviderCostFixture(t)
	_, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), FinalizeExecutionTokenCostsRequest{
		ExecutionID: "exec-duplicate", TaskID: "task-1", CatalogID: "cost-v1",
		Entries: []ExecutionTokenCostEntry{
			{Provider: "volcengine_ark", Model: "doubao-seed-evolving", IdempotencyKey: "first", Source: string(model.BillingProviderCostSourceClaudeResult), Usage: TokenUsage{Input: 1}},
			{Provider: "volcengine_ark", Model: "doubao-seed-evolving", IdempotencyKey: "second", Source: string(model.BillingProviderCostSourceClaudeResult), Usage: TokenUsage{Input: 1}},
		},
	})
	if err == nil {
		t.Fatal("duplicate provider/model batch succeeded")
	}
	assertProviderCostExecutionEmpty(t, fixture, "exec-duplicate")
}

func TestProviderCostFinalizeExecutionRejectsMoreThan128Models(t *testing.T) {
	fixture := newProviderCostFixture(t)
	entries := make([]ExecutionTokenCostEntry, 129)
	for index := range entries {
		entries[index] = ExecutionTokenCostEntry{
			Provider: "provider", Model: fmt.Sprintf("model-%d", index), IdempotencyKey: fmt.Sprintf("key-%d", index),
			Source: string(model.BillingProviderCostSourceClaudeResult), Usage: TokenUsage{Input: 1},
		}
	}
	_, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), FinalizeExecutionTokenCostsRequest{
		ExecutionID: "exec-too-many", TaskID: "task-1", CatalogID: "cost-v1", Entries: entries,
	})
	if err == nil || !strings.Contains(err.Error(), "128") {
		t.Fatalf("129-entry batch error = %v, want limit error", err)
	}
	assertProviderCostExecutionEmpty(t, fixture, "exec-too-many")
}

func TestProviderCostFinalizeExecutionRecordsAllModelsThenReconcilesOnce(t *testing.T) {
	fixture := newProviderCostFixtureWithBundle(t, providerCostBundleWithTurbo())
	req := executionTokenBatchRequest("exec-batch")
	first, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), req)
	if err != nil {
		t.Fatalf("FinalizeExecutionTokenCosts: %v", err)
	}
	if len(first) != 2 || first[0].Model == first[1].Model {
		t.Fatalf("events = %#v", first)
	}
	status, err := fixture.costRepo.FindExecutionCostStatus(context.Background(), "exec-batch")
	if err != nil || status.Status != model.BillingProviderCostStatusReconciled {
		t.Fatalf("status = %#v, %v", status, err)
	}
	replayed, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), req)
	if err != nil {
		t.Fatalf("batch replay: %v", err)
	}
	if len(replayed) != 2 || replayed[0].ID != first[0].ID || replayed[1].ID != first[1].ID {
		t.Fatalf("batch replay = %#v, want original IDs", replayed)
	}
}

func TestProviderCostFinalizeExecutionRejectsSupersetAfterSubset(t *testing.T) {
	fixture := newProviderCostFixtureWithBundle(t, providerCostBundleWithTurbo())
	full := executionTokenBatchRequest("exec-set-drift")
	subset := full
	subset.Entries = append([]ExecutionTokenCostEntry(nil), full.Entries[:1]...)
	if _, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), subset); err != nil {
		t.Fatalf("finalize subset: %v", err)
	}
	if _, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), full); !errors.Is(err, repository.ErrProviderCostConflict) {
		t.Fatalf("superset error = %v, want conflict", err)
	}
	events, err := fixture.costRepo.ListEventsByExecution(context.Background(), full.ExecutionID)
	if err != nil || len(events) != 1 || events[0].Model != subset.Entries[0].Model {
		t.Fatalf("events after superset rollback = %#v, %v", events, err)
	}
}

func TestProviderCostFinalizeExecutionRejectsSubsetAfterSuperset(t *testing.T) {
	fixture := newProviderCostFixtureWithBundle(t, providerCostBundleWithTurbo())
	full := executionTokenBatchRequest("exec-reverse-set-drift")
	if _, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), full); err != nil {
		t.Fatalf("finalize superset: %v", err)
	}
	subset := full
	subset.Entries = append([]ExecutionTokenCostEntry(nil), full.Entries[:1]...)
	if _, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), subset); !errors.Is(err, repository.ErrProviderCostConflict) {
		t.Fatalf("subset error = %v, want conflict", err)
	}
}

func TestProviderCostFinalizeExecutionEntryOrderIsSemanticNoop(t *testing.T) {
	fixture := newProviderCostFixtureWithBundle(t, providerCostBundleWithTurbo())
	req := executionTokenBatchRequest("exec-order-independent")
	first, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), req)
	if err != nil {
		t.Fatalf("first finalize: %v", err)
	}
	req.Entries[0], req.Entries[1] = req.Entries[1], req.Entries[0]
	replayed, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), req)
	if err != nil {
		t.Fatalf("reversed replay: %v", err)
	}
	if len(replayed) != 2 || replayed[0].ID != first[0].ID || replayed[1].ID != first[1].ID {
		t.Fatalf("reversed replay order/IDs = %#v, want canonical %#v", replayed, first)
	}
}

func TestProviderCostFinalizeExecutionConcurrentDifferentSetsConflict(t *testing.T) {
	fixture := newProviderCostConcurrentFixture(t, providerCostBundleWithTurbo())
	full := executionTokenBatchRequest("exec-concurrent-set")
	subset := full
	subset.Entries = append([]ExecutionTokenCostEntry(nil), full.Entries[:1]...)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, req := range []FinalizeExecutionTokenCostsRequest{subset, full} {
		req := req
		go func() {
			<-start
			_, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), req)
			results <- err
		}()
	}
	close(start)
	firstErr, secondErr := <-results, <-results
	successes, conflicts := 0, 0
	for _, err := range []error{firstErr, secondErr} {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, repository.ErrProviderCostConflict):
			conflicts++
		default:
			t.Fatalf("concurrent different-set error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent results success/conflict = %d/%d", successes, conflicts)
	}
	events, err := fixture.costRepo.ListEventsByExecution(context.Background(), full.ExecutionID)
	if err != nil || (len(events) != 1 && len(events) != 2) {
		t.Fatalf("winning event set = %#v, %v", events, err)
	}
}

func TestProviderCostFinalizeExecutionConcurrentReplay(t *testing.T) {
	fixture := newProviderCostFixtureWithBundle(t, providerCostBundleWithTurbo())
	req := executionTokenBatchRequest("exec-concurrent")
	const workers = 4
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fixture.service.FinalizeExecutionTokenCosts(context.Background(), req)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent replay: %v", err)
		}
	}
	events, err := fixture.costRepo.ListEventsByExecution(context.Background(), "exec-concurrent")
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %#v, %v", events, err)
	}
}

func TestProviderCostRecordOutputPixelsUsesRequestIdentityWithoutExecutionStatus(t *testing.T) {
	fixture := newProviderCostFixtureWithBundle(t, providerCostBundleWithPixels())
	req := RecordOutputPixelCostRequest{
		TaskID: "task-1", Provider: "volcengine_ark", Model: "doubao-seedream-5-0-pro-260628",
		ProviderRequestID: "image-request-1", CatalogID: "cost-v1", IdempotencyKey: "image-request-1",
		Width: 2_360, Height: 1_000, Source: string(model.BillingProviderCostSourceProviderResponse),
	}
	first, err := fixture.service.RecordOutputPixelCost(context.Background(), req)
	if err != nil {
		t.Fatalf("RecordOutputPixelCost: %v", err)
	}
	if first.ExecutionID != "" || first.ProviderRequestID != req.ProviderRequestID || first.CostMicroCNY != 300_000 {
		t.Fatalf("pixel event = %#v", first)
	}
	var evidence map[string]any
	if err := json.Unmarshal(first.UsageEvidence, &evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if len(evidence) != 4 || evidence["kind"] != "output_pixels" || evidence["width"] != float64(2_360) || evidence["height"] != float64(1_000) || evidence["pixels"] != float64(2_360_000) {
		t.Fatalf("unsafe or incomplete pixel evidence = %#v", evidence)
	}
	if _, err := fixture.costRepo.FindExecutionCostStatus(context.Background(), "image-request-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("request event execution status error = %v, want not found", err)
	}
	replayed, err := fixture.service.RecordOutputPixelCost(context.Background(), req)
	if err != nil || replayed.ID != first.ID {
		t.Fatalf("pixel replay = %#v, %v", replayed, err)
	}
	req.Width++
	if _, err := fixture.service.RecordOutputPixelCost(context.Background(), req); !errors.Is(err, repository.ErrProviderCostConflict) {
		t.Fatalf("pixel drift error = %v, want conflict", err)
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
	if err := fixture.service.MarkExecutionUnreconciled(ctx, "exec-1", model.BillingExecutionCostReasonMissingTerminalModelUsage); err != nil {
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
	if status.Status != model.BillingProviderCostStatusUnreconciled || status.ReasonCode != model.BillingExecutionCostReasonMissingTerminalModelUsage {
		t.Fatalf("execution cost status = %#v", status)
	}
}

func TestProviderCostExecutionCanReconcileAfterMissingUsage(t *testing.T) {
	fixture := newProviderCostFixture(t)
	ctx := context.Background()
	if err := fixture.service.MarkExecutionUnreconciled(ctx, "exec-1", model.BillingExecutionCostReasonMissingTerminalModelUsage); err != nil {
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
	if status.Status != model.BillingProviderCostStatusReconciled || status.ReasonCode != "" {
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
	if err := fixture.service.MarkExecutionUnreconciled(ctx, "exec-1", model.BillingExecutionCostReasonMissingProviderUsage); err != nil {
		t.Fatalf("late MarkExecutionUnreconciled: %v", err)
	}
	status, err := fixture.costRepo.FindExecutionCostStatus(ctx, "exec-1")
	if err != nil {
		t.Fatalf("find status: %v", err)
	}
	if status.Status != model.BillingProviderCostStatusReconciled || status.ReasonCode != "" {
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
		ReasonCode: model.BillingProviderCostAdjustmentReasonProviderInvoiceReconciliation,
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
	return newProviderCostFixtureWithBundle(t, providerCostBundle())
}

func newProviderCostFixtureWithBundle(t *testing.T, bundle *billing.Bundle) providerCostFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("database pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	account := model.BillingWalletAccount{UserID: "user-1", PaidCredits: 7_000, PromotionalCredits: 800, DebtCredits: 300, Version: 9}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	costRepo := repository.NewBillingCostRepository(db)
	return providerCostFixture{db: db, costRepo: costRepo, service: NewProviderCostService(costRepo, bundle)}
}

func newProviderCostConcurrentFixture(t *testing.T, bundle *billing.Bundle) providerCostFixture {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "provider-cost.db") + "?_busy_timeout=5000&_journal_mode=WAL&_txlock=immediate"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open concurrent database: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("database pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	costRepo := repository.NewBillingCostRepository(db)
	return providerCostFixture{db: db, costRepo: costRepo, service: NewProviderCostService(costRepo, bundle)}
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

func providerCostBundleWithTurbo() *billing.Bundle {
	bundle := providerCostBundle()
	bundle.Costs.Models["volcengine_ark/doubao-seed-2-1-turbo-260628"] = billing.ModelCostConfig{
		PricingType: "token", Currency: "CNY", Unit: 1_000_000,
		Input: 3_000_000, CacheReadInput: 600_000, CacheCreationInput: 3_000_000, Output: 15_000_000,
		OperatorEvidence: "test-turbo-price", EffectiveAt: time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC),
	}
	return bundle
}

func providerCostBundleWithPixels() *billing.Bundle {
	bundle := providerCostBundle()
	bundle.Costs.Models["volcengine_ark/doubao-seedream-5-0-pro-260628"] = billing.ModelCostConfig{
		PricingType: "output_pixel_tier", Currency: "CNY",
		Tiers:            []billing.CostTier{{MaxPixels: 2_360_000, Price: 300_000}, {Price: 600_000}},
		OperatorEvidence: "test-pixel-price", EffectiveAt: time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC),
	}
	return bundle
}

func executionTokenBatchRequest(executionID string) FinalizeExecutionTokenCostsRequest {
	return FinalizeExecutionTokenCostsRequest{
		ExecutionID: executionID, TaskID: "task-1", CatalogID: "cost-v1",
		Entries: []ExecutionTokenCostEntry{
			{Provider: "volcengine_ark", Model: "doubao-seed-evolving", IdempotencyKey: executionID + "/evolving", Source: string(model.BillingProviderCostSourceClaudeResult), Usage: TokenUsage{Input: 4_807, CacheRead: 7_792, Output: 198}},
			{Provider: "volcengine_ark", Model: "doubao-seed-2-1-turbo-260628", IdempotencyKey: executionID + "/turbo", Source: string(model.BillingProviderCostSourceClaudeResult), Usage: TokenUsage{Input: 9, Output: 2}},
		},
	}
}

func assertProviderCostExecutionEmpty(t *testing.T, fixture providerCostFixture, executionID string) {
	t.Helper()
	events, err := fixture.costRepo.ListEventsByExecution(context.Background(), executionID)
	if err != nil || len(events) != 0 {
		t.Fatalf("events = %#v, %v, want empty", events, err)
	}
	if _, err := fixture.costRepo.FindExecutionCostStatus(context.Background(), executionID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("execution status error = %v, want not found", err)
	}
}
