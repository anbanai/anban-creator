package repository

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBillingRepositoryAccessorParity(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	if repo.Billing() == nil {
		t.Fatal("root Billing() should not be nil")
	}

	ctx := context.Background()
	errRollback := errors.New("rollback")
	err := repo.WithTx(ctx, func(txRepo Repository) error {
		if txRepo.Billing() == nil {
			t.Fatal("transaction Billing() should not be nil")
		}
		root, rootOK := repo.Billing().(*billingRepository)
		tx, txOK := txRepo.Billing().(*billingRepository)
		if !rootOK || !txOK {
			t.Fatalf("billing repository implementations = %T and %T", repo.Billing(), txRepo.Billing())
		}
		if root.db == tx.db {
			t.Fatal("transaction Billing() is not bound to the transaction DB")
		}
		if err := txRepo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: "tx-wallet"}); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("WithTx error = %v, want rollback sentinel", err)
	}
	if _, err := repo.Billing().FindAccount(ctx, "tx-wallet"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("transaction-scoped billing write persisted after rollback: %v", err)
	}

	if err := repo.WithTx(ctx, func(txRepo Repository) error {
		return txRepo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: "committed-wallet"})
	}); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}
	if _, err := repo.Billing().FindAccount(ctx, "committed-wallet"); err != nil {
		t.Fatalf("transaction-scoped billing write missing after commit: %v", err)
	}
}

func TestBillingRepositoryLockingSQLContracts(t *testing.T) {
	db, logs := openBillingMySQLDryRunDB(t)
	repo := newBillingRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	_, _ = repo.LockAccount(ctx, "user-1")
	_, _ = repo.LockLotByID(ctx, "lot-1")
	_, _ = repo.ListSpendableLots(ctx, "user-1", string(model.BillingCreditLotKindPromotional), now)
	_, _ = repo.ListSpendableLots(ctx, "user-1", string(model.BillingCreditLotKindPaid), now)
	_, _ = repo.LockQuote(ctx, "quote-1")

	sql := logs.String()
	for _, table := range []string{"billing_wallet_accounts", "billing_credit_lots", "billing_quotes"} {
		if !strings.Contains(sql, "FROM `"+table+"`") {
			t.Errorf("locking SQL does not query %s:\n%s", table, sql)
		}
	}
	if got := strings.Count(sql, "FOR UPDATE"); got != 5 {
		t.Fatalf("FOR UPDATE count = %d, want 5:\n%s", got, sql)
	}
	if !strings.Contains(sql, "ORDER BY expires_at ASC, created_at ASC, id ASC") {
		t.Errorf("promotional lot order missing:\n%s", sql)
	}
	if !strings.Contains(sql, "ORDER BY created_at ASC, id ASC") {
		t.Errorf("paid lot order missing:\n%s", sql)
	}
}

func TestBillingRepositoryListSpendableLotsFiltersAndOrders(t *testing.T) {
	repo := New(setupTestDB(t)).Billing()
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	created := now.Add(-time.Hour)
	expiresSoon := now.Add(time.Hour)
	expiresLater := now.Add(2 * time.Hour)
	expired := now.Add(-time.Second)

	lots := []model.BillingCreditLot{
		billingLot("promo-late", "u1", model.BillingCreditLotKindPromotional, 20, created, &expiresLater),
		billingLot("promo-id-b", "u1", model.BillingCreditLotKindPromotional, 20, created, &expiresSoon),
		billingLot("promo-id-a", "u1", model.BillingCreditLotKindPromotional, 20, created, &expiresSoon),
		billingLot("promo-empty", "u1", model.BillingCreditLotKindPromotional, 0, created, &expiresSoon),
		billingLot("promo-expired", "u1", model.BillingCreditLotKindPromotional, 20, created, &expired),
		billingLot("promo-other-user", "u2", model.BillingCreditLotKindPromotional, 20, created, &expiresSoon),
		billingLot("paid-later", "u1", model.BillingCreditLotKindPaid, 10, created.Add(time.Minute), nil),
		billingLot("paid-id-b", "u1", model.BillingCreditLotKindPaid, 10, created, nil),
		billingLot("paid-id-a", "u1", model.BillingCreditLotKindPaid, 10, created, nil),
	}
	for i := range lots {
		if err := repo.CreateLot(ctx, &lots[i]); err != nil {
			t.Fatalf("CreateLot(%s): %v", lots[i].ID, err)
		}
	}

	promotional, err := repo.ListSpendableLots(ctx, "u1", string(model.BillingCreditLotKindPromotional), now)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := billingLotIDs(promotional), []string{"promo-id-a", "promo-id-b", "promo-late"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("promotional IDs = %v, want %v", got, want)
	}
	paid, err := repo.ListSpendableLots(ctx, "u1", string(model.BillingCreditLotKindPaid), now)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := billingLotIDs(paid), []string{"paid-id-a", "paid-id-b", "paid-later"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paid IDs = %v, want %v", got, want)
	}
}

