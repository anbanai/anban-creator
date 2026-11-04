package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/app/config"
	"github.com/anbanai/anban-creator/server/app/image"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// ErrURLNotOwned is returned by UploadReferenceFromURL when the supplied URL
// does not point at this backend's storage or is not owned by the calling user.
// Handlers should map this to a 4xx response.
var ErrURLNotOwned = errors.New("url not allowed")

// ErrDesignerReferenceInvalid covers malformed, missing, and unowned Designer
// reference identities without disclosing which ownership check failed.
var ErrDesignerReferenceInvalid = errors.New("designer reference is invalid or unavailable")

// ErrDesignerCapabilityAccessDenied is returned when a valid capability is
// configured but the caller's subscription tier does not include it.
var ErrDesignerCapabilityAccessDenied = errors.New("designer capability access denied")

// ErrDesignerCapabilityUnavailable is returned for unknown or disabled public
// capability identifiers.
var ErrDesignerCapabilityUnavailable = errors.New("designer capability unavailable")

var ErrDesignerInvalidInput = errors.New("designer invalid input")

type DesignerValidationError struct{ Message string }

func (e *DesignerValidationError) Error() string        { return e.Message }
func (e *DesignerValidationError) Is(target error) bool { return target == ErrDesignerInvalidInput }

func designerInputError(format string, args ...any) error {
	return &DesignerValidationError{Message: fmt.Sprintf(format, args...)}
}

var ErrDesignerReferenceUnsupported = errors.New("selected image capability does not support reference images")
var ErrDesignerReferenceLimitExceeded = errors.New("selected image capability reference limit exceeded")

type DesignerReferenceLimitError struct {
	Requested          int `json:"requested"`
	MaxReferenceImages int `json:"max_reference_images"`
}

func (e *DesignerReferenceLimitError) Error() string {
	return fmt.Sprintf("%s: requested %d, maximum %d", ErrDesignerReferenceLimitExceeded, e.Requested, e.MaxReferenceImages)
}

func (e *DesignerReferenceLimitError) Unwrap() error { return ErrDesignerReferenceLimitExceeded }

const maxDesignerReferenceBytes int64 = 10 * 1024 * 1024

const (
	designerOutputUnavailableMessage = "图片已生成，但保存到作品库失败，请稍后重试"
	designerGenericFailureMessage    = "图片生成失败，请稍后重试"
)

type DesignerService struct {
	db              *gorm.DB
	repo            repository.Repository
	fullCfg         *srvconfig.Config
	storage         storage.Provider
	referenceRepo   repository.DesignerReferenceRepository
	logger          *zerolog.Logger
	providerFactory func(*config.ImageAPI, *zerolog.Logger) (image.Provider, error)
	providerCostSvc *ProviderCostService
	billingCatalog  *BillingCatalogService
	billingWallet   *BillingWalletService
}

func (s *DesignerService) SetProviderCostService(providerCostSvc *ProviderCostService) {
	if s != nil {
		s.providerCostSvc = providerCostSvc
	}
}

func (s *DesignerService) SetBillingCatalogService(catalog *BillingCatalogService) {
	if s != nil {
		s.billingCatalog = catalog
	}
}

func (s *DesignerService) SetBillingWalletService(wallet *BillingWalletService) {
	if s != nil {
		s.billingWallet = wallet
	}
}

func NewDesignerService(
	db *gorm.DB,
	fullCfg *srvconfig.Config,
	store storage.Provider,
	logger *zerolog.Logger,
) *DesignerService {
	var repo repository.Repository
	var referenceRepo repository.DesignerReferenceRepository
	if db != nil {
		repo = repository.New(db)
		referenceRepo = repository.NewDesignerReferenceRepository(db)
	}
	return &DesignerService{
		db:              db,
		repo:            repo,
		referenceRepo:   referenceRepo,
		fullCfg:         fullCfg,
		storage:         store,
		logger:          logger,
		providerFactory: image.NewProvider,
	}
}

type DesignerGenerateRequest struct {
	QuoteID            string   `json:"quote_id"`
	OperationID        string   `json:"operation_id"`
	RequestFingerprint string   `json:"request_fingerprint"`
	ProjectID          string   `json:"project_id"`
	Prompt             string   `json:"prompt"`
	CapabilityKey      string   `json:"capability_key"`
	Quality            string   `json:"quality"`
	Size               string   `json:"size"`
	N                  int      `json:"n"`
	OutputFormat       string   `json:"output_format"`
	OutputCompression  int      `json:"output_compression,omitempty"`
	Background         string   `json:"background,omitempty"`
	BackgroundOpacity  *int     `json:"background_opacity,omitempty"`
	ReferenceFileIDs   []string `json:"reference_file_ids,omitempty"`
	MaskFileID         string   `json:"mask_file_id,omitempty"`
	Watermark          *bool    `json:"watermark,omitempty"`
}

type DesignerGenerationCreated struct {
	GenerationID string `json:"generation_id"`
	Status       string `json:"status"`
	PriceCredits int    `json:"price_credits"`
}

type DesignerGenerationQuote struct {
	QuoteID            string    `json:"quote_id"`
	OperationID        string    `json:"operation_id"`
	RequestFingerprint string    `json:"request_fingerprint"`
	CatalogID          string    `json:"catalog_id"`
	SKUID              string    `json:"sku_id"`
	PricingTier        string    `json:"pricing_tier"`
	ListPriceCredits   int64     `json:"list_price_credits"`
	PriceCredits       int64     `json:"price_credits"`
	DiscountCredits    int64     `json:"discount_credits"`
	ExpiresAt          time.Time `json:"expires_at"`
}

type DesignerGenerationPublic struct {
	ID                string                        `json:"id"`
	ProjectID         string                        `json:"project_id"`
	Prompt            string                        `json:"prompt"`
	RevisedPrompt     string                        `json:"revised_prompt,omitempty"`
	CapabilityKey     string                        `json:"capability_key,omitempty"`
	CapabilityName    string                        `json:"capability_name,omitempty"`
	Quality           string                        `json:"quality,omitempty"`
	Size              string                        `json:"size,omitempty"`
	N                 int                           `json:"n"`
	OutputFormat      string                        `json:"output_format,omitempty"`
	OutputCompression int                           `json:"output_compression"`
	Background        string                        `json:"background,omitempty"`
	BackgroundOpacity *int                          `json:"background_opacity,omitempty"`
	Watermark         bool                          `json:"watermark"`
	ReferenceFileIDs  []string                      `json:"reference_file_ids,omitempty"`
	MaskFileID        string                        `json:"mask_file_id,omitempty"`
	Status            string                        `json:"status"`
	Error             string                        `json:"error,omitempty"`
	ErrorCode         string                        `json:"error_code,omitempty"`
	Cost              int                           `json:"cost,omitempty"`
	EstimatedCost     int                           `json:"estimated_cost,omitempty"`
	FinalCost         int                           `json:"final_cost,omitempty"`
	BillingMode       string                        `json:"billing_mode,omitempty"`
	BillingStatus     string                        `json:"billing_status,omitempty"`
	CreatedAt         time.Time                     `json:"created_at"`
	UpdatedAt         time.Time                     `json:"updated_at"`
	Results           []model.ImageGenerationResult `json:"results,omitempty"`
}

