package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	SKU              *model.BillingSKU
	PricingTier      model.Tier
	ListPriceCredits int64
	PriceCredits     int64
	DiscountCredits  int64
	PricingRuleID    string
	PricingSnapshot  datatypes.JSON
}

type QuoteRequest struct {
	UserID               string
	CatalogID            string
	Operation            string
	Route                string
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
		if snapshot.Products.PricingModel == "" {
			snapshot.Products.PricingModel = billing.PricingModelFlatV1
		}
		snapshot.Products.SKUs = make([]billing.SKUConfig, len(bundle.Products.SKUs))
		for index, sku := range bundle.Products.SKUs {
			snapshot.Products.SKUs[index] = sku
			if sku.TierPrices != nil {
				snapshot.Products.SKUs[index].TierPrices = make(map[string]int64, len(sku.TierPrices))
				for tier, price := range sku.TierPrices {
					snapshot.Products.SKUs[index].TierPrices[tier] = price
				}
			}
		}
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
		CatalogID: s.bundle.Products.CatalogID, Currency: s.bundle.Products.Currency, PricingModel: s.bundle.Products.PricingModel,
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
			priceCredits, ok := item.TierPrices[tierName]
			if !ok {
				continue
			}
			priceSnapshot, marshalErr := json.Marshal(struct {
				CatalogID     string `json:"catalog_id"`
				SKUID         string `json:"sku_id"`
				Tier          string `json:"tier"`
				ListPrice     int64  `json:"list_price_credits"`
				PriceCredits  int64  `json:"price_credits"`
				PricingRuleID string `json:"pricing_rule_id"`
			}{
				CatalogID: catalog.CatalogID, SKUID: item.ID, Tier: tierName, ListPrice: item.PriceCredits,
				PriceCredits: priceCredits, PricingRuleID: catalog.CatalogID + ":" + item.ID + ":" + tierName,
			})
			if marshalErr != nil {
				return nil, fmt.Errorf("marshal billing SKU tier price %q/%s: %w", item.ID, tierName, marshalErr)
			}
			tierPrices = append(tierPrices, model.BillingSKUTierPrice{
				ID: uuid.NewString(), CatalogID: catalog.CatalogID, SKUID: item.ID, Tier: model.Tier(tierName),
				PriceCredits: priceCredits, RuleID: catalog.CatalogID + ":" + item.ID + ":" + tierName,
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
	user, err := s.repo.Users().FindByID(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	tier := model.ResolveTier(user.Tier)
	sku, err := s.ResolveSKUForExecutionProfile(ctx, catalogID, operation, executionProfile)
	if err != nil {
		return nil, err
	}
	return s.resolvePriceForSKU(ctx, sku, tier)
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

func (s *BillingCatalogService) ResolvePrice(ctx context.Context, userID, catalogID, operation, route string) (*ResolvedSKUPrice, error) {
	user, err := s.repo.Users().FindByID(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	tier := model.ResolveTier(user.Tier)
	return s.ResolvePriceForTier(ctx, catalogID, operation, route, tier)
}

func (s *BillingCatalogService) ResolvePriceForTier(ctx context.Context, catalogID, operation, route string, tier model.Tier) (*ResolvedSKUPrice, error) {
	if !model.ValidTiers[tier] {
		return nil, fmt.Errorf("%w: invalid pricing tier", ErrBillingInvalid)
	}
	sku, err := s.ResolveSKU(ctx, catalogID, operation, route)
	if err != nil {
		return nil, err
	}
	return s.resolvePriceForSKU(ctx, sku, tier)
}

func (s *BillingCatalogService) resolvePriceForSKU(ctx context.Context, sku *model.BillingSKU, tier model.Tier) (*ResolvedSKUPrice, error) {
	if sku == nil || !model.ValidTiers[tier] {
		return nil, fmt.Errorf("%w: invalid pricing identity", ErrBillingInvalid)
	}
	catalog, err := s.repo.Billing().FindCatalogVersion(ctx, sku.CatalogID)
	if err != nil {
		return nil, err
	}
	resolved := &ResolvedSKUPrice{
		SKU: sku, PricingTier: tier, ListPriceCredits: sku.PriceCredits, PriceCredits: sku.PriceCredits,
		PricingRuleID: sku.CatalogID + ":" + sku.SKUID + ":flat",
	}
	if catalog.PricingModel != billing.PricingModelTierMatrixV1 {
		resolved.PricingSnapshot, _ = json.Marshal(struct {
			CatalogID     string     `json:"catalog_id"`
			SKUID         string     `json:"sku_id"`
			Tier          model.Tier `json:"tier"`
			PriceCredits  int64      `json:"price_credits"`
			PricingRuleID string     `json:"pricing_rule_id"`
		}{sku.CatalogID, sku.SKUID, tier, sku.PriceCredits, resolved.PricingRuleID})
		return resolved, nil
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
	resolved.PriceCredits = price.PriceCredits
	resolved.DiscountCredits = sku.PriceCredits - price.PriceCredits
	resolved.PricingRuleID = price.RuleID
	resolved.PricingSnapshot = append(datatypes.JSON(nil), price.Snapshot...)
	return resolved, nil
}

func (s *BillingCatalogService) CreateQuote(ctx context.Context, req QuoteRequest) (*model.BillingQuote, error) {
	var err error
	req, err = canonicalQuoteRequest(req)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(req.Operation, "task.") && req.ExecutionProfile == "" && req.Operation != "task.viral_analysis" {
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
	if req.ExecutionProfile != "" {
		resolved, err = s.ResolvePriceForExecutionProfile(ctx, req.UserID, req.CatalogID, req.Operation, req.ExecutionProfile)
	} else {
		resolved, err = s.ResolvePrice(ctx, req.UserID, req.CatalogID, req.Operation, req.Route)
	}
	if err != nil {
		return nil, err
	}
	sku := resolved.SKU
	now := s.now().UTC()
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
	resolved, err := s.ResolvePriceForExecutionProfile(ctx, canonical.UserID, "", operation, canonical.ExecutionProfile)
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
	quote, err := s.CreateQuote(ctx, quoteRequest)
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
	switch taskType {
	case "article", "seednote", "moments", "ecommerce", "montage":
		return "task." + taskType, true
	default:
		return "", false
	}
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
	snapshot := struct {
		Products billing.ProductCatalog `json:"products"`
		Policy   struct {
			Version       string                      `json:"version"`
			TaskAdmission billing.TaskAdmissionPolicy `json:"task_admission"`
			AcceptedTask  billing.AcceptedTaskPolicy  `json:"accepted_task"`
			TopUp         billing.TopUpPolicy         `json:"top_up"`
			Promotions    billing.PromotionsPolicy    `json:"promotions"`
		} `json:"policy"`
	}{Products: bundle.Products}
	snapshot.Policy.Version = bundle.Policy.Version
	snapshot.Policy.TaskAdmission = bundle.Policy.TaskAdmission
	snapshot.Policy.AcceptedTask = bundle.Policy.AcceptedTask
	snapshot.Policy.TopUp = bundle.Policy.TopUp
	snapshot.Policy.Promotions = bundle.Policy.Promotions
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal retail billing catalog: %w", err)
	}
	return encoded, nil
}

func sameCatalog(left, right *model.BillingCatalogVersion) bool {
	return left != nil && right != nil && left.CatalogID == right.CatalogID && left.Currency == right.Currency &&
		left.PricingModel == right.PricingModel && left.Status == right.Status && sameJSONSemantic(left.Snapshot, right.Snapshot)
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
	if pinned.ID != existing.SKUID || pinned.Operation != strings.TrimSpace(req.Operation) || pinned.Route != strings.TrimSpace(req.Route) || pinned.ExecutionProfile != strings.TrimSpace(req.ExecutionProfile) {
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
