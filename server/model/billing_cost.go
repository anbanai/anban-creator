package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
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

type BillingProviderCostAdjustmentReasonCode string

type BillingExecutionCostReasonCode string

const (
	BillingProviderCostStatusReconciled   BillingProviderCostStatus = "reconciled"
	BillingProviderCostStatusUnreconciled BillingProviderCostStatus = "unreconciled"
)

const (
	BillingExecutionCostReasonMissingTerminalModelUsage  BillingExecutionCostReasonCode = "missing_terminal_model_usage"
	BillingExecutionCostReasonInvalidTerminalModelUsage  BillingExecutionCostReasonCode = "invalid_terminal_model_usage"
	BillingExecutionCostReasonMissingProviderUsage       BillingExecutionCostReasonCode = "missing_provider_usage"
	BillingExecutionCostReasonMissingOutputMetadata      BillingExecutionCostReasonCode = "missing_output_metadata"
	BillingExecutionCostReasonPricingPeriodUnknown       BillingExecutionCostReasonCode = "pricing_period_unknown"
	BillingExecutionCostReasonPricingCatalogNotEffective BillingExecutionCostReasonCode = "pricing_catalog_not_effective"
)

const (
	BillingProviderCostAdjustmentReasonProviderInvoiceReconciliation BillingProviderCostAdjustmentReasonCode = "provider_invoice_reconciliation"
	BillingProviderCostAdjustmentReasonManualReconciliation          BillingProviderCostAdjustmentReasonCode = "manual_reconciliation"
)

