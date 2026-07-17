package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

func TestBillingWalletTaskAdmissionDebtInsufficientReplayAndConflict(t *testing.T) {
	t.Run("debt", func(t *testing.T) {
		f := newBillingWalletFixture(t, 500, 0, 1)
		quote := f.quote(t, "u1", "task.article", "", "task-debt")
		_, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-debt", "charge-debt"))
		if !errors.Is(err, ErrBillingDebtOutstanding) {
			t.Fatalf("ChargeTaskAdmission error = %v, want ErrBillingDebtOutstanding", err)
		}
	})

	t.Run("insufficient", func(t *testing.T) {
		f := newBillingWalletFixture(t, 499, 0, 0)
		quote := f.quote(t, "u1", "task.article", "", "task-insufficient")
		_, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-insufficient", "charge-insufficient"))
		if !errors.Is(err, ErrBillingInsufficientForTask) {
			t.Fatalf("ChargeTaskAdmission error = %v, want ErrBillingInsufficientForTask", err)
		}
	})

	t.Run("replay and conflict", func(t *testing.T) {
		f := newBillingWalletFixture(t, 500, 0, 0)
		quote := f.quote(t, "u1", "task.article", "", "task-replay")
		req := taskChargeRequest(quote, "task-replay", "charge-replay")
		first, err := f.wallet.ChargeTaskAdmission(context.Background(), req)
		if err != nil {
			t.Fatalf("first admission: %v", err)
		}
		second, err := f.wallet.ChargeTaskAdmission(context.Background(), req)
		if err != nil || second.ID != first.ID {
			t.Fatalf("exact replay = %+v, %v; want %s", second, err, first.ID)
		}
		conflict := req
		conflict.RequestFingerprint = billingFingerprint("different-task")
		if _, err := f.wallet.ChargeTaskAdmission(context.Background(), conflict); !errors.Is(err, ErrBillingConflict) {
			t.Fatalf("conflicting replay error = %v, want ErrBillingConflict", err)
		}
		account := f.account(t, "u1")
		if account.PaidCredits != 0 || account.DebtCredits != 0 {
			t.Fatalf("account after replay = %+v", account)
		}
	})
}

func TestAcceptedTaskOperationMayCreateDebt(t *testing.T) {
	f := newBillingWalletFixture(t, 100, 0, 0)
	req := acceptedOperationRequest("u1", "task-1", "attempt-1", "call-1", "operation-1")
	c, err := f.wallet.ChargeAcceptedOperation(context.Background(), req)
	if err != nil {
		t.Fatalf("ChargeAcceptedOperation: %v", err)
	}
	if c.PriceCredits != 500 || c.PaidCredits != 100 || c.PromotionalCredits != 0 || c.DebtCredits != 400 {
		t.Fatalf("charge = %+v", c)
	}
	if got := f.account(t, "u1"); got.PaidCredits != 0 || got.DebtCredits != 400 {
		t.Fatalf("account = %+v", got)
	}
	second, err := f.wallet.ChargeAcceptedOperation(context.Background(), req)
	if err != nil || second.ID != c.ID {
		t.Fatalf("operation replay = %+v, %v", second, err)
	}
	conflict := req
	conflict.RequestFingerprint = billingFingerprint("other-operation")
	if _, err := f.wallet.ChargeAcceptedOperation(context.Background(), conflict); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("operation conflict error = %v", err)
	}
}

func TestBillingWalletPromotionEligibilityAndExpiryBoundary(t *testing.T) {
	f := newBillingWalletFixture(t, 500, 0, 0)
	f.addPromotion(t, "promo-ineligible", 300, f.now.Add(time.Hour))
	f.wallet.promotionEligible = func(lot model.BillingCreditLot, sku model.BillingSKU) bool {
		return lot.SourceID != "promo-ineligible"
	}
	quote := f.quote(t, "u1", "task.article", "", "task-promo-ineligible")
	charge, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-promo-ineligible", "charge-promo-ineligible"))
	if err != nil || charge.PaidCredits != 500 || charge.PromotionalCredits != 0 {
		t.Fatalf("ineligible promotion charge = %+v, %v", charge, err)
	}

	f2 := newBillingWalletFixture(t, 0, 0, 0)
	f2.addPromotion(t, "promo-at-boundary", 500, f2.now)
	quote2 := f2.quote(t, "u1", "task.article", "", "task-promo-expired")
	if _, err := f2.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote2, "task-promo-expired", "charge-promo-expired")); !errors.Is(err, ErrBillingInsufficientForTask) {
		t.Fatalf("expiry boundary error = %v, want insufficient", err)
	}
}

