package repository

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
	"reflect"
	"regexp"
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

func TestBillingRepositoryEnsureAccountRequiresTransactionAndIsIdempotent(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	if err := repo.Billing().EnsureAccount(ctx, "u-ensure"); !errors.Is(err, ErrBillingRequiresTransaction) {
		t.Fatalf("root EnsureAccount error = %v", err)
	}
	if err := repo.WithTx(ctx, func(tx Repository) error {
		if err := tx.Billing().EnsureAccount(ctx, "u-ensure"); err != nil {
			return err
		}
		if err := tx.Billing().EnsureAccount(ctx, "u-ensure"); err != nil {
			return err
		}
		account, err := tx.Billing().LockAccount(ctx, "u-ensure")
		if err != nil || account.UserID != "u-ensure" {
			t.Fatalf("ensured account = %+v, %v", account, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBillingRepositoryLockingSQLContracts(t *testing.T) {
	db, logs := openBillingMySQLDryRunDB(t)
	repo := newTxBillingRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	_, _ = repo.LockAccount(ctx, "user-1")
	_, _ = repo.LockLotByID(ctx, "lot-1")
	_, _ = repo.ListSpendableLots(ctx, "user-1", string(model.BillingCreditLotKindPromotional), now)
	_, _ = repo.ListSpendableLots(ctx, "user-1", string(model.BillingCreditLotKindPaid), now)
	_, _ = repo.ListExpiredPromotionalLotsForUpdate(ctx, now, 25)
	_, _ = repo.LockQuote(ctx, "quote-1")

	sql := logs.String()
	for _, table := range []string{"billing_wallet_accounts", "billing_credit_lots", "billing_quotes"} {
		if !strings.Contains(sql, "FROM `"+table+"`") {
			t.Errorf("locking SQL does not query %s:\n%s", table, sql)
		}
	}
	if got := strings.Count(sql, "FOR UPDATE"); got != 6 {
		t.Fatalf("FOR UPDATE count = %d, want 6:\n%s", got, sql)
	}
	if !strings.Contains(sql, "ORDER BY expires_at ASC, created_at ASC, id ASC") {
		t.Errorf("promotional lot order missing:\n%s", sql)
	}
	if !strings.Contains(sql, "ORDER BY created_at ASC, id ASC") {
		t.Errorf("paid lot order missing:\n%s", sql)
	}
	if !strings.Contains(sql, "kind = 'promotional' AND available_credits > 0 AND expires_at IS NOT NULL AND expires_at <=") ||
		!strings.Contains(sql, "ORDER BY expires_at ASC, created_at ASC, id ASC LIMIT 25 FOR UPDATE") {
		t.Errorf("expired promotional lot locking contract missing:\n%s", sql)
	}
}

func TestBillingRepositoryListSpendableLotsFiltersAndOrders(t *testing.T) {
	repo := New(setupTestDB(t))
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
		if err := repo.Billing().CreateLot(ctx, &lots[i]); err != nil {
			t.Fatalf("CreateLot(%s): %v", lots[i].ID, err)
		}
	}

	if err := repo.WithTx(ctx, func(txRepo Repository) error {
		promotional, err := txRepo.Billing().ListSpendableLots(ctx, "u1", string(model.BillingCreditLotKindPromotional), now)
		if err != nil {
			return err
		}
		if got, want := billingLotIDs(promotional), []string{"promo-id-a", "promo-id-b", "promo-late"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("promotional IDs = %v, want %v", got, want)
		}
		paid, err := txRepo.Billing().ListSpendableLots(ctx, "u1", string(model.BillingCreditLotKindPaid), now)
		if err != nil {
			return err
		}
		if got, want := billingLotIDs(paid), []string{"paid-id-a", "paid-id-b", "paid-later"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("paid IDs = %v, want %v", got, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBillingRepositoryRootRejectsLocksAndChargeWrites(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	for name, call := range map[string]func() error{
		"LockAccount": func() error {
			_, err := repo.Billing().LockAccount(ctx, "u1")
			return err
		},
		"LockLotByID": func() error {
			_, err := repo.Billing().LockLotByID(ctx, "lot-1")
			return err
		},
		"ListSpendableLots": func() error {
			_, err := repo.Billing().ListSpendableLots(ctx, "u1", string(model.BillingCreditLotKindPaid), now)
			return err
		},
		"LockQuote": func() error {
			_, err := repo.Billing().LockQuote(ctx, "quote-1")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			requireBillingTransactionError(t, call())
		})
	}

	charge := billingTaskCharge("root-charge", "root-task", "root-key")
	err := repo.Billing().CreateCharge(ctx, charge, []model.BillingChargeAllocation{{ID: "root-allocation", ChargeID: charge.ID, LotID: "root-lot", Credits: 100}})
	requireBillingTransactionError(t, err)
	if _, err := repo.Billing().FindChargeByID(ctx, charge.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("root CreateCharge persisted data: %v", err)
	}
}

func TestBillingRepositoryRootMySQLRejectsLocksAndChargeWrites(t *testing.T) {
	db, logs := openBillingMySQLDryRunDB(t)
	repo := newBillingRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	for name, call := range map[string]func() error{
		"LockAccount": func() error {
			_, err := repo.LockAccount(ctx, "u1")
			return err
		},
		"LockLotByID": func() error {
			_, err := repo.LockLotByID(ctx, "lot-1")
			return err
		},
		"ListSpendableLots": func() error {
			_, err := repo.ListSpendableLots(ctx, "u1", string(model.BillingCreditLotKindPaid), now)
			return err
		},
		"ListExpiredPromotionalLotsForUpdate": func() error {
			_, err := repo.ListExpiredPromotionalLotsForUpdate(ctx, now, 10)
			return err
		},
		"LockQuote": func() error {
			_, err := repo.LockQuote(ctx, "quote-1")
			return err
		},
		"CreateCharge": func() error {
			return repo.CreateCharge(ctx, billingTaskCharge("root-mysql-charge", "root-mysql-task", "root-mysql-key"), nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			requireBillingTransactionError(t, call())
		})
	}
	if logs.Len() != 0 {
		t.Fatalf("root transaction-required operations issued MySQL SQL:\n%s", logs.String())
	}
}

func TestBillingRepositoryLockMethodsUseOuterTransactionAndRollback(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	account := &model.BillingWalletAccount{UserID: "u1", PaidCredits: 10}
	lot := billingLot("paid-lot-lock", "u1", model.BillingCreditLotKindPaid, 10, now.Add(-time.Hour), nil)
	quote := &model.BillingQuote{
		ID: "quote-lock", UserID: "u1", CatalogID: "retail-v1", SKUID: "task.seednote.v1",
		PriceCredits: 10, RequestFingerprint: strings.Repeat("e", 64), SKUSnapshot: datatypes.JSON(`{}`),
		ExpiresAt: now.Add(time.Hour), IdempotencyScope: "quote-lock", IdempotencyKey: "quote-lock",
	}
	if err := repo.Billing().CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateLot(ctx, &lot); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateQuote(ctx, quote); err != nil {
		t.Fatal(err)
	}

	errRollback := errors.New("rollback lock transaction")
	err := repo.WithTx(ctx, func(txRepo Repository) error {
		if _, err := txRepo.Billing().LockAccount(ctx, account.UserID); err != nil {
			return err
		}
		lockedLot, err := txRepo.Billing().LockLotByID(ctx, lot.ID)
		if err != nil {
			return err
		}
		if _, err := txRepo.Billing().ListSpendableLots(ctx, account.UserID, string(model.BillingCreditLotKindPaid), now); err != nil {
			return err
		}
		if _, err := txRepo.Billing().LockQuote(ctx, quote.ID); err != nil {
			return err
		}
		lockedLot.AvailableCredits = 0
		lockedLot.ConsumedCredits = lockedLot.OriginalCredits
		if err := txRepo.Billing().UpdateLot(ctx, lockedLot); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("WithTx error = %v", err)
	}
	found, err := repo.Billing().FindLotBySource(ctx, lot.SourceType, lot.SourceID)
	if err != nil || found.AvailableCredits != 10 || found.ConsumedCredits != 0 {
		t.Fatalf("lot after outer rollback = %+v, %v", found, err)
	}
}

func TestBillingRepositoryExpiredPromotionalLotsForUpdate(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	created := now.Add(-time.Hour)
	before := now.Add(-time.Second)
	future := now.Add(time.Second)
	lots := []model.BillingCreditLot{
		billingLot("expired-before", "u1", model.BillingCreditLotKindPromotional, 10, created, &before),
		billingLot("expired-at-b", "u1", model.BillingCreditLotKindPromotional, 10, created, &now),
		billingLot("expired-at-a", "u1", model.BillingCreditLotKindPromotional, 10, created, &now),
		billingLot("future", "u1", model.BillingCreditLotKindPromotional, 10, created, &future),
		billingLot("paid", "u1", model.BillingCreditLotKindPaid, 10, created, nil),
		billingLot("exhausted", "u1", model.BillingCreditLotKindPromotional, 0, created, &before),
	}
	for i := range lots {
		if err := repo.Billing().CreateLot(ctx, &lots[i]); err != nil {
			t.Fatalf("CreateLot(%s): %v", lots[i].ID, err)
		}
	}

	_, err := repo.Billing().ListExpiredPromotionalLotsForUpdate(ctx, now, 2)
	requireBillingTransactionError(t, err)
	errRollback := errors.New("rollback expiry")
	err = repo.WithTx(ctx, func(txRepo Repository) error {
		expired, err := txRepo.Billing().ListExpiredPromotionalLotsForUpdate(ctx, now, 2)
		if err != nil {
			return err
		}
		if got, want := billingLotIDs(expired), []string{"expired-before", "expired-at-a"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("expired lot IDs = %v, want %v", got, want)
		}
		expired[0].AvailableCredits = 0
		expired[0].ExpiredCredits = expired[0].OriginalCredits
		if err := txRepo.Billing().UpdateLot(ctx, &expired[0]); err != nil {
			return err
		}
		again, err := txRepo.Billing().ListExpiredPromotionalLotsForUpdate(ctx, now, 2)
		if err != nil {
			return err
		}
		if got, want := billingLotIDs(again), []string{"expired-at-a", "expired-at-b"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("expired lots after mutation = %v, want %v", got, want)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("expiry transaction error = %v", err)
	}
	rolledBack, err := repo.Billing().FindLotBySource(ctx, "test", "expired-before")
	if err != nil || rolledBack.AvailableCredits != 10 || rolledBack.ExpiredCredits != 0 {
		t.Fatalf("rolled back expired lot = %+v, %v", rolledBack, err)
	}
}

func TestBillingRepositoryExpiredPromotionalLotsUseCallerMySQLTransaction(t *testing.T) {
	db, mock := openBillingMySQLMockDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	errRollback := errors.New("rollback expired lots")

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `billing_credit_lots` WHERE .*kind = .*available_credits > 0.*expires_at IS NOT NULL.*expires_at <= .*ORDER BY expires_at ASC, created_at ASC, id ASC LIMIT .* FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "kind", "original_credits", "available_credits", "consumed_credits", "expired_credits", "expires_at", "created_at"}).
			AddRow("expired-mysql", "u1", "promotional", 10, 10, 0, 0, now.Add(-time.Minute), now.Add(-time.Hour)))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `billing_credit_lots` SET `available_credits`=?,`consumed_credits`=?,`expired_credits`=? WHERE id = ?")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	err := repo.WithTx(ctx, func(txRepo Repository) error {
		lots, err := txRepo.Billing().ListExpiredPromotionalLotsForUpdate(ctx, now, 1)
		if err != nil {
			return err
		}
		if len(lots) != 1 || lots[0].ID != "expired-mysql" {
			t.Fatalf("expired MySQL lots = %+v", lots)
		}
		lots[0].AvailableCredits = 0
		lots[0].ExpiredCredits = lots[0].OriginalCredits
		if err := txRepo.Billing().UpdateLot(ctx, &lots[0]); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("WithTx error = %v, want rollback sentinel", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expired lot MySQL transaction expectations: %v", err)
	}
}

func TestBillingRepositoryAccountUpdatesUseMonotonicVersionCAS(t *testing.T) {
	repo := New(setupTestDB(t)).Billing()
	ctx := context.Background()
	account := &model.BillingWalletAccount{UserID: "account-cas", PaidCredits: 100, Version: 3}
	if err := repo.CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}

	updated := *account
	updated.PaidCredits = 80
	updated.Version = 4
	if err := repo.UpdateAccount(ctx, &updated, 3); err != nil {
		t.Fatalf("monotonic update: %v", err)
	}
	found, err := repo.FindAccount(ctx, account.UserID)
	if err != nil || found.PaidCredits != 80 || found.Version != 4 {
		t.Fatalf("updated account = %+v, %v", found, err)
	}

	stale := updated
	stale.PaidCredits = 70
	stale.Version = 4
	if err := repo.UpdateAccount(ctx, &stale, 3); !errors.Is(err, ErrBillingVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	missing := &model.BillingWalletAccount{UserID: "missing-account", Version: 1}
	if err := repo.UpdateAccount(ctx, missing, 0); !errors.Is(err, ErrBillingNotFound) {
		t.Fatalf("missing update error = %v", err)
	}

	for name, version := range map[string]int64{"rollback": 4, "jump": 6} {
		t.Run(name, func(t *testing.T) {
			invalid := *found
			invalid.Version = version
			if err := repo.UpdateAccount(ctx, &invalid, 4); !errors.Is(err, ErrBillingVersionConflict) {
				t.Fatalf("invalid version update error = %v", err)
			}
		})
	}
	afterInvalid, err := repo.FindAccount(ctx, account.UserID)
	if err != nil || afterInvalid.PaidCredits != 80 || afterInvalid.Version != 4 {
		t.Fatalf("account changed after invalid CAS = %+v, %v", afterInvalid, err)
	}
}

func TestBillingRepositoryUpdatesRejectMissingRows(t *testing.T) {
	repo := New(setupTestDB(t)).Billing()
	ctx := context.Background()
	if err := repo.UpdateLot(ctx, &model.BillingCreditLot{ID: "missing-lot"}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing lot update error = %v", err)
	}
	if err := repo.UpdateReferralIssue(ctx, &model.BillingReferralIssue{ID: "missing-referral"}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing referral update error = %v", err)
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
		ID:                 "entry-1",
		UserID:             "u1",
		EventKind:          model.BillingWalletEventKindTopUp,
		PaidDelta:          1000,
		CatalogID:          "retail-v1",
		RequestFingerprint: strings.Repeat("a", 64),
		SourceType:         &sourceType,
		SourceID:           &sourceID,
		IdempotencyScope:   "top-up",
		IdempotencyKey:     "key-1",
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
	rootRepo := New(setupTestDB(t))
	repo := rootRepo.Billing()
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
	listed, err := repo.ListSKUsByCatalog(ctx, catalog.CatalogID)
	if err != nil || len(listed) != len(skus) {
		t.Fatalf("ListSKUsByCatalog = %+v, %v", listed, err)
	}
	for i := range listed {
		if listed[i].SKUID != skus[i].SKUID {
			t.Fatalf("listed SKU %d = %+v, want %+v", i, listed[i], skus[i])
		}
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
	if err := rootRepo.WithTx(ctx, func(txRepo Repository) error {
		locked, err := txRepo.Billing().LockQuote(ctx, quote.ID)
		if err != nil || locked.ConsumedAt == nil || locked.ResourceID != "task-1" {
			t.Fatalf("LockQuote after consume = %+v, %v", locked, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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
	promotionalLot := &model.BillingCreditLot{
		ID: "promotional-lot", UserID: "u1", Kind: model.BillingCreditLotKindPromotional,
		ProgramID: "program-v1", SourceType: "promotion", SourceID: "reward-1", CatalogID: "retail-v1",
		OriginalCredits: 40, ConsumedCredits: 40, ExpiresAt: ptrTime(time.Now().Add(time.Hour)),
	}
	paidLot := &model.BillingCreditLot{
		ID: "paid-lot", UserID: "u1", Kind: model.BillingCreditLotKindPaid,
		SourceType: "topup", SourceID: "payment-1", CatalogID: "retail-v1",
		OriginalCredits: 60, ConsumedCredits: 60,
	}
	for _, lot := range []*model.BillingCreditLot{promotionalLot, paidLot} {
		if err := repo.Billing().CreateLot(ctx, lot); err != nil {
			t.Fatalf("CreateLot(%s): %v", lot.ID, err)
		}
	}
	charge := billingTaskCharge("charge-allocated", "task-allocated", "allocated-key")
	charge.PaidCredits = 60
	charge.PromotionalCredits = 40
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
	var allocated int64
	for _, allocation := range found {
		allocated += allocation.Credits
	}
	if allocated != charge.PaidCredits+charge.PromotionalCredits {
		t.Fatalf("allocated credits = %d, charge lot-backed credits = %d", allocated, charge.PaidCredits+charge.PromotionalCredits)
	}
	if err := repo.WithTx(ctx, func(txRepo Repository) error {
		for _, allocation := range found {
			lockedLot, err := txRepo.Billing().LockLotByID(ctx, allocation.LotID)
			if err != nil {
				return err
			}
			if lockedLot.UserID != charge.UserID || lockedLot.AvailableCredits != 0 || lockedLot.ConsumedCredits != allocation.Credits {
				t.Fatalf("locked allocation lot = %+v for allocation %+v", lockedLot, allocation)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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
	rootRepo := New(db)
	repo := rootRepo.Billing()
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	due1 := billingSettlement("settlement-1", "key-1", "pending", nil, now.Add(-2*time.Minute))
	due2 := billingSettlement("settlement-2", "key-2", "retry", ptrTime(now.Add(-time.Minute)), now.Add(-time.Minute))
	due2.LastError = "previous transient error"
	future := billingSettlement("settlement-future", "key-future", "retry", ptrTime(now.Add(time.Minute)), now)
	staleProcessing := billingSettlement("settlement-stale", "key-stale", "processing", nil, now.Add(-6*time.Minute))
	staleProcessing.Attempts = 1
	staleProcessing.UpdatedAt = now.Add(-6 * time.Minute)
	freshProcessing := billingSettlement("settlement-processing", "key-processing", "processing", nil, now.Add(-4*time.Minute))
	freshProcessing.UpdatedAt = now.Add(-4 * time.Minute)
	for _, row := range []*model.BillingSettlementOutbox{due2, future, due1, staleProcessing, freshProcessing} {
		if err := rootRepo.WithTx(ctx, func(tx Repository) error { return tx.Billing().EnqueueSettlement(ctx, row) }); err != nil {
			t.Fatalf("EnqueueSettlement(%s): %v", row.ID, err)
		}
	}
	if err := rootRepo.WithTx(ctx, func(tx Repository) error {
		return tx.Billing().EnqueueSettlement(ctx, billingSettlement("settlement-duplicate", "key-1", "pending", nil, now))
	}); err == nil {
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
		wantLastError := ""
		if row.ID == due2.ID {
			wantLastError = "previous transient error"
		}
		if row.Status != "processing" || row.Attempts != wantAttempts || row.NextAttemptAt != nil || row.LastError != wantLastError {
			t.Errorf("claimed row = %+v", row)
		}
	}
	var claimedRetry model.BillingSettlementOutbox
	if err := db.First(&claimedRetry, "id = ?", due2.ID).Error; err != nil {
		t.Fatal(err)
	}
	if claimedRetry.NextAttemptAt != nil || claimedRetry.LastError != "previous transient error" {
		t.Fatalf("claimed retry processing state = %+v", claimedRetry)
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

func TestBillingRepositoryEnqueueSettlementRequiresCallerTransaction(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	row := billingSettlement("settlement-tx-only", "key-tx-only", "pending", nil, time.Now().UTC())
	if err := repo.Billing().EnqueueSettlement(ctx, row); !errors.Is(err, ErrBillingRequiresTransaction) {
		t.Fatalf("root EnqueueSettlement error = %v", err)
	}
	if _, err := repo.Billing().FindSettlementByKey(ctx, row.IdempotencyScope, row.IdempotencyKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("root enqueue emitted SQL or persisted row: %v", err)
	}
	if err := repo.WithTx(ctx, func(tx Repository) error { return tx.Billing().EnqueueSettlement(ctx, row) }); err != nil {
		t.Fatalf("transactional enqueue: %v", err)
	}
}

func TestBillingRepositoryCurrentReadsRequireCallerTransaction(t *testing.T) {
	repo := New(setupTestDB(t)).Billing()
	ctx := context.Background()
	tests := []struct {
		name string
		call func() error
	}{
		{name: "charge key", call: func() error { _, err := repo.LockChargeByKey(ctx, "scope", "key"); return err }},
		{name: "task charge", call: func() error { _, err := repo.LockChargeByTask(ctx, "task"); return err }},
		{name: "operation charge", call: func() error {
			_, err := repo.LockChargeByOperation(ctx, "task", "attempt", "call", "catalog", "sku")
			return err
		}},
		{name: "quote charge", call: func() error { _, err := repo.LockChargeByQuote(ctx, "quote"); return err }},
		{name: "reversal", call: func() error { _, err := repo.LockReversal(ctx, "charge"); return err }},
		{name: "entry key", call: func() error { _, err := repo.LockEntryByKey(ctx, "scope", "key"); return err }},
		{name: "entry source", call: func() error { _, err := repo.LockEntryBySource(ctx, "source", "id"); return err }},
		{name: "debt allocations", call: func() error {
			_, err := repo.LockDebtAllocationsBySourceEntryID(ctx, "user", "entry")
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, ErrBillingRequiresTransaction) {
				t.Fatalf("current read error = %v", err)
			}
		})
	}
}

func TestBillingRepositoryMySQLCurrentReadsUseForUpdate(t *testing.T) {
	db, logs := openBillingMySQLDryRunDB(t)
	repo := newTxBillingRepository(db)
	ctx := context.Background()
	tests := []struct {
		name string
		call func()
	}{
		{name: "charge key", call: func() { _, _ = repo.LockChargeByKey(ctx, "scope", "key") }},
		{name: "task charge", call: func() { _, _ = repo.LockChargeByTask(ctx, "task") }},
		{name: "operation charge", call: func() { _, _ = repo.LockChargeByOperation(ctx, "task", "attempt", "call", "catalog", "sku") }},
		{name: "quote charge", call: func() { _, _ = repo.LockChargeByQuote(ctx, "quote") }},
		{name: "reversal", call: func() { _, _ = repo.LockReversal(ctx, "charge") }},
		{name: "entry key", call: func() { _, _ = repo.LockEntryByKey(ctx, "scope", "key") }},
		{name: "entry source", call: func() { _, _ = repo.LockEntryBySource(ctx, "source", "id") }},
		{name: "debt allocations", call: func() { _, _ = repo.LockDebtAllocationsBySourceEntryID(ctx, "user", "entry") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.Reset()
			tt.call()
			if sql := logs.String(); !strings.Contains(sql, "FOR UPDATE") {
				t.Fatalf("current read SQL missing FOR UPDATE:\n%s", sql)
			}
		})
	}
}

func TestBillingRepositoryMySQLCurrentReadsUseCallerTransactionWithoutSavepoint(t *testing.T) {
	db, mock := openBillingMySQLMockDB(t)
	repo := New(db)
	ctx := context.Background()
	rollbackErr := errors.New("rollback current reads")

	mock.ExpectBegin()
	for index := 0; index < 5; index++ {
		mock.ExpectQuery("SELECT .* FROM `billing_charges` .* FOR UPDATE").
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
	}
	for index := 0; index < 2; index++ {
		mock.ExpectQuery("SELECT .* FROM `billing_wallet_entries` .* FOR UPDATE").
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
	}
	mock.ExpectQuery("SELECT .* FROM `billing_debt_allocations` .* FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	err := repo.WithTx(ctx, func(tx Repository) error {
		billingRepo := tx.Billing()
		_, _ = billingRepo.LockChargeByKey(ctx, "scope", "key")
		_, _ = billingRepo.LockChargeByTask(ctx, "task")
		_, _ = billingRepo.LockChargeByOperation(ctx, "task", "attempt", "call", "catalog", "sku")
		_, _ = billingRepo.LockChargeByQuote(ctx, "quote")
		_, _ = billingRepo.LockReversal(ctx, "charge")
		_, _ = billingRepo.LockEntryByKey(ctx, "scope", "key")
		_, _ = billingRepo.LockEntryBySource(ctx, "source", "id")
		_, _ = billingRepo.LockDebtAllocationsBySourceEntryID(ctx, "user", "entry")
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("caller transaction error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("current read transaction expectations: %v", err)
	}
}

func TestBillingRepositoryDebtAllocationsProvideIndexedOutstandingBalances(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	chargeA := billingOperationCharge("debt-charge-a", "task-a", "attempt-a", "call-a", "key-a")
	chargeA.PaidCredits, chargeA.DebtCredits, chargeA.PriceCredits, chargeA.CreatedAt = 0, 100, 100, now
	chargeB := billingOperationCharge("debt-charge-b", "task-b", "attempt-b", "call-b", "key-b")
	chargeB.PaidCredits, chargeB.DebtCredits, chargeB.PriceCredits, chargeB.CreatedAt = 0, 200, 200, now.Add(time.Second)
	if err := repo.WithTx(ctx, func(tx Repository) error {
		if err := tx.Billing().CreateCharge(ctx, chargeA, nil); err != nil {
			return err
		}
		if err := tx.Billing().CreateCharge(ctx, chargeB, nil); err != nil {
			return err
		}
		return tx.Billing().CreateDebtAllocation(ctx, &model.BillingDebtAllocation{
			ID: "allocation-a", UserID: "u1", ChargeID: chargeA.ID, EntryID: "repayment-entry-a",
			SourceEntryID: "topup-entry", Kind: model.BillingDebtAllocationKindRepayment, Credits: 40, CreatedAt: now.Add(2 * time.Second),
		})
	}); err != nil {
		t.Fatal(err)
	}
	outstanding, err := repo.Billing().ListOutstandingDebtCharges(ctx, "u1")
	if err != nil || len(outstanding) != 2 || outstanding[0].ChargeID != chargeA.ID || outstanding[0].OutstandingCredits != 60 ||
		outstanding[1].ChargeID != chargeB.ID || outstanding[1].OutstandingCredits != 200 {
		t.Fatalf("outstanding debt = %+v, %v", outstanding, err)
	}
	amount, err := repo.Billing().GetOutstandingDebtForCharge(ctx, "u1", chargeA.ID)
	if err != nil || amount != 60 {
		t.Fatalf("GetOutstandingDebtForCharge = %d, %v", amount, err)
	}
	allocations, err := repo.Billing().ListDebtAllocationsBySourceEntryID(ctx, "u1", "topup-entry")
	if err != nil || len(allocations) != 1 || allocations[0].Credits != 40 {
		t.Fatalf("ListDebtAllocationsBySourceEntryID = %+v, %v", allocations, err)
	}
	sum, err := repo.Billing().SumDebtAllocationsBySourceEntryID(ctx, "u1", "topup-entry")
	if err != nil || sum != 40 {
		t.Fatalf("SumDebtAllocationsBySourceEntryID = %d, %v", sum, err)
	}
	if err := repo.Billing().CreateDebtAllocation(ctx, &model.BillingDebtAllocation{}); !errors.Is(err, ErrBillingRequiresTransaction) {
		t.Fatalf("root CreateDebtAllocation error = %v", err)
	}
}

func TestBillingRepositorySettlementOutboxFailedStateIsTerminalAndFenced(t *testing.T) {
	db := setupTestDB(t)
	rootRepo := New(db)
	repo := rootRepo.Billing()
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	row := billingSettlement("settlement-failed", "key-failed", "pending", nil, now.Add(-time.Minute))
	if err := rootRepo.WithTx(ctx, func(tx Repository) error { return tx.Billing().EnqueueSettlement(ctx, row) }); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimSettlements(ctx, now, 1)
	if err != nil || len(claimed) != 1 || claimed[0].Attempts != 1 {
		t.Fatalf("claim = %+v, %v", claimed, err)
	}
	failedAt := now.Add(time.Second)
	if err := repo.MarkSettlementFailed(ctx, row.ID, 0, failedAt, "permanent failure"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("stale failure mark error = %v, want fenced not found", err)
	}
	if err := repo.MarkSettlementFailed(ctx, row.ID, 1, failedAt, "permanent failure"); err != nil {
		t.Fatalf("MarkSettlementFailed: %v", err)
	}
	var failed model.BillingSettlementOutbox
	if err := db.First(&failed, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" || failed.FailedAt == nil || !failed.FailedAt.Equal(failedAt) ||
		failed.LastError != "permanent failure" || failed.NextAttemptAt != nil || failed.ProcessedAt != nil {
		t.Fatalf("failed state = %+v", failed)
	}
	claimed, err = repo.ClaimSettlements(ctx, now.Add(24*time.Hour), 1)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("failed settlement reclaimed = %+v, %v", claimed, err)
	}
}

func TestBillingRepositorySettlementOutboxConcurrentClaimsDoNotDuplicate(t *testing.T) {
	db := setupBillingConcurrentSQLiteDB(t)
	rootRepo := New(db)
	repo := rootRepo.Billing()
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if err := rootRepo.WithTx(ctx, func(tx Repository) error {
		return tx.Billing().EnqueueSettlement(ctx, billingSettlement("settlement-only", "only-key", "pending", nil, now))
	}); err != nil {
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
	for _, fragment := range []string{
		"FROM `billing_settlement_outbox`",
		"status IN ('pending','retry')",
		"next_attempt_at IS NULL OR next_attempt_at <=",
		"status = 'processing' AND updated_at <=",
		"ORDER BY created_at ASC, id ASC LIMIT 10 FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("MySQL settlement claim SQL missing %q:\n%s", fragment, sql)
		}
	}
}

func TestBillingRepositoryMySQLClaimUsesCallerTransactionWithoutSavepoint(t *testing.T) {
	db, mock := openBillingMySQLMockDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	errRollback := errors.New("rollback outer transaction")

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `billing_settlement_outbox` WHERE .*status IN .*next_attempt_at IS NULL OR next_attempt_at <= .*status = .*updated_at <= .*ORDER BY created_at ASC, id ASC LIMIT .* FOR UPDATE SKIP LOCKED").
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "attempts", "last_error", "created_at", "updated_at"}).
			AddRow("settlement-mysql", "retry", 0, "previous transient error", now.Add(-time.Minute), now.Add(-time.Minute)))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `billing_settlement_outbox` SET `attempts`=attempts + 1,`next_attempt_at`=?,`processed_at`=?,`status`=?,`updated_at`=? WHERE id = ? AND ((status IN (?,?) AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND updated_at <= ?))")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `billing_settlement_outbox` SET `last_error`=?,`next_attempt_at`=?,`processed_at`=?,`status`=?,`updated_at`=? WHERE id = ? AND status = ? AND attempts = ?")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	err := repo.WithTx(ctx, func(txRepo Repository) error {
		claimed, err := txRepo.Billing().ClaimSettlements(ctx, now, 1)
		if err != nil {
			return err
		}
		if len(claimed) != 1 || claimed[0].ID != "settlement-mysql" || claimed[0].Attempts != 1 || claimed[0].LastError != "previous transient error" {
			t.Fatalf("claimed rows = %+v", claimed)
		}
		if err := txRepo.Billing().MarkSettlementProcessed(ctx, claimed[0].ID, claimed[0].Attempts, now); err != nil {
			return err
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
	inviteeLotID, inviterLotID := "invitee-lot-1", "inviter-lot-1"
	issuedAt := time.Now().UTC()
	issue := &model.BillingReferralIssue{
		ID: "referral-1", ProgramID: "program-v1", CatalogID: "promotion-v1",
		InviteeUserID: "invitee-1", InviterUserID: "inviter-1", QualifyingTopUpEntryID: "topup-entry-1",
		RequestFingerprint: strings.Repeat("a", 64), InviteeLotID: &inviteeLotID, InviterLotID: &inviterLotID,
		Status: model.BillingReferralStatusIssued, IssuedAt: &issuedAt,
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

func requireBillingTransactionError(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrBillingRequiresTransaction) {
		t.Fatalf("error = %v, want caller transaction requirement", err)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
