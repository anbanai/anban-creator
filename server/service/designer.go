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
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anbanai/anban-creator/app/config"
	"github.com/anbanai/anban-creator/app/image"
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

const maxDesignerReferenceBytes int64 = 10 * 1024 * 1024

type DesignerService struct {
	db              *gorm.DB
	repo            repository.Repository
	fullCfg         *srvconfig.Config
	imageCfg        *srvconfig.ImageAPIConfig
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
	var imageCfg *srvconfig.ImageAPIConfig
	if fullCfg != nil {
		imageCfg = &fullCfg.ImageAPI
	}
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
		imageCfg:        imageCfg,
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
	Provider           string   `json:"provider"`
	ProviderID         string   `json:"provider_id,omitempty"`
	Model              string   `json:"model"`
	Quality            string   `json:"quality,omitempty"`
	Size               string   `json:"size,omitempty"`
	N                  int      `json:"n,omitempty"`
	OutputFormat       string   `json:"output_format,omitempty"`
	OutputCompression  int      `json:"output_compression,omitempty"`
	Background         string   `json:"background,omitempty"`
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
	ID             string                        `json:"id"`
	ProjectID      string                        `json:"project_id"`
	Prompt         string                        `json:"prompt"`
	RevisedPrompt  string                        `json:"revised_prompt,omitempty"`
	CapabilityKey  string                        `json:"capability_key,omitempty"`
	CapabilityName string                        `json:"capability_name,omitempty"`
	Quality        string                        `json:"quality,omitempty"`
	Size           string                        `json:"size,omitempty"`
	N              int                           `json:"n"`
	OutputFormat   string                        `json:"output_format,omitempty"`
	Status         string                        `json:"status"`
	Error          string                        `json:"error,omitempty"`
	Cost           int                           `json:"cost,omitempty"`
	EstimatedCost  int                           `json:"estimated_cost,omitempty"`
	FinalCost      int                           `json:"final_cost,omitempty"`
	BillingMode    string                        `json:"billing_mode,omitempty"`
	BillingStatus  string                        `json:"billing_status,omitempty"`
	CreatedAt      time.Time                     `json:"created_at"`
	UpdatedAt      time.Time                     `json:"updated_at"`
	Results        []model.ImageGenerationResult `json:"results,omitempty"`
}

func (s *DesignerService) publicGeneration(gen *model.ImageGeneration) *DesignerGenerationPublic {
	if gen == nil {
		return nil
	}
	capKey, capName := "", ""
	if route, ok := s.designerRoute(gen.ProviderID); ok {
		capKey, capName = route.SelectionKey, route.Alias
	}
	if capName == "" {
		capName = "图像能力"
	}
	return &DesignerGenerationPublic{ID: gen.ID, ProjectID: gen.ProjectID, Prompt: gen.Prompt,
		RevisedPrompt: gen.RevisedPrompt, CapabilityKey: capKey, CapabilityName: capName,
		Quality: gen.Quality, Size: gen.Size, N: gen.N, OutputFormat: gen.OutputFormat,
		Status: gen.Status, Error: gen.Error, Cost: gen.Cost, EstimatedCost: gen.EstimatedCost,
		FinalCost: gen.FinalCost, BillingMode: gen.BillingMode, BillingStatus: gen.BillingStatus,
		CreatedAt: gen.CreatedAt, UpdatedAt: gen.UpdatedAt, Results: gen.Results}
}