func (s *DesignerService) publicGeneration(gen *model.ImageGeneration) *DesignerGenerationPublic {
	if gen == nil {
		return nil
	}
	capKey, capName := "", ""
	if route, ok := s.designerRoute(gen.ProviderID); ok {
		capKey, capName = gen.ProviderID, route.Alias
	}
	if capName == "" {
		capName = "图像能力"
	}
	finalCost := gen.FinalCost
	if gen.Status == model.ImageGenerationStatusCompleted && gen.BillingMode == "fixed_sku" {
		finalCost = gen.Cost
	}
	var referenceFileIDs []string
	_ = json.Unmarshal([]byte(gen.ReferenceFiles), &referenceFileIDs)
	return &DesignerGenerationPublic{ID: gen.ID, ProjectID: gen.ProjectID, Prompt: gen.Prompt,
		RevisedPrompt: gen.RevisedPrompt, CapabilityKey: capKey, CapabilityName: capName,
		Quality: gen.Quality, Size: gen.Size, N: gen.N, OutputFormat: gen.OutputFormat,
		OutputCompression: gen.OutputCompression, Background: gen.Background, BackgroundOpacity: gen.BackgroundOpacity, Watermark: gen.Watermark,
		ReferenceFileIDs: referenceFileIDs, MaskFileID: gen.MaskFileID,
		Status: gen.Status, Error: gen.Error, ErrorCode: designerGenerationErrorCode(gen.Error), Cost: gen.Cost, EstimatedCost: gen.EstimatedCost,
		FinalCost: finalCost, BillingMode: gen.BillingMode, BillingStatus: gen.BillingStatus,
		CreatedAt: gen.CreatedAt, UpdatedAt: gen.UpdatedAt, Results: gen.Results}
}

func (s *DesignerService) decorateDesignerBilling(ctx context.Context, gen *model.ImageGeneration, public *DesignerGenerationPublic) error {
	if gen.Status != model.ImageGenerationStatusFailed || gen.BillingChargeID == nil || *gen.BillingChargeID == "" {
		return nil
	}
	public.FinalCost = gen.Cost
	var reversalCount int64
	if err := s.db.WithContext(ctx).Model(&model.BillingCharge{}).Where("reversal_of_id = ? AND charge_kind = ?", *gen.BillingChargeID, model.BillingChargeKindReversal).Count(&reversalCount).Error; err != nil {
		return err
	}
	if reversalCount > 0 {
		public.BillingStatus = "reversed"
		public.FinalCost = 0
		return nil
	}
	var pendingCount int64
	if err := s.db.WithContext(ctx).Model(&model.BillingSettlementOutbox{}).Where("resource_type = ? AND resource_id = ? AND status IN ?", "image_generation", gen.ID, []string{"pending", "processing"}).Count(&pendingCount).Error; err != nil {
		return err
	}
	if pendingCount > 0 {
		public.BillingStatus = "reversal_pending"
	}
	return nil
}

func designerGenerationErrorCode(message string) string {
	if strings.HasPrefix(strings.TrimSpace(message), "image_ratio_mismatch:") {
		return "image_ratio_mismatch"
	}
	if message == designerOutputUnavailableMessage {
		return "image_output_unavailable"
	}
	return ""
}

func (s *DesignerService) CreateGenerationQuote(ctx context.Context, userID string, req DesignerGenerateRequest) (*DesignerGenerationQuote, error) {
	if strings.TrimSpace(req.Prompt) == "" || strings.TrimSpace(req.CapabilityKey) == "" {
		return nil, designerInputError("prompt and capability_key are required")
	}
	if _, err := uuid.Parse(req.OperationID); err != nil {
		return nil, designerInputError("operation_id must be a UUID")
	}
	if req.N != 1 {
		if req.N < 1 {
			return nil, designerInputError("n is required")
		}
		return nil, designerInputError("n must equal 1; batch designer SKUs are not configured")
	}
	if s == nil || s.billingCatalog == nil {
		return nil, fmt.Errorf("fixed-SKU designer billing is not configured")
	}
	_, route, err := s.authorizeDesignerCapability(ctx, userID, req.CapabilityKey)
	if err != nil {
		return nil, err
	}
	if err := validateDesignerGenerateRequest(req, route.GenerationFeatures); err != nil {
		return nil, err
	}
	if err := s.validateDesignerInputs(ctx, userID, req); err != nil {
		return nil, err
	}
	fingerprint := DesignerGenerationFingerprint(userID, req)
	internalID := s.designerInternalID(req.CapabilityKey)
	quoteRequest := QuoteRequest{
		UserID: userID, Operation: "image.generate", Route: "image_generation.capabilities." + internalID,
		RequestFingerprint: fingerprint, IdempotencyScope: "designer-quote", IdempotencyKey: req.OperationID,
	}
	if strings.TrimSpace(route.BillingSKU) != "" {
		if _, err := s.resolveDesignerCapabilitySKU(ctx, route, quoteRequest.Route); err != nil {
			return nil, err
		}
		quoteRequest.SKUID = strings.TrimSpace(route.BillingSKU)
	}
	quote, err := s.billingCatalog.CreateQuote(ctx, quoteRequest)
	if err != nil {
		return nil, err
	}
	return &DesignerGenerationQuote{
		QuoteID: quote.ID, OperationID: req.OperationID, RequestFingerprint: fingerprint,
		CatalogID: quote.CatalogID, SKUID: quote.SKUID, PricingTier: quote.PricingTier,
		ListPriceCredits: quote.ListPriceCredits, PriceCredits: quote.PriceCredits,
		DiscountCredits: quote.DiscountCredits, ExpiresAt: quote.ExpiresAt,
	}, nil
}

