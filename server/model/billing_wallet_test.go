package model

import (
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBillingWalletAccountDisplayBalanceAndValidation(t *testing.T) {
	tests := []struct {
		name    string
		account BillingWalletAccount
		want    int64
		wantErr bool
	}{
		{
			name:    "positive balance",
			account: BillingWalletAccount{PaidCredits: 800, PromotionalCredits: 200, DebtCredits: 100},
			want:    900,
		},
		{
			name:    "negative display balance",
			account: BillingWalletAccount{PaidCredits: 25, PromotionalCredits: 5, DebtCredits: 40},
			want:    -10,
		},
		{
			name:    "negative paid bucket",
			account: BillingWalletAccount{PaidCredits: -1},
			wantErr: true,
		},
		{
			name:    "negative promotional bucket",
			account: BillingWalletAccount{PromotionalCredits: -1},
			wantErr: true,
		},
		{
			name:    "negative debt bucket",
			account: BillingWalletAccount{DebtCredits: -1},
			wantErr: true,
		},
		{
			name:    "negative version",
			account: BillingWalletAccount{Version: -1},
			wantErr: true,
		},
		{
			name:    "display balance overflow",
			account: BillingWalletAccount{PaidCredits: math.MaxInt64, PromotionalCredits: 1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.account.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.account.DisplayBalance() != tt.want {
				t.Fatalf("DisplayBalance() = %d, want %d", tt.account.DisplayBalance(), tt.want)
			}
		})
	}
}

func TestBillingCreditLotValidation(t *testing.T) {
	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour)
	tests := []struct {
		name    string
		lot     BillingCreditLot
		wantErr bool
	}{
		{
			name: "paid lot conserves credits",
			lot: BillingCreditLot{
				Kind: BillingCreditLotKindPaid, SourceType: "topup", SourceID: "payment-1",
				CatalogID: "retail-v1", OriginalCredits: 100, AvailableCredits: 40,
				ConsumedCredits: 60,
			},
		},
		{
			name: "promotional lot requires program and expiry",
			lot: BillingCreditLot{
				Kind: BillingCreditLotKindPromotional, SourceType: "referral", SourceID: "issue-1",
				ProgramID: "referral-v1", CatalogID: "promotion-v1", OriginalCredits: 100,
				AvailableCredits: 100, ExpiresAt: &expiresAt,
			},
		},
		{
			name: "conservation mismatch",
			lot: BillingCreditLot{
				Kind: BillingCreditLotKindPaid, SourceType: "topup", SourceID: "payment-2",
				CatalogID: "retail-v1", OriginalCredits: 100, AvailableCredits: 99,
			},
			wantErr: true,
		},
		{
			name: "paid lot cannot expire",
			lot: BillingCreditLot{
				Kind: BillingCreditLotKindPaid, SourceType: "topup", SourceID: "payment-3",
				CatalogID: "retail-v1", OriginalCredits: 100, AvailableCredits: 100,
				ExpiresAt: &expiresAt,
			},
			wantErr: true,
		},
		{
			name: "paid lot cannot contain expired credits",
			lot: BillingCreditLot{
				Kind: BillingCreditLotKindPaid, SourceType: "topup", SourceID: "payment-expired",
				CatalogID: "retail-v1", OriginalCredits: 100, AvailableCredits: 90,
				ExpiredCredits: 10,
			},
			wantErr: true,
		},
		{
			name: "promotional lot requires expiry",
			lot: BillingCreditLot{
				Kind: BillingCreditLotKindPromotional, SourceType: "referral", SourceID: "issue-2",
				ProgramID: "referral-v1", CatalogID: "promotion-v1", OriginalCredits: 100,
				AvailableCredits: 100,
			},
			wantErr: true,
		},
		{
			name: "promotional lot requires program",
			lot: BillingCreditLot{
				Kind: BillingCreditLotKindPromotional, SourceType: "referral", SourceID: "issue-3",
				CatalogID: "promotion-v1", OriginalCredits: 100, AvailableCredits: 100,
				ExpiresAt: &expiresAt,
			},
			wantErr: true,
		},
		{
			name: "negative component",
			lot: BillingCreditLot{
				Kind: BillingCreditLotKindPaid, SourceType: "topup", SourceID: "payment-4",
				CatalogID: "retail-v1", OriginalCredits: 100, AvailableCredits: 101,
				ConsumedCredits: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.lot.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBillingChargeValidation(t *testing.T) {
	originalID := uuid.NewString()
	valid := BillingCharge{
		Kind: BillingChargeKindTask, Status: BillingChargeStatusPosted,
		PriceCredits: 100, PaidCredits: 60, PromotionalCredits: 30, DebtCredits: 10,
	}
	tests := []struct {
		name    string
		charge  BillingCharge
		wantErr bool
	}{
		{name: "posted original conserves price", charge: valid},
		{
			name: "posted reversal uses positive exact magnitudes and original identity",
			charge: BillingCharge{
				Kind: BillingChargeKindReversal, Status: BillingChargeStatusPosted,
				PriceCredits: 100, PaidCredits: 60, PromotionalCredits: 30, DebtCredits: 10,
				ReversalOfID: &originalID,
			},
		},
		{
			name:    "posted charge rejects conservation mismatch",
			charge:  BillingCharge{Kind: BillingChargeKindTask, Status: BillingChargeStatusPosted, PriceCredits: 100, PaidCredits: 99},
			wantErr: true,
		},
		{
			name:    "negative paid component",
			charge:  BillingCharge{Kind: BillingChargeKindTask, Status: BillingChargeStatusPosted, PriceCredits: 100, PaidCredits: -1, DebtCredits: 101},
			wantErr: true,
		},
		{
			name:    "negative promotional component",
			charge:  BillingCharge{Kind: BillingChargeKindTask, Status: BillingChargeStatusPosted, PriceCredits: 100, PromotionalCredits: -1, DebtCredits: 101},
			wantErr: true,
		},
		{
			name:    "negative debt component",
			charge:  BillingCharge{Kind: BillingChargeKindTask, Status: BillingChargeStatusPosted, PriceCredits: 100, PaidCredits: 101, DebtCredits: -1},
			wantErr: true,
		},
		{
			name:    "reversal requires original identity",
			charge:  BillingCharge{Kind: BillingChargeKindReversal, Status: BillingChargeStatusPosted, PriceCredits: 100, PaidCredits: 100},
			wantErr: true,
		},
		{
			name: "original charge cannot name reversal identity",
			charge: BillingCharge{
				Kind: BillingChargeKindTask, Status: BillingChargeStatusPosted,
				PriceCredits: 100, PaidCredits: 100, ReversalOfID: &originalID,
			},
			wantErr: true,
		},
		{
			name:    "component sum overflow",
			charge:  BillingCharge{Kind: BillingChargeKindTask, Status: BillingChargeStatusPosted, PriceCredits: math.MaxInt64, PaidCredits: math.MaxInt64, PromotionalCredits: 1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.charge.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBillingModelTableNames(t *testing.T) {
	tests := []struct {
		model interface{ TableName() string }
		want  string
	}{
		{BillingWalletAccount{}, "billing_wallet_accounts"},
		{BillingCreditLot{}, "billing_credit_lots"},
		{BillingWalletEntry{}, "billing_wallet_entries"},
		{BillingCatalogVersion{}, "billing_catalog_versions"},
		{BillingSKU{}, "billing_skus"},
		{BillingQuote{}, "billing_quotes"},
		{BillingCharge{}, "billing_charges"},
		{BillingChargeAllocation{}, "billing_charge_allocations"},
		{BillingSettlementOutbox{}, "billing_settlement_outbox"},
		{BillingReferralIssue{}, "billing_referral_issues"},
	}

	for _, tt := range tests {
		if got := tt.model.TableName(); got != tt.want {
			t.Errorf("%T.TableName() = %q, want %q", tt.model, got, tt.want)
		}
	}
}

func TestBillingAutoMigrateCreatesExactTablesAndIndexes(t *testing.T) {
	db := openBillingModelTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	wantTables := []string{
		"billing_catalog_versions",
		"billing_charge_allocations",
		"billing_charges",
		"billing_credit_lots",
		"billing_quotes",
		"billing_referral_issues",
		"billing_settlement_outbox",
		"billing_skus",
		"billing_wallet_accounts",
		"billing_wallet_entries",
	}
	var gotTables []string
	if err := db.Raw(`SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'billing_%' ORDER BY name`).Scan(&gotTables).Error; err != nil {
		t.Fatalf("list billing tables: %v", err)
	}
	if !reflect.DeepEqual(gotTables, wantTables) {
		t.Fatalf("billing tables = %v, want %v", gotTables, wantTables)
	}

	assertBillingIndex(t, db, &BillingCreditLot{}, "idx_billing_credit_lot_source")
	assertBillingIndex(t, db, &BillingWalletEntry{}, "idx_billing_wallet_entry_idempotency")
	assertBillingIndex(t, db, &BillingQuote{}, "idx_billing_quote_idempotency")
	assertBillingIndex(t, db, &BillingCharge{}, "idx_billing_charge_idempotency")
	assertBillingIndex(t, db, &BillingCharge{}, "idx_billing_charge_task_identity")
	assertBillingIndex(t, db, &BillingCharge{}, "idx_billing_charge_tool_identity")
	assertBillingIndex(t, db, &BillingCharge{}, "idx_billing_charge_reversal")
	assertBillingIndex(t, db, &BillingSettlementOutbox{}, "idx_billing_settlement_outbox_idempotency")
	assertBillingIndex(t, db, &BillingReferralIssue{}, "idx_billing_referral_invitee_program")
}

func TestBillingWalletEntryAllowsAbsentExternalSourceIdentity(t *testing.T) {
	db := openBillingModelTestDB(t)
	if err := db.AutoMigrate(&BillingWalletEntry{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	entries := []BillingWalletEntry{
		{ID: uuid.NewString(), UserID: uuid.NewString(), EventKind: BillingWalletEventKindCharge, IdempotencyScope: "charge", IdempotencyKey: uuid.NewString()},
		{ID: uuid.NewString(), UserID: uuid.NewString(), EventKind: BillingWalletEventKindCharge, IdempotencyScope: "charge", IdempotencyKey: uuid.NewString()},
	}
	if err := db.Create(&entries).Error; err != nil {
		t.Fatalf("create entries without external source identities: %v", err)
	}
}

func TestBillingModelsExcludeLegacyAndProviderCostContracts(t *testing.T) {
	models := []any{
		BillingWalletAccount{}, BillingCreditLot{}, BillingWalletEntry{},
		BillingCatalogVersion{}, BillingSKU{}, BillingQuote{}, BillingCharge{},
		BillingChargeAllocation{}, BillingSettlementOutbox{}, BillingReferralIssue{},
	}
	forbiddenFieldFragments := []string{"hold", "reserved", "payment", "shortfall", "totalcostusd", "providercost", "monetarybudget"}
	for _, value := range models {
		typeOf := reflect.TypeOf(value)
		for i := 0; i < typeOf.NumField(); i++ {
			fieldName := strings.ToLower(typeOf.Field(i).Name)
			for _, forbidden := range forbiddenFieldFragments {
				if strings.Contains(fieldName, forbidden) {
					t.Errorf("%s contains forbidden field %s", typeOf.Name(), typeOf.Field(i).Name)
				}
			}
		}
	}

	source, err := os.ReadFile("billing_wallet.go")
	if err != nil {
		t.Fatalf("read billing model source: %v", err)
	}
	lowerSource := strings.ToLower(string(source))
	for _, forbidden := range []string{"type billinghold", "reservedcredits", "payment_required", "shortfall", "total_cost_usd"} {
		if strings.Contains(lowerSource, forbidden) {
			t.Errorf("billing model source contains forbidden contract %q", forbidden)
		}
	}
}

func openBillingModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return db
}

func assertBillingIndex(t *testing.T, db *gorm.DB, model any, name string) {
	t.Helper()
	if !db.Migrator().HasIndex(model, name) {
		t.Errorf("%T missing index %s", model, name)
	}
}
