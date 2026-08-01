package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrBillingConflict           = errors.New("billing idempotency conflict")
	ErrBillingInvalid            = errors.New("invalid billing request")
	ErrBillingSKUNotFound        = errors.New("billing SKU not found")
	ErrBillingCatalogNotFound    = errors.New("billing catalog not found")
	ErrBillingUserNotFound       = errors.New("billing user not found")
	ErrBillingQuoteExpired       = errors.New("billing quote expired")
	ErrBillingQuoteConsumed      = errors.New("billing quote already consumed")
	ErrBillingProfileSKUNotFound = errors.New("billing profile SKU not found")
)

const defaultBillingQuoteTTL = 5 * time.Minute

type BillingCatalogOptions struct {
	Now      func() time.Time
	QuoteTTL time.Duration
}

type BillingCatalogService struct {
	repo     repository.Repository
	bundle   billing.Bundle
	now      func() time.Time
	quoteTTL time.Duration
	profiles *AgentProfileRegistry
}

func (s *BillingCatalogService) SetAgentProfileRegistry(registry *AgentProfileRegistry) {
	if s != nil {
		s.profiles = registry
	}
}

type ResolvedSKUPrice struct {
	SKU                 *model.BillingSKU
	PricingTier         model.Tier
	ListPriceCredits    int64
	PriceCredits        int64
	DiscountCredits     int64
	PricingRuleID       string
	PricingSnapshot     datatypes.JSON
	PeakPriceCredits    int64
	OffPeakPriceCredits int64
	TaskTimePriced      bool
}

type TaskTimePricingStatus struct {
	Timezone           string               `json:"timezone"`
	PeakWindows        []billing.TimeWindow `json:"peak_windows"`
	OffPeakWindows     []billing.TimeWindow `json:"off_peak_windows"`
	OffPeakRatePercent int64                `json:"off_peak_rate_percent"`
	CurrentPeriod      string               `json:"current_period"`
	ServerTime         time.Time            `json:"server_time"`
	NextTransitionAt   time.Time            `json:"next_transition_at"`
}

type QuoteRequest struct {
	UserID    string
	CatalogID string
	Operation string
	Route     string
	// SKUID is an internal server-selected SKU override. Public callers must
	// never populate it; it is used when a configurable capability owns a SKU
	// that is intentionally independent from its semantic route.
	SKUID                string
	ExecutionProfile     string
	AgentProfileSnapshot *model.AgentProfileSnapshot
	RequestFingerprint   string
	IdempotencyScope     string
	IdempotencyKey       string
}

// TaskQuoteRequest is the complete client-controlled identity for an Agent task
// admission quote. Catalog and SKU routing remain server-owned.
type TaskQuoteRequest struct {
	UserID             string
	TaskType           string
	ExecutionProfile   string
	RequestFingerprint string
	IdempotencyScope   string
	IdempotencyKey     string
}

func NewBillingCatalogService(repo repository.Repository, bundle *billing.Bundle, opts BillingCatalogOptions) *BillingCatalogService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	ttl := opts.QuoteTTL
	if ttl <= 0 {
		ttl = defaultBillingQuoteTTL
	}
	var snapshot billing.Bundle
	if bundle != nil {
		snapshot = *bundle
		if bundle.Products.TierRatesPercent != nil {
			snapshot.Products.TierRatesPercent = make(map[string]int64, len(bundle.Products.TierRatesPercent))
			for tier, rate := range bundle.Products.TierRatesPercent {
				snapshot.Products.TierRatesPercent[tier] = rate
			}
		}
		snapshot.Products.SKUs = make([]billing.SKUConfig, len(bundle.Products.SKUs))
		copy(snapshot.Products.SKUs, bundle.Products.SKUs)
		snapshot.Products.TaskTimePricing.PeakWindows = append([]billing.TimeWindow(nil), bundle.Products.TaskTimePricing.PeakWindows...)
		snapshot.Products.TaskTimePricing.OffPeakWindows = append([]billing.TimeWindow(nil), bundle.Products.TaskTimePricing.OffPeakWindows...)
	}
	return &BillingCatalogService{repo: repo, bundle: snapshot, now: now, quoteTTL: ttl}
}

