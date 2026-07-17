package model

import (
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/datatypes"
)

type BillingCreditLotKind string

const (
	BillingCreditLotKindPaid        BillingCreditLotKind = "paid"
	BillingCreditLotKindPromotional BillingCreditLotKind = "promotional"
)

type BillingWalletEventKind string

const (
	BillingWalletEventKindTopUp         BillingWalletEventKind = "topup"
	BillingWalletEventKindPromotion     BillingWalletEventKind = "promotion"
	BillingWalletEventKindCharge        BillingWalletEventKind = "charge"
	BillingWalletEventKindDebtCreated   BillingWalletEventKind = "debt_created"
	BillingWalletEventKindDebtRepayment BillingWalletEventKind = "debt_repayment"
	BillingWalletEventKindReversal      BillingWalletEventKind = "reversal"
	BillingWalletEventKindExpiry        BillingWalletEventKind = "expiry"
)

type BillingChargeKind string

const (
	BillingChargeKindTask      BillingChargeKind = "task"
	BillingChargeKindOperation BillingChargeKind = "operation"
	BillingChargeKindReversal  BillingChargeKind = "reversal"
)

type BillingChargeStatus string

const (
	BillingChargeStatusPending BillingChargeStatus = "pending"
	BillingChargeStatusPosted  BillingChargeStatus = "posted"
	BillingChargeStatusFailed  BillingChargeStatus = "failed"
)

type BillingSettlementAction string

const (
	BillingSettlementActionChargeOperation BillingSettlementAction = "charge_operation"
	BillingSettlementActionReverseTask     BillingSettlementAction = "reverse_task"
)

// BillingWalletAccount is a rebuildable projection of a user's wallet.
type BillingWalletAccount struct {
	UserID             string    `gorm:"type:char(36);primaryKey" json:"user_id"`
	PaidCredits        int64     `gorm:"not null;default:0;check:chk_billing_wallet_paid_spendable,paid_credits >= 0 AND paid_credits <= 9223372036854775807 - promotional_credits" json:"paid_credits"`
	PromotionalCredits int64     `gorm:"not null;default:0;check:chk_billing_wallet_promotional,promotional_credits >= 0" json:"promotional_credits"`
	DebtCredits        int64     `gorm:"not null;default:0;check:chk_billing_wallet_debt,debt_credits >= 0" json:"debt_credits"`
	Version            int64     `gorm:"not null;default:0;check:chk_billing_wallet_version,version >= 0" json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (BillingWalletAccount) TableName() string { return "billing_wallet_accounts" }

// DisplayBalance returns paid plus promotional credits less debt and rejects
// invalid projections rather than returning an ambiguous numeric value.
func (a BillingWalletAccount) DisplayBalance() (int64, error) {
	if a.PaidCredits < 0 {
		return 0, fmt.Errorf("paid credits must be nonnegative")
	}
	if a.PromotionalCredits < 0 {
		return 0, fmt.Errorf("promotional credits must be nonnegative")
	}
	if a.DebtCredits < 0 {
		return 0, fmt.Errorf("debt credits must be nonnegative")
	}
	if a.Version < 0 {
		return 0, fmt.Errorf("version must be nonnegative")
	}
	return a.validatedDisplayBalance()
}

func (a BillingWalletAccount) Validate() error {
	_, err := a.DisplayBalance()
	return err
}

func (a BillingWalletAccount) validatedDisplayBalance() (int64, error) {
	spendable, ok := checkedAddInt64(a.PaidCredits, a.PromotionalCredits)
	if !ok {
		return 0, fmt.Errorf("wallet display balance overflows int64")
	}
	balance, ok := checkedSubInt64(spendable, a.DebtCredits)
	if !ok {
		return 0, fmt.Errorf("wallet display balance overflows int64")
	}
	return balance, nil
}

// BillingCreditLot tracks exact conservation for one immutable credit source.
type BillingCreditLot struct {
	ID               string               `gorm:"type:char(36);primaryKey" json:"id"`
	UserID           string               `gorm:"type:char(36);index;not null" json:"user_id"`
	Kind             BillingCreditLotKind `gorm:"type:varchar(20);index;not null" json:"kind"`
	SourceType       string               `gorm:"type:varchar(40);uniqueIndex:idx_billing_credit_lot_source,priority:1;not null" json:"source_type"`
	SourceID         string               `gorm:"type:varchar(128);uniqueIndex:idx_billing_credit_lot_source,priority:2;not null" json:"source_id"`
	ProgramID        string               `gorm:"type:varchar(128);index" json:"program_id,omitempty"`
	CatalogID        string               `gorm:"type:varchar(128);index;not null" json:"catalog_id"`
	OriginalCredits  int64                `gorm:"not null;check:chk_billing_credit_lot_conservation,original_credits >= 0 AND original_credits = available_credits + consumed_credits + expired_credits" json:"original_credits"`
	AvailableCredits int64                `gorm:"not null;check:chk_billing_credit_lot_available,available_credits >= 0" json:"available_credits"`
	ConsumedCredits  int64                `gorm:"not null;check:chk_billing_credit_lot_consumed,consumed_credits >= 0" json:"consumed_credits"`
	ExpiredCredits   int64                `gorm:"not null;check:chk_billing_credit_lot_expired,expired_credits >= 0" json:"expired_credits"`
	ExpiresAt        *time.Time           `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt        time.Time            `json:"created_at"`
}

