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
	ErrBillingConflict        = errors.New("billing idempotency conflict")
	ErrBillingInvalid         = errors.New("invalid billing request")
	ErrBillingSKUNotFound     = errors.New("billing SKU not found")
	ErrBillingCatalogNotFound = errors.New("billing catalog not found")
	ErrBillingUserNotFound    = errors.New("billing user not found")
	ErrBillingQuoteExpired    = errors.New("billing quote expired")
	ErrBillingQuoteConsumed   = errors.New("billing quote already consumed")
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
}

type QuoteRequest struct {
	UserID             string
	CatalogID          string
	Operation          string
	Route              string
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
		snapshot.Products.SKUs = append([]billing.SKUConfig(nil), bundle.Products.SKUs...)
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
	for _, item := range s.bundle.Products.SKUs {
		itemSnapshot, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal billing SKU %q: %w", item.ID, marshalErr)
		}
		skus = append(skus, model.BillingSKU{
			ID: uuid.NewString(), CatalogID: catalog.CatalogID, SKUID: item.ID,
			Operation: item.Operation, PriceCredits: item.PriceCredits, Policy: item.ChargePolicy,
			Route: item.Route, Delivery: item.Delivery, Snapshot: itemSnapshot, CreatedAt: now,
		})
	}
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		existing, findErr := tx.Billing().FindCatalogVersion(ctx, catalog.CatalogID)
		if findErr == nil {
			matches, evidenceErr := sameCatalogEvidence(ctx, tx.Billing(), existing, catalog, skus)
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
		return tx.Billing().CreateSKUs(ctx, skus)
	}); err != nil {
		// A concurrent publisher can win after the initial lookup. Re-read the
		// immutable snapshot and accept it only when it is byte-identical.
		existing, findErr := s.repo.Billing().FindCatalogVersion(ctx, catalog.CatalogID)
		if findErr == nil {
			matches, evidenceErr := sameCatalogEvidence(ctx, s.repo.Billing(), existing, catalog, skus)
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

func (s *BillingCatalogService) CreateQuote(ctx context.Context, req QuoteRequest) (*model.BillingQuote, error) {
	var err error
	req, err = canonicalQuoteRequest(req)
	if err != nil {
		return nil, err
	}
	if existing, err := s.repo.Billing().FindQuoteByKey(ctx, req.IdempotencyScope, req.IdempotencyKey); err == nil {
		if quoteMatchesRequest(existing, req) {
			return existing, nil
		}
		return nil, ErrBillingConflict
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	sku, err := s.ResolveSKU(ctx, req.CatalogID, req.Operation, req.Route)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	quote := &model.BillingQuote{
		ID: uuid.NewString(), UserID: req.UserID, CatalogID: sku.CatalogID,
		SKUID: sku.SKUID, PriceCredits: sku.PriceCredits, RequestFingerprint: req.RequestFingerprint,
		SKUSnapshot: append(datatypes.JSON(nil), sku.Snapshot...), ExpiresAt: now.Add(s.quoteTTL), CreatedAt: now,
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
		left.Status == right.Status && sameJSONSemantic(left.Snapshot, right.Snapshot)
}

func sameCatalogEvidence(ctx context.Context, repo repository.BillingRepository, existing, expected *model.BillingCatalogVersion, expectedSKUs []model.BillingSKU) (bool, error) {
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
		if !ok || sku.CatalogID != want.CatalogID || sku.Operation != want.Operation || sku.PriceCredits != want.PriceCredits ||
			sku.Policy != want.Policy || sku.Route != want.Route || sku.Delivery != want.Delivery || !sameJSONSemantic(sku.Snapshot, want.Snapshot) {
			return false, nil
		}
		delete(expectedByID, sku.SKUID)
	}
	return len(expectedByID) == 0, nil
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
	return pinned.ID == existing.SKUID && pinned.Operation == strings.TrimSpace(req.Operation) && pinned.Route == strings.TrimSpace(req.Route)
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