func TestBillingWalletStandaloneOperationNeverOverdraws(t *testing.T) {
	f := newBillingWalletFixture(t, 499, 0, 0)
	quote := f.quote(t, "u1", "designer.generate_image", "image.designer", "standalone-1")
	req := OperationChargeRequest{
		UserID: "u1", QuoteID: quote.ID, CatalogID: quote.CatalogID, SKUID: quote.SKUID,
		ResourceType: "image", ResourceID: "image-1", RequestFingerprint: quote.RequestFingerprint,
		IdempotencyScope: "standalone-charge", IdempotencyKey: "standalone-1",
	}
	if _, err := f.wallet.ChargeStandaloneOperation(context.Background(), req); !errors.Is(err, ErrBillingInsufficientForStandaloneOperation) {
		t.Fatalf("standalone error = %v, want insufficient", err)
	}
	if got := f.account(t, "u1"); got.PaidCredits != 499 || got.DebtCredits != 0 {
		t.Fatalf("standalone changed wallet: %+v", got)
	}
}

func TestTopUpRepaysDebtBeforePaidBalance(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 400)
	result, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: "u1", Credits: 1000, ExternalSourceType: "payment", ExternalSourceID: "api-1",
		IdempotencyScope: "topup", IdempotencyKey: "topup-1",
	})
	if err != nil || result.DebtRepaid != 400 || result.PaidAdded != 600 {
		t.Fatalf("TopUp = %+v, %v", result, err)
	}
	if got := f.account(t, "u1"); got.DebtCredits != 0 || got.PaidCredits != 600 {
		t.Fatalf("account = %+v", got)
	}
	replay, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: "u1", Credits: 1000, ExternalSourceType: "payment", ExternalSourceID: "api-1",
		IdempotencyScope: "topup", IdempotencyKey: "topup-1",
	})
	if err != nil || replay.EntryID != result.EntryID {
		t.Fatalf("topup replay = %+v, %v", replay, err)
	}
	if _, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: "u1", Credits: 999, ExternalSourceType: "payment", ExternalSourceID: "api-1",
		IdempotencyScope: "topup", IdempotencyKey: "topup-1",
	}); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("topup conflict error = %v", err)
	}
	if _, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: "u1", Credits: 999, ExternalSourceType: "payment", ExternalSourceID: "api-1",
		IdempotencyScope: "topup", IdempotencyKey: "different-key-same-source",
	}); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("external source conflict error = %v, want ErrBillingConflict", err)
	}
}

func TestBillingWalletPromotionCannotRepayDebt(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 400)
	f.addPromotion(t, "promo-debt", 1000, f.now.Add(time.Hour))
	if got := f.account(t, "u1"); got.DebtCredits != 400 || got.PromotionalCredits != 1000 {
		t.Fatalf("promotion repaid debt: %+v", got)
	}
	result, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: "u1", Credits: 100, ExternalSourceType: "payment", ExternalSourceID: "small",
		IdempotencyScope: "topup", IdempotencyKey: "small",
	})
	if err != nil || result.DebtRepaid != 100 || result.PaidAdded != 0 {
		t.Fatalf("small topup = %+v, %v", result, err)
	}
	if _, err := f.repo.Billing().FindLotBySource(context.Background(), "payment", "small"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("pure debt repayment lot lookup error = %v, want not found", err)
	}
}

