package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProviderCostRepositoryMySQLZeroRowsUsesSemanticReadback(t *testing.T) {
	for _, test := range []struct {
		name            string
		readFingerprint string
		wantConflict    bool
	}{
		{name: "identical replay", readFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{name: "parameter drift", readFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", wantConflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock: %v", err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
			if err != nil {
				t.Fatalf("open mysql: %v", err)
			}
			event := repositoryCostEvent("cost-1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO `billing_provider_cost_events`").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectCommit()
			mock.ExpectQuery("SELECT .* FROM `billing_provider_cost_events` WHERE idempotency_scope = \\? AND idempotency_key = \\?").
				WithArgs(event.IdempotencyScope, event.IdempotencyKey, 1).
				WillReturnRows(sqlmock.NewRows([]string{"id", "request_fingerprint"}).AddRow("persisted-cost", test.readFingerprint))

			got, err := NewBillingCostRepository(db).AppendEvent(context.Background(), event)
			if test.wantConflict {
				if !errors.Is(err, ErrProviderCostConflict) {
					t.Fatalf("AppendEvent error = %v, want ErrProviderCostConflict", err)
				}
			} else if err != nil || got.ID != "persisted-cost" {
				t.Fatalf("AppendEvent = %#v, %v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestProviderCostRepositoryDuplicateIsIdempotentAndDriftConflicts(t *testing.T) {
	db := newProviderCostRepositoryDB(t)
	repo := NewBillingCostRepository(db)
	ctx := context.Background()
	event := repositoryCostEvent("cost-1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	created, err := repo.AppendEvent(ctx, event)
	if err != nil {
		t.Fatalf("AppendEvent first: %v", err)
	}
	replayed, err := repo.AppendEvent(ctx, repositoryCostEvent("cost-2", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	if err != nil {
		t.Fatalf("AppendEvent replay: %v", err)
	}
	if replayed.ID != created.ID {
		t.Fatalf("replay ID = %q, want %q", replayed.ID, created.ID)
	}

	drifted := repositoryCostEvent("cost-3", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	drifted.CostMicroCNY++
	if _, err := repo.AppendEvent(ctx, drifted); !errors.Is(err, ErrProviderCostConflict) {
		t.Fatalf("AppendEvent drift error = %v, want ErrProviderCostConflict", err)
	}

	var count int64
	if err := db.Model(&model.BillingProviderCostEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 1 {
		t.Fatalf("event count = %d, want 1", count)
	}
}

func TestProviderCostRepositoryBaseIdentityCannotBeBypassedWithAnotherIdempotencyKey(t *testing.T) {
	db := newProviderCostRepositoryDB(t)
	repo := NewBillingCostRepository(db)
	ctx := context.Background()
	if _, err := repo.AppendEvent(ctx, repositoryCostEvent("cost-1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")); err != nil {
		t.Fatalf("append base: %v", err)
	}
	duplicate := repositoryCostEvent("cost-2", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	duplicate.IdempotencyKey = "caller-chose-another-key"
	if _, err := repo.AppendEvent(ctx, duplicate); !errors.Is(err, ErrProviderCostConflict) {
		t.Fatalf("same base identity with another idempotency key error = %v, want ErrProviderCostConflict", err)
	}
	var count int64
	if err := db.Model(&model.BillingProviderCostEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 1 {
		t.Fatalf("base event count = %d, want 1", count)
	}
}

func TestProviderCostRepositoryProviderRequestIdentityIsUniqueWithoutExecution(t *testing.T) {
	db := newProviderCostRepositoryDB(t)
	repo := NewBillingCostRepository(db)
	ctx := context.Background()
	requestIdentity, err := model.ProviderCostBaseIdentityKey(model.BillingProviderCostIdentityProviderRequest, "", "volcengine_ark", "doubao-seed-evolving", "request-1")
	if err != nil {
		t.Fatalf("request identity: %v", err)
	}
	event := repositoryCostEvent("request-cost-1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	event.IdentityKind = model.BillingProviderCostIdentityProviderRequest
	event.ExecutionID = ""
	event.ProviderRequestID = "request-1"
	event.BaseIdentityKey = &requestIdentity
	if _, err := repo.AppendEvent(ctx, event); err != nil {
		t.Fatalf("append request-based event: %v", err)
	}
	duplicate := *event
	duplicate.ID = "request-cost-2"
	duplicate.IdempotencyKey = "another-caller-key"
	duplicate.RequestFingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := repo.AppendEvent(ctx, &duplicate); !errors.Is(err, ErrProviderCostConflict) {
		t.Fatalf("duplicate provider request error = %v, want ErrProviderCostConflict", err)
	}
}

func TestProviderCostRepositoryRequestBasedEventCannotCreateExecutionStatus(t *testing.T) {
	db := newProviderCostRepositoryDB(t)
	repo := NewBillingCostRepository(db)
	requestIdentity, err := model.ProviderCostBaseIdentityKey(model.BillingProviderCostIdentityProviderRequest, "", "volcengine_ark", "doubao-seed-evolving", "request-2")
	if err != nil {
		t.Fatalf("request identity: %v", err)
	}
	event := repositoryCostEvent("request-cost-2", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	event.IdentityKind = model.BillingProviderCostIdentityProviderRequest
	event.ExecutionID = ""
	event.ProviderRequestID = "request-2"
	event.BaseIdentityKey = &requestIdentity
	status := &model.BillingExecutionCostStatus{ExecutionID: "fabricated-execution", Status: model.BillingProviderCostStatusReconciled}
	if _, err := repo.AppendEventAndUpsertExecutionCostStatus(context.Background(), event, status); err == nil {
		t.Fatal("request-based provider cost event created execution status")
	}
	var count int64
	if err := db.Model(&model.BillingProviderCostEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 0 {
		t.Fatalf("request-based event was appended despite invalid execution status transaction")
	}
}

func TestProviderCostRepositoryAppendsAdjustmentWithoutUpdatingBase(t *testing.T) {
	db := newProviderCostRepositoryDB(t)
	repo := NewBillingCostRepository(db)
	ctx := context.Background()
	base, err := repo.AppendEvent(ctx, repositoryCostEvent("cost-1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	if err != nil {
		t.Fatalf("append base: %v", err)
	}
	baseCost := base.CostMicroCNY
	originalID := base.ID
	adjustment := repositoryCostEvent("adjustment-1", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	adjustment.EventKind = model.BillingProviderCostEventKindAdjustment
	adjustment.IdentityKind = ""
	adjustment.BaseIdentityKey = nil
	adjustment.IdempotencyScope = "provider_cost_adjustment"
	adjustment.IdempotencyKey = "invoice-line-1"
	adjustment.OriginalEventID = &originalID
	adjustment.Source = model.BillingProviderCostSourceInvoiceAdjustment
	adjustment.CostMicroCNY = -133
	adjustment.UsageEvidence = []byte(`{"kind":"invoice_adjustment","reason":"invoice"}`)
	appended, err := repo.AppendEvent(ctx, adjustment)
	if err != nil {
		t.Fatalf("append adjustment: %v", err)
	}
	if appended.ID == base.ID || appended.OriginalEventID == nil || *appended.OriginalEventID != base.ID {
		t.Fatalf("adjustment link = %#v, base = %q", appended.OriginalEventID, base.ID)
	}
	persistedBase, err := repo.FindEventByID(ctx, base.ID)
	if err != nil {
		t.Fatalf("find base: %v", err)
	}
	if persistedBase.CostMicroCNY != baseCost || persistedBase.OriginalEventID != nil {
		t.Fatalf("base event was mutated: %#v", persistedBase)
	}
}

func newProviderCostRepositoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

func repositoryCostEvent(id, fingerprint string) *model.BillingProviderCostEvent {
	baseIdentity, err := model.ProviderCostBaseIdentityKey(model.BillingProviderCostIdentityExecutionModel, "exec-1", "volcengine_ark", "doubao-seed-evolving", "")
	if err != nil {
		panic(err)
	}
	return &model.BillingProviderCostEvent{
		ID: id, EventKind: model.BillingProviderCostEventKindBase,
		IdentityKind: model.BillingProviderCostIdentityExecutionModel,
		ExecutionID:  "exec-1", TaskID: "task-1", Provider: "volcengine_ark", Model: "doubao-seed-evolving",
		CatalogID: "cost-v1", IdempotencyScope: "execution_model", IdempotencyKey: "exec-1/evolving",
		BaseIdentityKey:    &baseIdentity,
		RequestFingerprint: fingerprint, Source: model.BillingProviderCostSourceClaudeResult,
		Status: model.BillingProviderCostStatusReconciled, CostMicroCNY: 44_133,
		UsageEvidence:       []byte(`{"kind":"token","input_tokens":4807,"cache_read_input_tokens":7792,"cache_creation_input_tokens":0,"output_tokens":198}`),
		CalculationSnapshot: []byte(`{"catalog_id":"cost-v1","cost_micro_cny":44133}`),
	}
}