func TestBillingRepositoryWalletEntriesAreAppendOnlyAndReplayable(t *testing.T) {
	contract := reflect.TypeOf((*BillingRepository)(nil)).Elem()
	for _, forbidden := range []string{"UpdateEntry", "DeleteEntry"} {
		if _, ok := contract.MethodByName(forbidden); ok {
			t.Errorf("append-only repository exposes %s", forbidden)
		}
	}

	repo := New(setupTestDB(t)).Billing()
	ctx := context.Background()
	sourceType, sourceID := "wechatpay", "payment-1"
	entry := &model.BillingWalletEntry{
		ID:               "entry-1",
		UserID:           "u1",
		EventKind:        model.BillingWalletEventKindTopUp,
		PaidDelta:        1000,
		SourceType:       &sourceType,
		SourceID:         &sourceID,
		IdempotencyScope: "top-up",
		IdempotencyKey:   "key-1",
	}
	if err := repo.AppendEntry(ctx, entry); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}
	bySource, err := repo.FindEntryBySource(ctx, sourceType, sourceID)
	if err != nil || bySource.ID != entry.ID {
		t.Fatalf("FindEntryBySource = %+v, %v", bySource, err)
	}
	byKey, err := repo.FindEntryByKey(ctx, "top-up", "key-1")
	if err != nil || byKey.ID != entry.ID {
		t.Fatalf("FindEntryByKey = %+v, %v", byKey, err)
	}
	duplicate := *entry
	duplicate.ID = "entry-2"
	duplicate.IdempotencyKey = "key-2"
	if err := repo.AppendEntry(ctx, &duplicate); err == nil {
		t.Fatal("duplicate external top-up identity unexpectedly succeeded")
	}
	entries, err := repo.ListEntriesByUser(ctx, "u1", 0, 10)
	if err != nil || len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("ListEntriesByUser = %+v, %v", entries, err)
	}
}