// CreateGenerationRecord validates the request, resolves config, and creates
// a generation record with "generating" status. Returns the generation ID.
func (s *DesignerService) CreateGenerationRecord(ctx context.Context, userID string, req DesignerGenerateRequest) (*DesignerGenerationCreated, error) {
	if req.Prompt == "" {
		return nil, designerInputError("prompt is required")
	}
	if strings.TrimSpace(req.CapabilityKey) == "" {
		return nil, designerInputError("capability_key is required")
	}
	if strings.TrimSpace(req.QuoteID) == "" {
		return nil, designerInputError("quote_id is required")
	}
	if _, err := uuid.Parse(req.OperationID); err != nil {
		return nil, designerInputError("operation_id must be a UUID")
	}
	if req.N != 1 {
		if req.N < 1 {
			return nil, designerInputError("n is required")
		}
		return nil, designerInputError("n must equal 1; batch designer SKUs are not configured")
	}
	wantFingerprint := DesignerGenerationFingerprint(userID, req)
	if req.RequestFingerprint != wantFingerprint {
		return nil, fmt.Errorf("%w: designer request fingerprint mismatch", ErrBillingQuoteMismatch)
	}
	if s.billingCatalog == nil || s.billingWallet == nil {
		return nil, fmt.Errorf("fixed-SKU designer billing is not configured")
	}
	authorizedConfig, authorizedRoute, err := s.authorizeDesignerCapability(ctx, userID, req.CapabilityKey)
	if err != nil {
		return nil, err
	}
	if err := validateDesignerGenerateRequest(req, authorizedRoute.GenerationFeatures); err != nil {
		return nil, err
	}
	if err := s.validateDesignerInputs(ctx, userID, req); err != nil {
		return nil, err
	}
	provider := authorizedConfig.Provider
	modelName := authorizedConfig.Model
	routeName := "image_generation.capabilities." + s.designerInternalID(req.CapabilityKey)
	var sku *model.BillingSKU
	if strings.TrimSpace(authorizedRoute.BillingSKU) != "" {
		sku, err = s.resolveDesignerCapabilitySKU(ctx, authorizedRoute, routeName)
	} else {
		sku, err = s.billingCatalog.ResolveSKU(ctx, "", "image.generate", routeName)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve fixed designer SKU: %w", err)
	}
	genID := req.OperationID
	if s.repo == nil {
		return nil, fmt.Errorf("designer repository is not configured")
	}
	existing, findErr := s.repo.ImageGenerations().FindByID(ctx, genID)
	if findErr == nil {
		if existing.UserID != userID || existing.RequestFingerprint != wantFingerprint {
			return nil, ErrBillingConflict
		}
		return &DesignerGenerationCreated{GenerationID: genID, Status: existing.Status, PriceCredits: existing.Cost}, nil
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}

	refFilesJSON, _ := json.Marshal(req.ReferenceFileIDs)
	watermark := false
	if req.Watermark != nil {
		watermark = *req.Watermark
	}
	gen := &model.ImageGeneration{
		ID:                 genID,
		UserID:             userID,
		ProjectID:          req.ProjectID,
		Prompt:             req.Prompt,
		Provider:           provider,
		ProviderID:         req.CapabilityKey,
		Model:              modelName,
		Quality:            req.Quality,
		Size:               req.Size,
		N:                  req.N,
		OutputFormat:       req.OutputFormat,
		OutputCompression:  req.OutputCompression,
		Background:         req.Background,
		BackgroundOpacity:  req.BackgroundOpacity,
		Watermark:          watermark,
		Status:             model.ImageGenerationStatusGenerating,
		ReferenceFiles:     string(refFilesJSON),
		MaskFileID:         req.MaskFileID,
		BillingMode:        "fixed_sku",
		BillingStatus:      "charged",
		BillingQuoteID:     req.QuoteID,
		RequestFingerprint: wantFingerprint,
	}
	var chargedPrice int64
	chargeReq := OperationChargeRequest{
		UserID: userID, QuoteID: req.QuoteID, CatalogID: sku.CatalogID, SKUID: sku.SKUID,
		ResourceType: "image_generation", ResourceID: genID, RequestFingerprint: wantFingerprint,
		IdempotencyScope: "designer-generation", IdempotencyKey: genID,
	}
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		charge, chargeErr := s.billingWallet.ChargeStandaloneOperationInTx(ctx, tx, chargeReq)
		if chargeErr != nil {
			return chargeErr
		}
		chargedPrice = charge.PriceCredits
		gen.Cost = int(charge.PriceCredits)
		gen.EstimatedCost = int(charge.PriceCredits)
		gen.PriceSnapshot = append([]byte(nil), charge.PricingSnapshot...)
		gen.BillingChargeID = stringPtr(charge.ID)
		return tx.ImageGenerations().Create(ctx, gen)
	})
	if err != nil {
		if replay, replayErr := s.repo.ImageGenerations().FindByID(ctx, genID); replayErr == nil && replay.UserID == userID && replay.RequestFingerprint == wantFingerprint {
			return &DesignerGenerationCreated{GenerationID: genID, Status: replay.Status, PriceCredits: replay.Cost}, nil
		}
		return nil, fmt.Errorf("create fixed-SKU generation: %w", err)
	}

	return &DesignerGenerationCreated{GenerationID: genID, Status: model.ImageGenerationStatusGenerating, PriceCredits: int(chargedPrice)}, nil
}

// SubmitGeneration owns both admission and dispatch. Database claim guards retries.
func (s *DesignerService) SubmitGeneration(ctx context.Context, userID string, req DesignerGenerateRequest) (*DesignerGenerationCreated, error) {
	created, err := s.CreateGenerationRecord(ctx, userID, req)
	if err != nil {
		return nil, err
	}
	if created.Status == model.ImageGenerationStatusGenerating {
		go s.ExecuteGeneration(context.Background(), created.GenerationID)
	}
	return created, nil
}