func (s *BillingCatalogService) Publish(ctx context.Context) (*model.BillingCatalogVersion, error) {
	if s == nil || s.repo == nil || strings.TrimSpace(s.bundle.Products.CatalogID) == "" || strings.TrimSpace(s.bundle.Products.Currency) == "" {
		return nil, fmt.Errorf("%w: catalog identity and currency are required", ErrBillingInvalid)
	}
	snapshot, err := retailCatalogSnapshot(s.bundle)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	catalog := &model.BillingCatalogVersion{
		CatalogID: s.bundle.Products.CatalogID, Currency: s.bundle.Products.Currency,
		Status: "published", PublishedAt: now, Snapshot: snapshot, CreatedAt: now,
	}
	skus := make([]model.BillingSKU, 0, len(s.bundle.Products.SKUs))
	tierPrices := make([]model.BillingSKUTierPrice, 0, len(s.bundle.Products.SKUs)*len(billing.RequiredPricingTiers))
	for _, item := range s.bundle.Products.SKUs {
		itemSnapshot, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal billing SKU %q: %w", item.ID, marshalErr)
		}
		skus = append(skus, model.BillingSKU{
			ID: uuid.NewString(), CatalogID: catalog.CatalogID, SKUID: item.ID,
			Operation: item.Operation, ExecutionProfile: item.ExecutionProfile, PriceCredits: item.PriceCredits, Policy: item.ChargePolicy,
			Route: item.Route, Delivery: item.Delivery, Snapshot: itemSnapshot, CreatedAt: now,
		})
		for _, tierName := range billing.RequiredPricingTiers {
			priceCredits, ok := s.bundle.Products.PriceForTier(item.PriceCredits, tierName)
			if !ok {
				return nil, fmt.Errorf("resolve billing SKU tier price %q/%s: %w", item.ID, tierName, ErrBillingInvalid)
			}
			ratePercent := s.bundle.Products.TierRatesPercent[tierName]
			ruleID := catalog.CatalogID + ":" + tierName
			priceSnapshot, marshalErr := json.Marshal(struct {
				CatalogID     string `json:"catalog_id"`
				SKUID         string `json:"sku_id"`
				Tier          string `json:"tier"`
				ListPrice     int64  `json:"list_price_credits"`
				RatePercent   int64  `json:"rate_percent"`
				Rounding      string `json:"rounding"`
				PriceCredits  int64  `json:"price_credits"`
				PricingRuleID string `json:"pricing_rule_id"`
			}{
				CatalogID: catalog.CatalogID, SKUID: item.ID, Tier: tierName, ListPrice: item.PriceCredits,
				RatePercent: ratePercent, Rounding: "floor", PriceCredits: priceCredits, PricingRuleID: ruleID,
			})
			if marshalErr != nil {
				return nil, fmt.Errorf("marshal billing SKU tier price %q/%s: %w", item.ID, tierName, marshalErr)
			}
			tierPrices = append(tierPrices, model.BillingSKUTierPrice{
				ID: uuid.NewString(), CatalogID: catalog.CatalogID, SKUID: item.ID, Tier: model.Tier(tierName),
				PriceCredits: priceCredits, RuleID: ruleID,
				Snapshot: priceSnapshot, CreatedAt: now,
			})
		}
	}
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		existing, findErr := tx.Billing().FindCatalogVersion(ctx, catalog.CatalogID)
		if findErr == nil {
			matches, evidenceErr := sameCatalogEvidence(ctx, tx.Billing(), existing, catalog, skus, tierPrices)
			if evidenceErr != nil {
				return evidenceErr
			}
			if !matches {
				return ErrBillingConflict
			}
			catalog = existing
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if err := tx.Billing().CreateCatalogVersion(ctx, catalog); err != nil {
			return err
		}
		if err := tx.Billing().CreateSKUs(ctx, skus); err != nil {
			return err
		}
		return tx.Billing().CreateSKUTierPrices(ctx, tierPrices)
	}); err != nil {
		// A concurrent publisher can win after the initial lookup. Re-read the
		// immutable snapshot and accept it only when it is byte-identical.
		existing, findErr := s.repo.Billing().FindCatalogVersion(ctx, catalog.CatalogID)
		if findErr == nil {
			matches, evidenceErr := sameCatalogEvidence(ctx, s.repo.Billing(), existing, catalog, skus, tierPrices)
			if evidenceErr != nil {
				return nil, evidenceErr
			}
			if matches {
				return existing, nil
			}
			return nil, ErrBillingConflict
		}
		return nil, err
	}
	return catalog, nil
}

func (s *BillingCatalogService) ResolveSKUForExecutionProfile(ctx context.Context, catalogID, operation, executionProfile string) (*model.BillingSKU, error) {
	var ok bool
	if catalogID, ok = canonicalBillingText(catalogID, 128, false); !ok {
		return nil, fmt.Errorf("%w: invalid SKU identity", ErrBillingInvalid)
	}
	if operation, ok = canonicalBillingText(operation, 128, true); !ok {
		return nil, fmt.Errorf("%w: invalid SKU identity", ErrBillingInvalid)
	}
	if executionProfile, ok = canonicalBillingText(executionProfile, 40, true); !ok {
		return nil, fmt.Errorf("%w: invalid execution profile", ErrBillingInvalid)
	}
	if catalogID == "" {
		catalog, err := s.repo.Billing().FindLatestPublishedCatalog(ctx)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrBillingSKUNotFound
			}
			return nil, err
		}
		catalogID = catalog.CatalogID
	}
	sku, err := s.repo.Billing().FindSKUByOperationAndProfile(ctx, catalogID, operation, executionProfile)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBillingSKUNotFound
	}
	return sku, err
}

func (s *BillingCatalogService) ResolvePriceForExecutionProfile(ctx context.Context, userID, catalogID, operation, executionProfile string) (*ResolvedSKUPrice, error) {
	return s.ResolvePriceForExecutionProfileAt(ctx, userID, catalogID, operation, executionProfile, s.now())
}