func TestBillingRepositoryCatalogSKUAndQuotePersistence(t *testing.T) {
	repo := New(setupTestDB(t)).Billing()
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	catalog := &model.BillingCatalogVersion{CatalogID: "retail-v1", Currency: "credits", Status: "published", PublishedAt: now, Snapshot: datatypes.JSON(`{"id":"retail-v1"}`)}
	if err := repo.CreateCatalogVersion(ctx, catalog); err != nil {
		t.Fatalf("CreateCatalogVersion: %v", err)
	}
	skus := []model.BillingSKU{
		{ID: "sku-row-1", CatalogID: catalog.CatalogID, SKUID: "task.seednote.v1", Operation: "task.create", PriceCredits: 5000, Policy: "task_admission", Route: "seednote", Delivery: "task", Snapshot: datatypes.JSON(`{}`)},
		{ID: "sku-row-2", CatalogID: catalog.CatalogID, SKUID: "image.seedream.v1", Operation: "image.generate", PriceCredits: 1000, Policy: "accepted_task_operation", Route: "seedream", Delivery: "image", Snapshot: datatypes.JSON(`{}`)},
		{ID: "sku-row-3", CatalogID: catalog.CatalogID, SKUID: "image.default.v1", Operation: "image.generate", PriceCredits: 800, Policy: "accepted_task_operation", Route: "", Delivery: "image", Snapshot: datatypes.JSON(`{}`)},
	}
	if err := repo.CreateSKUs(ctx, skus); err != nil {
		t.Fatalf("CreateSKUs: %v", err)
	}
	foundCatalog, err := repo.FindCatalogVersion(ctx, catalog.CatalogID)
	if err != nil || foundCatalog.CatalogID != catalog.CatalogID {
		t.Fatalf("FindCatalogVersion = %+v, %v", foundCatalog, err)
	}
	latest, err := repo.FindLatestPublishedCatalog(ctx)
	if err != nil || latest.CatalogID != catalog.CatalogID {
		t.Fatalf("FindLatestPublishedCatalog = %+v, %v", latest, err)
	}
	sku, err := repo.FindSKU(ctx, catalog.CatalogID, "task.seednote.v1")
	if err != nil || sku.PriceCredits != 5000 {
		t.Fatalf("FindSKU = %+v, %v", sku, err)
	}
	byOperation, err := repo.FindSKUByOperation(ctx, catalog.CatalogID, "image.generate", "seedream")
	if err != nil || byOperation.SKUID != "image.seedream.v1" {
		t.Fatalf("FindSKUByOperation = %+v, %v", byOperation, err)
	}
	defaultRoute, err := repo.FindSKUByOperation(ctx, catalog.CatalogID, "image.generate", "")
	if err != nil || defaultRoute.SKUID != "image.default.v1" {
		t.Fatalf("FindSKUByOperation(empty route) = %+v, %v", defaultRoute, err)
	}

	quote := &model.BillingQuote{
		ID: "quote-1", UserID: "u1", CatalogID: catalog.CatalogID, SKUID: sku.SKUID,
		PriceCredits: 5000, RequestFingerprint: strings.Repeat("a", 64), SKUSnapshot: datatypes.JSON(`{}`),
		ExpiresAt: now.Add(time.Minute), IdempotencyScope: "quote", IdempotencyKey: "quote-key-1",
	}
	if err := repo.CreateQuote(ctx, quote); err != nil {
		t.Fatalf("CreateQuote: %v", err)
	}
	byKey, err := repo.FindQuoteByKey(ctx, "quote", "quote-key-1")
	if err != nil || byKey.ID != quote.ID {
		t.Fatalf("FindQuoteByKey = %+v, %v", byKey, err)
	}
	consumedAt := now.Add(time.Second)
	changed, err := repo.MarkQuoteConsumed(ctx, quote.ID, consumedAt, "task", "task-1")
	if err != nil || !changed {
		t.Fatalf("MarkQuoteConsumed first = %v, %v", changed, err)
	}
	changed, err = repo.MarkQuoteConsumed(ctx, quote.ID, consumedAt, "task", "task-2")
	if err != nil || changed {
		t.Fatalf("MarkQuoteConsumed replay = %v, %v", changed, err)
	}
	locked, err := repo.LockQuote(ctx, quote.ID)
	if err != nil || locked.ConsumedAt == nil || locked.ResourceID != "task-1" {
		t.Fatalf("LockQuote after consume = %+v, %v", locked, err)
	}
}

func TestBillingRepositoryCreateChargeUsesCallerTransactionAtomically(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	charge := billingTaskCharge("charge-atomic", "task-atomic", "atomic-key")
	allocations := []model.BillingChargeAllocation{
		{ID: "alloc-1", ChargeID: charge.ID, LotID: "lot-1", Credits: 10},
		{ID: "alloc-2", ChargeID: charge.ID, LotID: "lot-1", Credits: 10},
	}
	err := repo.WithTx(ctx, func(txRepo Repository) error {
		return txRepo.Billing().CreateCharge(ctx, charge, allocations)
	})
	if err == nil {
		t.Fatal("duplicate allocation unexpectedly succeeded")
	}
	if _, err := repo.Billing().FindChargeByID(ctx, charge.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("charge survived failed allocation insert: %v", err)
	}
}

