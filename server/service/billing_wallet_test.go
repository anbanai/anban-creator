package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	billingWalletUserID    = "10000000-0000-4000-8000-000000000001"
	billingWalletNewUserID = "10000000-0000-4000-8000-000000000002"
)

func TestBillingTopUpRequiresCanonicalIdentity(t *testing.T) {
	if _, exists := reflect.TypeOf(TopUpRequest{}).FieldByName("ExternalRef"); exists {
		t.Fatal("TopUpRequest still exposes legacy ExternalRef alias")
	}
	tests := []struct {
		name   string
		mutate func(*TopUpRequest)
	}{
		{name: "external source type", mutate: func(req *TopUpRequest) { req.ExternalSourceType = "" }},
		{name: "external source ID", mutate: func(req *TopUpRequest) { req.ExternalSourceID = "" }},
		{name: "catalog ID", mutate: func(req *TopUpRequest) { req.CatalogID = "" }},
		{name: "request fingerprint", mutate: func(req *TopUpRequest) { req.RequestFingerprint = "" }},
		{name: "idempotency scope", mutate: func(req *TopUpRequest) { req.IdempotencyScope = "" }},
		{name: "idempotency key", mutate: func(req *TopUpRequest) { req.IdempotencyKey = "" }},
		{name: "invalid user UUID", mutate: func(req *TopUpRequest) { req.UserID = "not-a-uuid" }},
		{name: "source type too long", mutate: func(req *TopUpRequest) { req.ExternalSourceType = strings.Repeat("s", 41) }},
		{name: "source ID too long", mutate: func(req *TopUpRequest) { req.ExternalSourceID = strings.Repeat("s", 129) }},
		{name: "catalog ID too long", mutate: func(req *TopUpRequest) { req.CatalogID = strings.Repeat("c", 129) }},
		{name: "scope too long", mutate: func(req *TopUpRequest) { req.IdempotencyScope = strings.Repeat("s", 81) }},
		{name: "key too long", mutate: func(req *TopUpRequest) { req.IdempotencyKey = strings.Repeat("k", 129) }},
		{name: "actor type too long", mutate: func(req *TopUpRequest) { req.ActorType = strings.Repeat("a", 41) }},
		{name: "actor ID too long", mutate: func(req *TopUpRequest) { req.ActorID = strings.Repeat("a", 129) }},
		{name: "source service too long", mutate: func(req *TopUpRequest) { req.SourceService = strings.Repeat("s", 81) }},
		{name: "request ID too long", mutate: func(req *TopUpRequest) { req.RequestID = strings.Repeat("r", 129) }},
		{name: "correlation ID too long", mutate: func(req *TopUpRequest) { req.CorrelationID = strings.Repeat("c", 129) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBillingWalletFixture(t, 0, 0, 0)
			guard := &rejectNestedTxRepository{Repository: f.repo}
			f.wallet.repo = guard
			req := TopUpRequest{
				UserID: billingWalletUserID, Credits: 1_000, ExternalSourceType: "payment", ExternalSourceID: "canonical-" + tt.name,
				CatalogID: "retail-test-v1", RequestFingerprint: billingFingerprint("canonical", tt.name),
				IdempotencyScope: "topup", IdempotencyKey: "canonical-" + tt.name,
			}
			tt.mutate(&req)
			if _, err := f.wallet.TopUp(context.Background(), req); !errors.Is(err, ErrBillingInvalid) {
				t.Fatalf("TopUp error = %v, want ErrBillingInvalid", err)
			}
			if guard.withTxCalls != 0 {
				t.Fatalf("invalid TopUp opened %d transactions", guard.withTxCalls)
			}
		})
	}
	canonical, err := CanonicalTopUpRequest(TopUpRequest{
		UserID: " " + billingWalletUserID + " ", Credits: 1_000,
		ExternalSourceType: " payment ", ExternalSourceID: " source ", CatalogID: " retail-test-v1 ",
		RequestFingerprint: " " + billingFingerprint("canonical-metadata") + " ",
		IdempotencyScope:   " topup ", IdempotencyKey: " key ", ActorType: " admin ", ActorID: " actor ",
		SourceService: " billing-api ", RequestID: " request ", CorrelationID: " correlation ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if canonical.ActorType != "admin" || canonical.ActorID != "actor" || canonical.SourceService != "billing-api" ||
		canonical.RequestID != "request" || canonical.CorrelationID != "correlation" {
		t.Fatalf("canonical metadata = %+v", canonical)
	}
}

func TestBillingTopUpRequiresExistingUserAndPublishedCatalogBeforeMutation(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, *billingWalletFixture, *TopUpRequest)
		wantErr error
	}{
		{
			name: "missing user",
			prepare: func(_ *testing.T, _ *billingWalletFixture, req *TopUpRequest) {
				req.UserID = "10000000-0000-4000-8000-000000000099"
			},
			wantErr: ErrBillingUserNotFound,
		},
		{
			name: "missing catalog",
			prepare: func(_ *testing.T, _ *billingWalletFixture, req *TopUpRequest) {
				req.CatalogID = "retail-missing-v1"
			},
			wantErr: ErrBillingCatalogNotFound,
		},
		{
			name: "unpublished catalog",
			prepare: func(t *testing.T, f *billingWalletFixture, req *TopUpRequest) {
				req.CatalogID = "retail-draft-v1"
				if err := f.repo.Billing().CreateCatalogVersion(context.Background(), &model.BillingCatalogVersion{
					CatalogID: req.CatalogID, Currency: "credits", Status: "draft", PublishedAt: f.now, Snapshot: []byte(`{}`), CreatedAt: f.now,
				}); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: ErrBillingCatalogNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBillingWalletFixture(t, 0, 0, 0)
			req := TopUpRequest{
				UserID: billingWalletUserID, Credits: 100, ExternalSourceType: "payment", ExternalSourceID: "provenance-" + tt.name,
				CatalogID: "retail-test-v1", RequestFingerprint: billingFingerprint("provenance", tt.name),
				IdempotencyScope: "topup", IdempotencyKey: "provenance-" + tt.name,
			}
			tt.prepare(t, f, &req)
			if _, err := f.wallet.TopUp(context.Background(), req); !errors.Is(err, tt.wantErr) {
				t.Fatalf("TopUp error = %v, want %v", err, tt.wantErr)
			}
			entries, err := f.repo.Billing().ListEntriesByUser(context.Background(), req.UserID, 0, 10)
			if err != nil || len(entries) != 0 {
				t.Fatalf("entries after rejected topup = %+v, %v", entries, err)
			}
			if _, err := f.repo.Billing().FindLotBySource(context.Background(), req.ExternalSourceType, req.ExternalSourceID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("lot after rejected topup = %v", err)
			}
			if tt.name == "missing user" {
				if _, err := f.repo.Billing().FindAccount(context.Background(), req.UserID); !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("orphan wallet after missing user topup = %v", err)
				}
			} else if account := f.account(t, billingWalletUserID); account.Version != 0 || account.PaidCredits != 0 {
				t.Fatalf("wallet mutated by rejected topup: %+v", account)
			}
		})
	}
}