func (s *BillingCatalogService) ResolvePriceForExecutionProfileAt(ctx context.Context, userID, catalogID, operation, executionProfile string, evaluatedAt time.Time) (*ResolvedSKUPrice, error) {
	user, err := s.repo.Users().FindByID(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	tier := model.ResolveTier(user.Tier)
	sku, err := s.ResolveSKUForExecutionProfile(ctx, catalogID, operation, executionProfile)
	if err != nil {
		return nil, err
	}
	return s.resolvePriceForSKU(ctx, sku, tier, evaluatedAt)
}

func (s *BillingCatalogService) ResolveSKU(ctx context.Context, catalogID, operation, route string) (*model.BillingSKU, error) {
	var ok bool
	if catalogID, ok = canonicalBillingText(catalogID, 128, false); !ok {
		return nil, fmt.Errorf("%w: invalid SKU identity", ErrBillingInvalid)
	}
	if operation, ok = canonicalBillingText(operation, 128, true); !ok {
		return nil, fmt.Errorf("%w: invalid SKU identity", ErrBillingInvalid)
	}
	if route, ok = canonicalBillingText(route, 128, false); !ok {
		return nil, fmt.Errorf("%w: invalid SKU identity", ErrBillingInvalid)
	}
	if catalogID == "" {
		catalog, err := s.repo.Billing().FindLatestPublishedCatalog(ctx)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrBillingSKUNotFound
			}
			return nil, err
		}
		catalogID = catalog.CatalogID
	}
	sku, err := s.repo.Billing().FindSKUByOperation(ctx, catalogID, operation, route)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBillingSKUNotFound
	}
	return sku, err
}

// ResolveSKUByID resolves a configured retail SKU identity. SKU IDs are
// internal billing identities and must never be accepted from a public client
// as a provider/model selector.
func (s *BillingCatalogService) ResolveSKUByID(ctx context.Context, catalogID, skuID string) (*model.BillingSKU, error) {
	var ok bool
	if catalogID, ok = canonicalBillingText(catalogID, 128, false); !ok {
		return nil, fmt.Errorf("%w: invalid SKU identity", ErrBillingInvalid)
	}
	if skuID, ok = canonicalBillingText(skuID, 128, false); !ok || skuID == "" {
		return nil, fmt.Errorf("%w: invalid SKU identity", ErrBillingInvalid)
	}
	if catalogID == "" {
		catalog, err := s.repo.Billing().FindLatestPublishedCatalog(ctx)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrBillingSKUNotFound
			}
			return nil, err
		}
		catalogID = catalog.CatalogID
	}
	sku, err := s.repo.Billing().FindSKU(ctx, catalogID, skuID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBillingSKUNotFound
	}
	return sku, err
}

func (s *BillingCatalogService) ResolvePrice(ctx context.Context, userID, catalogID, operation, route string) (*ResolvedSKUPrice, error) {
	return s.ResolvePriceAt(ctx, userID, catalogID, operation, route, s.now())
}