func TestBillingRepositoryChargeAllocationsSupportExactReversal(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	exhaustedLot := &model.BillingCreditLot{
		ID: "promotional-lot", UserID: "u1", Kind: model.BillingCreditLotKindPromotional,
		ProgramID: "program-v1", SourceType: "promotion", SourceID: "reward-1", CatalogID: "retail-v1",
		OriginalCredits: 40, ConsumedCredits: 40, ExpiresAt: ptrTime(time.Now().Add(time.Hour)),
	}
	if err := repo.Billing().CreateLot(ctx, exhaustedLot); err != nil {
		t.Fatalf("CreateLot: %v", err)
	}
	charge := billingTaskCharge("charge-allocated", "task-allocated", "allocated-key")
	allocations := []model.BillingChargeAllocation{
		{ID: "allocation-b", ChargeID: charge.ID, LotID: "paid-lot", Credits: 60},
		{ID: "allocation-a", ChargeID: charge.ID, LotID: "promotional-lot", Credits: 40},
	}
	if err := repo.WithTx(ctx, func(txRepo Repository) error {
		return txRepo.Billing().CreateCharge(ctx, charge, allocations)
	}); err != nil {
		t.Fatalf("CreateCharge: %v", err)
	}

	found, err := repo.Billing().ListChargeAllocations(ctx, charge.ID)
	if err != nil {
		t.Fatalf("ListChargeAllocations: %v", err)
	}
	if got, want := []string{found[0].ID, found[1].ID}, []string{"allocation-a", "allocation-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allocation IDs = %v, want %v", got, want)
	}
	lockedLot, err := repo.Billing().LockLotByID(ctx, exhaustedLot.ID)
	if err != nil || lockedLot.AvailableCredits != 0 || lockedLot.ConsumedCredits != 40 {
		t.Fatalf("LockLotByID(exhausted) = %+v, %v", lockedLot, err)
	}
}

func TestBillingRepositoryChargeReplayAndDatabaseIdentities(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	create := func(charge *model.BillingCharge) error {
		return repo.WithTx(ctx, func(txRepo Repository) error {
			return txRepo.Billing().CreateCharge(ctx, charge, nil)
		})
	}

	taskCharge := billingTaskCharge("charge-task", "task-1", "task-key-1")
	if err := create(taskCharge); err != nil {
		t.Fatalf("create task charge: %v", err)
	}
	byKey, err := repo.Billing().FindChargeByKey(ctx, taskCharge.IdempotencyScope, taskCharge.IdempotencyKey)
	if err != nil || byKey.RequestFingerprint != taskCharge.RequestFingerprint {
		t.Fatalf("FindChargeByKey = %+v, %v", byKey, err)
	}
	byTask, err := repo.Billing().FindChargeByTask(ctx, "task-1")
	if err != nil || byTask.ID != taskCharge.ID {
		t.Fatalf("FindChargeByTask = %+v, %v", byTask, err)
	}
	keyConflict := billingTaskCharge("charge-key-conflict", "task-2", taskCharge.IdempotencyKey)
	if err := create(keyConflict); err == nil {
		t.Fatal("duplicate charge idempotency key unexpectedly succeeded")
	}
	taskConflict := billingTaskCharge("charge-task-conflict", "task-1", "task-key-2")
	if err := create(taskConflict); err == nil {
		t.Fatal("duplicate task charge identity unexpectedly succeeded")
	}

	operation := billingOperationCharge("charge-operation", "task-op", "attempt-1", "call-1", "op-key-1")
	if err := create(operation); err != nil {
		t.Fatalf("create operation charge: %v", err)
	}
	byOperation, err := repo.Billing().FindChargeByOperation(ctx, "task-op", "attempt-1", "call-1", operation.CatalogID, operation.SKUID)
	if err != nil || byOperation.ID != operation.ID {
		t.Fatalf("FindChargeByOperation = %+v, %v", byOperation, err)
	}
	operationConflict := billingOperationCharge("charge-operation-conflict", "task-op", "attempt-1", "call-1", "op-key-2")
	if err := create(operationConflict); err == nil {
		t.Fatal("duplicate accepted-operation identity unexpectedly succeeded")
	}

	reversal := billingReversalCharge("charge-reversal", taskCharge.ID, "reversal-key-1")
	if err := create(reversal); err != nil {
		t.Fatalf("create reversal: %v", err)
	}
	foundReversal, err := repo.Billing().FindReversal(ctx, taskCharge.ID)
	if err != nil || foundReversal.ID != reversal.ID {
		t.Fatalf("FindReversal = %+v, %v", foundReversal, err)
	}
	reversalConflict := billingReversalCharge("charge-reversal-conflict", taskCharge.ID, "reversal-key-2")
	if err := create(reversalConflict); err == nil {
		t.Fatal("duplicate reversal unexpectedly succeeded")
	}
}