func TestBillingTopUpInTxCatalogFailureDoesNotPoisonCallerTransaction(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	req := TopUpRequest{
		UserID: billingWalletUserID, Credits: 100, ExternalSourceType: "payment", ExternalSourceID: "missing-catalog-in-tx",
		CatalogID: "retail-missing-v1", RequestFingerprint: billingFingerprint("missing-catalog-in-tx"),
		IdempotencyScope: "topup", IdempotencyKey: "missing-catalog-in-tx",
	}
	markerID := uuid.NewString()
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		if _, err := f.wallet.TopUpInTx(context.Background(), tx, req); !errors.Is(err, ErrBillingCatalogNotFound) {
			t.Fatalf("TopUpInTx error = %v, want ErrBillingCatalogNotFound", err)
		}
		return tx.Tasks().Create(context.Background(), &model.Task{
			ID: markerID, UserID: billingWalletUserID, Type: model.PlatformArticle, Status: model.TaskStatusPending,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.Tasks().FindByID(context.Background(), markerID); err != nil {
		t.Fatalf("caller transaction marker missing: %v", err)
	}
	if account := f.account(t, billingWalletUserID); account.Version != 0 || account.PaidCredits != 0 {
		t.Fatalf("wallet mutated by missing catalog: %+v", account)
	}
}

func TestBillingWalletTaskAdmissionDebtInsufficientReplayAndConflict(t *testing.T) {
	t.Run("debt", func(t *testing.T) {
		f := newBillingWalletFixture(t, 500, 0, 1)
		quote := f.quote(t, billingWalletUserID, "task.article", "", "task-debt")
		_, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-debt", "charge-debt"))
		if !errors.Is(err, ErrBillingDebtOutstanding) {
			t.Fatalf("ChargeTaskAdmission error = %v, want ErrBillingDebtOutstanding", err)
		}
	})

	t.Run("insufficient", func(t *testing.T) {
		f := newBillingWalletFixture(t, 499, 0, 0)
		quote := f.quote(t, billingWalletUserID, "task.article", "", "task-insufficient")
		_, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-insufficient", "charge-insufficient"))
		if !errors.Is(err, ErrBillingInsufficientForTask) {
			t.Fatalf("ChargeTaskAdmission error = %v, want ErrBillingInsufficientForTask", err)
		}
	})

	t.Run("replay and conflict", func(t *testing.T) {
		f := newBillingWalletFixture(t, 500, 0, 0)
		quote := f.quote(t, billingWalletUserID, "task.article", "", "task-replay")
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
		account := f.account(t, billingWalletUserID)
		if account.PaidCredits != 0 || account.DebtCredits != 0 {
			t.Fatalf("account after replay = %+v", account)
		}
	})
}

func TestBillingWalletTaskAdmissionInTxRollsBackWithCaller(t *testing.T) {
	f := newBillingWalletFixture(t, 500, 0, 0)
	quote := f.quote(t, billingWalletUserID, "task.article", "", "task-caller-tx")
	req := taskChargeRequest(quote, "task-caller-tx", "charge-caller-tx")
	errRollback := errors.New("rollback caller transaction")
	err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		guard := &rejectNestedTxRepository{Repository: tx}
		charge, err := f.wallet.ChargeTaskAdmissionInTx(context.Background(), guard, req)
		if err != nil || charge == nil {
			t.Fatalf("ChargeTaskAdmissionInTx = %+v, %v", charge, err)
		}
		if guard.withTxCalls != 0 {
			t.Fatalf("ChargeTaskAdmissionInTx opened %d nested transactions", guard.withTxCalls)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("caller transaction error = %v", err)
	}
	persistedQuote, err := f.repo.Billing().FindQuoteByKey(context.Background(), quote.IdempotencyScope, quote.IdempotencyKey)
	if err != nil || persistedQuote.ConsumedAt != nil {
		t.Fatalf("quote after rollback = %+v, %v", persistedQuote, err)
	}
	if _, err := f.repo.Billing().FindChargeByKey(context.Background(), req.IdempotencyScope, req.IdempotencyKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("charge after rollback error = %v, want not found", err)
	}
	entries, err := f.repo.Billing().ListEntriesByUser(context.Background(), billingWalletUserID, 0, 10)
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries after rollback = %+v, %v", entries, err)
	}
	if got := f.account(t, billingWalletUserID); got.PaidCredits != 500 || got.Version != 0 {
		t.Fatalf("account after rollback = %+v", got)
	}
}

func TestBillingWalletTaskAdmissionInTxInsufficientDoesNotPartiallyMutateCommittedCaller(t *testing.T) {
	f := newBillingWalletFixture(t, 499, 0, 0)
	quote := f.quote(t, billingWalletUserID, "task.article", "", "task-insufficient-commit")
	req := taskChargeRequest(quote, "task-insufficient-commit", "charge-insufficient-commit")
	markerID := uuid.NewString()
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		if _, err := f.wallet.ChargeTaskAdmissionInTx(context.Background(), tx, req); !errors.Is(err, ErrBillingInsufficientForTask) {
			t.Fatalf("admission error = %v, want insufficient", err)
		}
		return tx.Tasks().Create(context.Background(), &model.Task{
			ID: markerID, UserID: billingWalletUserID, Type: model.PlatformArticle, Status: model.TaskStatusPending,
		})
	}); err != nil {
		t.Fatalf("commit caller transaction: %v", err)
	}
	if _, err := f.repo.Tasks().FindByID(context.Background(), markerID); err != nil {
		t.Fatalf("caller marker did not commit: %v", err)
	}
	lot, err := f.repo.Billing().FindLotBySource(context.Background(), "fixture", "fixture-paid-u1")
	if err != nil || lot.AvailableCredits != 499 || lot.ConsumedCredits != 0 {
		t.Fatalf("lot after committed insufficient admission = %+v, %v", lot, err)
	}
	if got := f.account(t, billingWalletUserID); got.PaidCredits != 499 || got.Version != 0 {
		t.Fatalf("account after committed insufficient admission = %+v", got)
	}
	persistedQuote, err := f.repo.Billing().FindQuoteByKey(context.Background(), quote.IdempotencyScope, quote.IdempotencyKey)
	if err != nil || persistedQuote.ConsumedAt != nil {
		t.Fatalf("quote after committed insufficient admission = %+v, %v", persistedQuote, err)
	}
	if _, err := f.repo.Billing().FindChargeByKey(context.Background(), req.IdempotencyScope, req.IdempotencyKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("charge after committed insufficient admission = %v", err)
	}
}

func TestBillingWalletRejectsQuoteWithMismatchedSKUSnapshot(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		route     string
		charge    func(*billingWalletFixture, *model.BillingQuote) error
	}{
		{
			name: "task", operation: "task.article",
			charge: func(f *billingWalletFixture, quote *model.BillingQuote) error {
				_, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-snapshot", "charge-snapshot"))
				return err
			},
		},
		{
			name: "standalone", operation: "designer.generate_image", route: "image.designer",
			charge: func(f *billingWalletFixture, quote *model.BillingQuote) error {
				_, err := f.wallet.ChargeStandaloneOperation(context.Background(), OperationChargeRequest{
					UserID: quote.UserID, QuoteID: quote.ID, CatalogID: quote.CatalogID, SKUID: quote.SKUID,
					ResourceType: "image", ResourceID: "snapshot-image", RequestFingerprint: quote.RequestFingerprint,
					IdempotencyScope: "standalone-charge", IdempotencyKey: "snapshot-standalone",
				})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBillingWalletFixture(t, 500, 0, 0)
			valid := f.quote(t, billingWalletUserID, tt.operation, tt.route, "snapshot-"+tt.name)
			mismatch := *valid
			mismatch.ID = uuid.NewString()
			mismatch.IdempotencyKey += "-mismatch"
			mismatch.SKUSnapshot = append([]byte(nil), valid.SKUSnapshot...)
			mismatch.SKUSnapshot[0] ^= 1
			if err := f.repo.Billing().CreateQuote(context.Background(), &mismatch); err != nil {
				t.Fatal(err)
			}
			if err := tt.charge(f, &mismatch); !errors.Is(err, ErrBillingQuoteMismatch) {
				t.Fatalf("snapshot mismatch error = %v, want ErrBillingQuoteMismatch", err)
			}
		})
	}
}