func (s *BillingCatalogService) ResolvePriceAt(ctx context.Context, userID, catalogID, operation, route string, evaluatedAt time.Time) (*ResolvedSKUPrice, error) {
	user, err := s.repo.Users().FindByID(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	tier := model.ResolveTier(user.Tier)
	return s.resolvePriceForTierAt(ctx, catalogID, operation, route, tier, evaluatedAt)
}

func (s *BillingCatalogService) ResolvePriceForTier(ctx context.Context, catalogID, operation, route string, tier model.Tier) (*ResolvedSKUPrice, error) {
	return s.resolvePriceForTierAt(ctx, catalogID, operation, route, tier, s.now())
}

func (s *BillingCatalogService) resolvePriceForTierAt(ctx context.Context, catalogID, operation, route string, tier model.Tier, evaluatedAt time.Time) (*ResolvedSKUPrice, error) {
	if !model.ValidTiers[tier] {
		return nil, fmt.Errorf("%w: invalid pricing tier", ErrBillingInvalid)
	}
	sku, err := s.ResolveSKU(ctx, catalogID, operation, route)
	if err != nil {
		return nil, err
	}
	return s.resolvePriceForSKU(ctx, sku, tier, evaluatedAt)
}

// ResolvePriceBySKUID resolves the current tier price for a configured SKU.
func (s *BillingCatalogService) ResolvePriceBySKUID(ctx context.Context, catalogID, skuID string, tier model.Tier) (*ResolvedSKUPrice, error) {
	return s.resolvePriceBySKUIDAt(ctx, catalogID, skuID, tier, s.now())
}

func (s *BillingCatalogService) resolvePriceBySKUIDAt(ctx context.Context, catalogID, skuID string, tier model.Tier, evaluatedAt time.Time) (*ResolvedSKUPrice, error) {
	if !model.ValidTiers[tier] {
		return nil, fmt.Errorf("%w: invalid pricing tier", ErrBillingInvalid)
	}
	sku, err := s.ResolveSKUByID(ctx, catalogID, skuID)
	if err != nil {
		return nil, err
	}
	return s.resolvePriceForSKU(ctx, sku, tier, evaluatedAt)
}

// ResolvePriceBySKUIDForUser resolves a configured SKU using the user's
// current tier. The SKU remains server-owned; callers never receive it from
// public client input.
func (s *BillingCatalogService) ResolvePriceBySKUIDForUser(ctx context.Context, userID, catalogID, skuID string) (*ResolvedSKUPrice, error) {
	return s.resolvePriceBySKUIDForUserAt(ctx, userID, catalogID, skuID, s.now())
}

func (s *BillingCatalogService) resolvePriceBySKUIDForUserAt(ctx context.Context, userID, catalogID, skuID string, evaluatedAt time.Time) (*ResolvedSKUPrice, error) {
	user, err := s.repo.Users().FindByID(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	return s.resolvePriceBySKUIDAt(ctx, catalogID, skuID, model.ResolveTier(user.Tier), evaluatedAt)
}

func (s *BillingCatalogService) resolvePriceForSKU(ctx context.Context, sku *model.BillingSKU, tier model.Tier, evaluatedAt time.Time) (*ResolvedSKUPrice, error) {
	if sku == nil || !model.ValidTiers[tier] {
		return nil, fmt.Errorf("%w: invalid pricing identity", ErrBillingInvalid)
	}
	price, err := s.repo.Billing().FindSKUTierPrice(ctx, sku.CatalogID, sku.SKUID, tier)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBillingSKUNotFound
	}
	if err != nil {
		return nil, err
	}
	if price.PriceCredits > sku.PriceCredits {
		return nil, fmt.Errorf("%w: tier price exceeds list price", ErrBillingInvalid)
	}
	resolved, err := tierResolvedSKUPrice(sku, price)
	if err != nil || sku.Policy != "task_admission" {
		return resolved, err
	}
	catalog, err := s.repo.Billing().FindCatalogVersion(ctx, sku.CatalogID)
	if err != nil {
		return nil, err
	}
	rule, configured, err := taskTimePricingFromCatalogSnapshot(catalog.Snapshot)
	if err != nil {
		return nil, err
	}
	if !configured {
		return resolved, nil
	}
	return applyTaskTimePricing(resolved, rule, evaluatedAt)
}

func (s *BillingCatalogService) CreateQuote(ctx context.Context, req QuoteRequest) (*model.BillingQuote, error) {
	return s.createQuoteAt(ctx, req, s.now().UTC())
}

func (s *BillingCatalogService) createQuoteAt(ctx context.Context, req QuoteRequest, evaluatedAt time.Time) (*model.BillingQuote, error) {
	var err error
	req, err = canonicalQuoteRequest(req)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(req.Operation, "task.") && req.ExecutionProfile == "" {
		return nil, fmt.Errorf("%w: agent task quotes require an execution profile", ErrBillingInvalid)
	}
	if existing, err := s.repo.Billing().FindQuoteByKey(ctx, req.IdempotencyScope, req.IdempotencyKey); err == nil {
		if quoteMatchesRequest(existing, req) {
			return existing, nil
		}
		return nil, ErrBillingConflict
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var resolved *ResolvedSKUPrice
	now := evaluatedAt.UTC()
	if req.SKUID != "" {
		resolved, err = s.resolvePriceBySKUIDForUserAt(ctx, req.UserID, req.CatalogID, req.SKUID, now)
	} else if req.ExecutionProfile != "" {
		resolved, err = s.ResolvePriceForExecutionProfileAt(ctx, req.UserID, req.CatalogID, req.Operation, req.ExecutionProfile, now)
	} else {
		resolved, err = s.ResolvePriceAt(ctx, req.UserID, req.CatalogID, req.Operation, req.Route, now)
	}
	if err != nil {
		return nil, err
	}
	sku := resolved.SKU
	var profileSnapshot datatypes.JSON
	if req.AgentProfileSnapshot != nil {
		if req.AgentProfileSnapshot.ProfileID != req.ExecutionProfile {
			return nil, fmt.Errorf("%w: execution profile snapshot mismatch", ErrBillingInvalid)
		}
		profileSnapshot, err = json.Marshal(req.AgentProfileSnapshot)
		if err != nil {
			return nil, fmt.Errorf("%w: encode execution profile snapshot: %v", ErrBillingInvalid, err)
		}
	}
	quote := &model.BillingQuote{
		ID: uuid.NewString(), UserID: req.UserID, CatalogID: sku.CatalogID,
		SKUID: sku.SKUID, PriceCredits: resolved.PriceCredits, PricingTier: string(resolved.PricingTier),
		ListPriceCredits: resolved.ListPriceCredits, DiscountCredits: resolved.DiscountCredits,
		PricingRuleID: resolved.PricingRuleID, PricingSnapshot: append(datatypes.JSON(nil), resolved.PricingSnapshot...),
		RequestFingerprint:   req.RequestFingerprint,
		SKUSnapshot:          append(datatypes.JSON(nil), sku.Snapshot...),
		AgentProfileSnapshot: append(datatypes.JSON(nil), profileSnapshot...), ExpiresAt: now.Add(s.quoteTTL), CreatedAt: now,
		IdempotencyScope: req.IdempotencyScope, IdempotencyKey: req.IdempotencyKey,
	}
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		existing, findErr := tx.Billing().FindQuoteByKey(ctx, quote.IdempotencyScope, quote.IdempotencyKey)
		if findErr == nil {
			if !quoteMatchesRequest(existing, req) {
				return ErrBillingConflict
			}
			quote = existing
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		return tx.Billing().CreateQuote(ctx, quote)
	})
	if err == nil {
		return quote, nil
	}
	if errors.Is(err, ErrBillingConflict) {
		return nil, err
	}
	existing, findErr := s.repo.Billing().FindQuoteByKey(ctx, quote.IdempotencyScope, quote.IdempotencyKey)
	if findErr == nil {
		if quoteMatchesRequest(existing, req) {
			return existing, nil
		}
		return nil, ErrBillingConflict
	}
	return nil, err
}

// CreateTaskQuote resolves a public Agent task against the current published
// catalog. It deliberately does not accept catalog, operation, route, provider,
// model, or price inputs from the caller.
func (s *BillingCatalogService) CreateTaskQuote(ctx context.Context, req TaskQuoteRequest) (*model.BillingQuote, error) {
	canonical, operation, err := canonicalTaskQuoteRequest(req)
	if err != nil {
		return nil, err
	}
	quoteRequest := QuoteRequest{
		UserID: canonical.UserID, Operation: operation, ExecutionProfile: canonical.ExecutionProfile,
		RequestFingerprint: canonical.RequestFingerprint, IdempotencyScope: canonical.IdempotencyScope, IdempotencyKey: canonical.IdempotencyKey,
	}
	if existing, findErr := s.repo.Billing().FindQuoteByKey(ctx, canonical.IdempotencyScope, canonical.IdempotencyKey); findErr == nil {
		if quoteMatchesRequest(existing, quoteRequest) {
			return existing, nil
		}
		return nil, ErrBillingConflict
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	profile, err := resolveAgentProfileForUser(ctx, s.repo, s.profiles, canonical.UserID, canonical.ExecutionProfile)
	if err != nil {
		return nil, err
	}
	profileSnapshot, _, err := profile.Freeze()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentProfileSnapshotInvalid, err)
	}
	quoteRequest.AgentProfileSnapshot = &profileSnapshot
	now := s.now().UTC()
	resolved, err := s.ResolvePriceForExecutionProfileAt(ctx, canonical.UserID, "", operation, canonical.ExecutionProfile, now)
	if err != nil {
		if errors.Is(err, ErrBillingSKUNotFound) {
			return nil, fmt.Errorf("%w: unavailable task execution profile", ErrBillingProfileSKUNotFound)
		}
		return nil, err
	}
	account, err := s.repo.Billing().FindAccount(ctx, canonical.UserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBillingLedgerInvalid
	}
	if err != nil {
		return nil, err
	}
	balance, err := account.DisplayBalance()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBillingLedgerInvalid, err)
	}
	if account.DebtCredits > 0 {
		return nil, ErrBillingDebtOutstanding
	}
	if balance < resolved.PriceCredits {
		return nil, ErrBillingInsufficientForTask
	}
	quote, err := s.createQuoteAt(ctx, quoteRequest, now)
	if errors.Is(err, ErrBillingSKUNotFound) {
		return nil, fmt.Errorf("%w: unavailable task execution profile", ErrBillingProfileSKUNotFound)
	}
	return quote, err
}

