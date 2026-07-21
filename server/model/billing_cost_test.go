package model

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProviderCostModelsAutoMigrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	for _, table := range []string{"billing_provider_cost_events", "billing_execution_cost_status"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("AutoMigrate did not create %s", table)
		}
	}
}

func TestProviderCostEventValidationSeparatesBaseAndAdjustment(t *testing.T) {
	baseIdentity, err := ProviderCostBaseIdentityKey(BillingProviderCostIdentityExecutionModel, "exec-1", "volcengine_ark", "doubao-seed-evolving", "")
	if err != nil {
		t.Fatalf("ProviderCostBaseIdentityKey: %v", err)
	}
	base := BillingProviderCostEvent{
		ID: "cost-1", EventKind: BillingProviderCostEventKindBase,
		IdentityKind: BillingProviderCostIdentityExecutionModel,
		ExecutionID:  "exec-1", Provider: "volcengine_ark", Model: "doubao-seed-evolving",
		CatalogID: "cost-v1", IdempotencyScope: "execution_model", IdempotencyKey: "exec-1/model",
		BaseIdentityKey:    &baseIdentity,
		RequestFingerprint: "c63d4dd2bc0bfc97cc4f58cf109f0f9cbde560e75fd3a4fdda4a6086863717ee",
		Source:             BillingProviderCostSourceClaudeResult, Status: BillingProviderCostStatusReconciled,
		CostMicroCNY: 44_133, UsageEvidence: []byte(`{"kind":"token","input_tokens":4807,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":0}`),
		CalculationSnapshot: []byte(`{"catalog_id":"cost-v1","cost_micro_cny":44133}`),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid base event: %v", err)
	}

	adjustment := base
	adjustment.ID = "cost-adjustment-1"
	adjustment.EventKind = BillingProviderCostEventKindAdjustment
	adjustment.IdentityKind = ""
	adjustment.OriginalEventID = &base.ID
	adjustment.BaseIdentityKey = nil
	adjustment.Source = BillingProviderCostSourceInvoiceAdjustment
	adjustment.CostMicroCNY = -133
	adjustment.UsageEvidence = []byte(`{"kind":"invoice_adjustment","reason_code":"provider_invoice_reconciliation","delta_micro_cny":-133}`)
	if err := adjustment.Validate(); err != nil {
		t.Fatalf("valid adjustment event: %v", err)
	}

	adjustment.OriginalEventID = nil
	if err := adjustment.Validate(); err == nil {
		t.Fatal("adjustment without original event was accepted")
	}
}

func TestProviderCostEventSupportsProviderRequestIdentityWithoutExecution(t *testing.T) {
	baseIdentity, err := ProviderCostBaseIdentityKey(BillingProviderCostIdentityProviderRequest, "", "volcengine_ark", "doubao-seedream-5-0-pro-260628", "request-1")
	if err != nil {
		t.Fatalf("ProviderCostBaseIdentityKey: %v", err)
	}
	event := BillingProviderCostEvent{
		ID: "cost-request-1", EventKind: BillingProviderCostEventKindBase,
		IdentityKind: BillingProviderCostIdentityProviderRequest, ProviderRequestID: "request-1",
		Provider: "volcengine_ark", Model: "doubao-seedream-5-0-pro-260628", CatalogID: "cost-v1",
		IdempotencyScope: "provider_cost_base/volcengine_ark", IdempotencyKey: "request-1",
		BaseIdentityKey:    &baseIdentity,
		RequestFingerprint: "c63d4dd2bc0bfc97cc4f58cf109f0f9cbde560e75fd3a4fdda4a6086863717ee",
		Source:             BillingProviderCostSourceProviderResponse, Status: BillingProviderCostStatusReconciled,
		CostMicroCNY: 300_000, UsageEvidence: []byte(`{"kind":"output_pixels","width":1024,"height":1024,"pixels":1048576}`),
		CalculationSnapshot: []byte(`{"catalog_id":"cost-v1","cost_micro_cny":300000}`),
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("valid request-based event without execution: %v", err)
	}
	event.ExecutionID = "exec-must-not-be-an-alternate-identity"
	if err := event.Validate(); err == nil {
		t.Fatal("provider/request base event also carrying execution identity was accepted")
	}
}

func TestProviderCostBaseIdentityHashDoesNotHaveDelimiterCollisions(t *testing.T) {
	first, err := ProviderCostBaseIdentityKey(BillingProviderCostIdentityExecutionModel, "a/b", "c", "d", "")
	if err != nil {
		t.Fatalf("first identity: %v", err)
	}
	second, err := ProviderCostBaseIdentityKey(BillingProviderCostIdentityExecutionModel, "a", "b/c", "d", "")
	if err != nil {
		t.Fatalf("second identity: %v", err)
	}
	if first == second {
		t.Fatalf("typed identities collided: %q", first)
	}
}