func TestBillingWalletDoesNotMapLotDatabaseErrorToInsufficient(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		route     string
		charge    func(*billingWalletFixture, *model.BillingQuote) error
	}{
		{
			name: "task", operation: "task.article",
			charge: func(f *billingWalletFixture, quote *model.BillingQuote) error {
				_, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-db-error", "charge-db-error"))
				return err
			},
		},
		{
			name: "standalone", operation: "designer.generate_image", route: "image.designer",
			charge: func(f *billingWalletFixture, quote *model.BillingQuote) error {
				_, err := f.wallet.ChargeStandaloneOperation(context.Background(), OperationChargeRequest{
					UserID: quote.UserID, QuoteID: quote.ID, CatalogID: quote.CatalogID, SKUID: quote.SKUID,
					ResourceType: "image", ResourceID: "db-error-image", RequestFingerprint: quote.RequestFingerprint,
					IdempotencyScope: "standalone-charge", IdempotencyKey: "db-error-standalone",
				})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared&_busy_timeout=10000"
			db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := model.AutoMigrate(db); err != nil {
				t.Fatal(err)
			}
			repo := repository.New(db)
			t.Cleanup(func() { _ = repo.Close() })
			f := newBillingWalletFixtureWithRepository(t, repo, 500, 0, 0)
			quote := f.quote(t, billingWalletUserID, tt.operation, tt.route, "db-error-"+tt.name)
			errInjected := errors.New("injected lot query failure")
			if err := db.Callback().Query().Before("gorm:query").Register("billing_test:lot_error", func(tx *gorm.DB) {
				if tx.Statement.Table == "billing_credit_lots" {
					tx.AddError(errInjected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := tt.charge(f, quote); !errors.Is(err, errInjected) {
				t.Fatalf("lot query error = %v, want injected error", err)
			}
		})
	}
}

func TestAcceptedTaskOperationMayCreateDebt(t *testing.T) {
	f := newBillingWalletFixture(t, 100, 0, 0)
	req := acceptedOperationRequest(billingWalletUserID, "task-1", "attempt-1", "call-1", "operation-1")
	c, err := f.wallet.ChargeAcceptedOperation(context.Background(), req)
	if err != nil {
		t.Fatalf("ChargeAcceptedOperation: %v", err)
	}
	if c.PriceCredits != 500 || c.PaidCredits != 100 || c.PromotionalCredits != 0 || c.DebtCredits != 400 {
		t.Fatalf("charge = %+v", c)
	}
	if got := f.account(t, billingWalletUserID); got.PaidCredits != 0 || got.DebtCredits != 400 {
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

func TestBillingWalletChargeReplayRejectsImmutableIdentityDrift(t *testing.T) {
	t.Run("task identity", func(t *testing.T) {
		f := newBillingWalletFixture(t, 1000, 0, 0)
		quote := f.quote(t, billingWalletUserID, "task.article", "", "task-identity")
		req := taskChargeRequest(quote, "task-identity", "task-identity-key")
		if _, err := f.wallet.ChargeTaskAdmission(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		secondQuote, err := f.catalog.CreateQuote(context.Background(), QuoteRequest{
			UserID: billingWalletUserID, Operation: "task.article", RequestFingerprint: quote.RequestFingerprint,
			IdempotencyScope: "quote", IdempotencyKey: "task-identity-second-quote",
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, mutate := range []func(*TaskChargeRequest){
			func(value *TaskChargeRequest) { value.TaskID = "different-task" },
			func(value *TaskChargeRequest) { value.QuoteID = secondQuote.ID },
			func(value *TaskChargeRequest) { value.UserID = "different-user" },
			func(value *TaskChargeRequest) { value.CatalogID = "different-catalog" },
			func(value *TaskChargeRequest) { value.SKUID = "different-sku" },
		} {
			drift := req
			mutate(&drift)
			if _, err := f.wallet.ChargeTaskAdmission(context.Background(), drift); !errors.Is(err, ErrBillingConflict) {
				t.Fatalf("task identity drift %+v error = %v, want conflict", drift, err)
			}
		}
	})

	t.Run("accepted operation identity", func(t *testing.T) {
		f := newBillingWalletFixture(t, 1000, 0, 0)
		req := acceptedOperationRequest(billingWalletUserID, "task-operation-identity", "attempt-operation-identity", "call-operation-identity", "operation-identity")
		if _, err := f.wallet.ChargeAcceptedOperation(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		for _, mutate := range []func(*OperationChargeRequest){
			func(value *OperationChargeRequest) { value.UserID = "different-user" },
			func(value *OperationChargeRequest) { value.TaskID = "different-task" },
			func(value *OperationChargeRequest) { value.AttemptID = "different-attempt" },
			func(value *OperationChargeRequest) { value.ToolCallID = "different-call" },
			func(value *OperationChargeRequest) { value.ResourceType = "video" },
			func(value *OperationChargeRequest) { value.ResourceID = "different-resource" },
			func(value *OperationChargeRequest) { value.CatalogID = "different-catalog" },
			func(value *OperationChargeRequest) { value.SKUID = "image.standalone.v1" },
		} {
			drift := req
			mutate(&drift)
			if _, err := f.wallet.ChargeAcceptedOperation(context.Background(), drift); !errors.Is(err, ErrBillingConflict) {
				t.Fatalf("operation identity drift %+v error = %v, want conflict", drift, err)
			}
		}
	})
}

func TestBillingWalletPromotionEligibilityAndExpiryBoundary(t *testing.T) {
	f := newBillingWalletFixture(t, 500, 0, 0)
	f.addPromotion(t, "promo-ineligible", 300, f.now.Add(time.Hour))
	f.wallet.promotionEligible = func(lot model.BillingCreditLot, sku model.BillingSKU) bool {
		return lot.SourceID != "promo-ineligible"
	}
	quote := f.quote(t, billingWalletUserID, "task.article", "", "task-promo-ineligible")
	charge, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-promo-ineligible", "charge-promo-ineligible"))
	if err != nil || charge.PaidCredits != 500 || charge.PromotionalCredits != 0 {
		t.Fatalf("ineligible promotion charge = %+v, %v", charge, err)
	}

	f2 := newBillingWalletFixture(t, 0, 0, 0)
	f2.addPromotion(t, "promo-at-boundary", 500, f2.now)
	quote2 := f2.quote(t, billingWalletUserID, "task.article", "", "task-promo-expired")
	if _, err := f2.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote2, "task-promo-expired", "charge-promo-expired")); !errors.Is(err, ErrBillingInsufficientForTask) {
		t.Fatalf("expiry boundary error = %v, want insufficient", err)
	}
}

func TestBillingWalletStandaloneOperationNeverOverdraws(t *testing.T) {
	f := newBillingWalletFixture(t, 499, 0, 0)
	quote := f.quote(t, billingWalletUserID, "designer.generate_image", "image.designer", "standalone-1")
	req := OperationChargeRequest{
		UserID: billingWalletUserID, QuoteID: quote.ID, CatalogID: quote.CatalogID, SKUID: quote.SKUID,
		ResourceType: "image", ResourceID: "image-1", RequestFingerprint: quote.RequestFingerprint,
		IdempotencyScope: "standalone-charge", IdempotencyKey: "standalone-1",
	}
	if _, err := f.wallet.ChargeStandaloneOperation(context.Background(), req); !errors.Is(err, ErrBillingInsufficientForStandaloneOperation) {
		t.Fatalf("standalone error = %v, want insufficient", err)
	}
	if got := f.account(t, billingWalletUserID); got.PaidCredits != 499 || got.DebtCredits != 0 {
		t.Fatalf("standalone changed wallet: %+v", got)
	}
}

func TestTopUpRepaysDebtBeforePaidBalance(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 400)
	fingerprint := billingFingerprint("topup-1")
	result, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: billingWalletUserID, Credits: 1000, ExternalSourceType: "payment", ExternalSourceID: "api-1",
		CatalogID: "retail-test-v1", RequestFingerprint: fingerprint, IdempotencyScope: "topup", IdempotencyKey: "topup-1",
	})
	if err != nil || result.DebtRepaid != 400 || result.PaidAdded != 600 {
		t.Fatalf("TopUp = %+v, %v", result, err)
	}
	if got := f.account(t, billingWalletUserID); got.DebtCredits != 0 || got.PaidCredits != 600 {
		t.Fatalf("account = %+v", got)
	}
	replay, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: billingWalletUserID, Credits: 1000, ExternalSourceType: "payment", ExternalSourceID: "api-1",
		CatalogID: "retail-test-v1", RequestFingerprint: fingerprint, IdempotencyScope: "topup", IdempotencyKey: "topup-1",
	})
	if err != nil || replay.EntryID != result.EntryID {
		t.Fatalf("topup replay = %+v, %v", replay, err)
	}
	if _, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: billingWalletUserID, Credits: 999, ExternalSourceType: "payment", ExternalSourceID: "api-1",
		CatalogID: "retail-test-v1", RequestFingerprint: fingerprint, IdempotencyScope: "topup", IdempotencyKey: "topup-1",
	}); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("topup conflict error = %v", err)
	}
	if _, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: billingWalletUserID, Credits: 999, ExternalSourceType: "payment", ExternalSourceID: "api-1",
		CatalogID: "retail-test-v1", RequestFingerprint: fingerprint, IdempotencyScope: "topup", IdempotencyKey: "different-key-same-source",
	}); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("external source conflict error = %v, want ErrBillingConflict", err)
	}
	for _, drift := range []TopUpRequest{
		{
			UserID: billingWalletUserID, Credits: 1000, ExternalSourceType: "payment", ExternalSourceID: "api-1", CatalogID: "retail-other",
			RequestFingerprint: fingerprint, IdempotencyScope: "topup", IdempotencyKey: "topup-1",
		},
		{
			UserID: billingWalletUserID, Credits: 1000, ExternalSourceType: "payment", ExternalSourceID: "api-1",
			CatalogID: "retail-test-v1", RequestFingerprint: billingFingerprint("topup-drift"), IdempotencyScope: "topup", IdempotencyKey: "topup-1",
		},
	} {
		if _, err := f.wallet.TopUp(context.Background(), drift); !errors.Is(err, ErrBillingConflict) {
			t.Fatalf("topup catalog/fingerprint drift error = %v, want conflict", err)
		}
	}
}

func TestBillingWalletTopUpRequiresFingerprint(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 400)
	_, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: billingWalletUserID, Credits: 100, ExternalSourceType: "payment", ExternalSourceID: "missing-fingerprint",
		CatalogID: "retail-test-v1", IdempotencyScope: "topup", IdempotencyKey: "missing-fingerprint",
	})
	if !errors.Is(err, ErrBillingInvalid) {
		t.Fatalf("missing fingerprint error = %v, want ErrBillingInvalid", err)
	}
}

func TestBillingWalletPromotionCannotRepayDebt(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 400)
	f.addPromotion(t, "promo-debt", 1000, f.now.Add(time.Hour))
	if got := f.account(t, billingWalletUserID); got.DebtCredits != 400 || got.PromotionalCredits != 1000 {
		t.Fatalf("promotion repaid debt: %+v", got)
	}
	result, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: billingWalletUserID, Credits: 100, ExternalSourceType: "payment", ExternalSourceID: "small",
		CatalogID: "retail-test-v1", RequestFingerprint: billingFingerprint("small"), IdempotencyScope: "topup", IdempotencyKey: "small",
	})
	if err != nil || result.DebtRepaid != 100 || result.PaidAdded != 0 {
		t.Fatalf("small topup = %+v, %v", result, err)
	}
	if _, err := f.repo.Billing().FindLotBySource(context.Background(), "payment", "small"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("pure debt repayment lot lookup error = %v, want not found", err)
	}
	audit, err := f.repo.Billing().FindEntryByKey(context.Background(), "topup", "small")
	if err != nil || audit.PaidDelta != 0 || audit.DebtDelta != 0 || audit.CatalogID != "retail-test-v1" ||
		audit.RequestFingerprint != billingFingerprint("small") {
		t.Fatalf("topup audit entry = %+v, %v", audit, err)
	}
	entries, err := f.repo.Billing().ListEntriesByUser(context.Background(), billingWalletUserID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var repayment *model.BillingWalletEntry
	for i := range entries {
		if entries[i].EventKind == model.BillingWalletEventKindDebtRepayment && entries[i].ResourceID == audit.ID {
			repayment = &entries[i]
			break
		}
	}
	if repayment == nil || repayment.DebtDelta != -100 || repayment.ChargeID == nil {
		t.Fatalf("FIFO repayment entry = %+v", repayment)
	}
}