func (s *DesignerService) RecoverGenerations(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return nil
	}
	generations, err := s.repo.ImageGenerations().ListGenerating(ctx, 100)
	if err != nil {
		return err
	}
	now := time.Now()
	for i := range generations {
		gen := &generations[i]
		if gen.StartedAt == nil {
			go s.ExecuteGeneration(ctx, gen.ID)
			continue
		}
		deadline := gen.ExecutionDeadline
		if deadline == nil {
			timeout := 8 * time.Minute
			if route, ok := s.designerRoute(gen.ProviderID); ok && route.Timeout > 0 {
				timeout = route.Timeout
			}
			computed := gen.StartedAt.Add(timeout)
			deadline = &computed
		}
		if !deadline.After(now) {
			if err := s.failGenerationWithReversal(ctx, gen, "execution_timeout", "图片生成超时，请稍后重试"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *DesignerService) resolveDesignerCapabilitySKU(ctx context.Context, route srvconfig.ImageGenerationRouteConfig, routeName string) (*model.BillingSKU, error) {
	sku, err := s.billingCatalog.ResolveSKUByID(ctx, "", strings.TrimSpace(route.BillingSKU))
	if err != nil {
		return nil, fmt.Errorf("resolve image capability SKU: %w", err)
	}
	if sku.Operation != "image.generate" || sku.Route != routeName {
		return nil, fmt.Errorf("image capability billing_sku %q resolves to %s/%s, want image.generate/%s", route.BillingSKU, sku.Operation, sku.Route, routeName)
	}
	return sku, nil
}

func (s *DesignerService) validateDesignerInputs(ctx context.Context, userID string, req DesignerGenerateRequest) error {
	if req.ProjectID != "" {
		project, err := s.repo.Projects().FindByID(ctx, req.ProjectID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrProjectNotFound
		}
		if err != nil {
			return err
		}
		if project.UserID != userID {
			return ErrProjectOwnedByUser
		}
	}
	return s.validateDesignerReferenceOwnership(ctx, userID, req.ReferenceFileIDs, req.MaskFileID)
}

// authorizeDesignerCapability resolves the public selection key to the
// configured route and enforces its minimum tier. All write paths (quote and
// generation) must use this helper; filtering GET /providers alone is not an
// authorization boundary.
func (s *DesignerService) authorizeDesignerCapability(ctx context.Context, userID, capabilityID string) (*config.ImageAPI, srvconfig.ImageGenerationRouteConfig, error) {
	capabilityID = strings.TrimSpace(capabilityID)
	cfg := s.findDesignerConfigByID(capabilityID)
	route, ok := s.designerRoute(capabilityID)
	if cfg == nil || !ok || !cfg.IsEnabled() || !route.Enabled {
		return nil, srvconfig.ImageGenerationRouteConfig{}, fmt.Errorf("%w: %s", ErrDesignerCapabilityUnavailable, capabilityID)
	}
	if s.repo == nil || strings.TrimSpace(userID) == "" {
		return nil, srvconfig.ImageGenerationRouteConfig{}, ErrDesignerCapabilityAccessDenied
	}
	user, err := s.repo.Users().FindByID(ctx, userID)
	if err != nil || user == nil {
		if err != nil {
			return nil, srvconfig.ImageGenerationRouteConfig{}, fmt.Errorf("resolve designer capability entitlement: %w", err)
		}
		return nil, srvconfig.ImageGenerationRouteConfig{}, ErrDesignerCapabilityAccessDenied
	}
	userTier := model.ResolveTier(user.Tier)
	requiredTier := model.NormalizeTier(route.MinTier)
	if !model.TierSatisfies(userTier, requiredTier) {
		return nil, srvconfig.ImageGenerationRouteConfig{}, fmt.Errorf("%w: capability requires %s", ErrDesignerCapabilityAccessDenied, requiredTier)
	}
	return cfg, route, nil
}

// ValidateCapabilityForUser validates a project/task default without exposing
// internal provider or model identifiers to callers.
func (s *DesignerService) ValidateCapabilityForUser(ctx context.Context, userID, capabilityID string) error {
	capabilityID = strings.TrimSpace(capabilityID)
	if capabilityID == "" {
		return nil
	}
	_, _, err := s.authorizeDesignerCapability(ctx, userID, capabilityID)
	return err
}

func DesignerGenerationFingerprint(userID string, req DesignerGenerateRequest) string {
	payload, _ := json.Marshal(struct {
		Operation, UserID, OperationID, ProjectID, Prompt, CapabilityKey string
		Quality, Size, OutputFormat, Background, MaskFileID              string
		N, OutputCompression                                             int
		ReferenceFileIDs                                                 []string
		Watermark                                                        *bool
		BackgroundOpacity                                                *int `json:",omitempty"`
	}{
		Operation: "image.generate", UserID: strings.TrimSpace(userID), OperationID: strings.TrimSpace(req.OperationID),
		ProjectID: strings.TrimSpace(req.ProjectID), Prompt: strings.TrimSpace(req.Prompt), CapabilityKey: strings.TrimSpace(req.CapabilityKey),
		Quality: strings.TrimSpace(req.Quality), Size: strings.TrimSpace(req.Size), N: req.N,
		OutputFormat: strings.TrimSpace(req.OutputFormat), OutputCompression: req.OutputCompression,
		Background: strings.TrimSpace(req.Background), ReferenceFileIDs: req.ReferenceFileIDs, MaskFileID: strings.TrimSpace(req.MaskFileID), Watermark: req.Watermark, BackgroundOpacity: req.BackgroundOpacity,
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

type ImageGenerationFeatures = srvconfig.ImageGenerationFeatures

// DesignerGenerationFeatures keeps the built-in size choices identical in the
// capability catalog and request validation, including with older deployment configs.
func DesignerGenerationFeatures(key string, features ImageGenerationFeatures) ImageGenerationFeatures {
	if key == "standard" || key == "professional" {
		features.SizePresets = []string{"1:1:2K", "3:4:2K", "4:3:2K", "16:9:2K", "9:16:2K"}
		if !stringInSet(features.DefaultSize, features.SizePresets) {
			features.DefaultSize = features.SizePresets[0]
		}
	}
	return features
}

func validateDesignerGenerateRequest(req DesignerGenerateRequest, caps ImageGenerationFeatures) error {
	if strings.TrimSpace(req.Size) == "" {
		return designerInputError("size is required")
	}
	if req.N < 1 {
		return designerInputError("n is required")
	}
	if strings.TrimSpace(req.OutputFormat) == "" {
		return designerInputError("output_format is required")
	}
	if strings.TrimSpace(req.Quality) == "" {
		return designerInputError("quality is required")
	}
	maxBatch := caps.MaxBatch
	if maxBatch <= 0 {
		maxBatch = 1
	}
	if req.N > maxBatch {
		return designerInputError("n exceeds provider max_batch %d", maxBatch)
	}
	if len(req.ReferenceFileIDs) > 0 {
		if !caps.SupportsReference {
			return ErrDesignerReferenceUnsupported
		}
		if caps.MaxReferenceImages > 0 && len(req.ReferenceFileIDs) > caps.MaxReferenceImages {
			return &DesignerReferenceLimitError{Requested: len(req.ReferenceFileIDs), MaxReferenceImages: caps.MaxReferenceImages}
		}
	}
	if req.MaskFileID != "" && !caps.SupportsMask {
		return designerInputError("selected provider does not support mask editing")
	}
	if len(caps.QualityLevels) > 0 && !stringInSet(req.Quality, caps.QualityLevels) {
		return designerInputError("quality is not supported by selected provider")
	}
	if len(caps.OutputFormats) > 0 && !stringInSet(req.OutputFormat, caps.OutputFormats) {
		return designerInputError("output_format is not supported by selected provider")
	}
	if len(caps.SizePresets) > 0 {
		size := strings.TrimSpace(req.Size)
		if !stringInSet(size, caps.SizePresets) {
			return designerInputError("size is not supported by selected provider")
		}
	}
	if req.OutputCompression > 0 && !caps.HasCompression {
		return designerInputError("selected provider does not support output compression")
	}
	if req.OutputCompression < 0 || req.OutputCompression >= 100 {
		return designerInputError("output_compression must be between 0 and 99")
	}
	if req.Watermark != nil && *req.Watermark && !caps.Watermark {
		return designerInputError("selected provider does not support watermark")
	}
	if req.BackgroundOpacity != nil {
		if *req.BackgroundOpacity < 0 || *req.BackgroundOpacity > 100 {
			return designerInputError("background_opacity must be between 0 and 100")
		}
		if req.Background != "transparent" || !caps.HasBackground {
			return designerInputError("background_opacity requires transparent background support")
		}
		if !stringInSet(req.OutputFormat, []string{"png", "jpeg", "webp"}) || !stringInSet("png", caps.OutputFormats) {
			return designerInputError("background opacity requires PNG generation and PNG, JPEG or WebP output")
		}
	}
	if req.Background != "" && !stringInSet(req.Background, []string{"auto", "transparent", "opaque"}) {
		return designerInputError("background is invalid")
	}
	if strings.TrimSpace(req.Background) != "" && !strings.EqualFold(req.Background, "auto") && !caps.HasBackground {
		return designerInputError("selected provider does not support background control")
	}
	return nil
}

func stringInSet(value string, allowed []string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, item := range allowed {
		if value == strings.ToLower(strings.TrimSpace(item)) {
			return true
		}
	}
	return false
}

func (s *DesignerService) designerRoute(id string) (srvconfig.ImageGenerationRouteConfig, bool) {
	if s.fullCfg == nil || s.fullCfg.ModelRoutes.ImageGeneration.Capabilities == nil {
		return srvconfig.ImageGenerationRouteConfig{}, false
	}
	id = strings.TrimSpace(id)
	route, ok := s.fullCfg.ModelRoutes.ImageGeneration.Capabilities[id]
	if ok {
		route.GenerationFeatures = DesignerGenerationFeatures(id, route.GenerationFeatures)
	}
	return route, ok
}

func (s *DesignerService) designerInternalID(id string) string {
	return strings.TrimSpace(id)
}

// ExecuteGeneration runs the actual image generation for the given ID.
// Reads the generation record from the database, runs generation, processes
// results, and updates the status. Designed to be called from a goroutine.
//
// Note: The handler passes context.Background() because the HTTP request
// context is cancelled as soon as the handler returns. The generation
// goroutine will not be interrupted on server shutdown, but will complete
// naturally. A server-level lifecycle context could be added later.
func (s *DesignerService) ExecuteGeneration(ctx context.Context, genID string) {
	start := time.Now()
	var gen model.ImageGeneration
	if err := s.db.Where("id = ?", genID).First(&gen).Error; err != nil {
		s.logger.Error().Err(err).Str("gen_id", genID).Msg("generation record not found")
		return
	}
	if gen.Status != model.ImageGenerationStatusGenerating {
		return
	}
	route, ok := s.designerRoute(gen.ProviderID)
	if !ok || !route.Enabled {
		_ = s.failGenerationWithReversal(context.WithoutCancel(ctx), &gen, "platform_error", "图像能力配置异常，请联系管理员")
		return
	}
	timeout := route.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Minute
	}
	deadline := start.Add(timeout)
	claimed, err := s.repo.ImageGenerations().Claim(ctx, genID, start, deadline)
	if err != nil || !claimed {
		if err != nil {
			s.logger.Error().Err(err).Str("gen_id", genID).Msg("claim designer generation")
		}
		return
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	fail := func(reason, message string) {
		persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer persistCancel()
		if err := s.failGenerationWithReversal(persistCtx, &gen, reason, message); err != nil {
			s.logger.Error().Err(err).Str("gen_id", genID).Str("reason", reason).Msg("persist designer failure and reversal intent")
		}
	}

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error().Str("gen_id", genID).Msg("generation panicked")
			fail("platform_error", designerGenericFailureMessage)
		}
	}()

	provider := gen.Provider
	modelName := gen.Model
	providerID := gen.ProviderID

	runtime, configured := s.fullCfg.ImageAPIForCapability(providerID)
	if !configured || runtime.API == nil {
		fail("platform_error", "图像能力配置异常，请联系管理员")
		return
	}
	apiKey, baseURL, responseFormat := runtime.API.Key, runtime.API.BaseURL, runtime.API.ResponseFormat

	var refFileIDs []string
	if gen.ReferenceFiles != "" {
		if err := json.Unmarshal([]byte(gen.ReferenceFiles), &refFileIDs); err != nil {
			s.logger.Error().Err(err).Str("gen_id", genID).Msg("decode persisted designer references failed")
			fail("platform_error", "designer reference is invalid or unavailable")
			return
		}
	}
	refPaths, maskPath, cleanupReferences, err := s.materializeDesignerReferences(ctx, gen.UserID, refFileIDs, gen.MaskFileID)
	if err != nil {
		s.logger.Error().Err(err).Str("gen_id", genID).Str("user_id", gen.UserID).Msg("resolve designer references failed")
		fail("platform_error", "designer reference is invalid or unavailable")
		return
	}
	defer cleanupReferences()

	s.logger.Info().
		Str("gen_id", genID).
		Str("user_id", gen.UserID).
		Str("prompt_preview", truncate(gen.Prompt, 80)).
		Str("provider", provider).
		Str("provider_id", providerID).
		Str("model", modelName).
		Str("base_url", baseURL).
		Str("response_format", responseFormat).
		Str("size", gen.Size).
		Str("quality", gen.Quality).
		Str("output_format", gen.OutputFormat).
		Bool("watermark", gen.Watermark).
		Int("n", gen.N).
		Int("ref_count", len(refFileIDs)).
		Bool("has_mask", gen.MaskFileID != "").
		Int("cost", gen.Cost).
		Msg("designer: starting image generation")

	apiCfg := &config.ImageAPI{
		Key:            apiKey,
		BaseURL:        baseURL,
		Provider:       provider,
		Model:          modelName,
		Size:           gen.Size,
		ResponseFormat: responseFormat,
	}

	providerFactory := s.providerFactory
	if providerFactory == nil {
		providerFactory = image.NewProvider
	}
	providerInst, err := providerFactory(apiCfg, s.logger)
	if err != nil {
		s.logger.Error().Err(err).Str("gen_id", genID).Str("provider", provider).Msg("designer: failed to create image provider")
		fail("platform_error", designerGenerationUserError(err))
		return
	}

	genOpts := &image.GenerateOptions{
		Quality:           gen.Quality,
		OutputFormat:      gen.OutputFormat,
		OutputCompression: gen.OutputCompression,
		Background:        gen.Background,
		N:                 gen.N,
		Size:              gen.Size,
		RefImagePaths:     refPaths,
		MaskPath:          maskPath,
		Watermark:         &gen.Watermark,
	}

	if gen.BackgroundOpacity != nil {
		// Obtain a lossless cutout before compositing/exporting the chosen format.
		genOpts.OutputFormat = "png"
		genOpts.OutputCompression = 0
	}

	result, err := providerInst.Generate(ctx, gen.Prompt, genOpts)
	if err != nil {
		s.recordDesignerProviderFailure(ctx, &gen)
		s.logger.Error().
			Str("gen_id", genID).
			Str("user_id", gen.UserID).
			Str("provider", provider).
			Str("model", modelName).
			Str("prompt_preview", truncate(gen.Prompt, 100)).
			Dur("elapsed", time.Since(start)).
			Msg("designer: image generation failed")
		fail("provider_error", designerGenerationUserError(err))
		return
	}

	s.logger.Info().
		Str("gen_id", genID).
		Str("user_id", gen.UserID).
		Str("provider", provider).
		Str("model", modelName).
		Str("prompt_preview", truncate(gen.Prompt, 80)).
		Str("result_type", result.ResponseType).
		Int("image_count", len(result.Images)).
		Dur("elapsed", time.Since(start)).
		Msg("designer: image generation completed")

	if strings.TrimSpace(result.ProviderRequestID) == "" {
		result.ProviderRequestID = "internal:designer:" + gen.ID
	}
	outputs, cleanupOutputs, materializeErr := materializeDesignerResultOutputs(ctx, result)
	if materializeErr != nil {
		s.recordDesignerProviderCost(ctx, &gen, result)
		s.logger.Error().Str("gen_id", genID).Msg("designer output materialization failed")
		fail("platform_error", designerOutputUnavailableMessage)
		return
	}
	defer cleanupOutputs()
	expectedRatio, _ := image.ParseSize(gen.Size)
	outputWidth, outputHeight, dimensionErr := inspectDesignerResultDimensions(outputs, expectedRatio)
	result.OutputWidth, result.OutputHeight = outputWidth, outputHeight
	s.recordDesignerProviderCost(ctx, &gen, result)
	if dimensionErr != nil {
		if strings.HasPrefix(dimensionErr.Error(), "image_ratio_mismatch:") {
			fail("platform_error", dimensionErr.Error())
		} else {
			s.logger.Error().Str("gen_id", genID).Msg("designer output dimension inspection failed")
			fail("platform_error", designerOutputUnavailableMessage)
		}
		return
	}
	if gen.BackgroundOpacity != nil {
		if err := applyDesignerBackgroundOpacity(outputs, &gen); err != nil {
			fail("provider_error", "背景透明度处理失败，图像接口需要返回透明背景图片，请重试")
			return
		}
	}
	var durableResults []model.ImageGenerationResult
	result.OutputWidth, result.OutputHeight, durableResults = s.processResults(ctx, gen.UserID, genID, outputs)
	if len(durableResults) != gen.N {
		fail("platform_error", "generated image could not be persisted")
		return
	}

	completedAt := time.Now()
	var completed bool
	err = s.repo.WithTx(context.WithoutCancel(ctx), func(tx repository.Repository) error {
		var completeErr error
		completed, completeErr = tx.ImageGenerations().Complete(context.WithoutCancel(ctx), genID, result.RevisedPrompt, durableResults, completedAt)
		return completeErr
	})
	if err != nil || !completed {
		fail("platform_error", "generated image could not be persisted")
	}
}

type designerGeneratedOutput struct {
	sourceURL string
	localPath string
	index     int
}

func materializeDesignerResultOutputs(ctx context.Context, result *image.GenerateResult) ([]designerGeneratedOutput, func(), error) {
	if result == nil {
		return nil, func() {}, errors.New("generated image dimensions unavailable")
	}
	outputs := make([]designerGeneratedOutput, 0, len(result.Images)+1)
	for _, generated := range result.Images {
		outputs = append(outputs, designerGeneratedOutput{sourceURL: generated.URL, index: generated.Index})
	}
	if len(outputs) == 0 && strings.TrimSpace(result.URL) != "" {
		outputs = append(outputs, designerGeneratedOutput{sourceURL: result.URL})
	}
	cleanup := func() {
		for _, output := range outputs {
			if strings.TrimSpace(output.localPath) != "" {
				_ = os.Remove(output.localPath)
			}
		}
	}
	for index := range outputs {
		outputs[index].localPath = outputs[index].sourceURL
		if isLocalFilePath(outputs[index].sourceURL) {
			continue
		}
		localPath, err := downloadToTempFile(ctx, outputs[index].sourceURL, outputs[index].index)
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("generated image dimensions unavailable: %w", err)
		}
		outputs[index].localPath = localPath
	}
	return outputs, cleanup, nil
}

func applyDesignerBackgroundOpacity(outputs []designerGeneratedOutput, gen *model.ImageGeneration) error {
	for index, output := range outputs {
		data, err := os.ReadFile(output.localPath)
		if err != nil {
			return err
		}
		encoded, err := image.ApplyBackgroundOpacity(data, *gen.BackgroundOpacity, gen.OutputFormat, gen.OutputCompression)
		if err != nil {
			return err
		}
		file, err := os.CreateTemp("", "anban-designer-background-*."+gen.OutputFormat)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(encoded)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(file.Name())
			return errors.Join(writeErr, closeErr)
		}
		_ = os.Remove(output.localPath)
		outputs[index].localPath = file.Name()
	}
	return nil
}

func inspectDesignerResultDimensions(outputs []designerGeneratedOutput, expectedRatio string) (int, int, error) {
	var firstWidth, firstHeight int
	for _, output := range outputs {
		width, height, err := image.GetImageDimensions(output.localPath)
		if err != nil {
			return 0, 0, fmt.Errorf("generated image dimensions unavailable: %w", err)
		}
		if firstWidth == 0 && firstHeight == 0 {
			firstWidth, firstHeight = width, height
		}
		if !imageDimensionsMatchRatio(width, height, expectedRatio) {
			return width, height, fmt.Errorf(
				"image_ratio_mismatch: requested %s, got %dx%d",
				expectedRatio, width, height,
			)
		}
	}
	if firstWidth == 0 || firstHeight == 0 {
		return 0, 0, errors.New("generated image dimensions unavailable")
	}
	return firstWidth, firstHeight, nil
}

func (s *DesignerService) failGenerationWithReversal(ctx context.Context, gen *model.ImageGeneration, reason, message string) error {
	if s == nil || s.repo == nil || gen == nil {
		return fmt.Errorf("designer failure persistence is not configured")
	}
	completedAt := time.Now().UTC()
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		changed, err := tx.ImageGenerations().MarkFailed(ctx, gen.ID, message, completedAt)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		if s.billingWallet == nil || gen.BillingChargeID == nil || strings.TrimSpace(*gen.BillingChargeID) == "" {
			return nil
		}
		chargeID := strings.TrimSpace(*gen.BillingChargeID)
		fingerprint := billingFingerprint("designer-reversal", gen.ID, chargeID, reason)
		_, err = s.billingWallet.EnqueueSettlementInTx(ctx, tx, SettlementIntent{
			Action: model.BillingSettlementActionReverseOperation, ResourceType: "image_generation", ResourceID: gen.ID,
			ChargeID: chargeID, Reason: reason, RequestFingerprint: fingerprint,
			IdempotencyScope: "designer-reversal", IdempotencyKey: billingFingerprint(gen.ID, reason),
		})
		return err
	})
}