func TestBillingWalletExactReversalRefundsRepaidDebtAsPaid(t *testing.T) {
	f := newBillingWalletFixture(t, 100, 0, 0)
	original, err := f.wallet.ChargeAcceptedOperation(context.Background(), acceptedOperationRequest("u1", "task-r", "attempt-r", "call-r", "operation-r"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: "u1", Credits: 400, ExternalSourceType: "payment", ExternalSourceID: "repay-r",
		IdempotencyScope: "topup", IdempotencyKey: "repay-r",
	}); err != nil {
		t.Fatal(err)
	}
	reversal, err := f.wallet.Reverse(context.Background(), original.ID, "provider_error", "reverse-r")
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if reversal.ReversalOfID == nil || *reversal.ReversalOfID != original.ID || reversal.PaidCredits != 100 || reversal.DebtCredits != 400 {
		t.Fatalf("reversal snapshot = %+v", reversal)
	}
	if got := f.account(t, "u1"); got.PaidCredits != 500 || got.DebtCredits != 0 {
		t.Fatalf("account after reversal = %+v", got)
	}
	replay, err := f.wallet.Reverse(context.Background(), original.ID, "provider_error", "reverse-r")
	if err != nil || replay.ID != reversal.ID {
		t.Fatalf("reversal replay = %+v, %v", replay, err)
	}
	if _, err := f.wallet.Reverse(context.Background(), original.ID, "different_reason", "reverse-r"); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("reversal conflict error = %v", err)
	}
}

func TestBillingWalletReversalDoesNotReviveExpiredPromotion(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	f.addPromotion(t, "promo-reverse-expired", 500, f.now.Add(time.Minute))
	quote := f.quote(t, "u1", "task.article", "", "task-promo-reverse")
	original, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-promo-reverse", "charge-promo-reverse"))
	if err != nil || original.PromotionalCredits != 500 {
		t.Fatalf("promotional task charge = %+v, %v", original, err)
	}
	f.wallet.now = func() time.Time { return f.now.Add(2 * time.Minute) }
	if _, err := f.wallet.Reverse(context.Background(), original.ID, "provider_error", "reverse-promo-expired"); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	lot, err := f.repo.Billing().FindLotBySource(context.Background(), "promotion", "promo-reverse-expired")
	if err != nil {
		t.Fatal(err)
	}
	if lot.AvailableCredits != 0 || lot.ConsumedCredits != 0 || lot.ExpiredCredits != 500 {
		t.Fatalf("expired promotional lot revived: %+v", lot)
	}
	if got := f.account(t, "u1"); got.PromotionalCredits != 0 {
		t.Fatalf("expired promotional projection revived: %+v", got)
	}
}

func TestBillingWalletReversalUsesCanonicalLotLockOrder(t *testing.T) {
	allocations := []model.BillingChargeAllocation{
		{ID: "allocation-a", LotID: "lot-z"},
		{ID: "allocation-z", LotID: "lot-a"},
	}

	sortChargeAllocationsForLocking(allocations)

	if allocations[0].LotID != "lot-a" || allocations[1].LotID != "lot-z" {
		t.Fatalf("lot lock order = %q, %q; want lot-a, lot-z", allocations[0].LotID, allocations[1].LotID)
	}
}

func TestBillingWalletConcurrentTopUpIdempotency(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 400)
	req := TopUpRequest{
		UserID: "u1", Credits: 1000, ExternalSourceType: "payment", ExternalSourceID: "concurrent-topup",
		IdempotencyScope: "topup", IdempotencyKey: "concurrent-topup",
	}
	var wg sync.WaitGroup
	results := make(chan *TopUpResult, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := f.wallet.TopUp(context.Background(), req)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent top-up: %v", err)
		}
	}
	var entryID string
	for result := range results {
		if result == nil {
			t.Fatal("nil concurrent top-up result")
		}
		if entryID == "" {
			entryID = result.EntryID
		} else if result.EntryID != entryID {
			t.Fatalf("top-up entry IDs differ: %s != %s", result.EntryID, entryID)
		}
	}
	if got := f.account(t, "u1"); got.DebtCredits != 0 || got.PaidCredits != 600 {
		t.Fatalf("concurrent top-up applied twice: %+v", got)
	}
}

func TestBillingWalletRebuildProjection(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	if _, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: "u1", Credits: 700, ExternalSourceType: "payment", ExternalSourceID: "rebuild",
		IdempotencyScope: "topup", IdempotencyKey: "rebuild",
	}); err != nil {
		t.Fatal(err)
	}
	account := f.account(t, "u1")
	account.PaidCredits = 1
	account.Version++
	if err := f.repo.Billing().UpdateAccount(context.Background(), account, account.Version-1); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := f.wallet.RebuildProjection(context.Background(), "u1")
	if err != nil || rebuilt.PaidCredits != 700 || rebuilt.PromotionalCredits != 0 || rebuilt.DebtCredits != 0 {
		t.Fatalf("RebuildProjection = %+v, %v", rebuilt, err)
	}
}