func (s *DesignerService) CreateGenerationQuote(ctx context.Context, userID string, req DesignerGenerateRequest) (*DesignerGenerationQuote, error) {
	if s == nil || s.billingCatalog == nil {
		return nil, fmt.Errorf("fixed-SKU designer billing is not configured")
	}
	if strings.TrimSpace(req.Prompt) == "" || strings.TrimSpace(req.ProviderID) == "" {
		return nil, fmt.Errorf("prompt and provider_id are required")
	}
	if _, err := uuid.Parse(req.OperationID); err != nil {
		return nil, fmt.Errorf("operation_id must be a UUID")
	}
	if req.ProjectID == "" {
		req.ProjectID = "default"
	}
	if req.N < 1 {
		req.N = 1
	}
	if req.N != 1 {
		return nil, fmt.Errorf("n must equal 1; batch designer SKUs are not configured")
	}
	cfg := s.findDesignerConfigByID(req.ProviderID)
	route, ok := s.designerRoute(req.ProviderID)
	if cfg == nil || !ok || !cfg.IsEnabled() {
		return nil, fmt.Errorf("designer provider %s is not available", req.ProviderID)
	}
	if err := validateDesignerGenerateRequest(req, route.Capabilities); err != nil {
		return nil, err
	}
	fingerprint := DesignerGenerationFingerprint(userID, req)
	internalID := s.designerInternalID(req.ProviderID)
	quoteRequest := QuoteRequest{
		UserID: userID, Operation: "designer.generate_image", Route: "image_generation.designer." + internalID,
		RequestFingerprint: fingerprint, IdempotencyScope: "designer-quote", IdempotencyKey: req.OperationID,
	}
	if strings.TrimSpace(route.BillingSKU) != "" {
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
		return nil, fmt.Errorf("prompt is required")
	}
	if req.ProjectID == "" {
		req.ProjectID = "default"
	}
	if strings.TrimSpace(req.ProviderID) == "" {
		return nil, fmt.Errorf("provider_id is required")
	}
	if strings.TrimSpace(req.QuoteID) == "" {
		return nil, fmt.Errorf("quote_id is required")
	}
	if _, err := uuid.Parse(req.OperationID); err != nil {
		return nil, fmt.Errorf("operation_id must be a UUID")
	}
	if req.N < 1 {
		req.N = 1
	}
	if req.N != 1 {
		return nil, fmt.Errorf("n must equal 1; batch designer SKUs are not configured")
	}
	wantFingerprint := DesignerGenerationFingerprint(userID, req)
	if req.RequestFingerprint != wantFingerprint {
		return nil, fmt.Errorf("%w: designer request fingerprint mismatch", ErrBillingQuoteMismatch)
	}
	if s.billingCatalog == nil || s.billingWallet == nil {
		return nil, fmt.Errorf("fixed-SKU designer billing is not configured")
	}

	provider := req.Provider
	if provider == "" {
		provider = s.resolveProvider()
	}
	switch provider {
	case "google":
		provider = "gemini"
	case "volc", "seedream":
		provider = "volcengine"
	}

	modelName := req.Model
	if modelName == "" {
		modelName = s.resolveModel(provider)
	}
	if modelName == "" {
		switch provider {
		case "openai":
			modelName = "gpt-image-2"
		case "gemini":
			modelName = image.DefaultGeminiModel
		case "volcengine":
			modelName = image.DefaultVolcengineModel
		}
	}

	var routeName string
	if req.ProviderID != "" {
		if cfg := s.findDesignerConfigByID(req.ProviderID); cfg != nil {
			if !cfg.IsEnabled() {
				return nil, fmt.Errorf("designer provider %s is disabled", req.ProviderID)
			}
			provider = cfg.Provider
			modelName = cfg.Model
			if route, ok := s.designerRoute(req.ProviderID); ok {
				if err := validateDesignerGenerateRequest(req, route.Capabilities); err != nil {
					return nil, err
				}
				routeName = "image_generation.designer." + s.designerInternalID(req.ProviderID)
			} else {
				return nil, fmt.Errorf("designer provider %s route is not configured", req.ProviderID)
			}
		} else {
			return nil, fmt.Errorf("designer provider %s is not configured", req.ProviderID)
		}
	}
	if err := s.validateDesignerReferenceOwnership(ctx, userID, req.ReferenceFileIDs, req.MaskFileID); err != nil {
		return nil, err
	}
	var sku *model.BillingSKU
	var err error
	if route, ok := s.designerRoute(req.ProviderID); ok && strings.TrimSpace(route.BillingSKU) != "" {
		sku, err = s.billingCatalog.ResolveSKUByID(ctx, "", strings.TrimSpace(route.BillingSKU))
	} else {
		sku, err = s.billingCatalog.ResolveSKU(ctx, "", "designer.generate_image", routeName)
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
		ProviderID:         req.ProviderID,
		Model:              modelName,
		Quality:            req.Quality,
		Size:               req.Size,
		N:                  req.N,
		OutputFormat:       req.OutputFormat,
		OutputCompression:  req.OutputCompression,
		Background:         req.Background,
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

func DesignerGenerationFingerprint(userID string, req DesignerGenerateRequest) string {
	payload, _ := json.Marshal(struct {
		Operation, UserID, OperationID, ProjectID, Prompt, ProviderID, Provider, Model string
		Quality, Size, OutputFormat, Background, MaskFileID                            string
		N, OutputCompression                                                           int
		ReferenceFileIDs                                                               []string
		Watermark                                                                      *bool
	}{
		Operation: "designer.generate_image", UserID: strings.TrimSpace(userID), OperationID: strings.TrimSpace(req.OperationID),
		ProjectID: strings.TrimSpace(req.ProjectID), Prompt: strings.TrimSpace(req.Prompt), ProviderID: strings.TrimSpace(req.ProviderID),
		Provider: strings.TrimSpace(req.Provider), Model: strings.TrimSpace(req.Model), Quality: strings.TrimSpace(req.Quality),
		Size: strings.TrimSpace(req.Size), N: req.N, OutputFormat: strings.TrimSpace(req.OutputFormat), OutputCompression: req.OutputCompression,
		Background: strings.TrimSpace(req.Background), ReferenceFileIDs: req.ReferenceFileIDs, MaskFileID: strings.TrimSpace(req.MaskFileID), Watermark: req.Watermark,
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func validateDesignerGenerateRequest(req DesignerGenerateRequest, caps DesignerProviderCapabilities) error {
	maxBatch := caps.MaxBatch
	if maxBatch <= 0 {
		maxBatch = 1
	}
	if req.N > maxBatch {
		return fmt.Errorf("n exceeds provider max_batch %d", maxBatch)
	}
	if len(req.ReferenceFileIDs) > 0 {
		if !caps.SupportsReference {
			return fmt.Errorf("selected provider does not support reference images")
		}
		if caps.MaxReferenceImages > 0 && len(req.ReferenceFileIDs) > caps.MaxReferenceImages {
			return fmt.Errorf("reference_file_ids exceeds provider max_reference_images %d", caps.MaxReferenceImages)
		}
	}
	if req.MaskFileID != "" && !caps.SupportsMask {
		return fmt.Errorf("selected provider does not support mask editing")
	}
	if len(caps.QualityLevels) > 0 && strings.TrimSpace(req.Quality) != "" && !stringInSet(req.Quality, caps.QualityLevels) {
		return fmt.Errorf("quality %q is not supported by selected provider", req.Quality)
	}
	if len(caps.OutputFormats) > 0 && strings.TrimSpace(req.OutputFormat) != "" && !stringInSet(req.OutputFormat, caps.OutputFormats) {
		return fmt.Errorf("output_format %q is not supported by selected provider", req.OutputFormat)
	}
	if len(caps.SizePresets) > 0 {
		size := strings.TrimSpace(req.Size)
		if size != "" {
			size = designerCapabilitySize(size)
			if !stringInSet(size, caps.SizePresets) {
				return fmt.Errorf("size %q is not supported by selected provider", size)
			}
		}
	}
	if req.OutputCompression > 0 && !caps.HasCompression {
		return fmt.Errorf("selected provider does not support output compression")
	}
	if strings.TrimSpace(req.Background) != "" && !strings.EqualFold(req.Background, "auto") && !caps.HasBackground {
		return fmt.Errorf("selected provider does not support background control")
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
	if s.fullCfg == nil || s.fullCfg.ModelRoutes.ImageGeneration.Designer == nil {
		return srvconfig.ImageGenerationRouteConfig{}, false
	}
	if route, ok := s.fullCfg.ModelRoutes.ImageGeneration.Designer[id]; ok {
		return route, true
	}
	for _, route := range s.fullCfg.ModelRoutes.ImageGeneration.Designer {
		if strings.TrimSpace(route.SelectionKey) == strings.TrimSpace(id) {
			return route, true
		}
	}
	return srvconfig.ImageGenerationRouteConfig{}, false
}

func (s *DesignerService) designerInternalID(id string) string {
	if s.fullCfg == nil {
		return id
	}
	if _, ok := s.fullCfg.ModelRoutes.ImageGeneration.Designer[id]; ok {
		return id
	}
	for internalID, route := range s.fullCfg.ModelRoutes.ImageGeneration.Designer {
		if strings.TrimSpace(route.SelectionKey) == strings.TrimSpace(id) {
			return internalID
		}
	}
	return id
}

func designerCapabilitySize(size string) string {
	size = strings.TrimSpace(size)
	if size == "" {
		return ""
	}
	lower := strings.ToLower(size)
	if lower == "auto" || strings.HasPrefix(lower, "auto:") {
		return "auto"
	}
	if image.IsPixelSize(size) {
		return size
	}

	upper := strings.ToUpper(size)
	for _, tier := range []string{"4K", "2K", "1K"} {
		suffix := ":" + tier
		if strings.HasSuffix(upper, suffix) {
			return strings.TrimSpace(size[:len(size)-len(suffix)])
		}
	}
	return size
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

	now := time.Now()
	s.db.Model(&model.ImageGeneration{}).Where("id = ?", genID).Update("started_at", &now)

	var failOnce sync.Once
	fail := func(reason, message string) {
		failOnce.Do(func() {
			if err := s.failGenerationWithReversal(ctx, &gen, reason, message); err != nil {
				s.logger.Error().Err(err).Str("gen_id", genID).Str("reason", reason).Msg("persist designer failure and reversal intent")
			}
		})
	}

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error().Str("gen_id", genID).Any("panic", r).Msg("generation panicked")
			fail("platform_error", fmt.Sprintf("internal error: %v", r))
		}
	}()

	provider := gen.Provider
	modelName := gen.Model
	providerID := gen.ProviderID

	// Resolve config by designer entry ID when available, fallback to provider type.
	var designerCfg *config.ImageAPI
	if providerID != "" {
		designerCfg = s.findDesignerConfigByID(providerID)
	}

	var apiKey, baseURL, responseFormat string
	if designerCfg != nil {
		apiKey = designerCfg.Key
		baseURL = designerCfg.BaseURL
		responseFormat = designerCfg.ResponseFormat
		if modelName == "" {
			modelName = designerCfg.Model
		}
	} else {
		apiKey = s.resolveAPIKey(provider)
		baseURL = s.resolveBaseURL(provider)
		responseFormat = s.resolveResponseFormat(provider)
	}

	keyPreview := ""
	if len(apiKey) > 4 {
		keyPreview = apiKey[:4] + "..."
	} else if apiKey != "" {
		keyPreview = "**"
	}

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
		Str("key_preview", keyPreview).
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

	result, err := providerInst.Generate(ctx, gen.Prompt, genOpts)
	if err != nil {
		s.recordDesignerProviderFailure(ctx, &gen)
		s.logger.Error().Err(err).
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
		Str("url_preview", truncate(result.URL, 80)).
		Dur("elapsed", time.Since(start)).
		Msg("designer: image generation completed")

	if strings.TrimSpace(result.ProviderRequestID) == "" {
		result.ProviderRequestID = "internal:designer:" + gen.ID
	}
	var durableResults int
	result.OutputWidth, result.OutputHeight, durableResults = s.processResults(ctx, gen.UserID, genID, result)
	s.recordDesignerProviderCost(ctx, &gen, result)
	if durableResults == 0 {
		fail("platform_error", "generated image could not be persisted")
		return
	}

	completedAt := time.Now()
	s.db.Model(&model.ImageGeneration{}).Where("id = ?", genID).Updates(map[string]any{
		"status":         model.ImageGenerationStatusCompleted,
		"revised_prompt": result.RevisedPrompt,
		"completed_at":   &completedAt,
	})
}

func (s *DesignerService) failGenerationWithReversal(ctx context.Context, gen *model.ImageGeneration, reason, message string) error {
	if s == nil || s.repo == nil || gen == nil {
		return fmt.Errorf("designer failure persistence is not configured")
	}
	completedAt := time.Now().UTC()
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.ImageGenerations().MarkFailed(ctx, gen.ID, message, completedAt); err != nil {
			return err
		}
		if s.billingWallet == nil || gen.BillingChargeID == nil || strings.TrimSpace(*gen.BillingChargeID) == "" {
			return nil
		}
		chargeID := strings.TrimSpace(*gen.BillingChargeID)
		fingerprint := billingFingerprint("designer-reversal", gen.ID, chargeID, reason)
		_, err := s.billingWallet.EnqueueSettlementInTx(ctx, tx, SettlementIntent{
			Action: model.BillingSettlementActionReverseOperation, ResourceType: "image_generation", ResourceID: gen.ID,
			ChargeID: chargeID, Reason: reason, RequestFingerprint: fingerprint,
			IdempotencyScope: "designer-reversal", IdempotencyKey: billingFingerprint(gen.ID, reason),
		})
		return err
	})
}

func (s *DesignerService) processResults(ctx context.Context, userID, genID string, result *image.GenerateResult) (int, int, int) {
	var outputWidth, outputHeight int
	var durableResults int
	collectURLs := func(rawURL string, idx int) {
		// Resolve to a local file path — download remote URLs if needed.
		localPath := rawURL
		isTemp := false
		if !isLocalFilePath(rawURL) {
			tmpPath, err := downloadToTempFile(ctx, rawURL, idx)
			if err != nil {
				s.logger.Error().Err(err).Str("url", rawURL).Msg("failed to download remote image")
				return
			}
			localPath = tmpPath
			isTemp = true
		}
		if outputWidth == 0 && outputHeight == 0 {
			if width, height, err := image.GetImageDimensions(localPath); err == nil {
				outputWidth, outputHeight = width, height
			}
		}

		serveURL := rawURL // fallback if upload fails
		var storageKey string

		if s.storage != nil {
			uploadedURL, k, err := s.uploadGeneratedImage(ctx, userID, genID, localPath, idx)
			if err != nil {
				s.logger.Error().Err(err).Str("path", localPath).Msg("failed to upload generated image to storage")
			} else {
				storageKey = k
				serveURL = uploadedURL
			}
		}

		if storageKey == "" {
			if isTemp {
				_ = os.Remove(localPath)
			}
			return
		}
		dbResult := model.ImageGenerationResult{
			GenerationID: genID,
			ImageURL:     serveURL,
			ImagePath:    rawURL,
			FileID:       storageKey,
			Index:        idx,
		}
		if err := s.db.Create(&dbResult).Error; err != nil {
			s.logger.Error().Err(err).Str("generation_id", genID).Int("index", idx).Msg("failed to save generation result")
		} else {
			durableResults++
		}

		// Cleanup temp file after successful upload.
		if isTemp {
			_ = os.Remove(localPath)
		} else if storageKey != "" {
			_ = os.Remove(localPath)
		}
	}

	if len(result.Images) > 0 {
		for _, img := range result.Images {
			collectURLs(img.URL, img.Index)
		}
		return outputWidth, outputHeight, durableResults
	}

	if result.URL != "" {
		collectURLs(result.URL, 0)
	}
	return outputWidth, outputHeight, durableResults
}

func (s *DesignerService) designerProviderIdentity(gen *model.ImageGeneration) (string, string) {
	if gen == nil {
		return "", ""
	}
	provider := gen.Provider
	if s.fullCfg != nil {
		if route, ok := s.fullCfg.ModelRoutes.ImageGeneration.Designer[gen.ProviderID]; ok && strings.TrimSpace(route.Provider) != "" {
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

// UploadReferenceFromURL downloads an image from a storage URL owned by this
// backend and by the calling user, then registers it as a reference file. Used
// to avoid CORS errors when the client needs to re-upload an image it has
// loaded from a cross-origin signed OSS URL. Returns the same fileID shape as
// UploadReference. Returns ErrURLNotOwned if the URL fails the bucket or
// per-user ownership checks.
func (s *DesignerService) UploadReferenceFromURL(ctx context.Context, userID, rawURL string) (string, error) {
	if s.storage == nil {
		return "", fmt.Errorf("storage not configured")
	}
	if !s.storage.IsOwnedURL(rawURL) {
		return "", ErrURLNotOwned
	}

	var data []byte
	var ext string

	// Local storage URLs are relative paths served by this backend — read
	// directly via the storage abstraction instead of an HTTP fetch.
	if key, ok := strings.CutPrefix(rawURL, "/api/v1/files/"); ok {
		if !strings.HasPrefix(key, userID+"/") {
			return "", ErrURLNotOwned
		}
		var err error
		data, err = storage.ReadObject(ctx, s.storage, key, maxDesignerReferenceBytes)
		if err != nil {
			return "", fmt.Errorf("read local reference: %w", err)
		}
		ext = filepath.Ext(key)
	} else {
		// Remote OSS URL: enforce per-user ownership on the path, then download
		// via the shared helper (limits to 10MB, infers extension from
		// Content-Type). OSS object keys for designer results are shaped
		// "{userID}/designer/{genID}/{index}{ext}" (see uploadGeneratedImage).
		u, err := url.Parse(rawURL)
		if err != nil {
			return "", fmt.Errorf("invalid url: %w", err)
		}
		if !strings.HasPrefix(u.Path, "/"+userID+"/") {
			return "", ErrURLNotOwned
		}
		tmpPath, err := downloadToTempFile(ctx, rawURL, 0)
		if err != nil {
			return "", fmt.Errorf("download reference: %w", err)
		}
		defer os.Remove(tmpPath)
		data, err = os.ReadFile(tmpPath)
		if err != nil {
			return "", fmt.Errorf("read downloaded reference: %w", err)
		}
		ext = filepath.Ext(tmpPath)
	}

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
		items = append(items, *s.publicGeneration(&generations[i]))
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
	return s.publicGeneration(&gen), nil
}

func (s *DesignerService) updateGenerationStatus(genID, status, errMsg string) {
	updates := map[string]any{"status": status}
	if errMsg != "" {
		updates["error"] = errMsg
	}
	if status == model.ImageGenerationStatusFailed {
		now := time.Now()
		updates["completed_at"] = &now
	}
	s.db.Model(&model.ImageGeneration{}).Where("id = ?", genID).Updates(updates)
}

func designerGenerationUserError(err error) string {
	const fallback = "图片生成失败，请稍后重试"
	if err == nil {
		return fallback
	}

	var genErr *image.GenerateError
	if errors.As(err, &genErr) {
		switch genErr.Code {
		case "url_download_error":
			return "图片已生成，但保存到作品库失败，请稍后重试"
		case "content_policy_violation":
			return "提示词可能不符合内容安全要求，请调整后重试"
		case "rate_limit", "server_error", "network_error":
			return "图片服务繁忙，请稍后重试"
		case "no_image":
			return "图片服务没有返回图片，请调整提示词后重试"
		case "endpoint_protocol", "usage_required_missing":
			return "图片服务配置异常，请联系管理员检查模型配置"
		}
		return sanitizeDesignerGenerationError(genErr.Message, fallback)
	}

	return sanitizeDesignerGenerationError(err.Error(), fallback)
}

func sanitizeDesignerGenerationError(raw, fallback string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return fallback
	}
	lower := strings.ToLower(msg)

	if strings.Contains(msg, "下载失败") || strings.Contains(msg, "下载图片失败") || strings.Contains(lower, "url_download_error") {
		return "图片已生成，但保存到作品库失败，请稍后重试"
	}
	if strings.Contains(lower, "content policy") || strings.Contains(lower, "content_filter") ||
		strings.Contains(msg, "敏感") || strings.Contains(msg, "审核") || strings.Contains(msg, "不合规") {
		return "提示词可能不符合内容安全要求，请调整后重试"
	}
	if strings.Contains(lower, "api key") || strings.Contains(lower, "base url") || strings.Contains(lower, "endpoint") ||
		strings.Contains(msg, "配置文件") || strings.Contains(msg, "配置异常") {
		return "图片服务配置异常，请联系管理员检查模型配置"
	}
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "timeout") ||
		strings.Contains(msg, "稍后重试") {
		return "图片服务繁忙，请稍后重试"
	}
	if looksLikeInternalDesignerError(msg) {
		return fallback
	}

	if i := strings.IndexAny(msg, "\r\n"); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	if len([]rune(msg)) > 120 {
		return fallback
	}
	return msg
}

func looksLikeInternalDesignerError(msg string) bool {
	lower := strings.ToLower(msg)
	internalMarkers := []string{
		"{\"level\"", "\"error\"", "<br/>", "revisedprompt", "http://", "https://",
		"[openai]", "[gemini]", "[volcengine]", "stack trace", "panic:",
	}
	for _, marker := range internalMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

type DesignerProviderInfo struct {
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Alias        string                       `json:"alias,omitempty"`
	Description  string                       `json:"description,omitempty"`
	MinTier      string                       `json:"min_tier,omitempty"`
	Provider     string                       `json:"-"`
	ProviderKey  string                       `json:"-"`
	Route        string                       `json:"-"`
	Model        string                       `json:"-"`
	Credits      int                          `json:"credits"`
	Enabled      bool                         `json:"enabled"`
	Idx          int                          `json:"idx"`
	Capabilities DesignerProviderCapabilities `json:"capabilities"`
	Pricing      DesignerProviderPricing      `json:"pricing"`
}

type DesignerProviderCapabilities = srvconfig.DesignerProviderCapabilities

type DesignerProviderPricing struct {
	PricingType      string `json:"pricing_type"`
	Currency         string `json:"currency"`
	BillingNote      string `json:"billing_note"`
	PricingTier      string `json:"pricing_tier,omitempty"`
	ListPriceCredits int    `json:"list_price_credits,omitempty"`
	DiscountCredits  int    `json:"discount_credits,omitempty"`
}

func (s *DesignerService) GetProviders(ctx context.Context, userID string) []DesignerProviderInfo {
	if s.imageCfg == nil || s.imageCfg.Designer == nil {
		return nil
	}

	// Build the ordered key list: YAML insertion order first, then any
	// extra keys present in the map but missing from the order snapshot
	// (defensive — should not happen in practice).
	order := s.imageCfg.DesignerOrder()
	seen := make(map[string]struct{}, len(order))
	for _, id := range order {
		seen[id] = struct{}{}
	}
	for id := range s.imageCfg.Designer {
		if _, ok := seen[id]; ok {
			continue
		}
		order = append(order, id)
		seen[id] = struct{}{}
	}

	providers := make([]DesignerProviderInfo, 0, len(order))
	userTier := model.TierFree
	if s.repo != nil && strings.TrimSpace(userID) != "" {
		if user, err := s.repo.Users().FindByID(ctx, userID); err == nil && user != nil {
			userTier = model.ResolveTier(user.Tier)
		}
	}
	for i, id := range order {
		cfg := s.imageCfg.Designer[id]
		if cfg == nil {
			continue
		}
		name := cfg.Alias
		if name == "" {
			name = "图像能力"
		}
		routeName := "image_generation.designer." + id
		providerKey := s.designerProviderKey(id)
		route, _ := s.designerRoute(id)
		requiredTier := model.NormalizeTier(route.MinTier)
		if !model.TierSatisfies(userTier, requiredTier) {
			continue
		}
		publicID := strings.TrimSpace(route.SelectionKey)
		if publicID == "" {
			publicID = id
		}
		capabilities := route.Capabilities
		capabilities.MaxBatch = 1
		credits := 0
		listPrice, discount := 0, 0
		pricingTier := ""
		if s.billingCatalog != nil {
			if userID != "" {
				var resolved *ResolvedSKUPrice
				var err error
				if strings.TrimSpace(route.BillingSKU) != "" {
					resolved, err = s.billingCatalog.ResolvePriceBySKUIDForUser(ctx, userID, "", route.BillingSKU)
				} else {
					resolved, err = s.billingCatalog.ResolvePrice(ctx, userID, "", "designer.generate_image", routeName)
				}
				if err == nil {
					credits, listPrice, discount = int(resolved.PriceCredits), int(resolved.ListPriceCredits), int(resolved.DiscountCredits)
					pricingTier = string(resolved.PricingTier)
				}
			} else if strings.TrimSpace(route.BillingSKU) != "" {
				if sku, err := s.billingCatalog.ResolveSKUByID(ctx, "", route.BillingSKU); err == nil {
					credits, listPrice = int(sku.PriceCredits), int(sku.PriceCredits)
				}
			} else if sku, err := s.billingCatalog.ResolveSKU(ctx, "", "designer.generate_image", routeName); err == nil {
				credits, listPrice = int(sku.PriceCredits), int(sku.PriceCredits)
			}
		}
		providers = append(providers, DesignerProviderInfo{
			ID:           publicID,
			Name:         name,
			Alias:        cfg.Alias,
			Description:  route.Description,
			MinTier:      string(requiredTier),
			Provider:     cfg.Provider,
			ProviderKey:  providerKey,
			Route:        routeName,
			Model:        cfg.Model,
			Credits:      credits,
			Enabled:      cfg.IsEnabled(),
			Idx:          i,
			Capabilities: capabilities,
			Pricing: DesignerProviderPricing{
				PricingType: "fixed_sku", Currency: "credits", BillingNote: "fixed retail SKU",
				PricingTier: pricingTier, ListPriceCredits: listPrice, DiscountCredits: discount,
			},
		})
	}
	return providers
}

func (s *DesignerService) designerProviderKey(id string) string {
	if s.fullCfg == nil {
		return ""
	}
	if route, ok := s.fullCfg.ModelRoutes.ImageGeneration.Designer[id]; ok {
		return route.Provider
	}
	return ""
}

// findDesignerConfig finds the first Designer entry matching the given provider name.
func (s *DesignerService) findDesignerConfig(provider string) *config.ImageAPI {
	if s.imageCfg == nil || s.imageCfg.Designer == nil {
		return nil
	}
	for _, cfg := range s.imageCfg.Designer {
		if cfg != nil && cfg.Provider == provider {
			return cfg
		}
	}
	return nil
}

// findDesignerConfigByID finds a Designer entry by its config map key (e.g., "wangcai").
func (s *DesignerService) findDesignerConfigByID(id string) *config.ImageAPI {
	if s.imageCfg == nil || s.imageCfg.Designer == nil {
		return nil
	}
	if cfg := s.imageCfg.Designer[id]; cfg != nil {
		return cfg
	}
	if s.fullCfg != nil {
		for internalID, route := range s.fullCfg.ModelRoutes.ImageGeneration.Designer {
			if strings.TrimSpace(route.SelectionKey) == strings.TrimSpace(id) {
				return s.imageCfg.Designer[internalID]
			}
		}
	}
	return nil
}

func (s *DesignerService) resolveAPIKey(provider string) string {
	if p := s.findDesignerConfig(provider); p != nil {
		return p.Key
	}
	if s.imageCfg != nil && s.imageCfg.Cover != nil {
		return s.imageCfg.Cover.Key
	}
	return ""
}

func (s *DesignerService) resolveBaseURL(provider string) string {
	if p := s.findDesignerConfig(provider); p != nil {
		return p.BaseURL
	}
	if s.imageCfg != nil && s.imageCfg.Cover != nil {
		return s.imageCfg.Cover.BaseURL
	}
	return ""
}

func (s *DesignerService) resolveModel(provider string) string {
	if p := s.findDesignerConfig(provider); p != nil {
		return p.Model
	}
	if s.imageCfg != nil && s.imageCfg.Cover != nil {
		return s.imageCfg.Cover.Model
	}
	return ""
}

func (s *DesignerService) resolveResponseFormat(provider string) string {
	if p := s.findDesignerConfig(provider); p != nil {
		return p.ResponseFormat
	}
	return ""
}

func (s *DesignerService) resolveProvider() string {
	if s.imageCfg == nil {
		return "openai"
	}
	if s.imageCfg.Cover != nil && s.imageCfg.Cover.Provider != "" {
		return s.imageCfg.Cover.Provider
	}
	return "openai"
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
	if s.storage == nil || s.storage.HasCustomDomain() {
		return
	}
	for i := range results {
		if results[i].FileID != "" {
			signedURL, err := s.storage.DownloadURL(ctx, results[i].FileID, 3600)
			if err == nil {
				results[i].ImageURL = signedURL
			} else {
				s.logger.Warn().Err(err).Str("file_id", results[i].FileID).Msg("failed to sign designer result URL")
			}
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