func (BillingCreditLot) TableName() string { return "billing_credit_lots" }

func (l BillingCreditLot) Validate() error {
	if l.OriginalCredits < 0 || l.AvailableCredits < 0 || l.ConsumedCredits < 0 || l.ExpiredCredits < 0 {
		return fmt.Errorf("credit lot buckets must be nonnegative")
	}
	if strings.TrimSpace(l.SourceType) == "" || strings.TrimSpace(l.SourceID) == "" {
		return fmt.Errorf("credit lot source identity is required")
	}
	if strings.TrimSpace(l.CatalogID) == "" {
		return fmt.Errorf("credit lot catalog identity is required")
	}
	consumedAndExpired, ok := checkedAddInt64(l.ConsumedCredits, l.ExpiredCredits)
	if !ok {
		return fmt.Errorf("credit lot conservation overflows int64")
	}
	accounted, ok := checkedAddInt64(l.AvailableCredits, consumedAndExpired)
	if !ok || accounted != l.OriginalCredits {
		return fmt.Errorf("original credits must equal available, consumed, and expired credits")
	}
	switch l.Kind {
	case BillingCreditLotKindPaid:
		if l.ExpiresAt != nil {
			return fmt.Errorf("paid credit lots cannot expire")
		}
		if l.ExpiredCredits != 0 {
			return fmt.Errorf("paid credit lots cannot contain expired credits")
		}
	case BillingCreditLotKindPromotional:
		if strings.TrimSpace(l.ProgramID) == "" {
			return fmt.Errorf("promotional credit lots require a program identity")
		}
		if l.ExpiresAt == nil {
			return fmt.Errorf("promotional credit lots require an expiry")
		}
	default:
		return fmt.Errorf("unsupported credit lot kind %q", l.Kind)
	}
	return nil
}