func TestBillingWalletReversalPolicyAndChargeKind(t *testing.T) {
	t.Run("allowlisted task failure", func(t *testing.T) {
		f := newBillingWalletFixture(t, 500, 0, 0)
		quote := f.quote(t, billingWalletUserID, "task.article", "", "task-reversal-policy")
		original, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-reversal-policy", "charge-reversal-policy"))
		if err != nil {
			t.Fatal(err)
		}
		reversal, err := f.wallet.Reverse(context.Background(), original.ID, "provider_error", "reverse-policy")
		if err != nil || reversal.ReversalOfID == nil || *reversal.ReversalOfID != original.ID || reversal.DebtCredits != 0 {
			t.Fatalf("allowlisted reversal = %+v, %v", reversal, err)
		}
	})

	for _, reason := range []string{"user_cancel", "different_reason"} {
		t.Run("reject reason "+reason, func(t *testing.T) {
			f := newBillingWalletFixture(t, 500, 0, 0)
			quote := f.quote(t, billingWalletUserID, "task.article", "", "task-reject-"+reason)
			original, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-reject-"+reason, "charge-reject-"+reason))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.wallet.Reverse(context.Background(), original.ID, reason, "reverse-reject-"+reason); !errors.Is(err, ErrBillingReversalNotAllowed) {
				t.Fatalf("reason %q error = %v", reason, err)
			}
		})
	}

	t.Run("disabled policy", func(t *testing.T) {
		f := newBillingWalletFixture(t, 500, 0, 0)
		quote := f.quote(t, billingWalletUserID, "task.article", "", "task-reversal-disabled")
		original, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-reversal-disabled", "charge-reversal-disabled"))
		if err != nil {
			t.Fatal(err)
		}
		f.wallet.bundle.Policy.TaskFailureReversal.Enabled = false
		if _, err := f.wallet.Reverse(context.Background(), original.ID, "provider_error", "reverse-disabled"); !errors.Is(err, ErrBillingReversalNotAllowed) {
			t.Fatalf("disabled reversal error = %v", err)
		}
	})

	t.Run("accepted operation", func(t *testing.T) {
		f := newBillingWalletFixture(t, 0, 0, 0)
		original, err := f.wallet.ChargeAcceptedOperation(context.Background(), acceptedOperationRequest(billingWalletUserID, "task-operation-reverse", "attempt-operation-reverse", "call-operation-reverse", "operation-reverse"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.wallet.Reverse(context.Background(), original.ID, "provider_error", "reverse-operation"); !errors.Is(err, ErrBillingReversalNotAllowed) {
			t.Fatalf("operation reversal error = %v", err)
		}
		if got := f.account(t, billingWalletUserID); got.DebtCredits != original.DebtCredits {
			t.Fatalf("operation reversal mutated debt = %+v", got)
		}
	})
}

func TestBillingWalletTopUpAndReplayDoNotScanWalletLedger(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	charge, err := f.wallet.ChargeAcceptedOperation(context.Background(), acceptedOperationRequest(billingWalletUserID, "task-indexed-debt", "attempt-indexed-debt", "call-indexed-debt", "indexed-debt"))
	if err != nil || charge.DebtCredits != 500 {
		t.Fatalf("debt charge = %+v, %v", charge, err)
	}
	guard := &noLedgerScanRepository{Repository: f.repo}
	f.wallet.repo = guard
	req := TopUpRequest{
		UserID: billingWalletUserID, Credits: 500, ExternalSourceType: "payment", ExternalSourceID: "indexed-topup",
		CatalogID: "retail-test-v1", RequestFingerprint: billingFingerprint("indexed-topup"), IdempotencyScope: "topup", IdempotencyKey: "indexed-topup",
	}
	first, err := f.wallet.TopUp(context.Background(), req)
	if err != nil || first.DebtRepaid != 500 {
		t.Fatalf("indexed topup = %+v, %v", first, err)
	}
	replay, err := f.wallet.TopUp(context.Background(), req)
	if err != nil || replay.EntryID != first.EntryID || replay.DebtRepaid != 500 {
		t.Fatalf("indexed replay = %+v, %v", replay, err)
	}
	if guard.listEntriesCalls != 0 {
		t.Fatalf("TopUp scanned wallet ledger %d times", guard.listEntriesCalls)
	}
}

func TestBillingWalletReversalDoesNotReviveExpiredPromotion(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	f.addPromotion(t, "promo-reverse-expired", 500, f.now.Add(time.Minute))
	quote := f.quote(t, billingWalletUserID, "task.article", "", "task-promo-reverse")
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
	if got := f.account(t, billingWalletUserID); got.PromotionalCredits != 0 {
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
		UserID: billingWalletUserID, Credits: 1000, ExternalSourceType: "payment", ExternalSourceID: "concurrent-topup",
		CatalogID: "retail-test-v1", RequestFingerprint: billingFingerprint("concurrent-topup"), IdempotencyScope: "topup", IdempotencyKey: "concurrent-topup",
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
	if got := f.account(t, billingWalletUserID); got.DebtCredits != 0 || got.PaidCredits != 600 {
		t.Fatalf("concurrent top-up applied twice: %+v", got)
	}
}

func TestBillingWalletConcurrentFirstAccountTopUpsBothSucceed(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	if err := f.repo.Users().Create(context.Background(), &model.User{
		ID: billingWalletNewUserID, Email: "new-wallet-user@example.test", Password: "fixture", InviteCode: "NEW-WALLET-USER",
	}); err != nil {
		t.Fatal(err)
	}
	f.wallet.repo = &ensureAccountRepository{Repository: f.repo, userID: billingWalletNewUserID}
	requests := []TopUpRequest{
		{
			UserID: billingWalletNewUserID, Credits: 100, ExternalSourceType: "payment", ExternalSourceID: "first-account-a",
			CatalogID: "retail-test-v1", RequestFingerprint: billingFingerprint("first-account-a"), IdempotencyScope: "topup", IdempotencyKey: "first-account-a",
		},
		{
			UserID: billingWalletNewUserID, Credits: 200, ExternalSourceType: "payment", ExternalSourceID: "first-account-b",
			CatalogID: "retail-test-v1", RequestFingerprint: billingFingerprint("first-account-b"), IdempotencyScope: "topup", IdempotencyKey: "first-account-b",
		},
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(requests))
	for _, req := range requests {
		req := req
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.wallet.TopUp(context.Background(), req)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent first-account top-up: %v", err)
		}
	}
	if got := f.account(t, billingWalletNewUserID); got.PaidCredits != 300 || got.PromotionalCredits != 0 || got.DebtCredits != 0 {
		t.Fatalf("first-account top-up projection = %+v", got)
	}
}

func TestBillingWalletRebuildProjection(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	if _, err := f.wallet.TopUp(context.Background(), TopUpRequest{
		UserID: billingWalletUserID, Credits: 700, ExternalSourceType: "payment", ExternalSourceID: "rebuild",
		CatalogID: "retail-test-v1", RequestFingerprint: billingFingerprint("rebuild"), IdempotencyScope: "topup", IdempotencyKey: "rebuild",
	}); err != nil {
		t.Fatal(err)
	}
	account := f.account(t, billingWalletUserID)
	account.PaidCredits = 1
	account.Version++
	if err := f.repo.Billing().UpdateAccount(context.Background(), account, account.Version-1); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := f.wallet.RebuildProjection(context.Background(), billingWalletUserID)
	if err != nil || rebuilt.PaidCredits != 700 || rebuilt.PromotionalCredits != 0 || rebuilt.DebtCredits != 0 {
		t.Fatalf("RebuildProjection = %+v, %v", rebuilt, err)
	}
}

func TestBillingWalletConcurrentIdempotency(t *testing.T) {
	f := newBillingWalletFixture(t, 1000, 0, 0)
	req := acceptedOperationRequest(billingWalletUserID, "task-c", "attempt-c", "call-c", "operation-c")
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
	if got := f.account(t, billingWalletUserID); got.PaidCredits != 500 {
		t.Fatalf("concurrent charge applied twice: %+v", got)
	}
}

func TestBillingWalletPostLockCurrentReadReplaysWithoutMutation(t *testing.T) {
	t.Run("task admission", func(t *testing.T) {
		f := newBillingWalletFixture(t, 500, 0, 0)
		quote := f.quote(t, billingWalletUserID, "task.article", "", "post-lock-task")
		req := taskChargeRequest(quote, "post-lock-task", "post-lock-task")
		sku, err := f.repo.Billing().FindSKU(context.Background(), req.CatalogID, req.SKUID)
		if err != nil {
			t.Fatal(err)
		}
		existing := newTaskCharge(req, sku, f.now)
		existing.ID = "committed-task-charge"
		state := &postLockCurrentReadState{keyCharge: existing, identityCharge: existing}
		raceRepo := &postLockCurrentReadRepository{Repository: f.repo, state: state}
		var replay *model.BillingCharge
		if err := raceRepo.WithTx(context.Background(), func(tx repository.Repository) error {
			var callErr error
			replay, callErr = f.wallet.ChargeTaskAdmissionInTx(context.Background(), tx, req)
			if callErr != nil {
				t.Errorf("post-lock task replay: %v", callErr)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if replay == nil || replay.ID != existing.ID || state.lockCalls < 2 {
			t.Fatalf("post-lock task replay = %+v, lock calls %d", replay, state.lockCalls)
		}
		assertTaskChargeUnchanged(t, f, quote, req, 500)
	})

	t.Run("accepted operation", func(t *testing.T) {
		f := newBillingWalletFixture(t, 1000, 0, 0)
		req := acceptedOperationRequest(billingWalletUserID, "post-lock-operation", "post-lock-attempt", "post-lock-call", "post-lock-operation")
		sku, err := f.repo.Billing().FindSKU(context.Background(), req.CatalogID, req.SKUID)
		if err != nil {
			t.Fatal(err)
		}
		existing := newOperationCharge(req, sku, true, f.now)
		existing.ID = "committed-operation-charge"
		state := &postLockCurrentReadState{keyCharge: existing, identityCharge: existing}
		f.wallet.repo = &postLockCurrentReadRepository{Repository: f.repo, state: state}
		replay, err := f.wallet.ChargeAcceptedOperation(context.Background(), req)
		if err != nil || replay == nil || replay.ID != existing.ID || state.lockCalls < 2 {
			t.Fatalf("post-lock operation replay = %+v, %v, lock calls %d", replay, err, state.lockCalls)
		}
		if got := f.account(t, billingWalletUserID); got.PaidCredits != 1000 || got.DebtCredits != 0 {
			t.Fatalf("operation replay mutated account: %+v", got)
		}
	})

	t.Run("standalone operation", func(t *testing.T) {
		f := newBillingWalletFixture(t, 500, 0, 0)
		quote := f.quote(t, billingWalletUserID, "designer.generate_image", "image.designer", "post-lock-standalone")
		req := OperationChargeRequest{
			UserID: billingWalletUserID, QuoteID: quote.ID, CatalogID: quote.CatalogID, SKUID: quote.SKUID,
			ResourceType: "image", ResourceID: "post-lock-standalone", RequestFingerprint: quote.RequestFingerprint,
			IdempotencyScope: "standalone-charge", IdempotencyKey: "post-lock-standalone",
		}
		sku, err := f.repo.Billing().FindSKU(context.Background(), req.CatalogID, req.SKUID)
		if err != nil {
			t.Fatal(err)
		}
		existing := newOperationCharge(req, sku, false, f.now)
		existing.ID = "committed-standalone-charge"
		state := &postLockCurrentReadState{keyCharge: existing, identityCharge: existing}
		f.wallet.repo = &postLockCurrentReadRepository{Repository: f.repo, state: state}
		replay, err := f.wallet.ChargeStandaloneOperation(context.Background(), req)
		if err != nil || replay == nil || replay.ID != existing.ID || state.lockCalls < 2 {
			t.Fatalf("post-lock standalone replay = %+v, %v, lock calls %d", replay, err, state.lockCalls)
		}
		assertOperationChargeUnchanged(t, f, quote, req, 500)
	})

	t.Run("reversal", func(t *testing.T) {
		f := newBillingWalletFixture(t, 500, 0, 0)
		quote := f.quote(t, billingWalletUserID, "task.article", "", "post-lock-reversal")
		original, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "post-lock-reversal", "post-lock-reversal"))
		if err != nil {
			t.Fatal(err)
		}
		existing := newReversalCharge(original, "post-lock-reversal", billingFingerprint(original.ID, "provider_error"), f.now)
		existing.ID = "committed-reversal-charge"
		state := &postLockCurrentReadState{keyCharge: existing, identityCharge: existing}
		f.wallet.repo = &postLockCurrentReadRepository{Repository: f.repo, state: state}
		replay, err := f.wallet.Reverse(context.Background(), original.ID, "provider_error", "post-lock-reversal")
		if err != nil || replay == nil || replay.ID != existing.ID || state.lockCalls < 2 {
			t.Fatalf("post-lock reversal replay = %+v, %v, lock calls %d", replay, err, state.lockCalls)
		}
		if got := f.account(t, billingWalletUserID); got.PaidCredits != 0 {
			t.Fatalf("reversal replay mutated account: %+v", got)
		}
		lot, err := f.repo.Billing().FindLotBySource(context.Background(), "fixture", "fixture-paid-u1")
		if err != nil || lot.AvailableCredits != 0 || lot.ConsumedCredits != 500 {
			t.Fatalf("reversal replay mutated lot: %+v, %v", lot, err)
		}
	})

	t.Run("top up", func(t *testing.T) {
		f := newBillingWalletFixture(t, 0, 0, 0)
		req := TopUpRequest{
			UserID: billingWalletUserID, Credits: 100, ExternalSourceType: "payment", ExternalSourceID: "post-lock-topup", CatalogID: "retail-test-v1",
			RequestFingerprint: billingFingerprint("post-lock-topup"), IdempotencyScope: "topup", IdempotencyKey: "post-lock-topup",
		}
		existing := &model.BillingWalletEntry{
			ID: "committed-topup-entry", UserID: req.UserID, EventKind: model.BillingWalletEventKindTopUp,
			PaidDelta: req.Credits, CatalogID: req.CatalogID, RequestFingerprint: req.RequestFingerprint,
			SourceType: &req.ExternalSourceType, SourceID: &req.ExternalSourceID,
			IdempotencyScope: req.IdempotencyScope, IdempotencyKey: req.IdempotencyKey,
		}
		state := &postLockCurrentReadState{keyEntry: existing, sourceEntry: existing}
		f.wallet.repo = &postLockCurrentReadRepository{Repository: f.repo, state: state}
		replay, err := f.wallet.TopUp(context.Background(), req)
		if err != nil || replay == nil || replay.EntryID != existing.ID || state.lockCalls < 2 {
			t.Fatalf("post-lock top-up replay = %+v, %v, lock calls %d", replay, err, state.lockCalls)
		}
		if got := f.account(t, billingWalletUserID); got.PaidCredits != 0 || got.DebtCredits != 0 {
			t.Fatalf("top-up replay mutated account: %+v", got)
		}
	})
}

func TestBillingWalletPostLockCurrentReadConflictsAreTypedAndDoNotMutate(t *testing.T) {
	t.Run("task caller commits conflict", func(t *testing.T) {
		f := newBillingWalletFixture(t, 500, 0, 0)
		quote := f.quote(t, billingWalletUserID, "task.article", "", "post-lock-task-conflict")
		req := taskChargeRequest(quote, "post-lock-task-conflict", "post-lock-task-conflict")
		sku, err := f.repo.Billing().FindSKU(context.Background(), req.CatalogID, req.SKUID)
		if err != nil {
			t.Fatal(err)
		}
		drift := newTaskCharge(req, sku, f.now)
		drift.ID = "committed-drift-task-charge"
		drift.ResourceID = "different-task"
		state := &postLockCurrentReadState{keyCharge: drift, identityCharge: drift}
		raceRepo := &postLockCurrentReadRepository{Repository: f.repo, state: state}
		var callErr error
		if err := raceRepo.WithTx(context.Background(), func(tx repository.Repository) error {
			_, callErr = f.wallet.ChargeTaskAdmissionInTx(context.Background(), tx, req)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(callErr, ErrBillingConflict) || state.lockCalls != 2 {
			t.Fatalf("post-lock task conflict = %v, lock calls %d", callErr, state.lockCalls)
		}
		assertTaskChargeUnchanged(t, f, quote, req, 500)
	})

	t.Run("charge identity split", func(t *testing.T) {
		expected := &model.BillingCharge{
			ID: "expected", UserID: billingWalletUserID, CatalogID: "catalog", SKUID: "sku", ResourceType: "image", ResourceID: "resource",
			Kind: model.BillingChargeKindOperation, Policy: "accepted_task_operation", Status: model.BillingChargeStatusPosted,
			IdempotencyScope: "scope", IdempotencyKey: "key", RequestFingerprint: billingFingerprint("locked-conflict"),
		}
		byKey := *expected
		byKey.ID = "by-key"
		byIdentity := *expected
		byIdentity.ID = "by-identity"
		if _, err := resolveLockedChargeReplay(&byKey, nil, &byIdentity, nil, expected); !errors.Is(err, ErrBillingConflict) {
			t.Fatalf("split charge identity error = %v", err)
		}
	})

	t.Run("top-up identity split", func(t *testing.T) {
		req := TopUpRequest{
			UserID: billingWalletUserID, Credits: 100, ExternalSourceType: "payment", ExternalSourceID: "source", CatalogID: "catalog",
			RequestFingerprint: billingFingerprint("locked-topup-conflict"), IdempotencyScope: "topup", IdempotencyKey: "key",
		}
		byKey := &model.BillingWalletEntry{ID: "by-key"}
		bySource := &model.BillingWalletEntry{ID: "by-source"}
		state := &postLockCurrentReadState{accountLocked: true, keyEntry: byKey, sourceEntry: bySource}
		repo := &postLockCurrentReadBillingRepository{state: state}
		if _, err := findLockedTopUpReplay(context.Background(), repo, req); !errors.Is(err, ErrBillingConflict) {
			t.Fatalf("split top-up identity error = %v", err)
		}
	})
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
	if got := f.account(t, billingWalletUserID); got.PromotionalCredits != 300 {
		t.Fatalf("account after expiry = %+v", got)
	}
	if count, err := f.wallet.ExpirePromotionalCredits(context.Background(), f.now, 10); err != nil || count != 0 {
		t.Fatalf("expiry replay = %d, %v", count, err)
	}
}

func TestBillingWalletEnqueueSettlementRejectsRootRepository(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	intent := SettlementIntent{
		Action:       model.BillingSettlementActionChargeOperation,
		ResourceType: "image", ResourceID: "root-repository", TaskID: uuid.NewString(),
		AttemptID: uuid.NewString(), ToolCallID: "root-repository", CatalogID: "retail-test-v1", SKUID: "image.cover.v1",
		RequestFingerprint: billingFingerprint("root-repository"), IdempotencyScope: "settlement", IdempotencyKey: "root-repository",
		ResultSnapshot: []byte(`{}`),
	}
	if _, err := f.wallet.EnqueueSettlementInTx(context.Background(), f.repo, intent); !errors.Is(err, repository.ErrBillingRequiresTransaction) {
		t.Fatalf("root repository enqueue error = %v", err)
	}
}

func TestBillingWalletSettlementSnapshotIsActionSpecific(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	charge := SettlementIntent{
		Action: model.BillingSettlementActionChargeOperation, ResourceType: "image", ResourceID: "snapshot-charge",
		TaskID: uuid.NewString(), AttemptID: uuid.NewString(), ToolCallID: "snapshot-charge",
		CatalogID: "retail-test-v1", SKUID: "image.cover.v1", RequestFingerprint: billingFingerprint("snapshot-charge"),
		IdempotencyScope: "settlement", IdempotencyKey: "snapshot-charge",
	}
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		_, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, charge)
		return err
	}); !errors.Is(err, ErrBillingInvalid) {
		t.Fatalf("charge without snapshot error = %v, want invalid", err)
	}
	reversal := SettlementIntent{
		Action: model.BillingSettlementActionReverseTask, ResourceType: "task", ResourceID: "snapshot-reversal",
		ChargeID: uuid.NewString(), Reason: "provider_error", RequestFingerprint: billingFingerprint("snapshot-reversal"),
		IdempotencyScope: "settlement", IdempotencyKey: "snapshot-reversal", ResultSnapshot: []byte(`{}`),
	}
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		_, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, reversal)
		return err
	}); !errors.Is(err, ErrBillingInvalid) {
		t.Fatalf("reversal with snapshot error = %v, want invalid", err)
	}
}