func TestBillingRepositorySettlementOutboxClaimRetryAndProcessedState(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db).Billing()
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	due1 := billingSettlement("settlement-1", "key-1", "pending", nil, now.Add(-2*time.Minute))
	due2 := billingSettlement("settlement-2", "key-2", "retry", ptrTime(now.Add(-time.Minute)), now.Add(-time.Minute))
	future := billingSettlement("settlement-future", "key-future", "retry", ptrTime(now.Add(time.Minute)), now)
	staleProcessing := billingSettlement("settlement-stale", "key-stale", "processing", nil, now.Add(-6*time.Minute))
	staleProcessing.Attempts = 1
	staleProcessing.UpdatedAt = now.Add(-6 * time.Minute)
	freshProcessing := billingSettlement("settlement-processing", "key-processing", "processing", nil, now.Add(-4*time.Minute))
	freshProcessing.UpdatedAt = now.Add(-4 * time.Minute)
	for _, row := range []*model.BillingSettlementOutbox{due2, future, due1, staleProcessing, freshProcessing} {
		if err := repo.EnqueueSettlement(ctx, row); err != nil {
			t.Fatalf("EnqueueSettlement(%s): %v", row.ID, err)
		}
	}
	if err := repo.EnqueueSettlement(ctx, billingSettlement("settlement-duplicate", "key-1", "pending", nil, now)); err == nil {
		t.Fatal("duplicate settlement idempotency key unexpectedly succeeded")
	}
	found, err := repo.FindSettlementByKey(ctx, "settlement", "key-1")
	if err != nil || found.ID != due1.ID {
		t.Fatalf("FindSettlementByKey = %+v, %v", found, err)
	}

	claimed, err := repo.ClaimSettlements(ctx, now, 10)
	if err != nil {
		t.Fatalf("ClaimSettlements: %v", err)
	}
	if got, want := settlementIDs(claimed), []string{"settlement-stale", "settlement-1", "settlement-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claimed IDs = %v, want %v", got, want)
	}
	for _, row := range claimed {
		wantAttempts := 1
		if row.ID == staleProcessing.ID {
			wantAttempts = 2
		}
		if row.Status != "processing" || row.Attempts != wantAttempts || row.NextAttemptAt != nil || row.LastError != "" {
			t.Errorf("claimed row = %+v", row)
		}
	}
	var claimedRetry model.BillingSettlementOutbox
	if err := db.First(&claimedRetry, "id = ?", due2.ID).Error; err != nil {
		t.Fatal(err)
	}
	if claimedRetry.NextAttemptAt != nil {
		t.Fatalf("claimed retry retained stale next_attempt_at: %+v", claimedRetry)
	}

	retryAt := now.Add(5 * time.Minute)
	if err := repo.MarkSettlementRetry(ctx, due1.ID, 1, retryAt, "temporary failure"); err != nil {
		t.Fatalf("MarkSettlementRetry: %v", err)
	}
	processedAt := now.Add(time.Second)
	if err := repo.MarkSettlementProcessed(ctx, due2.ID, 1, processedAt); err != nil {
		t.Fatalf("MarkSettlementProcessed: %v", err)
	}
	if err := repo.MarkSettlementProcessed(ctx, staleProcessing.ID, 1, processedAt); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("stale claimant mark error = %v, want record not found", err)
	}
	if err := repo.MarkSettlementProcessed(ctx, staleProcessing.ID, 2, processedAt); err != nil {
		t.Fatalf("current claimant mark: %v", err)
	}
	var retried, processed model.BillingSettlementOutbox
	if err := db.First(&retried, "id = ?", due1.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retried.Status != "retry" || retried.Attempts != 1 || retried.NextAttemptAt == nil || !retried.NextAttemptAt.Equal(retryAt) || retried.LastError != "temporary failure" {
		t.Fatalf("retried state = %+v", retried)
	}
	if err := db.First(&processed, "id = ?", due2.ID).Error; err != nil {
		t.Fatal(err)
	}
	if processed.Status != "processed" || processed.ProcessedAt == nil || !processed.ProcessedAt.Equal(processedAt) || processed.NextAttemptAt != nil || processed.LastError != "" {
		t.Fatalf("processed state = %+v", processed)
	}
	claimed, err = repo.ClaimSettlements(ctx, now, 10)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("second claim = %+v, %v", claimed, err)
	}
}

