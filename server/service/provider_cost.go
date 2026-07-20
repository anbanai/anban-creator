package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
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

type OutputPixelUsage struct {
	Width  int64 `json:"width"`
	Height int64 `json:"height"`
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

type OutputPixelTierSnapshot struct {
	Index     int   `json:"index"`
	MaxPixels int64 `json:"max_pixels"`
	Price     int64 `json:"price_micro_currency"`
}

type OutputPixelCostCalculation struct {
	Version                  int                       `json:"version"`
	CatalogID                string                    `json:"catalog_id"`
	ModelID                  string                    `json:"model_id"`
	PricingType              string                    `json:"pricing_type"`
	Currency                 string                    `json:"currency"`
	CurrencyRateMicroCNY     int64                     `json:"currency_rate_micro_cny"`
	Width                    int64                     `json:"width"`
	Height                   int64                     `json:"height"`
	Pixels                   int64                     `json:"pixels"`
	Tiers                    []OutputPixelTierSnapshot `json:"tiers"`
	SelectedTier             OutputPixelTierSnapshot   `json:"selected_tier"`
	CNYConversionNumerator   string                    `json:"cny_conversion_numerator"`
	CNYConversionDenominator string                    `json:"cny_conversion_denominator"`
	MicroCNY                 int64                     `json:"cost_micro_cny"`
	Rounding                 string                    `json:"rounding"`
	OperatorEvidence         string                    `json:"operator_evidence"`
	EffectiveAt              time.Time                 `json:"effective_at"`
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

func (c *ProviderCostCalculator) OutputPixelCost(modelID string, usage OutputPixelUsage) (OutputPixelCostCalculation, error) {
	if c == nil {
		return OutputPixelCostCalculation{}, errors.New("provider cost calculator is required")
	}
	price, ok := c.catalog.Models[modelID]
	if !ok {
		return OutputPixelCostCalculation{}, fmt.Errorf("provider cost model %q is not in catalog %q", modelID, c.catalog.CatalogID)
	}
	if price.PricingType != "output_pixel_tier" || len(price.Tiers) == 0 {
		return OutputPixelCostCalculation{}, fmt.Errorf("provider cost model %q is not output-pixel-tier priced", modelID)
	}
	if usage.Width <= 0 || usage.Height <= 0 || usage.Width > math.MaxInt64/usage.Height {
		return OutputPixelCostCalculation{}, errors.New("provider output pixel dimensions must be positive and non-overflowing")
	}
	pixels := usage.Width * usage.Height
	rate, ok := c.catalog.CurrencyRates[price.Currency]
	if !ok || rate <= 0 {
		return OutputPixelCostCalculation{}, fmt.Errorf("provider cost currency %q has no positive CNY rate", price.Currency)
	}
	tiers := make([]OutputPixelTierSnapshot, 0, len(price.Tiers))
	selected := OutputPixelTierSnapshot{Index: -1}
	var previousMax int64
	for index, tier := range price.Tiers {
		last := index == len(price.Tiers)-1
		if tier.Price <= 0 || (!last && (tier.MaxPixels <= previousMax || tier.MaxPixels <= 0)) || (last && tier.MaxPixels != 0) {
			return OutputPixelCostCalculation{}, fmt.Errorf("provider cost model %q has invalid output pixel tiers", modelID)
		}
		snapshot := OutputPixelTierSnapshot{Index: index, MaxPixels: tier.MaxPixels, Price: int64(tier.Price)}
		tiers = append(tiers, snapshot)
		if selected.Index < 0 && (tier.MaxPixels == 0 || pixels <= tier.MaxPixels) {
			selected = snapshot
		}
		if tier.MaxPixels > 0 {
			previousMax = tier.MaxPixels
		}
	}
	if selected.Index < 0 {
		return OutputPixelCostCalculation{}, fmt.Errorf("provider cost model %q has no output pixel tier for %d pixels", modelID, pixels)
	}
	numerator := new(big.Int).Mul(big.NewInt(selected.Price), big.NewInt(int64(rate)))
	denominator := big.NewInt(1_000_000)
	cost := ceilPositiveQuotient(numerator, denominator)
	if !cost.IsInt64() || cost.Sign() < 0 {
		return OutputPixelCostCalculation{}, errors.New("provider output pixel cost overflows signed micro-CNY")
	}
	return OutputPixelCostCalculation{
		Version: 1, CatalogID: c.catalog.CatalogID, ModelID: modelID, PricingType: price.PricingType,
		Currency: price.Currency, CurrencyRateMicroCNY: int64(rate), Width: usage.Width, Height: usage.Height, Pixels: pixels,
		Tiers: tiers, SelectedTier: selected, CNYConversionNumerator: numerator.String(),
		CNYConversionDenominator: denominator.String(), MicroCNY: cost.Int64(),
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

type ExecutionTokenCostEntry struct {
	Provider       string
	Model          string
	IdempotencyKey string
	Usage          TokenUsage
	Source         string
}

type FinalizeExecutionTokenCostsRequest struct {
	ExecutionID string
	TaskID      string
	CatalogID   string
	Entries     []ExecutionTokenCostEntry
}

type RecordOutputPixelCostRequest struct {
	TaskID            string
	Provider          string
	Model             string
	ProviderRequestID string
	CatalogID         string
	IdempotencyKey    string
	Width             int64
	Height            int64
	Source            string
}

type AdjustmentRequest struct {
	OriginalEventID   string
	IdempotencyKey    string
	CostDeltaMicroCNY int64
	ReasonCode        model.BillingProviderCostAdjustmentReasonCode
}

type tokenUsageEvidence struct {
	Kind                     string `json:"kind"`
	InputTokens              int64  `json:"input_tokens"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
}

type outputPixelEvidence struct {
	Kind   string `json:"kind"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
	Pixels int64  `json:"pixels"`
}

type invoiceAdjustmentEvidence struct {
	Kind          string                                        `json:"kind"`
	ReasonCode    model.BillingProviderCostAdjustmentReasonCode `json:"reason_code"`
	DeltaMicroCNY int64                                         `json:"delta_micro_cny"`
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

const maxExecutionTokenCostEntries = 128

func NewProviderCostService(repo repository.BillingCostRepository, bundle *billing.Bundle) *ProviderCostService {
	service := &ProviderCostService{repo: repo}
	if bundle != nil {
		service.catalogID = bundle.Costs.CatalogID
		service.calculator = NewProviderCostCalculator(bundle.Costs)
	}
	return service
}

// RecordTokenUsage finalizes a deliberately single-model execution. Terminal
// Claude modelUsage, which may contain parent and child models, must use
// FinalizeExecutionTokenCosts so all models and the status commit atomically.
func (s *ProviderCostService) RecordTokenUsage(ctx context.Context, req RecordTokenCostRequest) (*model.BillingProviderCostEvent, error) {
	result, err := s.FinalizeExecutionTokenCosts(ctx, FinalizeExecutionTokenCostsRequest{
		ExecutionID: req.ExecutionID, TaskID: req.TaskID, CatalogID: req.CatalogID,
		Entries: []ExecutionTokenCostEntry{{
			Provider: req.Provider, Model: req.Model, IdempotencyKey: req.IdempotencyKey,
			Usage: req.Usage, Source: req.Source,
		}},
	})
	if err != nil {
		return nil, err
	}
	return result[0], nil
}

func (s *ProviderCostService) FinalizeExecutionTokenCosts(ctx context.Context, req FinalizeExecutionTokenCostsRequest) ([]*model.BillingProviderCostEvent, error) {
	if s == nil || s.repo == nil || s.calculator == nil {
		return nil, errors.New("provider cost service is not configured")
	}
	req.ExecutionID, req.TaskID, req.CatalogID = strings.TrimSpace(req.ExecutionID), strings.TrimSpace(req.TaskID), strings.TrimSpace(req.CatalogID)
	if req.ExecutionID == "" || len(req.Entries) == 0 {
		return nil, errors.New("provider token cost finalization requires execution identity and model entries")
	}
	if len(req.Entries) > maxExecutionTokenCostEntries {
		return nil, fmt.Errorf("provider token cost finalization supports at most %d model entries", maxExecutionTokenCostEntries)
	}
	if req.CatalogID != s.catalogID {
		return nil, fmt.Errorf("provider token cost catalog %q does not match active catalog %q", req.CatalogID, s.catalogID)
	}
	events := make([]*model.BillingProviderCostEvent, 0, len(req.Entries))
	seenModels := make(map[[2]string]struct{}, len(req.Entries))
	for _, entry := range req.Entries {
		identity := [2]string{strings.TrimSpace(entry.Provider), strings.TrimSpace(entry.Model)}
		if _, exists := seenModels[identity]; exists {
			return nil, fmt.Errorf("provider token cost finalization contains duplicate provider/model %q/%q", identity[0], identity[1])
		}
		seenModels[identity] = struct{}{}
		event, err := s.buildTokenCostEvent(RecordTokenCostRequest{
			ExecutionID: req.ExecutionID, TaskID: req.TaskID, Provider: entry.Provider, Model: entry.Model,
			CatalogID: req.CatalogID, IdempotencyKey: entry.IdempotencyKey, Usage: entry.Usage, Source: entry.Source,
		})
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Provider != events[j].Provider {
			return events[i].Provider < events[j].Provider
		}
		return events[i].Model < events[j].Model
	})
	finalizationFingerprint, err := executionCostFinalizationFingerprint(req.ExecutionID, req.TaskID, req.CatalogID, events)
	if err != nil {
		return nil, err
	}
	status := &model.BillingExecutionCostStatus{
		ExecutionID: req.ExecutionID, TaskID: req.TaskID, Status: model.BillingProviderCostStatusReconciled,
		FinalizationFingerprint: finalizationFingerprint,
	}
	return s.repo.AppendEventsAndUpsertExecutionCostStatus(ctx, events, status)
}

func (s *ProviderCostService) buildTokenCostEvent(req RecordTokenCostRequest) (*model.BillingProviderCostEvent, error) {
	req.Provider, req.Model, req.IdempotencyKey = strings.TrimSpace(req.Provider), strings.TrimSpace(req.Model), strings.TrimSpace(req.IdempotencyKey)
	if req.Provider == "" || req.Model == "" || req.IdempotencyKey == "" {
		return nil, errors.New("provider token cost requires provider, model, and idempotency identities")
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
	return event, nil
}

func (s *ProviderCostService) RecordOutputPixelCost(ctx context.Context, req RecordOutputPixelCostRequest) (*model.BillingProviderCostEvent, error) {
	if s == nil || s.repo == nil || s.calculator == nil {
		return nil, errors.New("provider cost service is not configured")
	}
	req.TaskID, req.Provider, req.Model = strings.TrimSpace(req.TaskID), strings.TrimSpace(req.Provider), strings.TrimSpace(req.Model)
	req.ProviderRequestID, req.CatalogID = strings.TrimSpace(req.ProviderRequestID), strings.TrimSpace(req.CatalogID)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if req.Provider == "" || req.Model == "" || req.ProviderRequestID == "" || req.IdempotencyKey == "" {
		return nil, errors.New("provider output pixel cost requires provider, model, request, and idempotency identities")
	}
	if req.CatalogID != s.catalogID {
		return nil, fmt.Errorf("provider output pixel cost catalog %q does not match active catalog %q", req.CatalogID, s.catalogID)
	}
	source := model.BillingProviderCostSource(strings.TrimSpace(req.Source))
	if source != model.BillingProviderCostSourceProviderResponse && source != model.BillingProviderCostSourceManualReconciliation {
		return nil, fmt.Errorf("unsupported output pixel cost source %q", req.Source)
	}
	calculation, err := s.calculator.OutputPixelCost(req.Provider+"/"+req.Model, OutputPixelUsage{Width: req.Width, Height: req.Height})
	if err != nil {
		return nil, err
	}
	evidence := outputPixelEvidence{Kind: "output_pixels", Width: req.Width, Height: req.Height, Pixels: calculation.Pixels}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return nil, fmt.Errorf("marshal provider output pixel evidence: %w", err)
	}
	calculationJSON, err := json.Marshal(calculation)
	if err != nil {
		return nil, fmt.Errorf("marshal provider output pixel calculation: %w", err)
	}
	baseIdentity, err := model.ProviderCostBaseIdentityKey(model.BillingProviderCostIdentityProviderRequest, "", req.Provider, req.Model, req.ProviderRequestID)
	if err != nil {
		return nil, err
	}
	fingerprint, err := providerCostFingerprint(struct {
		TaskID, Provider, Model, ProviderRequestID, CatalogID, Source string
		Width, Height                                                 int64
	}{req.TaskID, req.Provider, req.Model, req.ProviderRequestID, req.CatalogID, string(source), req.Width, req.Height})
	if err != nil {
		return nil, err
	}
	return s.repo.AppendEvent(ctx, &model.BillingProviderCostEvent{
		ID: uuid.NewString(), EventKind: model.BillingProviderCostEventKindBase,
		IdentityKind: model.BillingProviderCostIdentityProviderRequest, ProviderRequestID: req.ProviderRequestID,
		TaskID: req.TaskID, Provider: req.Provider, Model: req.Model, CatalogID: req.CatalogID,
		IdempotencyScope: "provider_cost_base/" + req.Provider, IdempotencyKey: req.IdempotencyKey,
		BaseIdentityKey: &baseIdentity, RequestFingerprint: fingerprint, Source: source,
		Status: model.BillingProviderCostStatusReconciled, CostMicroCNY: calculation.MicroCNY,
		UsageEvidence: datatypes.JSON(evidenceJSON), CalculationSnapshot: datatypes.JSON(calculationJSON),
	})
}

func (s *ProviderCostService) MarkExecutionUnreconciled(ctx context.Context, executionID string, reasonCode model.BillingExecutionCostReasonCode) error {
	if s == nil || s.repo == nil {
		return errors.New("provider cost service is not configured")
	}
	executionID = strings.TrimSpace(executionID)
	if executionID == "" || !reasonCode.Valid() {
		return errors.New("unreconciled provider cost requires execution identity and supported reason code")
	}
	return s.repo.UpsertExecutionCostStatus(ctx, &model.BillingExecutionCostStatus{
		ExecutionID: executionID, Status: model.BillingProviderCostStatusUnreconciled, ReasonCode: reasonCode,
	})
}

func (s *ProviderCostService) AppendInvoiceAdjustment(ctx context.Context, req AdjustmentRequest) (*model.BillingProviderCostEvent, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("provider cost service is not configured")
	}
	req.OriginalEventID = strings.TrimSpace(req.OriginalEventID)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if req.OriginalEventID == "" || req.IdempotencyKey == "" || !req.ReasonCode.Valid() || req.CostDeltaMicroCNY == 0 {
		return nil, errors.New("provider cost adjustment requires original event, idempotency key, nonzero delta, and supported reason code")
	}
	base, err := s.repo.FindEventByID(ctx, req.OriginalEventID)
	if err != nil {
		return nil, fmt.Errorf("find original provider cost event: %w", err)
	}
	if base.EventKind != model.BillingProviderCostEventKindBase {
		return nil, errors.New("provider cost adjustment must reference a base event")
	}
	evidence := invoiceAdjustmentEvidence{Kind: "invoice_adjustment", ReasonCode: req.ReasonCode, DeltaMicroCNY: req.CostDeltaMicroCNY}
	calculation := invoiceAdjustmentCalculation{
		Version: 1, OriginalEventID: base.ID, OriginalCostMicroCNY: base.CostMicroCNY, DeltaMicroCNY: req.CostDeltaMicroCNY,
	}
	evidenceJSON, _ := json.Marshal(evidence)
	calculationJSON, _ := json.Marshal(calculation)
	fingerprint, err := providerCostFingerprint(struct {
		OriginalEventID string
		DeltaMicroCNY   int64
		ReasonCode      model.BillingProviderCostAdjustmentReasonCode
	}{base.ID, req.CostDeltaMicroCNY, req.ReasonCode})
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

func executionCostFinalizationFingerprint(executionID, taskID, catalogID string, events []*model.BillingProviderCostEvent) (string, error) {
	type finalizationEvent struct {
		Provider           string `json:"provider"`
		Model              string `json:"model"`
		RequestFingerprint string `json:"request_fingerprint"`
	}
	payload := struct {
		Version     int                 `json:"version"`
		ExecutionID string              `json:"execution_id"`
		TaskID      string              `json:"task_id,omitempty"`
		CatalogID   string              `json:"catalog_id"`
		Events      []finalizationEvent `json:"events"`
	}{Version: 1, ExecutionID: executionID, TaskID: taskID, CatalogID: catalogID, Events: make([]finalizationEvent, 0, len(events))}
	for _, event := range events {
		payload.Events = append(payload.Events, finalizationEvent{
			Provider: event.Provider, Model: event.Model, RequestFingerprint: event.RequestFingerprint,
		})
	}
	return providerCostFingerprint(payload)
}

func providerCostFingerprint(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal provider cost fingerprint: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}