func TestBillingWalletOwningResourceAndSettlementRollbackTogether(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	taskID := uuid.NewString()
	intent := SettlementIntent{
		Action:       model.BillingSettlementActionChargeOperation,
		ResourceType: "image", ResourceID: "rollback-resource", TaskID: taskID,
		AttemptID: uuid.NewString(), ToolCallID: "rollback-resource", CatalogID: "retail-test-v1", SKUID: "image.cover.v1",
		RequestFingerprint: billingFingerprint("rollback-resource"), IdempotencyScope: "settlement", IdempotencyKey: "rollback-resource",
		ResultSnapshot: []byte(`{}`),
	}
	rollbackErr := errors.New("rollback owning resource")
	err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		if err := tx.Tasks().Create(context.Background(), &model.Task{
			ID: taskID, UserID: billingWalletUserID, Type: model.PlatformArticle, Status: model.TaskStatusPending,
		}); err != nil {
			return err
		}
		if _, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, intent); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("caller transaction error = %v", err)
	}
	if _, err := f.repo.Tasks().FindByID(context.Background(), taskID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("rolled-back task lookup error = %v", err)
	}
	if _, err := f.repo.Billing().FindSettlementByKey(context.Background(), intent.IdempotencyScope, intent.IdempotencyKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("rolled-back settlement lookup error = %v", err)
	}
}

