package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
)

type BillingProviderCostEventKind string

type BillingProviderCostIdentityKind string

const (
	BillingProviderCostEventKindBase       BillingProviderCostEventKind = "base"
	BillingProviderCostEventKindAdjustment BillingProviderCostEventKind = "adjustment"
)

const (
	BillingProviderCostIdentityExecutionModel  BillingProviderCostIdentityKind = "execution_model"
	BillingProviderCostIdentityProviderRequest BillingProviderCostIdentityKind = "provider_request"
)

type BillingProviderCostSource string

const (
	BillingProviderCostSourceProviderResponse     BillingProviderCostSource = "provider_response"
	BillingProviderCostSourceClaudeResult         BillingProviderCostSource = "claude_result_model_usage"
	BillingProviderCostSourceInvoiceAdjustment    BillingProviderCostSource = "invoice_adjustment"
	BillingProviderCostSourceManualReconciliation BillingProviderCostSource = "manual_reconciliation"
)

type BillingProviderCostStatus string

const (
	BillingProviderCostStatusReconciled   BillingProviderCostStatus = "reconciled"
	BillingProviderCostStatusUnreconciled BillingProviderCostStatus = "unreconciled"
)

// BillingProviderCostEvent is an immutable internal provider-cost fact. It is
// deliberately unrelated to user wallet entries and charges.
type BillingProviderCostEvent struct {
	ID                  string                          `gorm:"type:char(36);primaryKey" json:"id"`
	EventKind           BillingProviderCostEventKind    `gorm:"type:varchar(20);index;not null" json:"event_kind"`
	IdentityKind        BillingProviderCostIdentityKind `gorm:"type:varchar(24);index" json:"identity_kind,omitempty"`
	ExecutionID         string                          `gorm:"type:varchar(128);index" json:"execution_id,omitempty"`
	ProviderRequestID   string                          `gorm:"type:varchar(256);index:idx_billing_provider_cost_request,priority:2" json:"provider_request_id,omitempty"`
	TaskID              string                          `gorm:"type:char(36);index" json:"task_id,omitempty"`
	Provider            string                          `gorm:"type:varchar(80);index:idx_billing_provider_cost_model,priority:1;index:idx_billing_provider_cost_request,priority:1;not null" json:"provider"`
	Model               string                          `gorm:"type:varchar(160);index:idx_billing_provider_cost_model,priority:2;not null" json:"model"`
	CatalogID           string                          `gorm:"type:varchar(128);index;not null" json:"catalog_id"`
	IdempotencyScope    string                          `gorm:"type:varchar(80);uniqueIndex:idx_billing_provider_cost_idempotency,priority:1;not null" json:"idempotency_scope"`
	IdempotencyKey      string                          `gorm:"type:varchar(256);uniqueIndex:idx_billing_provider_cost_idempotency,priority:2;not null" json:"idempotency_key"`
	BaseIdentityKey     *string                         `gorm:"type:char(64);uniqueIndex:idx_billing_provider_cost_base_identity" json:"base_identity_key,omitempty"`
	RequestFingerprint  string                          `gorm:"type:char(64);not null" json:"request_fingerprint"`
	OriginalEventID     *string                         `gorm:"type:char(36);index;check:chk_billing_provider_cost_adjustment_link,(event_kind = 'base' AND original_event_id IS NULL) OR (event_kind = 'adjustment' AND original_event_id IS NOT NULL)" json:"original_event_id,omitempty"`
	Source              BillingProviderCostSource       `gorm:"type:varchar(40);index;not null" json:"source"`
	Status              BillingProviderCostStatus       `gorm:"type:varchar(24);index;not null" json:"status"`
	CostMicroCNY        int64                           `gorm:"not null;check:chk_billing_provider_cost_base_nonnegative,event_kind <> 'base' OR cost_micro_cny >= 0;check:chk_billing_provider_cost_adjustment_nonzero,event_kind <> 'adjustment' OR cost_micro_cny <> 0" json:"cost_micro_cny"`
	UsageEvidence       datatypes.JSON                  `gorm:"type:json;not null" json:"usage_evidence"`
	CalculationSnapshot datatypes.JSON                  `gorm:"type:json;not null" json:"calculation_snapshot"`
	CreatedAt           time.Time                       `gorm:"index;not null" json:"created_at"`
}

func (BillingProviderCostEvent) TableName() string { return "billing_provider_cost_events" }