func TestBillingWalletConcurrentIdempotency(t *testing.T) {
	f := newBillingWalletFixture(t, 1000, 0, 0)
	req := acceptedOperationRequest("u1", "task-c", "attempt-c", "call-c", "operation-c")
	var wg sync.WaitGroup
	results := make(chan *model.BillingCharge, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			charge, err := f.wallet.ChargeAcceptedOperation(context.Background(), req)
			results <- charge
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent operation: %v", err)
		}
	}
	var id string
	for charge := range results {
		if charge == nil {
			t.Fatal("nil concurrent charge")
		}
		if id == "" {
			id = charge.ID
		} else if charge.ID != id {
			t.Fatalf("charge IDs differ: %s != %s", charge.ID, id)
		}
	}
	if got := f.account(t, "u1"); got.PaidCredits != 500 {
		t.Fatalf("concurrent charge applied twice: %+v", got)
	}
}

func TestBillingWalletExpiresPromotionalCreditsAtBoundary(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	f.addPromotion(t, "expired-before", 100, f.now.Add(-time.Second))
	f.addPromotion(t, "expired-at", 200, f.now)
	f.addPromotion(t, "future", 300, f.now.Add(time.Second))

	count, err := f.wallet.ExpirePromotionalCredits(context.Background(), f.now, 10)
	if err != nil || count != 2 {
		t.Fatalf("ExpirePromotionalCredits = %d, %v", count, err)
	}
	if got := f.account(t, "u1"); got.PromotionalCredits != 300 {
		t.Fatalf("account after expiry = %+v", got)
	}
	if count, err := f.wallet.ExpirePromotionalCredits(context.Background(), f.now, 10); err != nil || count != 0 {
		t.Fatalf("expiry replay = %d, %v", count, err)
	}
}

func TestBillingWalletSettlementOutboxRetryNoDuplicateAndFencing(t *testing.T) {
	f := newBillingWalletFixture(t, 1000, 0, 0)
	taskID := uuid.NewString()
	if err := f.repo.Tasks().Create(context.Background(), &model.Task{
		ID: taskID, UserID: "u1", Type: model.PlatformArticle, Status: model.TaskStatusCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	intent := SettlementIntent{
		Action:       model.BillingSettlementActionChargeOperation,
		ResourceType: "image", ResourceID: "outbox-image", TaskID: taskID,
		AttemptID: uuid.NewString(), ToolCallID: "outbox-call", CatalogID: "retail-test-v1", SKUID: "image.cover.v1",
		RequestFingerprint: billingFingerprint("outbox-operation"), IdempotencyScope: "settlement", IdempotencyKey: "outbox-operation",
	}
	var first *model.BillingSettlementOutbox
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		var err error
		first, err = f.wallet.EnqueueSettlementInTx(context.Background(), tx, intent)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		replay, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, intent)
		if err == nil && replay.ID != first.ID {
			t.Fatalf("enqueue replay ID = %s, want %s", replay.ID, first.ID)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	conflict := intent
	conflict.RequestFingerprint = billingFingerprint("outbox-conflict")
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		_, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, conflict)
		return err
	}); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("enqueue conflict error = %v", err)
	}

	processed, err := f.wallet.ProcessSettlementOutbox(context.Background(), 10)
	if err != nil || processed != 1 {
		t.Fatalf("ProcessSettlementOutbox = %d, %v", processed, err)
	}
	row, err := f.repo.Billing().FindSettlementByKey(context.Background(), intent.IdempotencyScope, intent.IdempotencyKey)
	if err != nil || row.Status != "processed" || row.Attempts != 1 {
		t.Fatalf("processed row = %+v, %v", row, err)
	}
	if processed, err := f.wallet.ProcessSettlementOutbox(context.Background(), 10); err != nil || processed != 0 {
		t.Fatalf("duplicate process = %d, %v", processed, err)
	}
	if got := f.account(t, "u1"); got.PaidCredits != 500 {
		t.Fatalf("outbox charged more than once: %+v", got)
	}
}