func TestBillingWalletSettlementOutboxRetryNoDuplicateAndFencing(t *testing.T) {
	f := newBillingWalletFixture(t, 1000, 0, 0)
	taskID := uuid.NewString()
	if err := f.repo.Tasks().Create(context.Background(), &model.Task{
		ID: taskID, UserID: billingWalletUserID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	intent := SettlementIntent{
		Action:       model.BillingSettlementActionChargeOperation,
		ResourceType: "image", ResourceID: "outbox-image", TaskID: taskID,
		AttemptID: uuid.NewString(), ToolCallID: "outbox-call", CatalogID: "retail-test-v1", SKUID: "image.cover.v1",
		RequestFingerprint: billingFingerprint("outbox-operation"), IdempotencyScope: "settlement", IdempotencyKey: "outbox-operation",
		ResultSnapshot: []byte(`{}`),
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
	if got := f.account(t, billingWalletUserID); got.PaidCredits != 500 {
		t.Fatalf("outbox charged more than once: %+v", got)
	}
}

func TestBillingWalletSettlementOutboxUsesSettlementIDForDownstreamIdentity(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	for index, scope := range []string{"settlement-a", "settlement-b"} {
		taskID := uuid.NewString()
		if err := f.repo.Tasks().Create(context.Background(), &model.Task{
			ID: taskID, UserID: billingWalletUserID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted,
		}); err != nil {
			t.Fatal(err)
		}
		intent := SettlementIntent{
			Action: model.BillingSettlementActionChargeOperation, ResourceType: "image", ResourceID: fmt.Sprintf("shared-key-image-%d", index),
			TaskID: taskID, AttemptID: uuid.NewString(), ToolCallID: fmt.Sprintf("shared-key-call-%d", index),
			CatalogID: "retail-test-v1", SKUID: "image.cover.v1", RequestFingerprint: billingFingerprint("shared-key", scope),
			IdempotencyScope: scope, IdempotencyKey: "same-upstream-key",
			ResultSnapshot: []byte(`{}`),
		}
		if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
			_, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, intent)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	processed, err := f.wallet.ProcessSettlementOutbox(context.Background(), 10)
	if err != nil || processed != 2 {
		t.Fatalf("ProcessSettlementOutbox = %d, %v", processed, err)
	}
	if got := f.account(t, billingWalletUserID); got.DebtCredits != 1000 {
		t.Fatalf("downstream identity collision account = %+v", got)
	}
	for _, scope := range []string{"settlement-a", "settlement-b"} {
		row, err := f.repo.Billing().FindSettlementByKey(context.Background(), scope, "same-upstream-key")
		if err != nil || row.Status != "processed" {
			t.Fatalf("settlement %s = %+v, %v", scope, row, err)
		}
	}
}

func TestBillingWalletSettlementOutboxPersistsComparesAndAppliesReversalReason(t *testing.T) {
	f := newBillingWalletFixture(t, 500, 0, 0)
	quote := f.quote(t, billingWalletUserID, "task.article", "", "task-outbox-reversal")
	original, err := f.wallet.ChargeTaskAdmission(context.Background(), taskChargeRequest(quote, "task-outbox-reversal", "charge-outbox-reversal"))
	if err != nil {
		t.Fatal(err)
	}
	intent := SettlementIntent{
		Action: model.BillingSettlementActionReverseTask, ResourceType: "task", ResourceID: "task-outbox-reversal",
		ChargeID: original.ID, Reason: "provider_error", RequestFingerprint: billingFingerprint("outbox-reversal"),
		IdempotencyScope: "settlement", IdempotencyKey: "outbox-reversal",
	}
	var first *model.BillingSettlementOutbox
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		var err error
		first, err = f.wallet.EnqueueSettlementInTx(context.Background(), tx, intent)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if first.Reason != intent.Reason {
		t.Fatalf("persisted reason = %q, want %q", first.Reason, intent.Reason)
	}
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		replay, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, intent)
		if err == nil && replay.ID != first.ID {
			t.Fatalf("replay ID = %s, want %s", replay.ID, first.ID)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	drift := intent
	drift.Reason = "platform_error"
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		_, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, drift)
		return err
	}); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("reason drift error = %v, want conflict", err)
	}
	processed, err := f.wallet.ProcessSettlementOutbox(context.Background(), 1)
	if err != nil || processed != 1 {
		t.Fatalf("process reversal = %d, %v", processed, err)
	}
	if got := f.account(t, billingWalletUserID); got.PaidCredits != 500 {
		t.Fatalf("reversal reason not applied: %+v", got)
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
		ResultSnapshot: []byte(`{}`),
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
	if err != nil || row.Status != "failed" || row.Attempts != 1 || row.NextAttemptAt != nil || row.FailedAt == nil ||
		row.LastError == "" || row.ProcessedAt != nil {
		t.Fatalf("terminal row = %+v, %v", row, err)
	}
	if err := f.repo.Tasks().Create(context.Background(), &model.Task{
		ID: missingTask, UserID: billingWalletUserID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	processed, err = f.wallet.ProcessSettlementOutbox(context.Background(), 10)
	if err != nil || processed != 0 {
		t.Fatalf("terminal settlement replay = %d, %v", processed, err)
	}
	if got := f.account(t, billingWalletUserID); got.DebtCredits != 0 {
		t.Fatalf("terminal settlement mutated wallet = %+v", got)
	}
}

func TestBillingWalletSettlementOutboxContextCancellationLeavesLeaseForRecovery(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	taskID := uuid.NewString()
	intent := SettlementIntent{
		Action:       model.BillingSettlementActionChargeOperation,
		ResourceType: "image", ResourceID: "cancelled-image", TaskID: taskID,
		AttemptID: uuid.NewString(), ToolCallID: "cancelled-call", CatalogID: "retail-test-v1", SKUID: "image.cover.v1",
		RequestFingerprint: billingFingerprint("cancelled-operation"), IdempotencyScope: "settlement", IdempotencyKey: "cancelled-operation",
		ResultSnapshot: []byte(`{}`),
	}
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		_, err := f.wallet.EnqueueSettlementInTx(context.Background(), tx, intent)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.wallet.repo = &taskOverrideRepository{
		Repository: f.repo,
		tasks: &cancelingTaskRepository{
			TaskRepository: f.repo.Tasks(),
			cancel:         cancel,
		},
	}
	processed, err := f.wallet.ProcessSettlementOutbox(ctx, 1)
	if processed != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled processing = %d, %v", processed, err)
	}
	row, err := f.repo.Billing().FindSettlementByKey(context.Background(), intent.IdempotencyScope, intent.IdempotencyKey)
	if err != nil || row.Status != "processing" || row.Attempts != 1 || row.FailedAt != nil || row.ProcessedAt != nil {
		t.Fatalf("cancelled settlement lease = %+v, %v", row, err)
	}
	var reclaimed []model.BillingSettlementOutbox
	if err := f.repo.WithTx(context.Background(), func(tx repository.Repository) error {
		var err error
		reclaimed, err = tx.Billing().ClaimSettlements(context.Background(), f.now.Add(6*time.Minute), 1)
		return err
	}); err != nil || len(reclaimed) != 1 || reclaimed[0].Attempts != 2 {
		t.Fatalf("reclaimed cancelled lease = %+v, %v", reclaimed, err)
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

func TestBillingWalletWithTxRetriesAllRetryableDatabaseErrors(t *testing.T) {
	retryable := []struct {
		name string
		err  error
	}{
		{name: "sqlite busy", err: sqlite3.Error{Code: sqlite3.ErrBusy}},
		{name: "sqlite locked", err: sqlite3.Error{Code: sqlite3.ErrLocked}},
		{name: "mysql lock timeout", err: &mysqlDriver.MySQLError{Number: 1205}},
		{name: "mysql deadlock", err: &mysqlDriver.MySQLError{Number: 1213}},
		{name: "bad connection", err: driver.ErrBadConn},
	}
	for _, tt := range retryable {
		t.Run(tt.name, func(t *testing.T) {
			repo := &scriptedWithTxRepository{errors: []error{tt.err, nil}}
			service := &BillingWalletService{repo: repo}
			if err := service.withTx(context.Background(), func(repository.Repository) error { return nil }); err != nil {
				t.Fatalf("withTx: %v", err)
			}
			if repo.calls != 2 {
				t.Fatalf("WithTx calls = %d, want 2", repo.calls)
			}
		})
	}

	applicationErr := errors.New("application failure")
	repo := &scriptedWithTxRepository{errors: []error{applicationErr, nil}}
	service := &BillingWalletService{repo: repo}
	if err := service.withTx(context.Background(), func(repository.Repository) error { return nil }); !errors.Is(err, applicationErr) || repo.calls != 1 {
		t.Fatalf("application error retry = %v, calls %d", err, repo.calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo = &scriptedWithTxRepository{errors: []error{&mysqlDriver.MySQLError{Number: 1213}, nil}}
	service = &BillingWalletService{repo: repo}
	if err := service.withTx(ctx, func(repository.Repository) error { return nil }); !errors.Is(err, context.Canceled) || repo.calls != 1 {
		t.Fatalf("cancelled retry = %v, calls %d", err, repo.calls)
	}
}

func TestBillingWalletSettlementOutboxStaleWorkerFencing(t *testing.T) {
	f := newBillingWalletFixture(t, 0, 0, 0)
	intent := SettlementIntent{
		Action:       model.BillingSettlementActionReverseTask,
		ResourceType: "task", ResourceID: "stale-task", ChargeID: uuid.NewString(),
		Reason:             "provider_error",
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
	return newBillingWalletFixtureWithRepository(t, repo, paid, promotional, debt)
}

func newBillingWalletFixtureWithOperationPrice(t *testing.T, price int64) *billingWalletFixture {
	t.Helper()
	repo := newBillingServiceRepository(t)
	return newBillingWalletFixtureWithRepositoryAndOperationPrice(t, repo, 0, 0, 0, price)
}

func newBillingWalletFixtureWithRepository(t *testing.T, repo repository.Repository, paid, promotional, debt int64) *billingWalletFixture {
	t.Helper()
	return newBillingWalletFixtureWithRepositoryAndOperationPrice(t, repo, paid, promotional, debt, 500)
}

func newBillingWalletFixtureWithRepositoryAndOperationPrice(t *testing.T, repo repository.Repository, paid, promotional, debt, operationPrice int64) *billingWalletFixture {
	t.Helper()
	bundle := testBillingBundle()
	// Operation and standalone prices intentionally differ from task price.
	bundle.Products.SKUs[1].PriceCredits = operationPrice
	bundle.Products.SKUs[2].PriceCredits = 500
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := repo.Users().FindByID(context.Background(), billingWalletUserID); errors.Is(err, gorm.ErrRecordNotFound) {
		if err := repo.Users().Create(context.Background(), &model.User{
			ID: billingWalletUserID, Email: billingWalletUserID + "@billing.test", Password: "fixture", InviteCode: "WALLET01",
		}); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	catalog := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }, QuoteTTL: time.Minute})
	if _, err := catalog.Publish(context.Background()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	account := &model.BillingWalletAccount{UserID: billingWalletUserID, PaidCredits: paid, PromotionalCredits: promotional, DebtCredits: debt}
	if err := repo.Billing().CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if paid > 0 {
		createBillingLot(t, repo, model.BillingCreditLot{
			ID: uuid.NewString(), UserID: billingWalletUserID, Kind: model.BillingCreditLotKindPaid,
			SourceType: "fixture", SourceID: "fixture-paid-u1", CatalogID: bundle.Products.CatalogID,
			OriginalCredits: paid, AvailableCredits: paid, CreatedAt: now.Add(-time.Hour),
		})
	}
	if promotional > 0 {
		expires := now.Add(time.Hour)
		createBillingLot(t, repo, model.BillingCreditLot{
			ID: uuid.NewString(), UserID: billingWalletUserID, Kind: model.BillingCreditLotKindPromotional,
			SourceType: "promotion", SourceID: uuid.NewString(), ProgramID: "fixture", CatalogID: bundle.Products.CatalogID,
			OriginalCredits: promotional, AvailableCredits: promotional, ExpiresAt: &expires, CreatedAt: now.Add(-time.Hour),
		})
	}
	if debt > 0 {
		chargeID := uuid.NewString()
		taskID, attemptID, toolCallID := uuid.NewString(), uuid.NewString(), uuid.NewString()
		fingerprint := billingFingerprint("fixture-debt", chargeID)
		if err := repo.WithTx(context.Background(), func(tx repository.Repository) error {
			if err := tx.Billing().CreateCharge(context.Background(), &model.BillingCharge{
				ID: chargeID, UserID: billingWalletUserID, CatalogID: bundle.Products.CatalogID, SKUID: bundle.Products.SKUs[1].ID,
				ResourceType: "fixture", ResourceID: chargeID, Kind: model.BillingChargeKindOperation,
				Policy: "accepted_task_operation", Status: model.BillingChargeStatusPosted,
				PriceCredits: debt, DebtCredits: debt, OperationTaskID: &taskID, AttemptID: &attemptID, ToolCallID: &toolCallID,
				IdempotencyScope: "fixture-debt-charge", IdempotencyKey: chargeID,
				RequestFingerprint: fingerprint, CreatedAt: now.Add(-time.Hour),
			}, nil); err != nil {
				return err
			}
			return tx.Billing().AppendEntry(context.Background(), &model.BillingWalletEntry{
				ID: uuid.NewString(), UserID: billingWalletUserID, EventKind: model.BillingWalletEventKindDebtCreated,
				DebtDelta: debt, ChargeID: &chargeID, CatalogID: bundle.Products.CatalogID,
				RequestFingerprint: fingerprint, ResourceType: "fixture", ResourceID: chargeID,
				IdempotencyScope: "fixture-debt", IdempotencyKey: chargeID, CreatedAt: now.Add(-time.Hour),
			})
		}); err != nil {
			t.Fatal(err)
		}
	}
	wallet := NewBillingWalletService(repo, &bundle, BillingWalletOptions{Now: func() time.Time { return now }})
	return &billingWalletFixture{repo: repo, catalog: catalog, wallet: wallet, now: now}
}

type rejectNestedTxRepository struct {
	repository.Repository
	withTxCalls int
}

type taskOverrideRepository struct {
	repository.Repository
	tasks repository.TaskRepository
}

func (r *taskOverrideRepository) Tasks() repository.TaskRepository { return r.tasks }

type cancelingTaskRepository struct {
	repository.TaskRepository
	cancel context.CancelFunc
}

func (r *cancelingTaskRepository) FindByID(context.Context, string) (*model.Task, error) {
	r.cancel()
	return nil, context.Canceled
}

type scriptedWithTxRepository struct {
	repository.Repository
	errors []error
	calls  int
}

type noLedgerScanRepository struct {
	repository.Repository
	listEntriesCalls int
}

type ensureAccountRepository struct {
	repository.Repository
	userID string
}

func (r *ensureAccountRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&ensureAccountTxRepository{Repository: tx, userID: r.userID})
	})
}

type ensureAccountTxRepository struct {
	repository.Repository
	userID string
}

func (r *ensureAccountTxRepository) Billing() repository.BillingRepository {
	return &ensureAccountBillingRepository{BillingRepository: r.Repository.Billing(), userID: r.userID}
}

type ensureAccountBillingRepository struct {
	repository.BillingRepository
	userID string
}

type postLockCurrentReadState struct {
	accountLocked   bool
	lockCalls       int
	keyCharge       *model.BillingCharge
	identityCharge  *model.BillingCharge
	keyEntry        *model.BillingWalletEntry
	sourceEntry     *model.BillingWalletEntry
	debtAllocations []model.BillingDebtAllocation
}

type postLockCurrentReadRepository struct {
	repository.Repository
	state *postLockCurrentReadState
}

func (r *postLockCurrentReadRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&postLockCurrentReadTxRepository{Repository: tx, state: r.state})
	})
}

