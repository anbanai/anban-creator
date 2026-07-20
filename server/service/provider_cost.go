package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type TokenUsage struct {
	Input         int64 `json:"input_tokens"`
	CacheRead     int64 `json:"cache_read_input_tokens"`
	CacheCreation int64 `json:"cache_creation_input_tokens"`
	Output        int64 `json:"output_tokens"`
}

type TokenCostCategoryNumerators struct {
	Input         string `json:"input"`
	CacheRead     string `json:"cache_read_input"`
	CacheCreation string `json:"cache_creation_input"`
	Output        string `json:"output"`
}

type TokenCostPrices struct {
	Input         int64 `json:"input"`
	CacheRead     int64 `json:"cache_read_input"`
	CacheCreation int64 `json:"cache_creation_input"`
	Output        int64 `json:"output"`
}

// TokenCostCalculation is the immutable arithmetic snapshot for one provider
// event. String numerators preserve exact intermediate values beyond int64.
type TokenCostCalculation struct {
	Version                  int                         `json:"version"`
	CatalogID                string                      `json:"catalog_id"`
	ModelID                  string                      `json:"model_id"`
	PricingType              string                      `json:"pricing_type"`
	Currency                 string                      `json:"currency"`
	CurrencyRateMicroCNY     int64                       `json:"currency_rate_micro_cny"`
	Unit                     int64                       `json:"unit"`
	Prices                   TokenCostPrices             `json:"prices_micro_currency"`
	CategoryNumerators       TokenCostCategoryNumerators `json:"category_numerators"`
	TotalCategoryNumerator   string                      `json:"total_category_numerator"`
	CNYConversionNumerator   string                      `json:"cny_conversion_numerator"`
	CNYConversionDenominator string                      `json:"cny_conversion_denominator"`
	MicroCNY                 int64                       `json:"cost_micro_cny"`
	Rounding                 string                      `json:"rounding"`
	OperatorEvidence         string                      `json:"operator_evidence"`
	EffectiveAt              time.Time                   `json:"effective_at"`
}

type ProviderCostCalculator struct {
	catalog billing.CostCatalog
}

func NewProviderCostCalculator(catalog billing.CostCatalog) *ProviderCostCalculator {
	snapshot := billing.CostCatalog{
		CatalogID: catalog.CatalogID, CurrencyRates: make(map[string]billing.MicroCNY, len(catalog.CurrencyRates)),
		Models: make(map[string]billing.ModelCostConfig, len(catalog.Models)),
	}
	for currency, rate := range catalog.CurrencyRates {
		snapshot.CurrencyRates[currency] = rate
	}
	for modelID, price := range catalog.Models {
		price.Tiers = append([]billing.CostTier(nil), price.Tiers...)
		snapshot.Models[modelID] = price
	}
	return &ProviderCostCalculator{catalog: snapshot}
}

func (c *ProviderCostCalculator) TokenCost(modelID string, usage TokenUsage) (TokenCostCalculation, error) {
	if c == nil {
		return TokenCostCalculation{}, errors.New("provider cost calculator is required")
	}
	price, ok := c.catalog.Models[modelID]
	if !ok {
		return TokenCostCalculation{}, fmt.Errorf("provider cost model %q is not in catalog %q", modelID, c.catalog.CatalogID)
	}
	if price.PricingType != "token" || price.Unit <= 0 {
		return TokenCostCalculation{}, fmt.Errorf("provider cost model %q is not token-priced", modelID)
	}
	counts := []int64{usage.Input, usage.CacheRead, usage.CacheCreation, usage.Output}
	for _, count := range counts {
		if count < 0 {
			return TokenCostCalculation{}, errors.New("provider token usage cannot be negative")
		}
	}
	rate, ok := c.catalog.CurrencyRates[price.Currency]
	if !ok || rate <= 0 {
		return TokenCostCalculation{}, fmt.Errorf("provider cost currency %q has no positive CNY rate", price.Currency)
	}
	prices := []billing.MicroCNY{price.Input, price.CacheReadInput, price.CacheCreationInput, price.Output}
	numerators := make([]*big.Int, len(counts))
	total := new(big.Int)
	for index := range counts {
		numerators[index] = new(big.Int).Mul(big.NewInt(counts[index]), big.NewInt(int64(prices[index])))
		total.Add(total, numerators[index])
	}
	conversionNumerator := new(big.Int).Mul(new(big.Int).Set(total), big.NewInt(int64(rate)))
	conversionDenominator := new(big.Int).Mul(big.NewInt(price.Unit), big.NewInt(1_000_000))
	cost := ceilPositiveQuotient(conversionNumerator, conversionDenominator)
	if !cost.IsInt64() || cost.Sign() < 0 {
		return TokenCostCalculation{}, errors.New("provider token cost overflows signed micro-CNY")
	}
	return TokenCostCalculation{
		Version: 1, CatalogID: c.catalog.CatalogID, ModelID: modelID, PricingType: price.PricingType,
		Currency: price.Currency, CurrencyRateMicroCNY: int64(rate), Unit: price.Unit,
		Prices: TokenCostPrices{Input: int64(price.Input), CacheRead: int64(price.CacheReadInput), CacheCreation: int64(price.CacheCreationInput), Output: int64(price.Output)},
		CategoryNumerators: TokenCostCategoryNumerators{
			Input: numerators[0].String(), CacheRead: numerators[1].String(),
			CacheCreation: numerators[2].String(), Output: numerators[3].String(),
		},
		TotalCategoryNumerator: total.String(), CNYConversionNumerator: conversionNumerator.String(),
		CNYConversionDenominator: conversionDenominator.String(), MicroCNY: cost.Int64(),
		Rounding: "ceil_once_per_provider_event", OperatorEvidence: price.OperatorEvidence, EffectiveAt: price.EffectiveAt,
	}, nil
}