// BillingWalletEntry is an append-only wallet event. Delta fields are signed.
type BillingWalletEntry struct {
	ID                 string                 `gorm:"type:char(36);primaryKey" json:"id"`
	UserID             string                 `gorm:"type:char(36);index:idx_billing_wallet_entry_user_created,priority:1;not null" json:"user_id"`
	EventKind          BillingWalletEventKind `gorm:"type:varchar(32);index;not null;check:chk_billing_wallet_entry_topup_source,event_kind <> 'topup' OR (source_type IS NOT NULL AND TRIM(source_type) <> '' AND source_id IS NOT NULL AND TRIM(source_id) <> '')" json:"event_kind"`
	PaidDelta          int64                  `gorm:"not null" json:"paid_delta"`
	PromotionalDelta   int64                  `gorm:"not null" json:"promotional_delta"`
	DebtDelta          int64                  `gorm:"not null" json:"debt_delta"`
	CatalogID          string                 `gorm:"type:varchar(128);index;check:chk_billing_wallet_entry_topup_catalog,event_kind <> 'topup' OR TRIM(catalog_id) <> ''" json:"catalog_id,omitempty"`
	RequestFingerprint string                 `gorm:"type:char(64);check:chk_billing_wallet_entry_topup_fingerprint,event_kind <> 'topup' OR LENGTH(request_fingerprint) = 64" json:"request_fingerprint,omitempty"`
	ChargeID           *string                `gorm:"type:char(36);index" json:"charge_id,omitempty"`
	LotID              *string                `gorm:"type:char(36);index" json:"lot_id,omitempty"`
	ResourceType       string                 `gorm:"type:varchar(40);index:idx_billing_wallet_entry_resource,priority:1" json:"resource_type,omitempty"`
	ResourceID         string                 `gorm:"type:varchar(128);index:idx_billing_wallet_entry_resource,priority:2" json:"resource_id,omitempty"`
	SourceType         *string                `gorm:"type:varchar(40);uniqueIndex:idx_billing_wallet_entry_source,priority:1" json:"source_type,omitempty"`
	SourceID           *string                `gorm:"type:varchar(128);uniqueIndex:idx_billing_wallet_entry_source,priority:2" json:"source_id,omitempty"`
	IdempotencyScope   string                 `gorm:"type:varchar(80);uniqueIndex:idx_billing_wallet_entry_idempotency,priority:1;not null" json:"idempotency_scope"`
	IdempotencyKey     string                 `gorm:"type:varchar(128);uniqueIndex:idx_billing_wallet_entry_idempotency,priority:2;not null" json:"idempotency_key"`
	ActorType          string                 `gorm:"type:varchar(40)" json:"actor_type,omitempty"`
	ActorID            string                 `gorm:"type:varchar(128)" json:"actor_id,omitempty"`
	SourceService      string                 `gorm:"type:varchar(80)" json:"source_service,omitempty"`
	RequestID          string                 `gorm:"type:varchar(128);index" json:"request_id,omitempty"`
	CorrelationID      string                 `gorm:"type:varchar(128);index" json:"correlation_id,omitempty"`
	CreatedAt          time.Time              `gorm:"index:idx_billing_wallet_entry_user_created,priority:2" json:"created_at"`
}

func (BillingWalletEntry) TableName() string { return "billing_wallet_entries" }

// BillingCatalogVersion is an immutable published retail-catalog snapshot.
type BillingCatalogVersion struct {
	CatalogID   string         `gorm:"type:varchar(128);primaryKey" json:"catalog_id"`
	Currency    string         `gorm:"type:varchar(20);not null" json:"currency"`
	Status      string         `gorm:"type:varchar(20);index;not null" json:"status"`
	PublishedAt time.Time      `gorm:"index;not null" json:"published_at"`
	Snapshot    datatypes.JSON `gorm:"type:json;not null" json:"snapshot"`
	CreatedAt   time.Time      `json:"created_at"`
}

func (BillingCatalogVersion) TableName() string { return "billing_catalog_versions" }

// BillingSKU is one immutable SKU row within a published catalog.
type BillingSKU struct {
	ID           string         `gorm:"type:char(36);primaryKey" json:"id"`
	CatalogID    string         `gorm:"type:varchar(128);uniqueIndex:idx_billing_sku_catalog_sku,priority:1;not null" json:"catalog_id"`
	SKUID        string         `gorm:"type:varchar(128);uniqueIndex:idx_billing_sku_catalog_sku,priority:2;not null" json:"sku_id"`
	Operation    string         `gorm:"type:varchar(128);index;not null" json:"operation"`
	PriceCredits int64          `gorm:"not null;check:chk_billing_sku_price,price_credits >= 0" json:"price_credits"`
	Policy       string         `gorm:"type:varchar(40);index;not null" json:"policy"`
	Route        string         `gorm:"type:varchar(128);index" json:"route,omitempty"`
	Delivery     string         `gorm:"type:varchar(128);not null" json:"delivery"`
	Snapshot     datatypes.JSON `gorm:"type:json;not null" json:"snapshot"`
	CreatedAt    time.Time      `json:"created_at"`
}