const (
	maxProviderCostEvidenceBytes    = 4 * 1024
	maxProviderCostCalculationBytes = 64 * 1024
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
	UsageAt             *time.Time                      `gorm:"index" json:"usage_at,omitempty"`
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
	if _, err := hex.DecodeString(e.RequestFingerprint); err != nil {
		return fmt.Errorf("provider cost request fingerprint must be hexadecimal")
	}
	if len(e.CalculationSnapshot) == 0 || len(e.CalculationSnapshot) > maxProviderCostCalculationBytes || !json.Valid(e.CalculationSnapshot) {
		return fmt.Errorf("provider cost calculation snapshot must be valid JSON within %d bytes", maxProviderCostCalculationBytes)
	}
	if err := validateProviderCostEvidence(e); err != nil {
		return err
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

func validateProviderCostEvidence(event BillingProviderCostEvent) error {
	if len(event.UsageEvidence) == 0 || len(event.UsageEvidence) > maxProviderCostEvidenceBytes || !json.Valid(event.UsageEvidence) {
		return fmt.Errorf("provider cost usage evidence must be valid JSON within %d bytes", maxProviderCostEvidenceBytes)
	}
	var envelope struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(event.UsageEvidence, &envelope); err != nil || envelope.Kind == "" {
		return fmt.Errorf("provider cost usage evidence requires a kind")
	}
	switch envelope.Kind {
	case "token":
		if event.EventKind != BillingProviderCostEventKindBase {
			return fmt.Errorf("token evidence is only valid for base provider cost events")
		}
		if event.Source != BillingProviderCostSourceClaudeResult && event.Source != BillingProviderCostSourceProviderResponse && event.Source != BillingProviderCostSourceManualReconciliation {
			return fmt.Errorf("token evidence is incompatible with provider cost source %q", event.Source)
		}
		var evidence struct {
			Kind                     string `json:"kind"`
			InputTokens              *int64 `json:"input_tokens"`
			CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
			OutputTokens             *int64 `json:"output_tokens"`
			UsageAt                  string `json:"usage_at,omitempty"`
			Aggregated               bool   `json:"aggregated,omitempty"`
		}
		if err := decodeStrictProviderCostJSON(event.UsageEvidence, &evidence); err != nil {
			return fmt.Errorf("invalid token provider cost evidence: %w", err)
		}
		if evidence.Kind != "token" {
			return fmt.Errorf("token provider cost evidence has mismatched kind %q", evidence.Kind)
		}
		counts := []*int64{evidence.InputTokens, evidence.CacheReadInputTokens, evidence.CacheCreationInputTokens, evidence.OutputTokens}
		for _, count := range counts {
			if count == nil || *count < 0 {
				return fmt.Errorf("token provider cost evidence requires all nonnegative token counts")
			}
		}
		if evidence.UsageAt != "" {
			parsed, err := time.Parse(time.RFC3339Nano, evidence.UsageAt)
			if err != nil {
				return fmt.Errorf("token provider cost evidence usage_at must be RFC3339")
			}
			if event.UsageAt == nil || !event.UsageAt.UTC().Equal(parsed.UTC()) {
				return fmt.Errorf("token provider cost evidence usage_at does not match column")
			}
		} else if event.UsageAt != nil {
			return fmt.Errorf("token provider cost evidence usage_at is missing")
		}
	case "output_pixels":
		if event.EventKind != BillingProviderCostEventKindBase {
			return fmt.Errorf("output pixel evidence is only valid for base provider cost events")
		}
		if event.Source != BillingProviderCostSourceProviderResponse && event.Source != BillingProviderCostSourceManualReconciliation {
			return fmt.Errorf("output pixel evidence is incompatible with provider cost source %q", event.Source)
		}
		var evidence struct {
			Kind   string `json:"kind"`
			Width  *int64 `json:"width"`
			Height *int64 `json:"height"`
			Pixels *int64 `json:"pixels"`
		}
		if err := decodeStrictProviderCostJSON(event.UsageEvidence, &evidence); err != nil {
			return fmt.Errorf("invalid output pixel provider cost evidence: %w", err)
		}
		if evidence.Kind != "output_pixels" || evidence.Width == nil || evidence.Height == nil || evidence.Pixels == nil {
			return fmt.Errorf("output pixel provider cost evidence requires kind, width, height, and pixels")
		}
		width, height, pixels := *evidence.Width, *evidence.Height, *evidence.Pixels
		if width <= 0 || height <= 0 || width > math.MaxInt64/height || pixels != width*height {
			return fmt.Errorf("output pixel provider cost evidence requires consistent positive dimensions and pixels")
		}
	case "openai_image_usage":
		if event.EventKind != BillingProviderCostEventKindBase || event.Source != BillingProviderCostSourceProviderResponse {
			return fmt.Errorf("OpenAI image usage evidence requires a provider-response base event")
		}
		var evidence struct {
			Kind                   string `json:"kind"`
			TextInputTokens        *int64 `json:"text_input_tokens"`
			TextCachedInputTokens  *int64 `json:"text_cached_input_tokens"`
			ImageInputTokens       *int64 `json:"image_input_tokens"`
			ImageCachedInputTokens *int64 `json:"image_cached_input_tokens"`
			ImageOutputTokens      *int64 `json:"image_output_tokens"`
		}
		if err := decodeStrictProviderCostJSON(event.UsageEvidence, &evidence); err != nil {
			return fmt.Errorf("invalid OpenAI image provider cost evidence: %w", err)
		}
		counts := []*int64{evidence.TextInputTokens, evidence.TextCachedInputTokens, evidence.ImageInputTokens, evidence.ImageCachedInputTokens, evidence.ImageOutputTokens}
		for _, count := range counts {
			if count == nil || *count < 0 {
				return fmt.Errorf("OpenAI image provider cost evidence requires all five nonnegative token counts")
			}
		}
	case "video_output":
		if event.EventKind != BillingProviderCostEventKindBase || event.Source != BillingProviderCostSourceProviderResponse {
			return fmt.Errorf("video output evidence requires a provider-response base event")
		}
		var evidence struct {
			Kind            string `json:"kind"`
			DurationSeconds *int64 `json:"duration_seconds"`
			Resolution      string `json:"resolution"`
			HasVideoInput   *bool  `json:"has_video_input"`
			HasAudioInput   *bool  `json:"has_audio_input"`
		}
		if err := decodeStrictProviderCostJSON(event.UsageEvidence, &evidence); err != nil {
			return fmt.Errorf("invalid video output provider cost evidence: %w", err)
		}
		if evidence.DurationSeconds == nil || *evidence.DurationSeconds <= 0 || strings.TrimSpace(evidence.Resolution) == "" || evidence.HasVideoInput == nil || evidence.HasAudioInput == nil {
			return fmt.Errorf("video output provider cost evidence requires actual duration, resolution, and typed input flags")
		}
	case "media_unreconciled":
		if event.EventKind != BillingProviderCostEventKindBase || event.Status != BillingProviderCostStatusUnreconciled || event.Source != BillingProviderCostSourceProviderResponse {
			return fmt.Errorf("unreconciled media evidence requires an unreconciled provider-response base event")
		}
		var evidence struct {
			Kind            string                         `json:"kind"`
			ReasonCode      BillingExecutionCostReasonCode `json:"reason_code"`
			MediaKind       string                         `json:"media_kind"`
			DurationSeconds int64                          `json:"duration_seconds,omitempty"`
			Resolution      string                         `json:"resolution,omitempty"`
			HasVideoInput   *bool                          `json:"has_video_input,omitempty"`
			HasAudioInput   *bool                          `json:"has_audio_input,omitempty"`
		}
		if err := decodeStrictProviderCostJSON(event.UsageEvidence, &evidence); err != nil {
			return fmt.Errorf("invalid unreconciled media provider cost evidence: %w", err)
		}
		if !evidence.ReasonCode.Valid() || (evidence.MediaKind != "image" && evidence.MediaKind != "video") {
			return fmt.Errorf("unreconciled media evidence requires a supported reason and media kind")
		}
		if evidence.MediaKind == "video" && (evidence.DurationSeconds < 0 || evidence.HasVideoInput == nil || evidence.HasAudioInput == nil) {
			return fmt.Errorf("unreconciled video evidence requires typed input flags and nonnegative actual duration")
		}
	case "token_unreconciled":
		if event.EventKind != BillingProviderCostEventKindBase || event.Status != BillingProviderCostStatusUnreconciled || event.Source != BillingProviderCostSourceProviderResponse {
			return fmt.Errorf("unreconciled token evidence requires an unreconciled provider-response base event")
		}
		var evidence struct {
			Kind                     string                         `json:"kind"`
			ReasonCode               BillingExecutionCostReasonCode `json:"reason_code"`
			InputTokens              int64                          `json:"input_tokens,omitempty"`
			CacheReadInputTokens     int64                          `json:"cache_read_input_tokens,omitempty"`
			CacheCreationInputTokens int64                          `json:"cache_creation_input_tokens,omitempty"`
			OutputTokens             int64                          `json:"output_tokens,omitempty"`
			UsageAt                  string                         `json:"usage_at,omitempty"`
		}
		if err := decodeStrictProviderCostJSON(event.UsageEvidence, &evidence); err != nil {
			return fmt.Errorf("invalid unreconciled token provider cost evidence: %w", err)
		}
		if !evidence.ReasonCode.Valid() {
			return fmt.Errorf("unreconciled token evidence requires a supported reason")
		}
		if evidence.InputTokens < 0 || evidence.CacheReadInputTokens < 0 || evidence.CacheCreationInputTokens < 0 || evidence.OutputTokens < 0 {
			return fmt.Errorf("unreconciled token evidence requires nonnegative token counts")
		}
		if evidence.UsageAt != "" {
			parsed, err := time.Parse(time.RFC3339Nano, evidence.UsageAt)
			if err != nil {
				return fmt.Errorf("unreconciled token evidence usage_at must be RFC3339")
			}
			if event.UsageAt == nil || !event.UsageAt.UTC().Equal(parsed.UTC()) {
				return fmt.Errorf("unreconciled token evidence usage_at does not match column")
			}
		} else if event.UsageAt != nil {
			return fmt.Errorf("unreconciled token evidence usage_at is missing")
		}
	case "invoice_adjustment":
		if event.EventKind != BillingProviderCostEventKindAdjustment {
			return fmt.Errorf("invoice adjustment evidence requires an adjustment provider cost event")
		}
		if event.Source != BillingProviderCostSourceInvoiceAdjustment && event.Source != BillingProviderCostSourceManualReconciliation {
			return fmt.Errorf("invoice adjustment evidence is incompatible with provider cost source %q", event.Source)
		}
		var evidence struct {
			Kind       string                                  `json:"kind"`
			ReasonCode BillingProviderCostAdjustmentReasonCode `json:"reason_code"`
			Delta      *int64                                  `json:"delta_micro_cny"`
		}
		if err := decodeStrictProviderCostJSON(event.UsageEvidence, &evidence); err != nil {
			return fmt.Errorf("invalid invoice adjustment provider cost evidence: %w", err)
		}
		if evidence.Kind != "invoice_adjustment" || !evidence.ReasonCode.Valid() || evidence.Delta == nil || *evidence.Delta != event.CostMicroCNY {
			return fmt.Errorf("invoice adjustment evidence requires a supported reason code and matching delta")
		}
	default:
		return fmt.Errorf("unsupported provider cost evidence kind %q", envelope.Kind)
	}
	return nil
}

func decodeStrictProviderCostJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func (c BillingProviderCostAdjustmentReasonCode) Valid() bool {
	switch c {
	case BillingProviderCostAdjustmentReasonProviderInvoiceReconciliation,
		BillingProviderCostAdjustmentReasonManualReconciliation:
		return true
	default:
		return false
	}
}

func (c BillingExecutionCostReasonCode) Valid() bool {
	switch c {
	case BillingExecutionCostReasonMissingTerminalModelUsage,
		BillingExecutionCostReasonInvalidTerminalModelUsage,
		BillingExecutionCostReasonMissingProviderUsage,
		BillingExecutionCostReasonMissingOutputMetadata,
		BillingExecutionCostReasonPricingPeriodUnknown,
		BillingExecutionCostReasonPricingCatalogNotEffective:
		return true
	default:
		return false
	}
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
	ExecutionID             string                         `gorm:"type:varchar(128);primaryKey" json:"execution_id"`
	TaskID                  string                         `gorm:"type:char(36);index" json:"task_id,omitempty"`
	Status                  BillingProviderCostStatus      `gorm:"type:varchar(24);index;not null" json:"status"`
	ReasonCode              BillingExecutionCostReasonCode `gorm:"type:varchar(64)" json:"reason_code,omitempty"`
	FinalizationFingerprint string                         `gorm:"type:char(64)" json:"finalization_fingerprint,omitempty"`
	CreatedAt               time.Time                      `gorm:"not null" json:"created_at"`
	UpdatedAt               time.Time                      `gorm:"not null" json:"updated_at"`
}

func (BillingExecutionCostStatus) TableName() string { return "billing_execution_cost_status" }

type BillingMarginFactKind string

const (
	BillingMarginFactRetailCharge   BillingMarginFactKind = "retail_charge"
	BillingMarginFactRetailReversal BillingMarginFactKind = "retail_reversal"
	BillingMarginFactTopUp          BillingMarginFactKind = "topup"
	BillingMarginFactProviderCost   BillingMarginFactKind = "provider_cost"
)

// BillingMarginFact is an immutable accounting projection. SourceKind and
// SourceID bind each fact to one durable wallet or provider-cost event.
type BillingMarginFact struct {
	ID                          string                    `gorm:"type:char(36);primaryKey" json:"id"`
	Kind                        BillingMarginFactKind     `gorm:"type:varchar(24);index;not null" json:"kind"`
	SourceKind                  string                    `gorm:"type:varchar(32);uniqueIndex:idx_billing_margin_source,priority:1;not null" json:"source_kind"`
	SourceID                    string                    `gorm:"type:varchar(128);uniqueIndex:idx_billing_margin_source,priority:2;not null" json:"source_id"`
	SourceFingerprint           string                    `gorm:"type:char(64);not null" json:"source_fingerprint"`
	UserID                      string                    `gorm:"type:char(36);index" json:"user_id,omitempty"`
	TaskID                      string                    `gorm:"type:char(36);index" json:"task_id,omitempty"`
	ExecutionID                 string                    `gorm:"type:varchar(128);index" json:"execution_id,omitempty"`
	CatalogID                   string                    `gorm:"type:varchar(128);index" json:"catalog_id,omitempty"`
	SKUID                       string                    `gorm:"column:sku_id;type:varchar(128);index" json:"sku_id,omitempty"`
	Provider                    string                    `gorm:"type:varchar(80);index" json:"provider,omitempty"`
	Model                       string                    `gorm:"type:varchar(160);index" json:"model,omitempty"`
	InputTokens                 int64                     `gorm:"not null;default:0" json:"input_tokens"`
	CacheReadInputTokens        int64                     `gorm:"not null;default:0" json:"cache_read_input_tokens"`
	CacheCreationInputTokens    int64                     `gorm:"not null;default:0" json:"cache_creation_input_tokens"`
	OutputTokens                int64                     `gorm:"not null;default:0" json:"output_tokens"`
	ProviderCostStatus          BillingProviderCostStatus `gorm:"type:varchar(24);index" json:"provider_cost_status,omitempty"`
	CashMicroCNY                int64                     `gorm:"not null" json:"cash_micro_cny"`
	DeferredPaidMicroCNY        int64                     `gorm:"not null" json:"deferred_paid_micro_cny"`
	RecognizedRevenueMicroCNY   int64                     `gorm:"not null" json:"recognized_revenue_micro_cny"`
	PromotionMicroCNY           int64                     `gorm:"not null" json:"promotion_micro_cny"`
	ReceivableCreatedMicroCNY   int64                     `gorm:"not null" json:"receivable_created_micro_cny"`
	ReceivableCollectedMicroCNY int64                     `gorm:"not null" json:"receivable_collected_micro_cny"`
	ProviderCostMicroCNY        int64                     `gorm:"not null" json:"provider_cost_micro_cny"`
	ContributionMarginMicroCNY  int64                     `gorm:"not null" json:"contribution_margin_micro_cny"`
	OccurredAt                  time.Time                 `gorm:"index;not null" json:"occurred_at"`
	CreatedAt                   time.Time                 `gorm:"not null" json:"created_at"`
}

func (BillingMarginFact) TableName() string { return "billing_margin_facts" }

func (f BillingMarginFact) Validate() error {
	if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.SourceKind) == "" || strings.TrimSpace(f.SourceID) == "" {
		return fmt.Errorf("margin fact identity is required")
	}
	if len(f.SourceFingerprint) != 64 {
		return fmt.Errorf("margin fact source fingerprint must be a SHA-256 digest")
	}
	if _, err := hex.DecodeString(f.SourceFingerprint); err != nil {
		return fmt.Errorf("margin fact source fingerprint must be hexadecimal")
	}
	switch f.Kind {
	case BillingMarginFactRetailCharge, BillingMarginFactRetailReversal, BillingMarginFactTopUp, BillingMarginFactProviderCost:
	default:
		return fmt.Errorf("unsupported margin fact kind %q", f.Kind)
	}
	margin, ok := checkedMarginSub(f.RecognizedRevenueMicroCNY, f.ProviderCostMicroCNY)
	if !ok || margin != f.ContributionMarginMicroCNY {
		return fmt.Errorf("margin fact contribution margin is inconsistent")
	}
	if f.OccurredAt.IsZero() || f.CreatedAt.IsZero() {
		return fmt.Errorf("margin fact timestamps are required")
	}
	if f.InputTokens < 0 || f.CacheReadInputTokens < 0 || f.CacheCreationInputTokens < 0 || f.OutputTokens < 0 {
		return fmt.Errorf("margin fact token counts cannot be negative")
	}
	return nil
}

func checkedMarginSub(left, right int64) (int64, bool) {
	if right > 0 && left < math.MinInt64+right {
		return 0, false
	}
	if right < 0 && left > math.MaxInt64+right {
		return 0, false
	}
	return left - right, true
}