func canonicalTaskQuoteRequest(req TaskQuoteRequest) (TaskQuoteRequest, string, error) {
	var ok bool
	if req.UserID, ok = canonicalBillingUUID(req.UserID); !ok {
		return TaskQuoteRequest{}, "", fmt.Errorf("%w: invalid task quote", ErrBillingInvalid)
	}
	if req.TaskType, ok = canonicalBillingText(req.TaskType, 20, true); !ok {
		return TaskQuoteRequest{}, "", fmt.Errorf("%w: invalid task quote", ErrBillingInvalid)
	}
	operation, ok := agentTaskOperation(req.TaskType)
	if !ok {
		return TaskQuoteRequest{}, "", fmt.Errorf("%w: unsupported agent task type", ErrBillingInvalid)
	}
	if req.ExecutionProfile, ok = canonicalBillingText(req.ExecutionProfile, 40, true); !ok {
		return TaskQuoteRequest{}, "", fmt.Errorf("%w: execution profile is required", ErrBillingInvalid)
	}
	req.RequestFingerprint = strings.TrimSpace(req.RequestFingerprint)
	if !validBillingFingerprint(req.RequestFingerprint) {
		return TaskQuoteRequest{}, "", fmt.Errorf("%w: invalid task quote", ErrBillingInvalid)
	}
	if req.IdempotencyScope, ok = canonicalBillingText(req.IdempotencyScope, 80, true); !ok {
		return TaskQuoteRequest{}, "", fmt.Errorf("%w: invalid task quote", ErrBillingInvalid)
	}
	if req.IdempotencyKey, ok = canonicalBillingText(req.IdempotencyKey, 128, true); !ok {
		return TaskQuoteRequest{}, "", fmt.Errorf("%w: invalid task quote", ErrBillingInvalid)
	}
	return req, operation, nil
}