func (BillingSKU) TableName() string { return "billing_skus" }

// BillingQuote pins a fixed SKU price for one idempotent client request.
type BillingQuote struct {
	ID                 string         `gorm:"type:char(36);primaryKey" json:"id"`
	UserID             string         `gorm:"type:char(36);index;not null" json:"user_id"`
	CatalogID          string         `gorm:"type:varchar(128);index;not null" json:"catalog_id"`
	SKUID              string         `gorm:"type:varchar(128);index;not null" json:"sku_id"`
	PriceCredits       int64          `gorm:"not null;check:chk_billing_quote_price,price_credits >= 0" json:"price_credits"`
	RequestFingerprint string         `gorm:"type:char(64);not null" json:"request_fingerprint"`
	SKUSnapshot        datatypes.JSON `gorm:"type:json;not null" json:"sku_snapshot"`
	ExpiresAt          time.Time      `gorm:"index;not null" json:"expires_at"`
	CreatedAt          time.Time      `json:"created_at"`
	ConsumedAt         *time.Time     `json:"consumed_at,omitempty"`
	ResourceType       string         `gorm:"type:varchar(40);index:idx_billing_quote_resource,priority:1" json:"resource_type,omitempty"`
	ResourceID         string         `gorm:"type:varchar(128);index:idx_billing_quote_resource,priority:2" json:"resource_id,omitempty"`
	IdempotencyScope   string         `gorm:"type:varchar(80);uniqueIndex:idx_billing_quote_idempotency,priority:1;not null" json:"idempotency_scope"`
	IdempotencyKey     string         `gorm:"type:varchar(128);uniqueIndex:idx_billing_quote_idempotency,priority:2;not null" json:"idempotency_key"`
}

func (BillingQuote) TableName() string { return "billing_quotes" }