func (s *DesignerService) processResults(ctx context.Context, userID, genID string, outputs []designerGeneratedOutput) (int, int, []model.ImageGenerationResult) {
	var outputWidth, outputHeight int
	var durableResults []model.ImageGenerationResult
	for _, output := range outputs {
		if outputWidth == 0 && outputHeight == 0 {
			if width, height, err := image.GetImageDimensions(output.localPath); err == nil {
				outputWidth, outputHeight = width, height
			}
		}

		serveURL := output.sourceURL
		var storageKey string

		if s.storage != nil {
			uploadedURL, k, err := s.uploadGeneratedImage(ctx, userID, genID, output.localPath, output.index)
			if err != nil {
				s.logger.Error().Err(err).Str("path", output.localPath).Msg("failed to upload generated image to storage")
			} else {
				storageKey = k
				serveURL = uploadedURL
			}
		}

		if storageKey == "" {
			continue
		}
		dbResult := model.ImageGenerationResult{
			GenerationID: genID,
			ImageURL:     serveURL,
			Width:        outputWidth,
			Height:       outputHeight,
			FileID:       storageKey,
			Index:        output.index,
		}
		durableResults = append(durableResults, dbResult)
	}
	return outputWidth, outputHeight, durableResults
}

func (s *DesignerService) designerProviderIdentity(gen *model.ImageGeneration) (string, string) {
	if gen == nil {
		return "", ""
	}
	provider := gen.Provider
	if s.fullCfg != nil {
		if route, ok := s.fullCfg.ModelRoutes.ImageGeneration.Capabilities[gen.ProviderID]; ok && strings.TrimSpace(route.Provider) != "" {
			provider = route.Provider
		}
	}
	return provider, gen.Model
}