func agentTaskOperation(taskType string) (string, bool) {
	return agentpack.Default().BillingOperation(taskType)
}

func canonicalQuoteRequest(req QuoteRequest) (QuoteRequest, error) {
	var ok bool
	if req.UserID, ok = canonicalBillingUUID(req.UserID); !ok {
		return QuoteRequest{}, fmt.Errorf("%w: invalid quote", ErrBillingInvalid)
	}
	if req.CatalogID, ok = canonicalBillingText(req.CatalogID, 128, false); !ok {
		return QuoteRequest{}, fmt.Errorf("%w: invalid quote", ErrBillingInvalid)
	}
	if req.Operation, ok = canonicalBillingText(req.Operation, 128, true); !ok {
		return QuoteRequest{}, fmt.Errorf("%w: invalid quote", ErrBillingInvalid)
	}
	if req.Route, ok = canonicalBillingText(req.Route, 128, false); !ok {
		return QuoteRequest{}, fmt.Errorf("%w: invalid quote", ErrBillingInvalid)
	}
	if req.ExecutionProfile, ok = canonicalBillingText(req.ExecutionProfile, 40, false); !ok {
		return QuoteRequest{}, fmt.Errorf("%w: invalid quote", ErrBillingInvalid)
	}
	if req.ExecutionProfile != "" && req.Route != "" {
		return QuoteRequest{}, fmt.Errorf("%w: route and execution_profile are mutually exclusive", ErrBillingInvalid)
	}
	if req.SKUID, ok = canonicalBillingText(req.SKUID, 128, false); !ok {
		return QuoteRequest{}, fmt.Errorf("%w: invalid quote", ErrBillingInvalid)
	}
	req.RequestFingerprint = strings.TrimSpace(req.RequestFingerprint)
	if !validBillingFingerprint(req.RequestFingerprint) {
		return QuoteRequest{}, fmt.Errorf("%w: invalid quote", ErrBillingInvalid)
	}
	if req.IdempotencyScope, ok = canonicalBillingText(req.IdempotencyScope, 80, true); !ok {
		return QuoteRequest{}, fmt.Errorf("%w: invalid quote", ErrBillingInvalid)
	}
	if req.IdempotencyKey, ok = canonicalBillingText(req.IdempotencyKey, 128, true); !ok {
		return QuoteRequest{}, fmt.Errorf("%w: invalid quote", ErrBillingInvalid)
	}
	return req, nil
}

func retailCatalogSnapshot(bundle billing.Bundle) (datatypes.JSON, error) {
	type productsSnapshot struct {
		Currency         string                  `json:"currency"`
		TierRatesPercent map[string]int64        `json:"tier_rates_percent"`
		TaskTimePricing  billing.TaskTimePricing `json:"task_time_pricing"`
		SKUs             []billing.SKUConfig     `json:"skus"`
	}
	snapshot := struct {
		Products  productsSnapshot        `json:"products"`
		Economics billing.EconomicsConfig `json:"economics"`
		Policy    billing.PolicySnapshot  `json:"policy"`
	}{
		Products:  productsSnapshot{Currency: bundle.Products.Currency, TierRatesPercent: bundle.Products.TierRatesPercent, TaskTimePricing: bundle.Products.TaskTimePricing, SKUs: bundle.Products.SKUs},
		Economics: bundle.Economics,
		Policy:    bundle.Policy,
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal retail billing catalog: %w", err)
	}
	return encoded, nil
}

func taskTimePricingFromCatalogSnapshot(snapshot []byte) (billing.TaskTimePricing, bool, error) {
	var value struct {
		Products struct {
			TaskTimePricing billing.TaskTimePricing `json:"task_time_pricing"`
		} `json:"products"`
	}
	if err := json.Unmarshal(snapshot, &value); err != nil {
		return billing.TaskTimePricing{}, false, fmt.Errorf("%w: catalog task time pricing is invalid", ErrBillingInvalid)
	}
	if strings.TrimSpace(value.Products.TaskTimePricing.Timezone) == "" {
		return billing.TaskTimePricing{}, false, nil
	}
	return value.Products.TaskTimePricing, true, nil
}