func ceilPositiveQuotient(numerator, denominator *big.Int) *big.Int {
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

type RecordTokenCostRequest struct {
	ExecutionID    string
	TaskID         string
	Provider       string
	Model          string
	CatalogID      string
	IdempotencyKey string
	Usage          TokenUsage
	Source         string
}

type AdjustmentRequest struct {
	OriginalEventID   string
	IdempotencyKey    string
	CostDeltaMicroCNY int64
	Reason            string
}

type tokenUsageEvidence struct {
	Kind                     string `json:"kind"`
	InputTokens              int64  `json:"input_tokens"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
}

type invoiceAdjustmentEvidence struct {
	Kind          string `json:"kind"`
	Reason        string `json:"reason"`
	DeltaMicroCNY int64  `json:"delta_micro_cny"`
}

type invoiceAdjustmentCalculation struct {
	Version              int    `json:"version"`
	OriginalEventID      string `json:"original_event_id"`
	OriginalCostMicroCNY int64  `json:"original_cost_micro_cny"`
	DeltaMicroCNY        int64  `json:"delta_micro_cny"`
}

type ProviderCostService struct {
	repo       repository.BillingCostRepository
	calculator *ProviderCostCalculator
	catalogID  string
}

func NewProviderCostService(repo repository.BillingCostRepository, bundle *billing.Bundle) *ProviderCostService {
	service := &ProviderCostService{repo: repo}
	if bundle != nil {
		service.catalogID = bundle.Costs.CatalogID
		service.calculator = NewProviderCostCalculator(bundle.Costs)
	}
	return service
}

func (s *ProviderCostService) RecordTokenUsage(ctx context.Context, req RecordTokenCostRequest) (*model.BillingProviderCostEvent, error) {
	if s == nil || s.repo == nil || s.calculator == nil {
		return nil, errors.New("provider cost service is not configured")
	}
	req.ExecutionID = strings.TrimSpace(req.ExecutionID)
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.Provider = strings.TrimSpace(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	req.CatalogID = strings.TrimSpace(req.CatalogID)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if req.ExecutionID == "" || req.Provider == "" || req.Model == "" || req.IdempotencyKey == "" {
		return nil, errors.New("provider token cost requires execution, provider, model, and idempotency identities")
	}
	if req.CatalogID != s.catalogID {
		return nil, fmt.Errorf("provider token cost catalog %q does not match active catalog %q", req.CatalogID, s.catalogID)
	}
	source := model.BillingProviderCostSource(strings.TrimSpace(req.Source))
	switch source {
	case model.BillingProviderCostSourceProviderResponse, model.BillingProviderCostSourceClaudeResult,
		model.BillingProviderCostSourceManualReconciliation:
	default:
		return nil, fmt.Errorf("unsupported token cost source %q", req.Source)
	}
	modelID := req.Provider + "/" + req.Model
	calculation, err := s.calculator.TokenCost(modelID, req.Usage)
	if err != nil {
		return nil, err
	}
	evidence := tokenUsageEvidence{
		Kind: "token", InputTokens: req.Usage.Input, CacheReadInputTokens: req.Usage.CacheRead,
		CacheCreationInputTokens: req.Usage.CacheCreation, OutputTokens: req.Usage.Output,
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return nil, fmt.Errorf("marshal provider token evidence: %w", err)
	}
	calculationJSON, err := json.Marshal(calculation)
	if err != nil {
		return nil, fmt.Errorf("marshal provider token calculation: %w", err)
	}
	baseIdentity, err := model.ProviderCostBaseIdentityKey(model.BillingProviderCostIdentityExecutionModel, req.ExecutionID, req.Provider, req.Model, "")
	if err != nil {
		return nil, err
	}
	fingerprint, err := providerCostFingerprint(struct {
		ExecutionID, TaskID, Provider, Model, CatalogID, Source string
		Usage                                                   TokenUsage
	}{req.ExecutionID, req.TaskID, req.Provider, req.Model, req.CatalogID, string(source), req.Usage})
	if err != nil {
		return nil, err
	}
	event := &model.BillingProviderCostEvent{
		ID: uuid.NewString(), EventKind: model.BillingProviderCostEventKindBase,
		IdentityKind: model.BillingProviderCostIdentityExecutionModel,
		ExecutionID:  req.ExecutionID, TaskID: req.TaskID, Provider: req.Provider, Model: req.Model,
		CatalogID: req.CatalogID, IdempotencyScope: "provider_cost_base/" + req.Provider,
		IdempotencyKey: req.IdempotencyKey, BaseIdentityKey: &baseIdentity, RequestFingerprint: fingerprint,
		Source: source, Status: model.BillingProviderCostStatusReconciled, CostMicroCNY: calculation.MicroCNY,
		UsageEvidence: datatypes.JSON(evidenceJSON), CalculationSnapshot: datatypes.JSON(calculationJSON),
	}
	status := &model.BillingExecutionCostStatus{
		ExecutionID: req.ExecutionID, TaskID: req.TaskID, Status: model.BillingProviderCostStatusReconciled,
	}
	return s.repo.AppendEventAndUpsertExecutionCostStatus(ctx, event, status)
}

func (s *ProviderCostService) MarkExecutionUnreconciled(ctx context.Context, executionID, reason string) error {
	if s == nil || s.repo == nil {
		return errors.New("provider cost service is not configured")
	}
	executionID, reason = strings.TrimSpace(executionID), strings.TrimSpace(reason)
	if executionID == "" || reason == "" {
		return errors.New("unreconciled provider cost requires execution identity and reason")
	}
	return s.repo.UpsertExecutionCostStatus(ctx, &model.BillingExecutionCostStatus{
		ExecutionID: executionID, Status: model.BillingProviderCostStatusUnreconciled, Reason: reason,
	})
}

func (s *ProviderCostService) AppendInvoiceAdjustment(ctx context.Context, req AdjustmentRequest) (*model.BillingProviderCostEvent, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("provider cost service is not configured")
	}
	req.OriginalEventID = strings.TrimSpace(req.OriginalEventID)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.OriginalEventID == "" || req.IdempotencyKey == "" || req.Reason == "" || req.CostDeltaMicroCNY == 0 {
		return nil, errors.New("provider cost adjustment requires original event, idempotency key, nonzero delta, and reason")
	}
	base, err := s.repo.FindEventByID(ctx, req.OriginalEventID)
	if err != nil {
		return nil, fmt.Errorf("find original provider cost event: %w", err)
	}
	if base.EventKind != model.BillingProviderCostEventKindBase {
		return nil, errors.New("provider cost adjustment must reference a base event")
	}
	evidence := invoiceAdjustmentEvidence{Kind: "invoice_adjustment", Reason: req.Reason, DeltaMicroCNY: req.CostDeltaMicroCNY}
	calculation := invoiceAdjustmentCalculation{
		Version: 1, OriginalEventID: base.ID, OriginalCostMicroCNY: base.CostMicroCNY, DeltaMicroCNY: req.CostDeltaMicroCNY,
	}
	evidenceJSON, _ := json.Marshal(evidence)
	calculationJSON, _ := json.Marshal(calculation)
	fingerprint, err := providerCostFingerprint(struct {
		OriginalEventID string
		DeltaMicroCNY   int64
		Reason          string
	}{base.ID, req.CostDeltaMicroCNY, req.Reason})
	if err != nil {
		return nil, err
	}
	originalID := base.ID
	return s.repo.AppendEvent(ctx, &model.BillingProviderCostEvent{
		ID: uuid.NewString(), EventKind: model.BillingProviderCostEventKindAdjustment,
		ExecutionID: base.ExecutionID, TaskID: base.TaskID, Provider: base.Provider, Model: base.Model,
		CatalogID: base.CatalogID, IdempotencyScope: "provider_cost_adjustment", IdempotencyKey: req.IdempotencyKey,
		RequestFingerprint: fingerprint, OriginalEventID: &originalID,
		Source: model.BillingProviderCostSourceInvoiceAdjustment, Status: model.BillingProviderCostStatusReconciled,
		CostMicroCNY: req.CostDeltaMicroCNY, UsageEvidence: datatypes.JSON(evidenceJSON),
		CalculationSnapshot: datatypes.JSON(calculationJSON),
	})
}

func providerCostFingerprint(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal provider cost fingerprint: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}