type postLockCurrentReadTxRepository struct {
	repository.Repository
	state *postLockCurrentReadState
}

func (r *postLockCurrentReadTxRepository) Billing() repository.BillingRepository {
	return &postLockCurrentReadBillingRepository{BillingRepository: r.Repository.Billing(), state: r.state}
}

type postLockCurrentReadBillingRepository struct {
	repository.BillingRepository
	state *postLockCurrentReadState
}

func (r *postLockCurrentReadBillingRepository) LockAccount(ctx context.Context, userID string) (*model.BillingWalletAccount, error) {
	account, err := r.BillingRepository.LockAccount(ctx, userID)
	if err == nil {
		r.state.accountLocked = true
	}
	return account, err
}

func (r *postLockCurrentReadBillingRepository) currentCharge(charge *model.BillingCharge) (*model.BillingCharge, error) {
	if !r.state.accountLocked {
		return nil, errors.New("current charge read occurred before account lock")
	}
	r.state.lockCalls++
	if charge == nil {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *charge
	return &copy, nil
}

func (r *postLockCurrentReadBillingRepository) LockChargeByKey(context.Context, string, string) (*model.BillingCharge, error) {
	return r.currentCharge(r.state.keyCharge)
}

func (r *postLockCurrentReadBillingRepository) LockChargeByTask(context.Context, string) (*model.BillingCharge, error) {
	return r.currentCharge(r.state.identityCharge)
}

func (r *postLockCurrentReadBillingRepository) LockChargeByOperation(context.Context, string, string, string, string, string) (*model.BillingCharge, error) {
	return r.currentCharge(r.state.identityCharge)
}

func (r *postLockCurrentReadBillingRepository) LockChargeByQuote(context.Context, string) (*model.BillingCharge, error) {
	return r.currentCharge(r.state.identityCharge)
}

func (r *postLockCurrentReadBillingRepository) LockReversal(context.Context, string) (*model.BillingCharge, error) {
	return r.currentCharge(r.state.identityCharge)
}

func (r *postLockCurrentReadBillingRepository) currentEntry(entry *model.BillingWalletEntry) (*model.BillingWalletEntry, error) {
	if !r.state.accountLocked {
		return nil, errors.New("current entry read occurred before account lock")
	}
	r.state.lockCalls++
	if entry == nil {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *entry
	return &copy, nil
}

func (r *postLockCurrentReadBillingRepository) LockEntryByKey(context.Context, string, string) (*model.BillingWalletEntry, error) {
	return r.currentEntry(r.state.keyEntry)
}

func (r *postLockCurrentReadBillingRepository) LockEntryBySource(context.Context, string, string) (*model.BillingWalletEntry, error) {
	return r.currentEntry(r.state.sourceEntry)
}

func (r *postLockCurrentReadBillingRepository) LockDebtAllocationsBySourceEntryID(context.Context, string, string) ([]model.BillingDebtAllocation, error) {
	if !r.state.accountLocked {
		return nil, errors.New("current debt allocation read occurred before account lock")
	}
	r.state.lockCalls++
	return append([]model.BillingDebtAllocation(nil), r.state.debtAllocations...), nil
}

func (r *ensureAccountBillingRepository) CreateAccount(ctx context.Context, account *model.BillingWalletAccount) error {
	if account.UserID == r.userID {
		return errors.New("direct account creation is not race safe")
	}
	return r.BillingRepository.CreateAccount(ctx, account)
}

func (r *noLedgerScanRepository) Billing() repository.BillingRepository {
	return &noLedgerScanBillingRepository{BillingRepository: r.Repository.Billing(), calls: &r.listEntriesCalls}
}

func assertTaskChargeUnchanged(t *testing.T, f *billingWalletFixture, quote *model.BillingQuote, req TaskChargeRequest, paid int64) {
	t.Helper()
	if got := f.account(t, req.UserID); got.PaidCredits != paid || got.DebtCredits != 0 {
		t.Fatalf("task replay mutated account: %+v", got)
	}
	persistedQuote, err := f.repo.Billing().FindQuoteByKey(context.Background(), quote.IdempotencyScope, quote.IdempotencyKey)
	if err != nil || persistedQuote.ConsumedAt != nil {
		t.Fatalf("task replay mutated quote: %+v, %v", persistedQuote, err)
	}
	lot, err := f.repo.Billing().FindLotBySource(context.Background(), "fixture", "fixture-paid-u1")
	if err != nil || lot.AvailableCredits != paid || lot.ConsumedCredits != 0 {
		t.Fatalf("task replay mutated lot: %+v, %v", lot, err)
	}
	if _, err := f.repo.Billing().FindChargeByKey(context.Background(), req.IdempotencyScope, req.IdempotencyKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("task replay persisted duplicate charge: %v", err)
	}
}

func assertOperationChargeUnchanged(t *testing.T, f *billingWalletFixture, quote *model.BillingQuote, req OperationChargeRequest, paid int64) {
	t.Helper()
	if got := f.account(t, req.UserID); got.PaidCredits != paid || got.DebtCredits != 0 {
		t.Fatalf("operation replay mutated account: %+v", got)
	}
	persistedQuote, err := f.repo.Billing().FindQuoteByKey(context.Background(), quote.IdempotencyScope, quote.IdempotencyKey)
	if err != nil || persistedQuote.ConsumedAt != nil {
		t.Fatalf("operation replay mutated quote: %+v, %v", persistedQuote, err)
	}
	lot, err := f.repo.Billing().FindLotBySource(context.Background(), "fixture", "fixture-paid-u1")
	if err != nil || lot.AvailableCredits != paid || lot.ConsumedCredits != 0 {
		t.Fatalf("operation replay mutated lot: %+v, %v", lot, err)
	}
	if _, err := f.repo.Billing().FindChargeByKey(context.Background(), req.IdempotencyScope, req.IdempotencyKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("operation replay persisted duplicate charge: %v", err)
	}
}

func (r *noLedgerScanRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&noLedgerScanTxRepository{Repository: tx, calls: &r.listEntriesCalls})
	})
}