func (e BillingProviderCostEvent) Validate() error {
	required := map[string]string{
		"id": e.ID, "provider": e.Provider,
		"model": e.Model, "catalog_id": e.CatalogID, "idempotency_scope": e.IdempotencyScope,
		"idempotency_key": e.IdempotencyKey,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("provider cost %s is required", field)
		}
	}
	if len(e.RequestFingerprint) != 64 {
		return fmt.Errorf("provider cost request fingerprint must be a 64-character SHA-256 digest")
	}
	if !json.Valid(e.UsageEvidence) || !json.Valid(e.CalculationSnapshot) {
		return fmt.Errorf("provider cost evidence and calculation snapshot must be valid JSON")
	}
	switch e.EventKind {
	case BillingProviderCostEventKindBase:
		if e.BaseIdentityKey == nil || strings.TrimSpace(*e.BaseIdentityKey) == "" {
			return fmt.Errorf("base provider cost event requires a base identity")
		}
		if e.OriginalEventID != nil {
			return fmt.Errorf("base provider cost event cannot reference an original event")
		}
		if e.CostMicroCNY < 0 {
			return fmt.Errorf("base provider cost cannot be negative")
		}
		switch e.IdentityKind {
		case BillingProviderCostIdentityExecutionModel:
			if strings.TrimSpace(e.ExecutionID) == "" {
				return fmt.Errorf("execution/model provider cost identity requires execution_id")
			}
			if strings.TrimSpace(e.ProviderRequestID) != "" {
				return fmt.Errorf("execution/model provider cost identity cannot also use provider_request_id")
			}
		case BillingProviderCostIdentityProviderRequest:
			if strings.TrimSpace(e.ProviderRequestID) == "" {
				return fmt.Errorf("provider/request cost identity requires provider_request_id")
			}
			if strings.TrimSpace(e.ExecutionID) != "" {
				return fmt.Errorf("provider/request cost identity cannot also use execution_id")
			}
		default:
			return fmt.Errorf("unsupported provider cost identity kind %q", e.IdentityKind)
		}
		expectedIdentity, err := ProviderCostBaseIdentityKey(e.IdentityKind, e.ExecutionID, e.Provider, e.Model, e.ProviderRequestID)
		if err != nil {
			return err
		}
		if *e.BaseIdentityKey != expectedIdentity {
			return fmt.Errorf("provider cost base identity does not match typed identity fields")
		}
	case BillingProviderCostEventKindAdjustment:
		if e.IdentityKind != "" || strings.TrimSpace(e.ProviderRequestID) != "" {
			return fmt.Errorf("provider cost adjustment cannot declare a base identity")
		}
		if e.BaseIdentityKey != nil {
			return fmt.Errorf("provider cost adjustment cannot occupy a base identity")
		}
		if e.OriginalEventID == nil || strings.TrimSpace(*e.OriginalEventID) == "" {
			return fmt.Errorf("provider cost adjustment requires an original event")
		}
		if e.CostMicroCNY == 0 {
			return fmt.Errorf("provider cost adjustment must be nonzero")
		}
	default:
		return fmt.Errorf("unsupported provider cost event kind %q", e.EventKind)
	}
	switch e.Source {
	case BillingProviderCostSourceProviderResponse, BillingProviderCostSourceClaudeResult,
		BillingProviderCostSourceInvoiceAdjustment, BillingProviderCostSourceManualReconciliation:
	default:
		return fmt.Errorf("unsupported provider cost source %q", e.Source)
	}
	switch e.Status {
	case BillingProviderCostStatusReconciled, BillingProviderCostStatusUnreconciled:
	default:
		return fmt.Errorf("unsupported provider cost status %q", e.Status)
	}
	return nil
}

// ProviderCostBaseIdentityKey hashes a typed identity tuple. JSON field
// boundaries prevent delimiter ambiguity and the digest keeps MySQL's unique
// index narrow even when provider request IDs are long.
func ProviderCostBaseIdentityKey(kind BillingProviderCostIdentityKind, executionID, provider, modelID, providerRequestID string) (string, error) {
	type identityTuple struct {
		Version           int                             `json:"version"`
		Kind              BillingProviderCostIdentityKind `json:"kind"`
		ExecutionID       string                          `json:"execution_id,omitempty"`
		Provider          string                          `json:"provider"`
		Model             string                          `json:"model,omitempty"`
		ProviderRequestID string                          `json:"provider_request_id,omitempty"`
	}
	tuple := identityTuple{Version: 1, Kind: kind, Provider: strings.TrimSpace(provider)}
	switch kind {
	case BillingProviderCostIdentityExecutionModel:
		tuple.ExecutionID = strings.TrimSpace(executionID)
		tuple.Model = strings.TrimSpace(modelID)
		if tuple.ExecutionID == "" || tuple.Provider == "" || tuple.Model == "" || strings.TrimSpace(providerRequestID) != "" {
			return "", fmt.Errorf("execution/model provider cost identity requires only execution, provider, and model")
		}
	case BillingProviderCostIdentityProviderRequest:
		tuple.ProviderRequestID = strings.TrimSpace(providerRequestID)
		if tuple.Provider == "" || tuple.ProviderRequestID == "" || strings.TrimSpace(executionID) != "" {
			return "", fmt.Errorf("provider/request cost identity requires only provider and provider_request_id")
		}
	default:
		return "", fmt.Errorf("unsupported provider cost identity kind %q", kind)
	}
	payload, err := json.Marshal(tuple)
	if err != nil {
		return "", fmt.Errorf("marshal provider cost base identity: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

// BillingExecutionCostStatus is the mutable reconciliation projection for one
// execution. Provider cost facts themselves remain append-only.
type BillingExecutionCostStatus struct {
	ExecutionID string                    `gorm:"type:varchar(128);primaryKey" json:"execution_id"`
	TaskID      string                    `gorm:"type:char(36);index" json:"task_id,omitempty"`
	Status      BillingProviderCostStatus `gorm:"type:varchar(24);index;not null" json:"status"`
	Reason      string                    `gorm:"type:varchar(128)" json:"reason,omitempty"`
	CreatedAt   time.Time                 `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time                 `gorm:"not null" json:"updated_at"`
}

func (BillingExecutionCostStatus) TableName() string { return "billing_execution_cost_status" }