// BillingCharge stores positive magnitudes for original and reversal rows.
// A reversal points to its original; signed wallet effects live in entries.
type BillingCharge struct {
	ID                 string              `gorm:"type:char(36);primaryKey" json:"id"`
	UserID             string              `gorm:"type:char(36);index;not null" json:"user_id"`
	CatalogID          string              `gorm:"type:varchar(128);index;uniqueIndex:idx_billing_charge_tool_identity,priority:4;not null" json:"catalog_id"`
	SKUID              string              `gorm:"column:sku_id;type:varchar(128);index;uniqueIndex:idx_billing_charge_tool_identity,priority:5;not null" json:"sku_id"`
	QuoteID            *string             `gorm:"type:char(36);index" json:"quote_id,omitempty"`
	ResourceType       string              `gorm:"type:varchar(40);index:idx_billing_charge_resource,priority:1;not null" json:"resource_type"`
	ResourceID         string              `gorm:"type:varchar(128);index:idx_billing_charge_resource,priority:2;not null" json:"resource_id"`
	Kind               BillingChargeKind   `gorm:"column:charge_kind;type:varchar(20);index;uniqueIndex:idx_billing_charge_task_identity,priority:2;not null;check:chk_billing_charge_task_id,charge_kind <> 'task' OR task_id IS NOT NULL" json:"kind"`
	Policy             string              `gorm:"type:varchar(40);index;not null;check:chk_billing_charge_accepted_operation_identity,policy <> 'accepted_task_operation' OR (charge_kind = 'operation' AND operation_task_id IS NOT NULL AND TRIM(operation_task_id) <> '' AND attempt_id IS NOT NULL AND TRIM(attempt_id) <> '' AND tool_call_id IS NOT NULL AND TRIM(tool_call_id) <> '')" json:"policy"`
	Status             BillingChargeStatus `gorm:"type:varchar(20);index;not null" json:"status"`
	PriceCredits       int64               `gorm:"not null;check:chk_billing_charge_conservation,price_credits >= 0 AND price_credits = paid_credits + promotional_credits + debt_credits" json:"price_credits"`
	PaidCredits        int64               `gorm:"not null;check:chk_billing_charge_paid,paid_credits >= 0" json:"paid_credits"`
	PromotionalCredits int64               `gorm:"not null;check:chk_billing_charge_promotional,promotional_credits >= 0" json:"promotional_credits"`
	DebtCredits        int64               `gorm:"not null;check:chk_billing_charge_debt,debt_credits >= 0" json:"debt_credits"`
	TaskID             *string             `gorm:"type:char(36);index;uniqueIndex:idx_billing_charge_task_identity,priority:1" json:"task_id,omitempty"`
	OperationTaskID    *string             `gorm:"type:char(36);index;uniqueIndex:idx_billing_charge_tool_identity,priority:1" json:"operation_task_id,omitempty"`
	AttemptID          *string             `gorm:"type:char(36);index;uniqueIndex:idx_billing_charge_tool_identity,priority:2" json:"attempt_id,omitempty"`
	ToolCallID         *string             `gorm:"type:varchar(128);index;uniqueIndex:idx_billing_charge_tool_identity,priority:3" json:"tool_call_id,omitempty"`
	IdempotencyScope   string              `gorm:"type:varchar(80);uniqueIndex:idx_billing_charge_idempotency,priority:1;not null" json:"idempotency_scope"`
	IdempotencyKey     string              `gorm:"type:varchar(128);uniqueIndex:idx_billing_charge_idempotency,priority:2;not null" json:"idempotency_key"`
	ReversalOfID       *string             `gorm:"type:char(36);uniqueIndex:idx_billing_charge_reversal;check:chk_billing_charge_reversal_identity,(charge_kind = 'reversal' AND reversal_of_id IS NOT NULL AND TRIM(reversal_of_id) <> '') OR (charge_kind <> 'reversal' AND reversal_of_id IS NULL)" json:"reversal_of_id,omitempty"`
	RequestFingerprint string              `gorm:"type:char(64);not null" json:"request_fingerprint"`
	ActorType          string              `gorm:"type:varchar(40)" json:"actor_type,omitempty"`
	ActorID            string              `gorm:"type:varchar(128)" json:"actor_id,omitempty"`
	SourceService      string              `gorm:"type:varchar(80)" json:"source_service,omitempty"`
	RequestID          string              `gorm:"type:varchar(128);index" json:"request_id,omitempty"`
	CorrelationID      string              `gorm:"type:varchar(128);index" json:"correlation_id,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
}

func (BillingCharge) TableName() string { return "billing_charges" }

func (c BillingCharge) Validate() error {
	if c.PriceCredits < 0 || c.PaidCredits < 0 || c.PromotionalCredits < 0 || c.DebtCredits < 0 {
		return fmt.Errorf("charge price and components must be nonnegative")
	}
	if c.Kind == BillingChargeKindReversal {
		if c.ReversalOfID == nil || strings.TrimSpace(*c.ReversalOfID) == "" {
			return fmt.Errorf("reversal charge requires an original charge identity")
		}
	} else if c.ReversalOfID != nil {
		return fmt.Errorf("original charge cannot identify a reversed charge")
	}
	paidAndPromotional, ok := checkedAddInt64(c.PaidCredits, c.PromotionalCredits)
	if !ok {
		return fmt.Errorf("charge component sum overflows int64")
	}
	total, ok := checkedAddInt64(paidAndPromotional, c.DebtCredits)
	if !ok {
		return fmt.Errorf("charge component sum overflows int64")
	}
	if total != c.PriceCredits {
		return fmt.Errorf("charge components must equal price credits")
	}
	return nil
}

// BillingChargeAllocation records exact paid or promotional lot consumption.
type BillingChargeAllocation struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	ChargeID  string    `gorm:"type:char(36);uniqueIndex:idx_billing_charge_allocation_lot,priority:1;not null" json:"charge_id"`
	LotID     string    `gorm:"type:char(36);uniqueIndex:idx_billing_charge_allocation_lot,priority:2;index;not null" json:"lot_id"`
	Credits   int64     `gorm:"not null;check:chk_billing_charge_allocation_credits,credits > 0" json:"credits"`
	CreatedAt time.Time `json:"created_at"`
}

func (BillingChargeAllocation) TableName() string { return "billing_charge_allocations" }

// BillingSettlementOutbox stores retryable settlement intent state.
type BillingSettlementOutbox struct {
	ID                 string                  `gorm:"type:char(36);primaryKey" json:"id"`
	Action             BillingSettlementAction `gorm:"type:varchar(32);index;not null" json:"action"`
	ResourceType       string                  `gorm:"type:varchar(40);index:idx_billing_settlement_resource,priority:1;not null" json:"resource_type"`
	ResourceID         string                  `gorm:"type:varchar(128);index:idx_billing_settlement_resource,priority:2;not null" json:"resource_id"`
	TaskID             *string                 `gorm:"type:char(36);index" json:"task_id,omitempty"`
	AttemptID          *string                 `gorm:"type:char(36);index" json:"attempt_id,omitempty"`
	ToolCallID         *string                 `gorm:"type:varchar(128);index" json:"tool_call_id,omitempty"`
	ChargeID           *string                 `gorm:"type:char(36);index" json:"charge_id,omitempty"`
	CatalogID          string                  `gorm:"type:varchar(128);index" json:"catalog_id,omitempty"`
	SKUID              string                  `gorm:"type:varchar(128);index" json:"sku_id,omitempty"`
	IdempotencyScope   string                  `gorm:"type:varchar(80);uniqueIndex:idx_billing_settlement_outbox_idempotency,priority:1;not null" json:"idempotency_scope"`
	IdempotencyKey     string                  `gorm:"type:varchar(128);uniqueIndex:idx_billing_settlement_outbox_idempotency,priority:2;not null" json:"idempotency_key"`
	Status             string                  `gorm:"type:varchar(20);index;not null" json:"status"`
	Attempts           int                     `gorm:"not null;default:0;check:chk_billing_settlement_attempts,attempts >= 0" json:"attempts"`
	NextAttemptAt      *time.Time              `gorm:"index" json:"next_attempt_at,omitempty"`
	LastError          string                  `gorm:"type:text" json:"last_error,omitempty"`
	ProcessedAt        *time.Time              `json:"processed_at,omitempty"`
	FailedAt           *time.Time              `json:"failed_at,omitempty"`
	RequestFingerprint string                  `gorm:"type:char(64);not null" json:"request_fingerprint"`
	CreatedAt          time.Time               `json:"created_at"`
	UpdatedAt          time.Time               `json:"updated_at"`
}

func (BillingSettlementOutbox) TableName() string { return "billing_settlement_outbox" }

// BillingReferralIssue tracks the paired reward for one invitee and program.
type BillingReferralIssue struct {
	ID            string     `gorm:"type:char(36);primaryKey" json:"id"`
	ProgramID     string     `gorm:"type:varchar(128);uniqueIndex:idx_billing_referral_invitee_program,priority:2;not null" json:"program_id"`
	CatalogID     string     `gorm:"type:varchar(128);index;not null" json:"catalog_id"`
	InviteeUserID string     `gorm:"type:char(36);uniqueIndex:idx_billing_referral_invitee_program,priority:1;index;not null" json:"invitee_user_id"`
	InviterUserID string     `gorm:"type:char(36);index;not null" json:"inviter_user_id"`
	InviteeLotID  *string    `gorm:"type:char(36);uniqueIndex" json:"invitee_lot_id,omitempty"`
	InviterLotID  *string    `gorm:"type:char(36);uniqueIndex" json:"inviter_lot_id,omitempty"`
	Status        string     `gorm:"type:varchar(20);index;not null" json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	IssuedAt      *time.Time `json:"issued_at,omitempty"`
}

func (BillingReferralIssue) TableName() string { return "billing_referral_issues" }

func checkedAddInt64(a, b int64) (int64, bool) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, false
	}
	return a + b, true
}

func checkedSubInt64(a, b int64) (int64, bool) {
	if (b > 0 && a < math.MinInt64+b) || (b < 0 && a > math.MaxInt64+b) {
		return 0, false
	}
	return a - b, true
}
