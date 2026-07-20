package repository

import (
	"context"
	"errors"
	"strings"
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
		readByBase      bool
		wantConflict    bool
	}{
		{name: "identical replay", readFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{name: "parameter drift", readFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", wantConflict: true},
		{name: "same base with another key", readFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", readByBase: true},
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
			if test.readByBase {
				event.IdempotencyKey = "another-key"
			}
			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO `billing_provider_cost_events`").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectCommit()
			idempotencyRows := sqlmock.NewRows([]string{"id", "request_fingerprint"})
			if !test.readByBase {
				idempotencyRows.AddRow("persisted-cost", test.readFingerprint)
			}
			mock.ExpectQuery("SELECT .* FROM `billing_provider_cost_events` WHERE idempotency_scope = \\? AND idempotency_key = \\?").
				WithArgs(event.IdempotencyScope, event.IdempotencyKey, 1).WillReturnRows(idempotencyRows)
			if test.readByBase {
				mock.ExpectQuery("SELECT .* FROM `billing_provider_cost_events` WHERE base_identity_key = \\?").
					WithArgs(*event.BaseIdentityKey, 1).
					WillReturnRows(sqlmock.NewRows([]string{"id", "request_fingerprint"}).AddRow("persisted-cost", test.readFingerprint))
			}

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

func TestProviderCostRepositoryMySQLFinalizationUsesCurrentLockingRead(t *testing.T) {
	for _, test := range []struct {
		name            string
		initialSnapshot bool
		currentRow      bool
		wantMissing     bool
	}{
		{name: "existing unreconciled snapshot candidate", initialSnapshot: true, currentRow: true},
		{name: "duplicate insert without initial status read", currentRow: true},
		{name: "current row unexpectedly missing", wantMissing: true},
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
			incoming := &model.BillingExecutionCostStatus{
				ExecutionID: "exec-current-read", Status: model.BillingProviderCostStatusReconciled,
				FinalizationFingerprint: strings.Repeat("a", 64),
			}
			mock.ExpectBegin()
			if test.initialSnapshot {
				mock.ExpectQuery("SELECT status FROM billing_execution_cost_status WHERE execution_id = \\?").
					WithArgs(incoming.ExecutionID).
					WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(model.BillingProviderCostStatusUnreconciled))
			}
			mock.ExpectExec("INSERT INTO `billing_execution_cost_status`").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec("UPDATE `billing_execution_cost_status` SET").WillReturnResult(sqlmock.NewResult(0, 0))
			rows := sqlmock.NewRows([]string{"execution_id", "status", "finalization_fingerprint"})
			if test.currentRow {
				rows.AddRow(incoming.ExecutionID, model.BillingProviderCostStatusReconciled, strings.Repeat("b", 64))
			}
			mock.ExpectQuery("SELECT .* FROM `billing_execution_cost_status` WHERE execution_id = \\?.* FOR UPDATE").
				WithArgs(incoming.ExecutionID, 1).WillReturnRows(rows)
			mock.ExpectRollback()

			err = db.Transaction(func(tx *gorm.DB) error {
				if test.initialSnapshot {
					var candidate string
					if err := tx.Raw("SELECT status FROM billing_execution_cost_status WHERE execution_id = ?", incoming.ExecutionID).Scan(&candidate).Error; err != nil {
						return err
					}
					if candidate != string(model.BillingProviderCostStatusUnreconciled) {
						t.Fatalf("snapshot candidate = %q", candidate)
					}
				}
				return NewBillingCostRepository(tx).UpsertExecutionCostStatus(context.Background(), incoming)
			})
			if test.wantMissing {
				if !errors.Is(err, ErrProviderCostStatusMissing) {
					t.Fatalf("error = %v, want ErrProviderCostStatusMissing", err)
				}
			} else if !errors.Is(err, ErrProviderCostConflict) {
				t.Fatalf("error = %v, want ErrProviderCostConflict", err)
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

func TestProviderCostRepositoryBaseIdentityReplayCanUseAnotherIdempotencyKey(t *testing.T) {
	db := newProviderCostRepositoryDB(t)
	repo := NewBillingCostRepository(db)
	ctx := context.Background()
	if _, err := repo.AppendEvent(ctx, repositoryCostEvent("cost-1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")); err != nil {
		t.Fatalf("append base: %v", err)
	}
	duplicate := repositoryCostEvent("cost-2", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	duplicate.IdempotencyKey = "caller-chose-another-key"
	replayed, err := repo.AppendEvent(ctx, duplicate)
	if err != nil || replayed.ID != "cost-1" {
		t.Fatalf("same base identity replay = %#v, %v", replayed, err)
	}
	var count int64
	if err := db.Model(&model.BillingProviderCostEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 1 {
		t.Fatalf("base event count = %d, want 1", count)
	}
}

func TestProviderCostRepositoryBatchRollsBackFirstEventWhenSecondConflicts(t *testing.T) {
	db := newProviderCostRepositoryDB(t)
	repo := NewBillingCostRepository(db)
	ctx := context.Background()
	second := repositoryCostEventForModel("existing-second", "exec-batch", "doubao-seed-2-1-turbo-260628", "exec-batch/turbo", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if _, err := repo.AppendEvent(ctx, second); err != nil {
		t.Fatalf("seed second identity: %v", err)
	}
	first := repositoryCostEventForModel("new-first", "exec-batch", "doubao-seed-evolving", "exec-batch/evolving", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	conflictingSecond := repositoryCostEventForModel("new-second", "exec-batch", "doubao-seed-2-1-turbo-260628", "exec-batch/turbo", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	status := &model.BillingExecutionCostStatus{ExecutionID: "exec-batch", Status: model.BillingProviderCostStatusReconciled, FinalizationFingerprint: strings.Repeat("d", 64)}
	result, err := repo.AppendEventsAndUpsertExecutionCostStatus(ctx, []*model.BillingProviderCostEvent{first, conflictingSecond}, status)
	if !errors.Is(err, ErrProviderCostConflict) {
		t.Fatalf("batch error = %v, want conflict", err)
	}
	if result != nil {
		t.Fatalf("failed batch returned partial result %#v", result)
	}
	events, err := repo.ListEventsByExecution(ctx, "exec-batch")
	if err != nil || len(events) != 1 || events[0].ID != second.ID {
		t.Fatalf("events after rollback = %#v, %v", events, err)
	}
	if _, err := repo.FindExecutionCostStatus(ctx, "exec-batch"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("status error = %v, want not found", err)
	}
}

func TestProviderCostRepositoryBatchRollsBackOnSecondDatabaseFailure(t *testing.T) {
	db := newProviderCostRepositoryDB(t)
	repo := NewBillingCostRepository(db)
	creates := 0
	callbackName := "test:fail_second_provider_cost_create"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == (model.BillingProviderCostEvent{}).TableName() {
			creates++
			if creates == 2 {
				tx.AddError(errors.New("injected second create failure"))
			}
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })
	first := repositoryCostEventForModel("new-first", "exec-db-failure", "doubao-seed-evolving", "exec-db-failure/evolving", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	second := repositoryCostEventForModel("new-second", "exec-db-failure", "doubao-seed-2-1-turbo-260628", "exec-db-failure/turbo", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	status := &model.BillingExecutionCostStatus{ExecutionID: "exec-db-failure", Status: model.BillingProviderCostStatusReconciled, FinalizationFingerprint: strings.Repeat("d", 64)}
	result, err := repo.AppendEventsAndUpsertExecutionCostStatus(context.Background(), []*model.BillingProviderCostEvent{first, second}, status)
	if err == nil {
		t.Fatal("injected second create failure succeeded")
	}
	if result != nil {
		t.Fatalf("failed batch returned partial result %#v", result)
	}
	events, err := repo.ListEventsByExecution(context.Background(), "exec-db-failure")
	if err != nil || len(events) != 0 {
		t.Fatalf("events after database rollback = %#v, %v", events, err)
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
	status := &model.BillingExecutionCostStatus{ExecutionID: "fabricated-execution", Status: model.BillingProviderCostStatusReconciled, FinalizationFingerprint: strings.Repeat("d", 64)}
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
	adjustment.UsageEvidence = []byte(`{"kind":"invoice_adjustment","reason_code":"provider_invoice_reconciliation","delta_micro_cny":-133}`)
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

func TestProviderCostRepositoryExecutionFinalizationFingerprintConflicts(t *testing.T) {
	repo := NewBillingCostRepository(newProviderCostRepositoryDB(t))
	ctx := context.Background()
	first := &model.BillingExecutionCostStatus{
		ExecutionID: "exec-fingerprint", Status: model.BillingProviderCostStatusReconciled,
		FinalizationFingerprint: strings.Repeat("a", 64),
	}
	if err := repo.UpsertExecutionCostStatus(ctx, first); err != nil {
		t.Fatalf("first reconciled status: %v", err)
	}
	if err := repo.UpsertExecutionCostStatus(ctx, first); err != nil {
		t.Fatalf("same fingerprint replay: %v", err)
	}
	drift := *first
	drift.FinalizationFingerprint = strings.Repeat("b", 64)
	if err := repo.UpsertExecutionCostStatus(ctx, &drift); !errors.Is(err, ErrProviderCostConflict) {
		t.Fatalf("fingerprint drift error = %v, want conflict", err)
	}
	for _, invalid := range []*model.BillingExecutionCostStatus{
		{ExecutionID: "exec-empty-fingerprint", Status: model.BillingProviderCostStatusReconciled},
		{ExecutionID: "exec-unreconciled-fingerprint", Status: model.BillingProviderCostStatusUnreconciled, ReasonCode: model.BillingExecutionCostReasonMissingTerminalModelUsage, FinalizationFingerprint: strings.Repeat("c", 64)},
		{ExecutionID: "exec-free-reason", Status: model.BillingProviderCostStatusUnreconciled, ReasonCode: model.BillingExecutionCostReasonCode("free_text")},
	} {
		if err := repo.UpsertExecutionCostStatus(ctx, invalid); err == nil {
			t.Fatalf("invalid status was accepted: %#v", invalid)
		}
	}
}

func TestProviderCostRepositoryRejectsUnsafeOrMalformedEvidence(t *testing.T) {
	tests := []struct {
		name     string
		evidence string
		mutate   func(*model.BillingProviderCostEvent)
	}{
		{name: "prompt field", evidence: `{"kind":"token","input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":0,"prompt":"secret"}`},
		{name: "url field", evidence: `{"kind":"output_pixels","width":10,"height":10,"pixels":100,"url":"https://secret"}`},
		{name: "token field", evidence: `{"kind":"invoice_adjustment","reason_code":"provider_invoice_reconciliation","delta_micro_cny":-1,"token":"secret"}`, mutate: makeRepositoryAdjustment(-1)},
		{name: "negative token count", evidence: `{"kind":"token","input_tokens":-1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":0}`},
		{name: "pixel mismatch", evidence: `{"kind":"output_pixels","width":10,"height":10,"pixels":99}`},
		{name: "unknown reason code", evidence: `{"kind":"invoice_adjustment","reason_code":"free_text","delta_micro_cny":-1}`, mutate: makeRepositoryAdjustment(-1)},
		{name: "adjustment delta mismatch", evidence: `{"kind":"invoice_adjustment","reason_code":"provider_invoice_reconciliation","delta_micro_cny":-2}`, mutate: makeRepositoryAdjustment(-1)},
		{name: "invoice source with token evidence", evidence: `{"kind":"token","input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":0}`, mutate: func(event *model.BillingProviderCostEvent) {
			event.Source = model.BillingProviderCostSourceInvoiceAdjustment
		}},
		{name: "claude source with pixel evidence", evidence: `{"kind":"output_pixels","width":10,"height":10,"pixels":100}`, mutate: func(event *model.BillingProviderCostEvent) {
			event.Source = model.BillingProviderCostSourceClaudeResult
		}},
		{name: "provider source with adjustment evidence", evidence: `{"kind":"invoice_adjustment","reason_code":"provider_invoice_reconciliation","delta_micro_cny":-1}`, mutate: func(event *model.BillingProviderCostEvent) {
			makeRepositoryAdjustment(-1)(event)
			event.Source = model.BillingProviderCostSourceProviderResponse
		}},
		{name: "oversized evidence", evidence: `{"kind":"token","input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":0,"padding":"` + strings.Repeat("x", 8_000) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newProviderCostRepositoryDB(t)
			event := repositoryCostEvent("unsafe", strings.Repeat("a", 64))
			if test.mutate != nil {
				test.mutate(event)
			}
			event.UsageEvidence = []byte(test.evidence)
			if _, err := NewBillingCostRepository(db).AppendEvent(context.Background(), event); err == nil {
				t.Fatal("unsafe evidence was persisted")
			}
			var count int64
			if err := db.Model(&model.BillingProviderCostEvent{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("persisted event count = %d, %v", count, err)
			}
		})
	}
}

func TestProviderCostRepositoryRejectsOversizedCalculationSnapshot(t *testing.T) {
	db := newProviderCostRepositoryDB(t)
	event := repositoryCostEvent("oversized-calculation", strings.Repeat("a", 64))
	event.CalculationSnapshot = []byte(`{"padding":"` + strings.Repeat("x", 70_000) + `"}`)
	if _, err := NewBillingCostRepository(db).AppendEvent(context.Background(), event); err == nil {
		t.Fatal("oversized calculation snapshot was persisted")
	}
}

func makeRepositoryAdjustment(cost int64) func(*model.BillingProviderCostEvent) {
	return func(event *model.BillingProviderCostEvent) {
		originalID := "original-cost"
		event.EventKind = model.BillingProviderCostEventKindAdjustment
		event.IdentityKind = ""
		event.BaseIdentityKey = nil
		event.OriginalEventID = &originalID
		event.Source = model.BillingProviderCostSourceInvoiceAdjustment
		event.CostMicroCNY = cost
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
	return repositoryCostEventForModel(id, "exec-1", "doubao-seed-evolving", "exec-1/evolving", fingerprint)
}

func repositoryCostEventForModel(id, executionID, modelID, idempotencyKey, fingerprint string) *model.BillingProviderCostEvent {
	baseIdentity, err := model.ProviderCostBaseIdentityKey(model.BillingProviderCostIdentityExecutionModel, executionID, "volcengine_ark", modelID, "")
	if err != nil {
		panic(err)
	}
	return &model.BillingProviderCostEvent{
		ID: id, EventKind: model.BillingProviderCostEventKindBase,
		IdentityKind: model.BillingProviderCostIdentityExecutionModel,
		ExecutionID:  executionID, TaskID: "task-1", Provider: "volcengine_ark", Model: modelID,
		CatalogID: "cost-v1", IdempotencyScope: "execution_model", IdempotencyKey: idempotencyKey,
		BaseIdentityKey:    &baseIdentity,
		RequestFingerprint: fingerprint, Source: model.BillingProviderCostSourceClaudeResult,
		Status: model.BillingProviderCostStatusReconciled, CostMicroCNY: 44_133,
		UsageEvidence:       []byte(`{"kind":"token","input_tokens":4807,"cache_read_input_tokens":7792,"cache_creation_input_tokens":0,"output_tokens":198}`),
		CalculationSnapshot: []byte(`{"catalog_id":"cost-v1","cost_micro_cny":44133}`),
	}
}