type noLedgerScanTxRepository struct {
	repository.Repository
	calls *int
}

func (r *noLedgerScanTxRepository) Billing() repository.BillingRepository {
	return &noLedgerScanBillingRepository{BillingRepository: r.Repository.Billing(), calls: r.calls}
}

type noLedgerScanBillingRepository struct {
	repository.BillingRepository
	calls *int
}

func (r *noLedgerScanBillingRepository) ListEntriesByUser(context.Context, string, int, int) ([]model.BillingWalletEntry, error) {
	*r.calls++
	return nil, errors.New("wallet ledger scan forbidden")
}

func (r *scriptedWithTxRepository) WithTx(context.Context, func(repository.Repository) error) error {
	index := r.calls
	r.calls++
	if index >= len(r.errors) {
		return nil
	}
	return r.errors[index]
}

func (r *rejectNestedTxRepository) WithTx(context.Context, func(repository.Repository) error) error {
	r.withTxCalls++
	return errors.New("nested transaction rejected")
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
		ID: uuid.NewString(), UserID: billingWalletUserID, Kind: model.BillingCreditLotKindPromotional,
		SourceType: "promotion", SourceID: sourceID, ProgramID: "program", CatalogID: "retail-test-v1",
		OriginalCredits: credits, AvailableCredits: credits, ExpiresAt: &expires, CreatedAt: f.now.Add(-time.Minute),
	})
	account := f.account(t, billingWalletUserID)
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