func (s *DesignerService) recordDesignerProviderFailure(ctx context.Context, gen *model.ImageGeneration) {
	if s == nil || s.providerCostSvc == nil || gen == nil {
		return
	}
	provider, modelID := s.designerProviderIdentity(gen)
	if _, err := s.providerCostSvc.RecordMediaUnreconciled(ctx, RecordMediaUnreconciledRequest{
		Provider: provider, Model: modelID, ProviderRequestID: "internal:designer:" + gen.ID, MediaKind: "image",
		ReasonCode: model.BillingExecutionCostReasonMissingProviderUsage,
	}); err != nil {
		s.logger.Error().Err(err).Str("gen_id", gen.ID).Msg("record failed designer provider cost evidence")
	}
}

func (s *DesignerService) recordDesignerProviderCost(ctx context.Context, gen *model.ImageGeneration, result *image.GenerateResult) {
	if s == nil || s.providerCostSvc == nil || gen == nil || result == nil {
		return
	}
	provider, modelID := s.designerProviderIdentity(gen)
	var usage *OpenAIImageUsage
	if result.Usage != nil {
		usage = &OpenAIImageUsage{TextInput: result.Usage.TextInputTokens, TextCachedInput: result.Usage.TextCachedInputTokens, ImageInput: result.Usage.ImageInputTokens, ImageCachedInput: result.Usage.ImageCachedInputTokens, ImageOutput: result.Usage.ImageOutputTokens}
	}
	if _, err := s.providerCostSvc.RecordImageGenerationCost(ctx, RecordImageGenerationCostRequest{
		Provider: provider, Model: modelID, ProviderRequestID: result.ProviderRequestID,
		Width: int64(result.OutputWidth), Height: int64(result.OutputHeight), Usage: usage,
	}); err != nil {
		s.logger.Error().Err(err).Str("gen_id", gen.ID).Msg("record designer provider cost; durable output remains valid")
	}
}