func TestBillingRepositorySettlementOutboxConcurrentClaimsDoNotDuplicate(t *testing.T) {
	db := setupBillingConcurrentSQLiteDB(t)
	repo := New(db).Billing()
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if err := repo.EnqueueSettlement(ctx, billingSettlement("settlement-only", "only-key", "pending", nil, now)); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make([][]model.BillingSettlementOutbox, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = repo.ClaimSettlements(ctx, now, 1)
		}(i)
	}
	close(start)
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("claim errors = %v", errs)
	}
	claimed := append(results[0], results[1]...)
	if len(claimed) != 1 || claimed[0].ID != "settlement-only" {
		t.Fatalf("concurrent claims = %+v", results)
	}
}

func setupBillingConcurrentSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "billing.db") + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open concurrent SQLite database: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate concurrent SQLite database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(4)
	return db
}

func TestBillingRepositorySettlementOutboxMySQLUsesSkipLocked(t *testing.T) {
	db, logs := openBillingMySQLDryRunDB(t)
	_, _ = newTxBillingRepository(db).ClaimSettlements(context.Background(), time.Now(), 10)
	sql := logs.String()
	if !strings.Contains(sql, "FROM `billing_settlement_outbox`") || !strings.Contains(sql, "FOR UPDATE SKIP LOCKED") {
		t.Fatalf("MySQL settlement claim SQL contract invalid:\n%s", sql)
	}
}