func applyTaskTimePricing(resolved *ResolvedSKUPrice, rule billing.TaskTimePricing, evaluatedAt time.Time) (*ResolvedSKUPrice, error) {
	if resolved == nil {
		return nil, fmt.Errorf("%w: missing resolved task price", ErrBillingInvalid)
	}
	location, err := time.LoadLocation(rule.Timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: catalog task time pricing timezone is invalid", ErrBillingInvalid)
	}
	local := evaluatedAt.In(location)
	period, _, err := evaluateTaskTimePeriod(rule, local)
	if err != nil {
		return nil, err
	}
	peakPrice := resolved.PriceCredits
	offPeakPrice := percentageFloor(peakPrice, rule.OffPeakRatePercent)
	finalPrice := peakPrice
	timePercent := int64(100)
	if period == "off_peak" {
		finalPrice = offPeakPrice
		timePercent = rule.OffPeakRatePercent
	}
	var tierSnapshot struct {
		RatePercent int64 `json:"rate_percent"`
	}
	if err := json.Unmarshal(resolved.PricingSnapshot, &tierSnapshot); err != nil {
		return nil, fmt.Errorf("%w: catalog tier pricing snapshot is invalid", ErrBillingInvalid)
	}
	ruleID := resolved.SKU.CatalogID + ":" + string(resolved.PricingTier) + ":" + period
	pricingSnapshot, err := json.Marshal(struct {
		CatalogID         string               `json:"catalog_id"`
		SKUID             string               `json:"sku_id"`
		Tier              string               `json:"tier"`
		ListPriceCredits  int64                `json:"list_price_credits"`
		Timezone          string               `json:"timezone"`
		PeakWindows       []billing.TimeWindow `json:"peak_windows"`
		OffPeakWindows    []billing.TimeWindow `json:"off_peak_windows"`
		MembershipPercent int64                `json:"membership_percent"`
		TimePercent       int64                `json:"time_percent"`
		EvaluatedAt       time.Time            `json:"evaluated_at"`
		Period            string               `json:"period"`
		Rounding          string               `json:"rounding"`
		FinalPriceCredits int64                `json:"final_price_credits"`
		PricingRuleID     string               `json:"pricing_rule_id"`
	}{resolved.SKU.CatalogID, resolved.SKU.SKUID, string(resolved.PricingTier), resolved.ListPriceCredits, rule.Timezone,
		rule.PeakWindows, rule.OffPeakWindows, tierSnapshot.RatePercent, timePercent, local, period, "floor", finalPrice, ruleID})
	if err != nil {
		return nil, err
	}
	resolved.PeakPriceCredits = peakPrice
	resolved.OffPeakPriceCredits = offPeakPrice
	resolved.TaskTimePriced = true
	resolved.PriceCredits = finalPrice
	resolved.DiscountCredits = resolved.ListPriceCredits - finalPrice
	resolved.PricingRuleID = ruleID
	resolved.PricingSnapshot = pricingSnapshot
	return resolved, nil
}

func percentageFloor(value, percent int64) int64 {
	return value/100*percent + value%100*percent/100
}

func evaluateTaskTimePeriod(rule billing.TaskTimePricing, local time.Time) (string, time.Time, error) {
	minute := local.Hour()*60 + local.Minute()
	period := "off_peak"
	boundaries := make([]int, 0, len(rule.PeakWindows)*2)
	for _, window := range rule.PeakWindows {
		start, ok := serviceClockMinute(window.Start)
		if !ok {
			return "", time.Time{}, fmt.Errorf("%w: catalog peak window is invalid", ErrBillingInvalid)
		}
		end, ok := serviceClockMinute(window.End)
		if !ok {
			return "", time.Time{}, fmt.Errorf("%w: catalog peak window is invalid", ErrBillingInvalid)
		}
		if minute >= start && minute < end {
			period = "peak"
		}
		boundaries = append(boundaries, start, end)
	}
	sort.Ints(boundaries)
	nextMinute := -1
	for _, boundary := range boundaries {
		if boundary > minute {
			nextMinute = boundary
			break
		}
	}
	nextDay := nextMinute < 0
	if nextDay {
		for _, boundary := range boundaries {
			if boundary < 24*60 {
				nextMinute = boundary
				break
			}
		}
	}
	if nextMinute < 0 {
		return "", time.Time{}, fmt.Errorf("%w: catalog peak windows have no transition", ErrBillingInvalid)
	}
	if nextMinute == 24*60 {
		nextMinute = 0
		nextDay = true
	}
	next := time.Date(local.Year(), local.Month(), local.Day(), nextMinute/60, nextMinute%60, 0, 0, local.Location())
	if nextDay {
		next = next.AddDate(0, 0, 1)
	}
	return period, next, nil
}

func serviceClockMinute(value string) (int, bool) {
	if len(value) != 5 || value[2] != ':' {
		return 0, false
	}
	hour, hourErr := strconv.Atoi(value[:2])
	minute, minuteErr := strconv.Atoi(value[3:])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 24 || minute < 0 || minute > 59 || (hour == 24 && minute != 0) {
		return 0, false
	}
	return hour*60 + minute, true
}

func (s *BillingCatalogService) CurrentTaskTimePricing() (TaskTimePricingStatus, error) {
	rule := s.bundle.Products.TaskTimePricing
	location, err := time.LoadLocation(rule.Timezone)
	if err != nil {
		return TaskTimePricingStatus{}, fmt.Errorf("%w: task time pricing timezone is invalid", ErrBillingInvalid)
	}
	local := s.now().In(location)
	period, next, err := evaluateTaskTimePeriod(rule, local)
	if err != nil {
		return TaskTimePricingStatus{}, err
	}
	return TaskTimePricingStatus{Timezone: rule.Timezone, PeakWindows: append([]billing.TimeWindow(nil), rule.PeakWindows...),
		OffPeakWindows: append([]billing.TimeWindow(nil), rule.OffPeakWindows...), OffPeakRatePercent: rule.OffPeakRatePercent,
		CurrentPeriod: period, ServerTime: local, NextTransitionAt: next}, nil
}