// downloadToTempFile downloads a remote URL to a temp file and returns its path.
func downloadToTempFile(ctx context.Context, url string, index int) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download image: HTTP %d", resp.StatusCode)
	}

	// Infer extension from URL or Content-Type.
	ext := ".png"
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		switch {
		case strings.Contains(ct, "jpeg") || strings.Contains(ct, "jpg"):
			ext = ".jpg"
		case strings.Contains(ct, "webp"):
			ext = ".webp"
		case strings.Contains(ct, "gif"):
			ext = ".gif"
		}
	} else if urlExt := filepath.Ext(url); urlExt != "" {
		ext = urlExt
	}

	f, err := os.CreateTemp("", fmt.Sprintf("anban-creator_download_%d_*%s", index, ext))
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
	written, err := io.Copy(f, io.LimitReader(resp.Body, maxDesignerReferenceBytes+1))
	if err != nil {
		cleanup()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if written > maxDesignerReferenceBytes {
		cleanup()
		return "", storage.ErrObjectExceedsMaxSize
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("close temp file: %w", err)
	}

	return f.Name(), nil
}

// uploadGeneratedImage uploads a local image file to storage and returns the serveable URL and storage key.
func (s *DesignerService) uploadGeneratedImage(ctx context.Context, userID, genID, filePath string, index int) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	key := fmt.Sprintf("%s/designer/%s/%d%s", userID, genID, index, ext)

	mimeType := DetectTaskFileMIME(filePath)
	result, err := s.storage.UploadFile(ctx, key, filePath, mimeType)
	if err != nil {
		return "", "", fmt.Errorf("upload generated image: %w", err)
	}

	return result.URL, result.Key, nil
}