func TestBillingRepositoryMySQLClaimUsesCallerTransactionWithoutSavepoint(t *testing.T) {
	db, mock := openBillingMySQLMockDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	errRollback := errors.New("rollback outer transaction")

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `billing_settlement_outbox` .*FOR UPDATE SKIP LOCKED").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "attempts", "created_at", "updated_at"}).
			AddRow("settlement-mysql", "pending", 0, now.Add(-time.Minute), now.Add(-time.Minute)))
	mock.ExpectExec("UPDATE `billing_settlement_outbox` SET .* WHERE id = .*").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	err := repo.WithTx(ctx, func(txRepo Repository) error {
		claimed, err := txRepo.Billing().ClaimSettlements(ctx, now, 1)
		if err != nil {
			return err
		}
		if len(claimed) != 1 || claimed[0].ID != "settlement-mysql" || claimed[0].Attempts != 1 {
			t.Fatalf("claimed rows = %+v", claimed)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("WithTx error = %v, want rollback sentinel", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("MySQL transaction expectations: %v", err)
	}
}

func TestBillingRepositoryRootMySQLClaimRequiresCallerTransaction(t *testing.T) {
	db, _ := openBillingMySQLDryRunDB(t)
	_, err := newBillingRepository(db).ClaimSettlements(context.Background(), time.Now(), 1)
	if err == nil || !strings.Contains(err.Error(), "caller transaction") {
		t.Fatalf("root MySQL claim error = %v, want caller transaction contract", err)
	}
}

func TestBillingRepositoryReferralUniquenessAndLookup(t *testing.T) {
	repo := New(setupTestDB(t)).Billing()
	ctx := context.Background()
	issue := &model.BillingReferralIssue{
		ID: "referral-1", ProgramID: "program-v1", CatalogID: "promotion-v1",
		InviteeUserID: "invitee-1", InviterUserID: "inviter-1", Status: "pending",
	}
	if err := repo.CreateReferralIssue(ctx, issue); err != nil {
		t.Fatalf("CreateReferralIssue: %v", err)
	}
	found, err := repo.FindReferralIssue(ctx, issue.InviteeUserID, issue.ProgramID)
	if err != nil || found.ID != issue.ID {
		t.Fatalf("FindReferralIssue = %+v, %v", found, err)
	}
	duplicate := *issue
	duplicate.ID = "referral-2"
	duplicate.InviterUserID = "inviter-2"
	if err := repo.CreateReferralIssue(ctx, &duplicate); err == nil {
		t.Fatal("duplicate invitee/program referral unexpectedly succeeded")
	}
	issuedAt := time.Now().UTC()
	issue.Status = "issued"
	issue.IssuedAt = &issuedAt
	if err := repo.UpdateReferralIssue(ctx, issue); err != nil {
		t.Fatalf("UpdateReferralIssue: %v", err)
	}
	count, err := repo.CountIssuedReferrals(ctx, "inviter-1", "program-v1")
	if err != nil || count != 1 {
		t.Fatalf("CountIssuedReferrals = %d, %v", count, err)
	}
}

func openBillingMySQLDryRunDB(t *testing.T) (*gorm.DB, *bytes.Buffer) {
	t.Helper()
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DryRun: true,
		Logger: logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, &logs
}

func openBillingMySQLMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func billingLot(id, userID string, kind model.BillingCreditLotKind, available int64, created time.Time, expires *time.Time) model.BillingCreditLot {
	lot := model.BillingCreditLot{
		ID: id, UserID: userID, Kind: kind, SourceType: "test", SourceID: id,
		CatalogID: "catalog-v1", OriginalCredits: available, AvailableCredits: available,
		ExpiresAt: expires, CreatedAt: created,
	}
	if kind == model.BillingCreditLotKindPromotional {
		lot.ProgramID = "program-v1"
	}
	return lot
}

func billingLotIDs(lots []model.BillingCreditLot) []string {
	ids := make([]string, len(lots))
	for i := range lots {
		ids[i] = lots[i].ID
	}
	return ids
}

func billingTaskCharge(id, taskID, key string) *model.BillingCharge {
	return &model.BillingCharge{
		ID: id, UserID: "u1", CatalogID: "retail-v1", SKUID: "task.seednote.v1",
		ResourceType: "task", ResourceID: taskID, Kind: model.BillingChargeKindTask,
		Policy: "task_admission", Status: model.BillingChargeStatusPosted,
		PriceCredits: 100, PaidCredits: 100, TaskID: &taskID,
		IdempotencyScope: "task-charge", IdempotencyKey: key,
		RequestFingerprint: strings.Repeat("a", 64),
	}
}

func billingOperationCharge(id, taskID, attemptID, toolCallID, key string) *model.BillingCharge {
	return &model.BillingCharge{
		ID: id, UserID: "u1", CatalogID: "retail-v1", SKUID: "image.seedream.v1",
		ResourceType: "image", ResourceID: uuid.NewString(), Kind: model.BillingChargeKindOperation,
		Policy: "accepted_task_operation", Status: model.BillingChargeStatusPosted,
		PriceCredits: 100, PaidCredits: 100, OperationTaskID: &taskID, AttemptID: &attemptID, ToolCallID: &toolCallID,
		IdempotencyScope: "operation-charge", IdempotencyKey: key,
		RequestFingerprint: strings.Repeat("b", 64),
	}
}

func billingReversalCharge(id, originalID, key string) *model.BillingCharge {
	return &model.BillingCharge{
		ID: id, UserID: "u1", CatalogID: "retail-v1", SKUID: "task.seednote.v1",
		ResourceType: "task", ResourceID: "task-1", Kind: model.BillingChargeKindReversal,
		Policy: "task_admission", Status: model.BillingChargeStatusPosted,
		PriceCredits: 100, PaidCredits: 100, ReversalOfID: &originalID,
		IdempotencyScope: "reversal", IdempotencyKey: key,
		RequestFingerprint: strings.Repeat("c", 64),
	}
}

func billingSettlement(id, key, status string, nextAttemptAt *time.Time, created time.Time) *model.BillingSettlementOutbox {
	return &model.BillingSettlementOutbox{
		ID: id, Action: model.BillingSettlementActionChargeOperation,
		ResourceType: "image", ResourceID: id, CatalogID: "retail-v1", SKUID: "image.seedream.v1",
		IdempotencyScope: "settlement", IdempotencyKey: key, Status: status,
		NextAttemptAt: nextAttemptAt, RequestFingerprint: strings.Repeat("d", 64), CreatedAt: created,
	}
}

func settlementIDs(rows []model.BillingSettlementOutbox) []string {
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	return ids
}

func ptrTime(value time.Time) *time.Time { return &value }
