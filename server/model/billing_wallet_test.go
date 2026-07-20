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
			validateErr := tt.account.Validate()
			if (validateErr != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", validateErr, tt.wantErr)
			}
			got, displayErr := tt.account.DisplayBalance()
			if (displayErr != nil) != tt.wantErr {
				t.Fatalf("DisplayBalance() error = %v, wantErr %v", displayErr, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("DisplayBalance() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBillingWalletAccountDatabaseRejectsSpendableOverflow(t *testing.T) {
	db := openBillingModelTestDB(t)
	if err := db.AutoMigrate(&BillingWalletAccount{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	overflow := BillingWalletAccount{
		UserID:             uuid.NewString(),
		PaidCredits:        math.MaxInt64,
		PromotionalCredits: 1,
	}
	if err := db.Create(&overflow).Error; err == nil {
		t.Fatal("wallet account with overflowing spendable credits unexpectedly persisted")
	}

	negativeDisplay := BillingWalletAccount{
		UserID:      uuid.NewString(),
		PaidCredits: 1,
		DebtCredits: 2,
	}
	if err := db.Create(&negativeDisplay).Error; err != nil {
		t.Fatalf("persist valid negative display balance: %v", err)
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
		PriceCredits: 100, PaidCredits: 60, PromotionalCredits: 40,
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
				PriceCredits: 100, PaidCredits: 60, PromotionalCredits: 40,
				ReversalOfID: &originalID,
			},
		},
		{
			name:    "posted charge rejects conservation mismatch",
			charge:  BillingCharge{Kind: BillingChargeKindTask, Status: BillingChargeStatusPosted, PriceCredits: 100, PaidCredits: 99},
			wantErr: true,
		},
		{
			name:    "pending charge rejects conservation mismatch",
			charge:  BillingCharge{Kind: BillingChargeKindTask, Status: BillingChargeStatusPending, PriceCredits: 100, PaidCredits: 99},
			wantErr: true,
		},
		{
			name:    "failed charge rejects conservation mismatch",
			charge:  BillingCharge{Kind: BillingChargeKindTask, Status: BillingChargeStatusFailed, PriceCredits: 100, PaidCredits: 99},
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
		{
			name: "accepted operation may contain debt",
			charge: BillingCharge{
				Kind: BillingChargeKindOperation, Policy: "accepted_task_operation", Status: BillingChargeStatusPosted,
				PriceCredits: 100, DebtCredits: 100,
			},
		},
		{
			name:    "task charge cannot contain debt",
			charge:  BillingCharge{Kind: BillingChargeKindTask, Policy: "task_admission", Status: BillingChargeStatusPosted, PriceCredits: 100, DebtCredits: 100},
			wantErr: true,
		},
		{
			name:    "standalone operation cannot contain debt",
			charge:  BillingCharge{Kind: BillingChargeKindOperation, Policy: "standalone_operation", Status: BillingChargeStatusPosted, PriceCredits: 100, DebtCredits: 100},
			wantErr: true,
		},
		{
			name: "reversal cannot contain debt",
			charge: BillingCharge{
				Kind: BillingChargeKindReversal, Policy: "reversal", Status: BillingChargeStatusPosted,
				PriceCredits: 100, DebtCredits: 100, ReversalOfID: &originalID,
			},
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
		{BillingDebtAllocation{}, "billing_debt_allocations"},
		{BillingSettlementOutbox{}, "billing_settlement_outbox"},
		{BillingReferralIssue{}, "billing_referral_issues"},
		{BillingProviderCostEvent{}, "billing_provider_cost_events"},
		{BillingExecutionCostStatus{}, "billing_execution_cost_status"},
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
		"billing_debt_allocations",
		"billing_execution_cost_status",
		"billing_provider_cost_events",
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
	assertBillingIndex(t, db, &BillingProviderCostEvent{}, "idx_billing_provider_cost_idempotency")
	assertBillingIndex(t, db, &BillingProviderCostEvent{}, "idx_billing_provider_cost_base_identity")

	var taskIndexColumns []struct {
		Name string
	}
	if err := db.Raw(`PRAGMA index_info('idx_billing_charge_task_identity')`).Scan(&taskIndexColumns).Error; err != nil {
		t.Fatalf("inspect task charge index: %v", err)
	}
	gotTaskIndexColumns := make([]string, 0, len(taskIndexColumns))
	for _, column := range taskIndexColumns {
		gotTaskIndexColumns = append(gotTaskIndexColumns, column.Name)
	}
	if want := []string{"task_id", "charge_kind"}; !reflect.DeepEqual(gotTaskIndexColumns, want) {
		t.Fatalf("task charge unique index columns = %v, want %v", gotTaskIndexColumns, want)
	}
}

func TestBillingDebtAllocationPersistenceConstraints(t *testing.T) {
	db := openBillingModelTestDB(t)
	if err := db.AutoMigrate(&BillingDebtAllocation{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	valid := BillingDebtAllocation{
		ID: uuid.NewString(), UserID: uuid.NewString(), ChargeID: uuid.NewString(), EntryID: uuid.NewString(),
		SourceEntryID: uuid.NewString(), Kind: BillingDebtAllocationKindRepayment, Credits: 100, CreatedAt: time.Now().UTC(),
	}
	if err := db.Create(&valid).Error; err != nil {
		t.Fatalf("create allocation: %v", err)
	}
	for _, mutate := range []struct {
		name string
		fn   func(*BillingDebtAllocation)
	}{
		{name: "zero credits", fn: func(row *BillingDebtAllocation) { row.Credits = 0 }},
		{name: "invalid kind", fn: func(row *BillingDebtAllocation) { row.Kind = "other" }},
		{name: "empty user", fn: func(row *BillingDebtAllocation) { row.UserID = "" }},
		{name: "empty charge", fn: func(row *BillingDebtAllocation) { row.ChargeID = "" }},
		{name: "empty entry", fn: func(row *BillingDebtAllocation) { row.EntryID = "" }},
		{name: "empty source", fn: func(row *BillingDebtAllocation) { row.SourceEntryID = "" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			row := valid
			row.ID = uuid.NewString()
			row.EntryID = uuid.NewString()
			row.SourceEntryID = uuid.NewString()
			mutate.fn(&row)
			if err := db.Create(&row).Error; err == nil {
				t.Fatalf("invalid allocation persisted: %+v", row)
			}
		})
	}
	duplicateEntry := valid
	duplicateEntry.ID = uuid.NewString()
	duplicateEntry.SourceEntryID = uuid.NewString()
	if err := db.Create(&duplicateEntry).Error; err == nil {
		t.Fatal("duplicate allocation entry identity persisted")
	}
	duplicateSourceCharge := valid
	duplicateSourceCharge.ID = uuid.NewString()
	duplicateSourceCharge.EntryID = uuid.NewString()
	if err := db.Create(&duplicateSourceCharge).Error; err == nil {
		t.Fatal("duplicate source/charge/kind allocation persisted")
	}
	if got, want := billingIndexColumns(t, db, "idx_billing_debt_allocation_source_charge"), []string{"source_entry_id", "charge_id", "kind"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source allocation unique index = %v, want %v", got, want)
	}
}

func TestBillingSettlementReverseReasonConstraint(t *testing.T) {
	db := openBillingModelTestDB(t)
	if err := db.AutoMigrate(&BillingSettlementOutbox{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	reverse := BillingSettlementOutbox{
		ID: uuid.NewString(), Action: BillingSettlementActionReverseTask,
		ResourceType: "task", ResourceID: uuid.NewString(), ChargeID: stringPointer(uuid.NewString()),
		IdempotencyScope: "settlement", IdempotencyKey: uuid.NewString(), Status: "pending",
		RequestFingerprint: strings.Repeat("a", 64),
	}
	if err := db.Create(&reverse).Error; err == nil {
		t.Fatal("reverse settlement without reason persisted")
	}
	reverse.ID = uuid.NewString()
	reverse.IdempotencyKey = uuid.NewString()
	reverse.Reason = "provider_error"
	if err := db.Create(&reverse).Error; err != nil {
		t.Fatalf("reverse settlement with reason: %v", err)
	}
	charge := reverse
	charge.ID = uuid.NewString()
	charge.Action = BillingSettlementActionChargeOperation
	charge.Reason = ""
	charge.IdempotencyKey = uuid.NewString()
	if err := db.Create(&charge).Error; err != nil {
		t.Fatalf("operation settlement without reason: %v", err)
	}
}

func TestBillingChargeTaskIdentityConstraint(t *testing.T) {
	db := openBillingModelTestDB(t)
	if err := db.AutoMigrate(&BillingCharge{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	taskID := uuid.NewString()
	charge := validPersistedBillingCharge(taskID)
	if err := db.Create(&charge).Error; err != nil {
		t.Fatalf("create first task charge: %v", err)
	}

	duplicate := validPersistedBillingCharge(taskID)
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate task_id and charge kind unexpectedly persisted")
	}
}

func TestBillingAcceptedTaskOperationIdentityConstraints(t *testing.T) {
	db := openBillingModelTestDB(t)
	if err := db.AutoMigrate(&BillingCharge{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	for _, missingField := range []string{"operation_task_id", "attempt_id", "tool_call_id"} {
		t.Run("missing_"+missingField, func(t *testing.T) {
			charge := validPersistedAcceptedOperationCharge()
			switch missingField {
			case "operation_task_id":
				charge.OperationTaskID = nil
			case "attempt_id":
				charge.AttemptID = nil
			case "tool_call_id":
				charge.ToolCallID = nil
			}
			if err := db.Create(&charge).Error; err == nil {
				t.Fatalf("accepted-task operation without %s unexpectedly persisted", missingField)
			}
		})
	}

	original := validPersistedAcceptedOperationCharge()
	if err := db.Create(&original).Error; err != nil {
		t.Fatalf("create accepted-task operation: %v", err)
	}
	duplicate := original
	duplicate.ID = uuid.NewString()
	duplicate.IdempotencyKey = uuid.NewString()
	duplicate.ResourceID = uuid.NewString()
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate accepted-task operation identity unexpectedly persisted")
	}

	standalone := validPersistedAcceptedOperationCharge()
	standalone.Policy = "standalone_operation"
	standalone.OperationTaskID = nil
	standalone.AttemptID = nil
	standalone.ToolCallID = nil
	if err := db.Create(&standalone).Error; err != nil {
		t.Fatalf("standalone operation without task tool identity: %v", err)
	}

	if got, want := billingIndexColumns(t, db, "idx_billing_charge_tool_identity"), []string{
		"operation_task_id", "attempt_id", "tool_call_id", "catalog_id", "sku_id",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operation charge unique index columns = %v, want %v", got, want)
	}
}

func TestBillingChargeReversalPersistenceConstraints(t *testing.T) {
	t.Run("reversal requires original identity", func(t *testing.T) {
		db := openBillingModelTestDB(t)
		if err := db.AutoMigrate(&BillingCharge{}); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		charge := validPersistedBillingCharge(uuid.NewString())
		charge.Kind = BillingChargeKindReversal
		if err := db.Create(&charge).Error; err == nil {
			t.Fatal("reversal without reversal_of_id unexpectedly persisted")
		}
	})

	t.Run("original charge rejects reversal identity", func(t *testing.T) {
		db := openBillingModelTestDB(t)
		if err := db.AutoMigrate(&BillingCharge{}); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		charge := validPersistedBillingCharge(uuid.NewString())
		originalID := uuid.NewString()
		charge.ReversalOfID = &originalID
		if err := db.Create(&charge).Error; err == nil {
			t.Fatal("non-reversal charge with reversal_of_id unexpectedly persisted")
		}
	})

	t.Run("original charge can be reversed once", func(t *testing.T) {
		db := openBillingModelTestDB(t)
		if err := db.AutoMigrate(&BillingCharge{}); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		originalID := uuid.NewString()
		first := validPersistedReversalCharge(originalID)
		if err := db.Create(&first).Error; err != nil {
			t.Fatalf("create first reversal: %v", err)
		}
		duplicate := validPersistedReversalCharge(originalID)
		if err := db.Create(&duplicate).Error; err == nil {
			t.Fatal("duplicate reversal_of_id unexpectedly persisted")
		}
		if got, want := billingIndexColumns(t, db, "idx_billing_charge_reversal"), []string{"reversal_of_id"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("reversal unique index columns = %v, want %v", got, want)
		}
	})
}

func TestBillingChargeDatabaseRejectsUnbalancedComponents(t *testing.T) {
	for _, status := range []BillingChargeStatus{
		BillingChargeStatusPending,
		BillingChargeStatusPosted,
		BillingChargeStatusFailed,
	} {
		for _, kind := range []BillingChargeKind{BillingChargeKindTask, BillingChargeKindReversal} {
			t.Run(string(status)+"_"+string(kind), func(t *testing.T) {
				db := openBillingModelTestDB(t)
				if err := db.AutoMigrate(&BillingCharge{}); err != nil {
					t.Fatalf("AutoMigrate: %v", err)
				}

				unbalanced := validPersistedBillingCharge(uuid.NewString())
				unbalanced.Status = status
				unbalanced.Kind = kind
				if kind == BillingChargeKindReversal {
					originalID := uuid.NewString()
					unbalanced.ReversalOfID = &originalID
				}
				unbalanced.PriceCredits++
				if err := db.Create(&unbalanced).Error; err == nil {
					t.Fatal("unbalanced charge unexpectedly persisted")
				}
			})
		}
	}
}

func TestBillingChargeDatabaseRestrictsDebtToAcceptedOperations(t *testing.T) {
	for _, tt := range []struct {
		name    string
		kind    BillingChargeKind
		policy  string
		wantErr bool
	}{
		{name: "accepted operation", kind: BillingChargeKindOperation, policy: "accepted_task_operation"},
		{name: "task", kind: BillingChargeKindTask, policy: "task_admission", wantErr: true},
		{name: "standalone", kind: BillingChargeKindOperation, policy: "standalone_operation", wantErr: true},
		{name: "reversal", kind: BillingChargeKindReversal, policy: "reversal", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := openBillingModelTestDB(t)
			if err := db.AutoMigrate(&BillingCharge{}); err != nil {
				t.Fatalf("AutoMigrate: %v", err)
			}
			charge := validPersistedBillingCharge(uuid.NewString())
			charge.Kind = tt.kind
			charge.Policy = tt.policy
			charge.PaidCredits = 0
			charge.DebtCredits = charge.PriceCredits
			if tt.kind == BillingChargeKindReversal {
				originalID := uuid.NewString()
				charge.ReversalOfID = &originalID
				charge.TaskID = nil
			}
			if tt.policy == "accepted_task_operation" {
				operationTaskID, attemptID, toolCallID := uuid.NewString(), uuid.NewString(), uuid.NewString()
				charge.OperationTaskID, charge.AttemptID, charge.ToolCallID = &operationTaskID, &attemptID, &toolCallID
				charge.TaskID = nil
			}
			err := db.Create(&charge).Error
			if (err != nil) != tt.wantErr {
				t.Fatalf("persist debt charge error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBillingWalletEntryNonTopUpAllowsAbsentExternalSourceIdentity(t *testing.T) {
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

func TestBillingWalletEntryTopUpSourceIdentityConstraints(t *testing.T) {
	for _, invalid := range []struct {
		name       string
		sourceType *string
		sourceID   *string
	}{
		{name: "missing both"},
		{name: "missing source type", sourceID: stringPointer("payment-1")},
		{name: "missing source id", sourceType: stringPointer("wechatpay")},
		{name: "empty source type", sourceType: stringPointer(""), sourceID: stringPointer("payment-2")},
		{name: "empty source id", sourceType: stringPointer("wechatpay"), sourceID: stringPointer("")},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			db := openBillingModelTestDB(t)
			if err := db.AutoMigrate(&BillingWalletEntry{}); err != nil {
				t.Fatalf("AutoMigrate: %v", err)
			}
			entry := validPersistedDebtOnlyTopUpEntry()
			entry.SourceType = invalid.sourceType
			entry.SourceID = invalid.sourceID
			if err := db.Create(&entry).Error; err == nil {
				t.Fatal("top-up without a complete external source identity unexpectedly persisted")
			}
		})
	}

	db := openBillingModelTestDB(t)
	if err := db.AutoMigrate(&BillingWalletEntry{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	first := validPersistedDebtOnlyTopUpEntry()
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create debt-only top-up entry: %v", err)
	}
	duplicate := validPersistedDebtOnlyTopUpEntry()
	duplicate.SourceType = first.SourceType
	duplicate.SourceID = first.SourceID
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate external top-up identity unexpectedly persisted")
	}
	if got, want := billingIndexColumns(t, db, "idx_billing_wallet_entry_source"), []string{"source_type", "source_id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wallet entry source unique index columns = %v, want %v", got, want)
	}
}

func TestBillingWalletEntryTopUpAuditIdentityConstraints(t *testing.T) {
	tests := []struct {
		name        string
		catalogID   string
		fingerprint string
	}{
		{name: "missing catalog", fingerprint: strings.Repeat("a", 64)},
		{name: "missing fingerprint", catalogID: "retail-v1"},
		{name: "short fingerprint", catalogID: "retail-v1", fingerprint: strings.Repeat("a", 63)},
		{name: "long fingerprint", catalogID: "retail-v1", fingerprint: strings.Repeat("a", 65)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openBillingModelTestDB(t)
			if err := db.AutoMigrate(&BillingWalletEntry{}); err != nil {
				t.Fatalf("AutoMigrate: %v", err)
			}
			entry := validPersistedDebtOnlyTopUpEntry()
			entry.CatalogID = tt.catalogID
			entry.RequestFingerprint = tt.fingerprint
			if err := db.Create(&entry).Error; err == nil {
				t.Fatalf("top-up with catalog %q and fingerprint length %d unexpectedly persisted", tt.catalogID, len(tt.fingerprint))
			}
		})
	}
}

func TestBillingReferralIssueIdentityConstraint(t *testing.T) {
	db := openBillingModelTestDB(t)
	if err := db.AutoMigrate(&BillingReferralIssue{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	inviteeUserID := uuid.NewString()
	first := validPersistedBillingReferralIssue(inviteeUserID)
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create referral issue: %v", err)
	}
	duplicate := validPersistedBillingReferralIssue(inviteeUserID)
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate invitee and referral program unexpectedly persisted")
	}
	if got, want := billingIndexColumns(t, db, "idx_billing_referral_invitee_program"), []string{"invitee_user_id", "program_id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("referral unique index columns = %v, want %v", got, want)
	}
}

func TestBillingWalletModelsExcludeLegacyAndProviderCostContracts(t *testing.T) {
	models := []any{
		BillingWalletAccount{}, BillingCreditLot{}, BillingWalletEntry{},
		BillingCatalogVersion{}, BillingSKU{}, BillingQuote{}, BillingCharge{},
		BillingChargeAllocation{}, BillingDebtAllocation{}, BillingSettlementOutbox{}, BillingReferralIssue{},
	}
	forbiddenFieldFragments := []string{
		"hold", "reserved", "reservation", "capture", "release", "payment",
		"shortfall", "totalcostusd", "providercost", "costbudget", "monetarybudget",
		"taskchargeidentity",
	}
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

	for _, sourcePath := range []string{"billing_wallet.go"} {
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read billing model source %s: %v", sourcePath, err)
		}
		lowerSource := strings.ToLower(string(source))
		for _, forbidden := range []string{
			"type billinghold", "reservedcredits", "reservation", "capture", "release",
			"payment_required", "shortfall", "total_cost_usd", "provider_cost", "cost_budget",
			"taskchargeidentity",
		} {
			if strings.Contains(lowerSource, forbidden) {
				t.Errorf("billing model source %s contains forbidden contract %q", sourcePath, forbidden)
			}
		}
	}

	db := openBillingModelTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	var billingTables []string
	if err := db.Raw(`SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'billing_%'`).Scan(&billingTables).Error; err != nil {
		t.Fatalf("list billing tables: %v", err)
	}
	for _, table := range billingTables {
		lowerTable := strings.ToLower(table)
		for _, forbidden := range []string{"hold", "reserved", "reservation", "capture", "release", "payment", "shortfall", "cost_budget"} {
			if strings.Contains(lowerTable, forbidden) {
				t.Errorf("migration created forbidden billing table %q", table)
			}
		}
	}
}

func validPersistedBillingCharge(taskID string) BillingCharge {
	return BillingCharge{
		ID:                 uuid.NewString(),
		UserID:             uuid.NewString(),
		CatalogID:          "retail-v1",
		SKUID:              "task.seednote.standard.v1",
		ResourceType:       "task",
		ResourceID:         taskID,
		Kind:               BillingChargeKindTask,
		Policy:             "task_admission",
		Status:             BillingChargeStatusPending,
		PriceCredits:       100,
		PaidCredits:        100,
		TaskID:             &taskID,
		IdempotencyScope:   "task-charge",
		IdempotencyKey:     uuid.NewString(),
		RequestFingerprint: strings.Repeat("a", 64),
	}
}

func validPersistedAcceptedOperationCharge() BillingCharge {
	operationTaskID := uuid.NewString()
	attemptID := uuid.NewString()
	toolCallID := uuid.NewString()
	return BillingCharge{
		ID:                 uuid.NewString(),
		UserID:             uuid.NewString(),
		CatalogID:          "retail-v1",
		SKUID:              "image.seedream.standard.v1",
		ResourceType:       "image",
		ResourceID:         uuid.NewString(),
		Kind:               BillingChargeKindOperation,
		Policy:             "accepted_task_operation",
		Status:             BillingChargeStatusPending,
		PriceCredits:       100,
		PaidCredits:        100,
		OperationTaskID:    &operationTaskID,
		AttemptID:          &attemptID,
		ToolCallID:         &toolCallID,
		IdempotencyScope:   "operation-charge",
		IdempotencyKey:     uuid.NewString(),
		RequestFingerprint: strings.Repeat("b", 64),
	}
}

func validPersistedReversalCharge(originalID string) BillingCharge {
	charge := validPersistedBillingCharge(uuid.NewString())
	charge.Kind = BillingChargeKindReversal
	charge.ReversalOfID = &originalID
	return charge
}

func validPersistedDebtOnlyTopUpEntry() BillingWalletEntry {
	return BillingWalletEntry{
		ID:                 uuid.NewString(),
		UserID:             uuid.NewString(),
		EventKind:          BillingWalletEventKindTopUp,
		DebtDelta:          -100,
		CatalogID:          "retail-v1",
		RequestFingerprint: strings.Repeat("d", 64),
		SourceType:         stringPointer("wechatpay"),
		SourceID:           stringPointer(uuid.NewString()),
		IdempotencyScope:   "top-up",
		IdempotencyKey:     uuid.NewString(),
	}
}

func validPersistedBillingReferralIssue(inviteeUserID string) BillingReferralIssue {
	inviteeLotID, inviterLotID := uuid.NewString(), uuid.NewString()
	issuedAt := time.Now().UTC()
	return BillingReferralIssue{
		ID: uuid.NewString(), ProgramID: "referral-first-topup-v1", CatalogID: "promotion-v1",
		InviteeUserID: inviteeUserID, InviterUserID: uuid.NewString(), QualifyingTopUpEntryID: uuid.NewString(),
		RequestFingerprint: strings.Repeat("e", 64), InviteeLotID: &inviteeLotID, InviterLotID: &inviterLotID,
		Status: BillingReferralStatusIssued, IssuedAt: &issuedAt,
	}
}

func stringPointer(value string) *string { return &value }

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

func billingIndexColumns(t *testing.T, db *gorm.DB, name string) []string {
	t.Helper()
	var columns []struct {
		Name string
	}
	if err := db.Raw("PRAGMA index_info('" + name + "')").Scan(&columns).Error; err != nil {
		t.Fatalf("inspect index %s: %v", name, err)
	}
	result := make([]string, 0, len(columns))
	for _, column := range columns {
		result = append(result, column.Name)
	}
	return result
}