// isLocalFilePath returns true if the URL looks like a local filesystem path.
func isLocalFilePath(url string) bool {
	return strings.HasPrefix(url, "/") || (!strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://"))
}

func (s *DesignerService) UploadReference(ctx context.Context, userID, filename, contentType string, data []byte) (string, error) {
	if s.storage == nil || s.referenceRepo == nil {
		return "", fmt.Errorf("designer reference storage is unavailable")
	}
	fileID := uuid.NewString()
	filename, contentType, err := validateDesignerReferenceMetadata(filename, contentType, int64(len(data)))
	if err != nil {
		return "", err
	}
	key := path.Join(userID, "designer/references", fileID, filename)
	if _, err := s.storage.Upload(ctx, key, bytes.NewReader(data), contentType); err != nil {
		return "", fmt.Errorf("upload designer reference: %w", err)
	}
	reference := &model.DesignerReference{ID: fileID, UserID: userID, StorageKey: key, FileName: filename, ContentType: contentType, Size: int64(len(data))}
	if err := s.referenceRepo.Create(ctx, reference); err != nil {
		_ = s.storage.Delete(ctx, key)
		return "", fmt.Errorf("persist designer reference: %w", err)
	}
	return fileID, nil
}

func (s *DesignerService) RegisterStoredReference(ctx context.Context, userID, key, filename, contentType string, size int64) (string, error) {
	if s.referenceRepo == nil {
		return "", fmt.Errorf("designer reference repository is unavailable")
	}
	filename, contentType, err := validateDesignerReferenceMetadata(filename, contentType, size)
	if err != nil {
		return "", err
	}
	fileID := uuid.NewString()
	if err := s.referenceRepo.Create(ctx, &model.DesignerReference{ID: fileID, UserID: userID, StorageKey: strings.TrimSpace(key), FileName: filename, ContentType: contentType, Size: size}); err != nil {
		return "", fmt.Errorf("persist designer reference: %w", err)
	}
	return fileID, nil
}

func (s *DesignerService) ReferenceMetadata(ctx context.Context, userID, fileID string) (*model.DesignerReference, error) {
	return s.referenceRepo.FindByIDAndUserID(ctx, fileID, userID)
}

func validateDesignerReferenceMetadata(filename, contentType string, size int64) (string, string, error) {
	filename = sanitizeUploadFilename(filename)
	if filename == "" {
		return "", "", fmt.Errorf("designer reference filename is invalid")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	contentType = normalizeDirectUploadContentType(contentType)
	if contentType == "" {
		contentType = contentTypeForUploadExt(ext)
	}
	if size < 0 || size > maxDesignerReferenceBytes || !isDirectUploadImage(contentType, ext) {
		return "", "", fmt.Errorf("designer reference metadata is invalid")
	}
	return filename, contentType, nil
}

// CanReadGeneratedFile authorizes only persisted results of a completed generation
// owned by the caller. A user-looking storage prefix alone grants no access.
func (s *DesignerService) CanReadGeneratedFile(ctx context.Context, userID, key string) (bool, error) {
	if s.db == nil || userID == "" || key == "" {
		return false, nil
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&model.ImageGenerationResult{}).
		Joins("JOIN image_generations ON image_generations.id = image_generation_results.generation_id").
		Where("image_generation_results.file_id = ? AND image_generations.user_id = ? AND image_generations.status = ?", key, userID, model.ImageGenerationStatusCompleted).
		Count(&count).Error
	return count > 0, err
}

// UploadReferenceFromURL reuses a persisted user-owned storage object as a reference.
func (s *DesignerService) UploadReferenceFromURL(ctx context.Context, userID, rawURL string) (string, error) {
	if s.storage == nil || s.db == nil {
		return "", fmt.Errorf("storage not configured")
	}
	if !s.storage.IsOwnedURL(rawURL) {
		return "", ErrURLNotOwned
	}
	key, ok := storage.StorageKeyFromURL(rawURL)
	if !ok {
		return "", ErrURLNotOwned
	}
	owned, err := s.CanReadGeneratedFile(ctx, userID, key)
	if err != nil {
		return "", err
	}
	var count int64
	if owned {
		count = 1
	}
	if count == 0 {
		if err := s.db.WithContext(ctx).Model(&model.DesignerReference{}).Where("storage_key = ? AND user_id = ?", key, userID).Count(&count).Error; err != nil {
			return "", err
		}
	}
	if count == 0 {
		if err := s.db.WithContext(ctx).Model(&model.Asset{}).Where("storage_key = ? AND user_id = ?", key, userID).Count(&count).Error; err != nil {
			return "", err
		}
	}
	if count == 0 {
		return "", ErrURLNotOwned
	}
	data, err := storage.ReadObject(ctx, s.storage, key, maxDesignerReferenceBytes)
	if err != nil {
		return "", fmt.Errorf("read reference: %w", err)
	}
	ext := filepath.Ext(key)
	if ext == "" {
		ext = ".png"
	}
	return s.UploadReference(ctx, userID, "source"+ext, contentTypeForUploadExt(ext), data)
}

func (s *DesignerService) GetHistory(ctx context.Context, userID, projectID string, page, pageSize int) ([]DesignerGenerationPublic, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}

	var total int64
	var generations []model.ImageGeneration

	query := s.db.Where("user_id = ?", userID)
	if projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}

	if err := query.Model(&model.ImageGeneration{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Preload("Results").Order("created_at DESC").
		Offset(offset).Limit(pageSize).Find(&generations).Error; err != nil {
		return nil, 0, err
	}

	for i := range generations {
		s.signResultURLs(ctx, generations[i].Results)
	}

	items := make([]DesignerGenerationPublic, 0, len(generations))
	for i := range generations {
		public := s.publicGeneration(&generations[i])
		if err := s.decorateDesignerBilling(ctx, &generations[i], public); err != nil {
			return nil, 0, err
		}
		items = append(items, *public)
	}
	return items, total, nil
}

func (s *DesignerService) GetGeneration(ctx context.Context, userID, generationID string) (*DesignerGenerationPublic, error) {
	var gen model.ImageGeneration
	if err := s.db.Where("id = ? AND user_id = ?", generationID, userID).
		Preload("Results").First(&gen).Error; err != nil {
		return nil, err
	}
	s.signResultURLs(ctx, gen.Results)
	public := s.publicGeneration(&gen)
	if err := s.decorateDesignerBilling(ctx, &gen, public); err != nil {
		return nil, err
	}
	return public, nil
}

func designerGenerationUserError(err error) string {
	if err == nil {
		return designerGenericFailureMessage
	}

	var genErr *image.GenerateError
	if errors.As(err, &genErr) {
		switch genErr.Code {
		case "url_download_error":
			return designerOutputUnavailableMessage
		case "content_policy_violation":
			return "提示词可能不符合内容安全要求，请调整后重试"
		case "rate_limit", "server_error", "network_error":
			return "图片服务繁忙，请稍后重试"
		case "no_image":
			return "图片服务没有返回图片，请调整提示词后重试"
		case "endpoint_protocol", "usage_required_missing":
			return "图像能力配置异常，请联系管理员"
		}
		return designerGenericFailureMessage
	}
	return designerGenericFailureMessage
}

func (s *DesignerService) findDesignerConfigByID(id string) *config.ImageAPI {
	if id == "" || s.fullCfg == nil {
		return nil
	}
	if runtime, ok := s.fullCfg.ImageAPIForCapability(id); ok {
		return runtime.API
	}
	return nil
}

func (s *DesignerService) materializeDesignerReferences(ctx context.Context, userID string, referenceIDs []string, maskID string) ([]string, string, func(), error) {
	paths := make([]string, 0, len(referenceIDs)+1)
	cleanup := func() {
		for _, filePath := range paths {
			_ = os.Remove(filePath)
		}
	}
	materialize := func(fileID string) (string, error) {
		if !isCanonicalDesignerReferenceID(fileID) {
			return "", ErrDesignerReferenceInvalid
		}
		if s.referenceRepo == nil || s.storage == nil {
			return "", fmt.Errorf("designer reference storage is unavailable")
		}
		reference, err := s.referenceRepo.FindByIDAndUserID(ctx, fileID, userID)
		if err != nil {
			return "", fmt.Errorf("designer reference not found")
		}
		data, err := storage.ReadObject(ctx, s.storage, reference.StorageKey, maxDesignerReferenceBytes)
		if err != nil {
			return "", fmt.Errorf("read designer reference: %w", err)
		}
		if int64(len(data)) != reference.Size {
			return "", fmt.Errorf("designer reference size mismatch")
		}
		ext := strings.ToLower(filepath.Ext(reference.FileName))
		file, err := os.CreateTemp("", "anban-designer-reference-"+reference.ID+"-*"+ext)
		if err != nil {
			return "", fmt.Errorf("create designer reference temp file: %w", err)
		}
		filePath := file.Name()
		paths = append(paths, filePath)
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			return "", fmt.Errorf("write designer reference temp file: %w", err)
		}
		if err := file.Close(); err != nil {
			return "", fmt.Errorf("close designer reference temp file: %w", err)
		}
		return filePath, nil
	}

	referencePaths := make([]string, 0, len(referenceIDs))
	for _, fileID := range referenceIDs {
		filePath, err := materialize(fileID)
		if err != nil {
			cleanup()
			return nil, "", func() {}, err
		}
		referencePaths = append(referencePaths, filePath)
	}
	maskPath := ""
	if maskID != "" {
		var err error
		maskPath, err = materialize(maskID)
		if err != nil {
			cleanup()
			return nil, "", func() {}, err
		}
	}
	return referencePaths, maskPath, cleanup, nil
}

func (s *DesignerService) validateDesignerReferenceOwnership(ctx context.Context, userID string, referenceIDs []string, maskID string) error {
	ids := make([]string, 0, len(referenceIDs)+1)
	ids = append(ids, referenceIDs...)
	if maskID != "" {
		ids = append(ids, maskID)
	}
	if len(ids) == 0 {
		return nil
	}
	if s.referenceRepo == nil {
		return fmt.Errorf("designer reference repository is unavailable")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, fileID := range ids {
		if !isCanonicalDesignerReferenceID(fileID) {
			return ErrDesignerReferenceInvalid
		}
		if _, ok := seen[fileID]; ok {
			continue
		}
		seen[fileID] = struct{}{}
		if _, err := s.referenceRepo.FindByIDAndUserID(ctx, fileID, userID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDesignerReferenceInvalid
			}
			if s.logger != nil {
				s.logger.Error().Err(err).Str("user_id", userID).Str("file_id", fileID).Msg("validate designer reference ownership failed")
			}
			return fmt.Errorf("validate designer reference ownership: %w", err)
		}
	}
	return nil
}

func isCanonicalDesignerReferenceID(fileID string) bool {
	parsed, err := uuid.Parse(fileID)
	return err == nil && parsed.String() == fileID
}

// signResultURLs re-signs OSS URLs for private buckets so that expired
// signed URLs get fresh signatures when results are fetched.
func (s *DesignerService) signResultURLs(ctx context.Context, results []model.ImageGenerationResult) {
	if s.storage == nil {
		return
	}
	for i := range results {
		if results[i].FileID != "" {
			if s.storage.HasCustomDomain() || s.storage.Name() == "local" {
				results[i].ImageURL = s.storage.GetURL(results[i].FileID)
			} else {
				signedURL, err := s.storage.DownloadURL(ctx, results[i].FileID, 3600)
				if err == nil {
					results[i].ImageURL = signedURL
				} else {
					results[i].ImageURL = ""
					s.logger.Warn().Err(err).Str("file_id", results[i].FileID).Msg("failed to sign designer result URL")
				}
			}
		} else {
			results[i].ImageURL = ""
		}
	}
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