func TestBillingWalletSettlementOutboxTerminatesPermanentFailure(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	missingTask := uuid.NewString()
	intent := SettlementIntent{
		Action:       model.BillingSettlementActionChargeOperation,
		ResourceType: "image", ResourceID: "retry-image", TaskID: missingTask,
		AttemptID: uuid.NewString(), ToolCallID: "retry-call", CatalogID: "retail-test-v1", SKUID: "image.cover.v1",
		RequestFingerprint: billingFingerprint("retry-operation"), IdempotencyScope: "settlement", IdempotencyKey: "retry-operation",
	}
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		_, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, intent)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	processed, err := f.wallet.ProcessSettlementOutbox(context.Background(), 10)
	if err != nil || processed != 1 {
		t.Fatalf("permanent ProcessSettlementOutbox = %d, %v", processed, err)
	}
	row, err := f.repo.Billing().FindSettlementByKey(context.Background(), intent.IdempotencyScope, intent.IdempotencyKey)
	if err != nil || row.Status != "processed" || row.Attempts != 1 || row.NextAttemptAt != nil {
		t.Fatalf("terminal row = %+v, %v", row, err)
	}
	if err := f.repo.Tasks().Create(context.Background(), &model.Task{
		ID: missingTask, UserID: "u1", Type: model.PlatformArticle, Status: model.TaskStatusCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	processed, err = f.wallet.ProcessSettlementOutbox(context.Background(), 10)
	if err != nil || processed != 0 {
		t.Fatalf("terminal settlement replay = %d, %v", processed, err)
	}
	if got := f.account(t, "u1"); got.DebtCredits != 0 {
		t.Fatalf("terminal settlement mutated wallet = %+v", got)
	}
}

func TestBillingWalletRetriesOnlyRetryableDatabaseErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "sqlite busy", err: sqlite3.Error{Code: sqlite3.ErrBusy}, want: true},
		{name: "sqlite locked", err: sqlite3.Error{Code: sqlite3.ErrLocked}, want: true},
		{name: "mysql lock timeout", err: &mysqlDriver.MySQLError{Number: 1205}, want: true},
		{name: "mysql deadlock", err: &mysqlDriver.MySQLError{Number: 1213}, want: true},
		{name: "bad connection", err: driver.ErrBadConn, want: true},
		{name: "mysql duplicate", err: &mysqlDriver.MySQLError{Number: 1062}, want: false},
		{name: "record not found", err: gorm.ErrRecordNotFound, want: false},
		{name: "ledger invalid", err: ErrBillingLedgerInvalid, want: false},
		{name: "application text", err: errors.New("database is locked while validating request"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableBillingDBError(tt.err); got != tt.want {
				t.Fatalf("isRetryableBillingDBError(%T) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}

func TestBillingWalletSettlementOutboxStaleWorkerFencing(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	intent := SettlementIntent{
		Action:       model.BillingSettlementActionReverseTask,
		ResourceType: "task", ResourceID: "stale-task", ChargeID: uuid.NewString(),
		RequestFingerprint: billingFingerprint("stale-settlement"), IdempotencyScope: "settlement", IdempotencyKey: "stale-settlement",
	}
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		_, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, intent)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var first, second []model.BillingSettlementOutbox
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		var err error
		first, err = tx.Billing().ClaimSettlements(context.Background(), f.now, 1)
		return err
	}); err != nil || len(first) != 1 || first[0].Attempts != 1 {
		t.Fatalf("first claim = %+v, %v", first, err)
	}
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		var err error
		second, err = tx.Billing().ClaimSettlements(context.Background(), f.now.Add(6*time.Minute), 1)
		return err
	}); err != nil || len(second) != 1 || second[0].Attempts != 2 {
		t.Fatalf("second claim = %+v, %v", second, err)
	}
	err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		return tx.Billing().MarkSettlementProcessed(context.Background(), first[0].ID, first[0].Attempts, f.now.Add(6*time.Minute))
	})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("stale worker mark error = %v, want fenced not found", err)
	}
}

type billingWalletFixture struct {
	repo    repository.Repository
	catalog *BillingCatalogService
	wallet  *BillingWalletService
	now     time.Time
}