func sameCatalog(left, right *model.BillingCatalogVersion) bool {
	return left != nil && right != nil && left.CatalogID == right.CatalogID && left.Currency == right.Currency &&
		left.Status == right.Status && sameJSONSemantic(left.Snapshot, right.Snapshot)
}

func sameCatalogEvidence(ctx context.Context, repo repository.BillingRepository, existing, expected *model.BillingCatalogVersion, expectedSKUs []model.BillingSKU, expectedTierPrices []model.BillingSKUTierPrice) (bool, error) {
	if !sameCatalog(existing, expected) {
		return false, nil
	}
	persisted, err := repo.ListSKUsByCatalog(ctx, existing.CatalogID)
	if err != nil {
		return false, err
	}
	if len(persisted) != len(expectedSKUs) {
		return false, nil
	}
	expectedByID := make(map[string]model.BillingSKU, len(expectedSKUs))
	for _, sku := range expectedSKUs {
		expectedByID[sku.SKUID] = sku
	}
	for _, sku := range persisted {
		want, ok := expectedByID[sku.SKUID]
		if !ok || sku.CatalogID != want.CatalogID || sku.Operation != want.Operation || sku.ExecutionProfile != want.ExecutionProfile || sku.PriceCredits != want.PriceCredits ||
			sku.Policy != want.Policy || sku.Route != want.Route || sku.Delivery != want.Delivery || !sameJSONSemantic(sku.Snapshot, want.Snapshot) {
			return false, nil
		}
		delete(expectedByID, sku.SKUID)
	}
	if len(expectedByID) != 0 {
		return false, nil
	}
	persistedPrices, err := repo.ListSKUTierPricesByCatalog(ctx, existing.CatalogID)
	if err != nil {
		return false, err
	}
	if len(persistedPrices) != len(expectedTierPrices) {
		return false, nil
	}
	expectedPriceByKey := make(map[string]model.BillingSKUTierPrice, len(expectedTierPrices))
	for _, price := range expectedTierPrices {
		expectedPriceByKey[price.SKUID+":"+string(price.Tier)] = price
	}
	for _, price := range persistedPrices {
		want, ok := expectedPriceByKey[price.SKUID+":"+string(price.Tier)]
		if !ok || price.CatalogID != want.CatalogID || price.PriceCredits != want.PriceCredits || price.RuleID != want.RuleID || !sameJSONSemantic(price.Snapshot, want.Snapshot) {
			return false, nil
		}
		delete(expectedPriceByKey, price.SKUID+":"+string(price.Tier))
	}
	return len(expectedPriceByKey) == 0, nil
}

func sameJSONSemantic(left, right []byte) bool {
	if !json.Valid(left) || !json.Valid(right) {
		return false
	}
	var leftValue, rightValue any
	leftDecoder := json.NewDecoder(bytes.NewReader(left))
	leftDecoder.UseNumber()
	rightDecoder := json.NewDecoder(bytes.NewReader(right))
	rightDecoder.UseNumber()
	if leftDecoder.Decode(&leftValue) != nil || rightDecoder.Decode(&rightValue) != nil {
		return false
	}
	leftCanonical, leftErr := json.Marshal(leftValue)
	rightCanonical, rightErr := json.Marshal(rightValue)
	return leftErr == nil && rightErr == nil && string(leftCanonical) == string(rightCanonical)
}

func quoteMatchesRequest(existing *model.BillingQuote, req QuoteRequest) bool {
	if existing == nil || existing.UserID != strings.TrimSpace(req.UserID) || existing.RequestFingerprint != req.RequestFingerprint {
		return false
	}
	if catalogID := strings.TrimSpace(req.CatalogID); catalogID != "" && existing.CatalogID != catalogID {
		return false
	}
	var pinned billing.SKUConfig
	if err := json.Unmarshal(existing.SKUSnapshot, &pinned); err != nil {
		return false
	}
	if pinned.ID != existing.SKUID || (req.SKUID != "" && pinned.ID != req.SKUID) || (req.SKUID == "" && (pinned.Operation != strings.TrimSpace(req.Operation) || pinned.Route != strings.TrimSpace(req.Route) || pinned.ExecutionProfile != strings.TrimSpace(req.ExecutionProfile))) {
		return false
	}
	if req.AgentProfileSnapshot == nil {
		return true
	}
	want, err := json.Marshal(req.AgentProfileSnapshot)
	return err == nil && sameJSONSemantic(existing.AgentProfileSnapshot, want)
}

func validBillingFingerprint(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func canonicalBillingText(value string, maxLength int, required bool) (string, bool) {
	value = strings.TrimSpace(value)
	if (required && value == "") || len(value) > maxLength {
		return "", false
	}
	return value, true
}

func canonicalBillingUUID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	parsed, err := uuid.Parse(value)
	if err != nil {
		return "", false
	}
	return parsed.String(), true
}

func billingFingerprint(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