func newBillingWalletFixture(t *testing.T, paid, promotional, debt int64) *billingWalletFixture {
	t.Helper()
	repo := newBillingServiceRepository(t)
	bundle := testBillingBundle()
	// Operation and standalone prices intentionally differ from task price.
	bundle.Products.SKUs[1].PriceCredits = 500
	bundle.Products.SKUs[2].PriceCredits = 500
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	catalog := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }, QuoteTTL: time.Minute})
	if _, err := catalog.Publish(context.Background()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	account := &model.BillingWalletAccount{UserID: "u1", PaidCredits: paid, PromotionalCredits: promotional, DebtCredits: debt}
	if err := repo.Billing().CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if paid > 0 {
		createBillingLot(t, repo, model.BillingCreditLot{
			ID: uuid.NewString(), UserID: "u1", Kind: model.BillingCreditLotKindPaid,
			SourceType: "fixture", SourceID: uuid.NewString(), CatalogID: bundle.Products.CatalogID,
			OriginalCredits: paid, AvailableCredits: paid, CreatedAt: now.Add(-time.Hour),
		})
	}
	if promotional > 0 {
		expires := now.Add(time.Hour)
		createBillingLot(t, repo, model.BillingCreditLot{
			ID: uuid.NewString(), UserID: "u1", Kind: model.BillingCreditLotKindPromotional,
			SourceType: "promotion", SourceID: uuid.NewString(), ProgramID: "fixture", CatalogID: bundle.Products.CatalogID,
			OriginalCredits: promotional, AvailableCredits: promotional, ExpiresAt: &expires, CreatedAt: now.Add(-time.Hour),
		})
	}
	wallet := NewBillingWalletService(repo, &bundle, BillingWalletOptions{Now: func() time.Time { return now }})
	return &billingWalletFixture{repo: repo, catalog: catalog, wallet: wallet, now: now}
}

func (f *billingWalletFixture) quote(t *testing.T, userID, operation, route, identity string) *model.BillingQuote {
	t.Helper()
	quote, err := f.catalog.CreateQuote(context.Background(), QuoteRequest{
		UserID: userID, Operation: operation, Route: route, RequestFingerprint: billingFingerprint(identity),
		IdempotencyScope: "quote", IdempotencyKey: identity,
	})
	if err != nil {
		t.Fatalf("CreateQuote: %v", err)
	}
	return quote
}

func (f *billingWalletFixture) account(t *testing.T, userID string) *model.BillingWalletAccount {
	t.Helper()
	account, err := f.repo.Billing().FindAccount(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func (f *billingWalletFixture) addPromotion(t *testing.T, sourceID string, credits int64, expires time.Time) {
	t.Helper()
	createBillingLot(t, f.repo, model.BillingCreditLot{
		ID: uuid.NewString(), UserID: "u1", Kind: model.BillingCreditLotKindPromotional,
		SourceType: "promotion", SourceID: sourceID, ProgramID: "program", CatalogID: "retail-test-v1",
		OriginalCredits: credits, AvailableCredits: credits, ExpiresAt: &expires, CreatedAt: f.now.Add(-time.Minute),
	})
	account := f.account(t, "u1")
	expected := account.Version
	account.PromotionalCredits += credits
	account.Version++
	if err := f.repo.Billing().UpdateAccount(context.Background(), account, expected); err != nil {
		t.Fatal(err)
	}
}

func createBillingLot(t *testing.T, repo repository.Repository, lot model.BillingCreditLot) {
	t.Helper()
	if err := lot.Validate(); err != nil {
		t.Fatalf("invalid fixture lot: %v", err)
	}
	if err := repo.Billing().CreateLot(context.Background(), &lot); err != nil {
		t.Fatal(err)
	}
}

func taskChargeRequest(quote *model.BillingQuote, taskID, key string) TaskChargeRequest {
	return TaskChargeRequest{
		UserID: quote.UserID, TaskID: taskID, QuoteID: quote.ID,
		CatalogID: quote.CatalogID, SKUID: quote.SKUID, RequestFingerprint: quote.RequestFingerprint,
		IdempotencyScope: "task-charge", IdempotencyKey: key,
	}
}

func acceptedOperationRequest(userID, taskID, attemptID, callID, identity string) OperationChargeRequest {
	return OperationChargeRequest{
		UserID: userID, CatalogID: "retail-test-v1", SKUID: "image.cover.v1",
		TaskID: taskID, AttemptID: attemptID, ToolCallID: callID,
		ResourceType: "image", ResourceID: fmt.Sprintf("image-%s", identity),
		RequestFingerprint: billingFingerprint(identity), IdempotencyScope: "operation-charge", IdempotencyKey: identity,
	}
}
