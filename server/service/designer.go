package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anbanai/anban-creator/app/config"
	"github.com/anbanai/anban-creator/app/image"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ErrURLNotOwned is returned by UploadReferenceFromURL when the supplied URL
// does not point at this backend's storage or is not owned by the calling user.
// Handlers should map this to a 4xx response.
var ErrURLNotOwned = errors.New("url not allowed")

type DesignerService struct {
	db              *gorm.DB
	imageSvc        *ImageService
	creditSvc       *CreditService
	fullCfg         *srvconfig.Config
	imageCfg        *srvconfig.ImageAPIConfig
	storage         storage.Provider
	logger          *zerolog.Logger
	providerFactory func(*config.ImageAPI, *zerolog.Logger) (image.Provider, error)
}

func NewDesignerService(
	db *gorm.DB,
	imageSvc *ImageService,
	creditSvc *CreditService,
	fullCfg *srvconfig.Config,
	store storage.Provider,
	logger *zerolog.Logger,
) *DesignerService {
	var imageCfg *srvconfig.ImageAPIConfig
	if fullCfg != nil {
		imageCfg = &fullCfg.ImageAPI
	}
	return &DesignerService{
		db:              db,
		imageSvc:        imageSvc,
		creditSvc:       creditSvc,
		fullCfg:         fullCfg,
		imageCfg:        imageCfg,
		storage:         store,
		logger:          logger,
		providerFactory: image.NewProvider,
	}
}

type DesignerGenerateRequest struct {
	ProjectID         string   `json:"project_id"`
	Prompt            string   `json:"prompt"`
	Provider          string   `json:"provider"`
	ProviderID        string   `json:"provider_id,omitempty"`
	Model             string   `json:"model"`
	Quality           string   `json:"quality,omitempty"`
	Size              string   `json:"size,omitempty"`
	N                 int      `json:"n,omitempty"`
	OutputFormat      string   `json:"output_format,omitempty"`
	OutputCompression int      `json:"output_compression,omitempty"`
	Background        string   `json:"background,omitempty"`
	ReferenceFileIDs  []string `json:"reference_file_ids,omitempty"`
	MaskFileID        string   `json:"mask_file_id,omitempty"`
	Watermark         *bool    `json:"watermark,omitempty"`
}

type DesignerGenerationCreated struct {
	GenerationID     string `json:"generation_id"`
	Status           string `json:"status"`
	EstimatedCredits int    `json:"estimated_credits"`
	BillingMode      string `json:"billing_mode"`
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
	if req.N < 1 {
		req.N = 1
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

	// Billing: look up per-image cost from model config.
	// Prefer designer entry ID for accurate cost lookup when multiple entries
	// share the same provider type (e.g., two "openai" entries).
	var totalCost int
	var billingMode string
	var providerKey string
	var routeName string
	var estimateCost srvconfig.ImageGenerationCreditCost
	if req.ProviderID != "" {
		if cfg := s.findDesignerConfigByID(req.ProviderID); cfg != nil {
			if !cfg.IsEnabled() {
				return nil, fmt.Errorf("designer provider %s is disabled", req.ProviderID)
			}
			provider = cfg.Provider
			modelName = cfg.Model
			if err := validateDesignerGenerateRequest(req, designerCapabilities(provider, modelName)); err != nil {
				return nil, err
			}
			if route, ok := s.designerRoute(req.ProviderID); ok {
				providerKey = route.Provider
				routeName = "image_generation.designer." + req.ProviderID
			}
			if providerKey != "" && s.fullCfg != nil {
				var err error
				estimateCost, err = s.fullCfg.CalculateImageGenerationEstimateCredits(providerKey, cfg.Model, srvconfig.ImageGenerationUsage{
					Size:                resolvedDesignerSize(req.Size, cfg.Model),
					Quality:             req.Quality,
					Count:               req.N,
					ReferenceImageCount: len(req.ReferenceFileIDs),
				}, string(s.userTier(ctx, userID)), s.userBillingMultiplier(ctx, userID))
				if err != nil {
					return nil, err
				}
				totalCost = estimateCost.FinalCredits
				billingMode = estimateCost.PriceSnapshot.PricingType
			} else {
				totalCost = cfg.Credits * req.N
			}
		} else {
			return nil, fmt.Errorf("designer provider %s is not configured", req.ProviderID)
		}
	}
	if totalCost == 0 {
		if unitCost := s.resolveCredits(provider, modelName); unitCost > 0 {
			totalCost = unitCost * req.N
		}
	}

	genID := uuid.New().String()

	if totalCost > 0 {
		if s.creditSvc == nil {
			return nil, fmt.Errorf("credit service is required for priced image generation")
		}
		metadata := model.CreditTransactionMetadata{}
		if estimateCost.FinalCredits > 0 {
			metadata = imageCreditMetadata(providerKey, modelName, routeName, estimateCost)
		}
		if _, err := s.creditSvc.DeductForOperationWithMetadata(ctx, userID, model.CreditTypeImageGen, totalCost, metadata, genID); err != nil {
			return nil, fmt.Errorf("deduct credits: %w", err)
		}
	}

	refFilesJSON, _ := json.Marshal(req.ReferenceFileIDs)
	watermark := false
	if req.Watermark != nil {
		watermark = *req.Watermark
	}
	gen := &model.ImageGeneration{
		ID:                genID,
		UserID:            userID,
		ProjectID:         req.ProjectID,
		Prompt:            req.Prompt,
		Provider:          provider,
		ProviderID:        req.ProviderID,
		Model:             modelName,
		Quality:           req.Quality,
		Size:              req.Size,
		N:                 req.N,
		OutputFormat:      req.OutputFormat,
		OutputCompression: req.OutputCompression,
		Background:        req.Background,
		Watermark:         watermark,
		Status:            model.ImageGenerationStatusGenerating,
		ReferenceFiles:    string(refFilesJSON),
		MaskFileID:        req.MaskFileID,
		Cost:              totalCost,
		EstimatedCost:     totalCost,
		BillingMode:       billingMode,
		BillingStatus:     billingStatus(totalCost, "estimated"),
	}
	if estimateCost.FinalCredits > 0 {
		if data, err := json.Marshal(estimateCost.PriceSnapshot); err == nil {
			gen.PriceSnapshot = data
		}
	}

	if err := s.db.Create(gen).Error; err != nil {
		// Refund on DB create failure to avoid losing credits.
		if totalCost > 0 && s.creditSvc != nil {
			if refundErr := s.creditSvc.RefundForOperation(ctx, userID, model.CreditTypeImageGen, totalCost, "生成记录创建失败退还", genID); refundErr != nil {
				s.logger.Error().Err(refundErr).Int("cost", totalCost).Msg("failed to refund after DB create failure")
			}
		}
		return nil, fmt.Errorf("create generation record: %w", err)
	}

	return &DesignerGenerationCreated{GenerationID: genID, Status: model.ImageGenerationStatusGenerating, EstimatedCredits: totalCost, BillingMode: billingMode}, nil
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
	route, ok := s.fullCfg.ModelRoutes.ImageGeneration.Designer[id]
	return route, ok
}

func (s *DesignerService) userTier(ctx context.Context, userID string) model.Tier {
	if s.db == nil || userID == "" {
		return model.TierFree
	}
	var user model.User
	if err := s.db.Select("tier").First(&user, "id = ?", userID).Error; err != nil {
		return model.TierFree
	}
	return model.ResolveTier(model.Tier(user.Tier))
}

func (s *DesignerService) userBillingMultiplier(ctx context.Context, userID string) float64 {
	if s.db == nil || userID == "" {
		return 1
	}
	var user model.User
	if err := s.db.Select("billing_multiplier").First(&user, "id = ?", userID).Error; err != nil {
		if s.logger != nil {
			s.logger.Warn().Err(err).Str("user_id", userID).Msg("designer billing multiplier lookup failed")
		}
		return 1
	}
	if user.BillingMultiplier == nil || *user.BillingMultiplier <= 0 {
		return 1
	}
	return *user.BillingMultiplier
}

func imageCreditMetadata(providerKey, modelName, routeName string, cost srvconfig.ImageGenerationCreditCost) model.CreditTransactionMetadata {
	snapshot := map[string]any{}
	if data, err := json.Marshal(cost.PriceSnapshot); err == nil {
		_ = json.Unmarshal(data, &snapshot)
	}
	return model.CreditTransactionMetadata{
		Provider:               providerKey,
		Model:                  modelName,
		Route:                  routeName,
		TextInputTokens:        cost.Usage.TextInputTokens,
		TextCachedInputTokens:  cost.Usage.TextCachedInputTokens,
		ImageInputTokens:       cost.Usage.ImageInputTokens,
		ImageCachedInputTokens: cost.Usage.ImageCachedInputTokens,
		ImageOutputTokens:      cost.Usage.ImageOutputTokens,
		TotalTokens:            cost.Usage.TotalTokens,
		BaseCredits:            cost.BaseCredits,
		TierMultiplier:         cost.TierMultiplier,
		UserMultiplier:         cost.UserMultiplier,
		FinalCredits:           cost.FinalCredits,
		PriceSnapshot:          snapshot,
	}
}

func resolvedDesignerSize(size, modelName string) string {
	size = designerCapabilitySize(size)
	if size == "" || strings.EqualFold(size, "auto") {
		return "1024x1024"
	}
	if strings.Contains(size, "x") {
		return size
	}
	if strings.HasPrefix(strings.ToLower(modelName), "gpt-image-") {
		switch size {
		case "16:9", "4:3":
			return "1536x1024"
		case "9:16", "3:4":
			return "1024x1536"
		}
		return "1024x1024"
	}
	return size
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

func billingStatus(cost int, defaultStatus string) string {
	if cost <= 0 {
		return ""
	}
	return defaultStatus
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
		// Attempt refund via the deduction's OperationID trace.
		if s.creditSvc != nil {
			if refundErr := s.creditSvc.RefundForOperationByID(ctx, genID,
				fmt.Sprintf("生成记录丢失退还 (genID=%s)", genID)); refundErr != nil {
				s.logger.Error().Err(refundErr).Str("gen_id", genID).Msg("failed to refund for missing record")
			}
		}
		return
	}

	now := time.Now()
	s.db.Model(&model.ImageGeneration{}).Where("id = ?", genID).Update("started_at", &now)

	var refundOnce sync.Once
	refund := func() {
		refundOnce.Do(func() {
			if gen.Cost > 0 && s.creditSvc != nil {
				if err := s.creditSvc.RefundForOperation(ctx, gen.UserID, model.CreditTypeImageGen, gen.Cost, fmt.Sprintf("设计师生成失败退还 +%d", gen.Cost), genID); err != nil {
					s.logger.Error().Err(err).Str("gen_id", genID).Int("cost", gen.Cost).Msg("failed to refund designer generation")
				}
			}
		})
	}

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error().Str("gen_id", genID).Any("panic", r).Msg("generation panicked")
			s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed, fmt.Sprintf("internal error: %v", r))
			refund()
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
		_ = json.Unmarshal([]byte(gen.ReferenceFiles), &refFileIDs)
	}

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
		s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed, err.Error())
		refund()
		return
	}

	refPaths := make([]string, 0, len(refFileIDs))
	for _, fileID := range refFileIDs {
		path, err := s.resolveFilePath(fileID)
		if err != nil {
			s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed,
				fmt.Sprintf("resolve reference file %s: %v", fileID, err))
			refund()
			return
		}
		refPaths = append(refPaths, path)
	}

	maskPath := ""
	if gen.MaskFileID != "" {
		path, err := s.resolveFilePath(gen.MaskFileID)
		if err != nil {
			s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed,
				fmt.Sprintf("resolve mask file %s: %v", gen.MaskFileID, err))
			refund()
			return
		}
		maskPath = path
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
		s.logger.Error().Err(err).
			Str("gen_id", genID).
			Str("user_id", gen.UserID).
			Str("provider", provider).
			Str("model", modelName).
			Str("prompt_preview", truncate(gen.Prompt, 100)).
			Dur("elapsed", time.Since(start)).
			Msg("designer: image generation failed")
		s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed, err.Error())
		refund()
		return
	}

	if ok := s.settleGenerationBilling(ctx, &gen, result); !ok {
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

	s.processResults(ctx, gen.UserID, genID, result)

	completedAt := time.Now()
	s.db.Model(&model.ImageGeneration{}).Where("id = ?", genID).Updates(map[string]any{
		"status":         model.ImageGenerationStatusCompleted,
		"revised_prompt": result.RevisedPrompt,
		"completed_at":   &completedAt,
	})
}

func (s *DesignerService) settleGenerationBilling(ctx context.Context, gen *model.ImageGeneration, result *image.GenerateResult) bool {
	if gen == nil || result == nil || gen.BillingMode != srvconfig.ImagePricingTypeOpenAIUsage {
		return true
	}
	providerKey, routeName, ok := s.generationBillingRoute(gen.ProviderID)
	if !ok || s.fullCfg == nil {
		s.failUnbillableGeneration(ctx, gen, "gpt-image-2 usage billing route is not configured", "usage_required_failed")
		return false
	}
	if result.Usage == nil || result.Usage.TotalTokens <= 0 || result.Usage.ImageOutputTokens <= 0 {
		s.failUnbillableGeneration(ctx, gen, "gpt-image-2 usage is required for billing", "usage_required_failed")
		return false
	}

	usage := srvconfig.ImageGenerationUsage{
		Size:                   resolvedDesignerSize(firstNonEmpty(result.Size, gen.Size), gen.Model),
		Quality:                gen.Quality,
		Count:                  gen.N,
		TextInputTokens:        result.Usage.TextInputTokens,
		TextCachedInputTokens:  result.Usage.TextCachedInputTokens,
		ImageInputTokens:       result.Usage.ImageInputTokens,
		ImageCachedInputTokens: result.Usage.ImageCachedInputTokens,
		ImageOutputTokens:      result.Usage.ImageOutputTokens,
		TotalTokens:            result.Usage.TotalTokens,
	}
	cost, err := s.fullCfg.CalculateImageGenerationUsageCredits(providerKey, gen.Model, usage, string(s.userTier(ctx, gen.UserID)), s.userBillingMultiplier(ctx, gen.UserID))
	if err != nil {
		s.failUnbillableGeneration(ctx, gen, err.Error(), "usage_required_failed")
		return false
	}

	delta := cost.FinalCredits - gen.EstimatedCost
	if delta > 0 {
		metadata := imageCreditMetadata(providerKey, gen.Model, routeName, cost)
		if s.creditSvc == nil {
			s.markSettlementFailed(ctx, gen, "credit service is required for GPT Image 2 settlement")
			return false
		}
		if _, err := s.creditSvc.DeductForOperationWithMetadata(ctx, gen.UserID, model.CreditTypeImageGen, delta, metadata, gen.ID+":settlement"); err != nil {
			s.markSettlementFailed(ctx, gen, fmt.Sprintf("settle GPT Image 2 billing: %v", err))
			return false
		}
	} else if delta < 0 && s.creditSvc != nil {
		refund := -delta
		if err := s.creditSvc.RefundForOperation(ctx, gen.UserID, model.CreditTypeImageGen, refund, fmt.Sprintf("GPT Image 2 usage settlement refund +%d", refund), gen.ID+":settlement_refund"); err != nil {
			s.markSettlementFailed(ctx, gen, fmt.Sprintf("settle GPT Image 2 refund: %v", err))
			return false
		}
	}

	priceSnapshot, _ := json.Marshal(cost.PriceSnapshot)
	updates := map[string]any{
		"cost":                      cost.FinalCredits,
		"final_cost":                cost.FinalCredits,
		"billing_status":            "settled",
		"text_input_tokens":         usage.TextInputTokens,
		"text_cached_input_tokens":  usage.TextCachedInputTokens,
		"image_input_tokens":        usage.ImageInputTokens,
		"image_cached_input_tokens": usage.ImageCachedInputTokens,
		"image_output_tokens":       usage.ImageOutputTokens,
		"total_tokens":              usage.TotalTokens,
		"price_snapshot":            datatypes.JSON(priceSnapshot),
	}
	if err := s.db.Model(&model.ImageGeneration{}).Where("id = ?", gen.ID).Updates(updates).Error; err != nil {
		s.markSettlementFailed(ctx, gen, fmt.Sprintf("persist GPT Image 2 billing settlement: %v", err))
		return false
	}
	gen.Cost = cost.FinalCredits
	gen.FinalCost = cost.FinalCredits
	gen.BillingStatus = "settled"
	gen.TextInputTokens = usage.TextInputTokens
	gen.TextCachedInputTokens = usage.TextCachedInputTokens
	gen.ImageInputTokens = usage.ImageInputTokens
	gen.ImageCachedInputTokens = usage.ImageCachedInputTokens
	gen.ImageOutputTokens = usage.ImageOutputTokens
	gen.TotalTokens = usage.TotalTokens
	gen.PriceSnapshot = datatypes.JSON(priceSnapshot)
	return true
}

func (s *DesignerService) generationBillingRoute(providerID string) (providerKey string, routeName string, ok bool) {
	if providerID == "" || s.fullCfg == nil {
		return "", "", false
	}
	route, ok := s.fullCfg.ModelRoutes.ImageGeneration.Designer[providerID]
	if !ok || route.Provider == "" {
		return "", "", false
	}
	return route.Provider, "image_generation.designer." + providerID, true
}

func (s *DesignerService) failUnbillableGeneration(ctx context.Context, gen *model.ImageGeneration, errMsg, billingStatus string) {
	if gen.Cost > 0 && s.creditSvc != nil {
		if err := s.creditSvc.RefundForOperation(ctx, gen.UserID, model.CreditTypeImageGen, gen.Cost, fmt.Sprintf("GPT Image 2 unbillable generation refund +%d", gen.Cost), gen.ID); err != nil {
			s.logger.Error().Err(err).Str("gen_id", gen.ID).Msg("failed to refund unbillable GPT Image 2 generation")
		}
	}
	now := time.Now()
	s.db.Model(&model.ImageGeneration{}).Where("id = ?", gen.ID).Updates(map[string]any{
		"status":         model.ImageGenerationStatusFailed,
		"error":          errMsg,
		"cost":           0,
		"final_cost":     0,
		"billing_status": billingStatus,
		"completed_at":   &now,
	})
}

func (s *DesignerService) markSettlementFailed(ctx context.Context, gen *model.ImageGeneration, errMsg string) {
	if gen.EstimatedCost > 0 && s.creditSvc != nil {
		if err := s.creditSvc.RefundForOperation(ctx, gen.UserID, model.CreditTypeImageGen, gen.EstimatedCost, fmt.Sprintf("GPT Image 2 settlement failed refund +%d", gen.EstimatedCost), gen.ID); err != nil {
			s.logger.Error().Err(err).Str("gen_id", gen.ID).Msg("failed to refund after GPT Image 2 settlement failure")
		}
	}
	now := time.Now()
	s.db.Model(&model.ImageGeneration{}).Where("id = ?", gen.ID).Updates(map[string]any{
		"status":         model.ImageGenerationStatusFailed,
		"error":          errMsg,
		"cost":           0,
		"final_cost":     0,
		"billing_status": "settlement_failed",
		"completed_at":   &now,
	})
}

func (s *DesignerService) processResults(ctx context.Context, userID, genID string, result *image.GenerateResult) {
	collectURLs := func(rawURL string, idx int) {
		// Resolve to a local file path — download remote URLs if needed.
		localPath := rawURL
		isTemp := false
		if !isLocalFilePath(rawURL) {
			tmpPath, err := downloadToTempFile(ctx, rawURL, idx)
			if err != nil {
				s.logger.Error().Err(err).Str("url", rawURL).Msg("failed to download remote image")
				if err := s.db.Create(&model.ImageGenerationResult{
					GenerationID: genID, ImageURL: rawURL, Index: idx,
				}).Error; err != nil {
					s.logger.Error().Err(err).Msg("failed to save fallback generation result")
				}
				return
			}
			localPath = tmpPath
			isTemp = true
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

		dbResult := model.ImageGenerationResult{
			GenerationID: genID,
			ImageURL:     serveURL,
			ImagePath:    rawURL,
			FileID:       storageKey,
			Index:        idx,
		}
		if err := s.db.Create(&dbResult).Error; err != nil {
			s.logger.Error().Err(err).Str("generation_id", genID).Int("index", idx).Msg("failed to save generation result")
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
		return
	}

	if result.URL != "" {
		collectURLs(result.URL, 0)
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
	// Limit to 10MB to prevent disk exhaustion from oversized responses.
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 10*1024*1024)); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write temp file: %w", err)
	}
	f.Close()

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

func (s *DesignerService) UploadReference(ctx context.Context, userID string, filename string, data []byte) (string, error) {
	return s.registerReferenceFile(ctx, userID, filename, data)
}

// registerReferenceFile saves the bytes to a temp file with the canonical
// "anban-creator_ref_{fileID}_{filename}" naming (which resolveFilePath globs
// against), mirrors them to remote storage when configured, and returns the fileID.
func (s *DesignerService) registerReferenceFile(ctx context.Context, userID, filename string, data []byte) (string, error) {
	fileID := uuid.New().String()

	// Always save locally so providers can read file paths
	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("anban-creator_ref_%s_%s", fileID, filename))
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return "", fmt.Errorf("save reference file: %w", err)
	}

	// Optionally persist to remote storage
	if s.storage != nil {
		ext := strings.ToLower(filepath.Ext(filename))
		contentType := "image/png"
		switch ext {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".gif":
			contentType = "image/gif"
		case ".webp":
			contentType = "image/webp"
		}
		key := fmt.Sprintf("designer/refs/%s/%s/%s", userID, fileID, filename)
		if _, err := s.storage.Upload(ctx, key, bytes.NewReader(data), contentType); err != nil {
			s.logger.Warn().Err(err).Str("file_id", fileID).Msg("failed to persist reference to storage, local file available")
		}
	}

	return fileID, nil
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
		data, err = s.storage.Read(ctx, key)
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
	return s.registerReferenceFile(ctx, userID, "source"+ext, data)
}

func (s *DesignerService) GetHistory(ctx context.Context, userID, projectID string, page, pageSize int) ([]model.ImageGeneration, int64, error) {
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

	return generations, total, nil
}

func (s *DesignerService) GetGeneration(ctx context.Context, userID, generationID string) (*model.ImageGeneration, error) {
	var gen model.ImageGeneration
	if err := s.db.Where("id = ? AND user_id = ?", generationID, userID).
		Preload("Results").First(&gen).Error; err != nil {
		return nil, err
	}
	s.signResultURLs(ctx, gen.Results)
	return &gen, nil
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

type DesignerProviderInfo struct {
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Alias        string                       `json:"alias,omitempty"`
	Provider     string                       `json:"provider"`
	ProviderKey  string                       `json:"provider_key,omitempty"`
	Route        string                       `json:"route,omitempty"`
	Model        string                       `json:"model"`
	Credits      int                          `json:"credits"`
	Enabled      bool                         `json:"enabled"`
	Idx          int                          `json:"idx"`
	Capabilities DesignerProviderCapabilities `json:"capabilities"`
	Pricing      DesignerProviderPricing      `json:"pricing"`
}

type DesignerProviderCapabilities struct {
	QualityLevels      []string `json:"quality_levels"`
	SizePresets        []string `json:"size_presets"`
	DefaultSize        string   `json:"default_size"`
	MaxBatch           int      `json:"max_batch"`
	MaxReferenceImages int      `json:"max_reference_images"`
	SupportsReference  bool     `json:"supports_reference"`
	SupportsMask       bool     `json:"supports_mask"`
	OutputFormats      []string `json:"output_formats"`
	HasBackground      bool     `json:"has_background"`
	HasCompression     bool     `json:"has_compression"`
	Watermark          bool     `json:"watermark"`
}

type DesignerProviderPricing struct {
	PricingType   string                        `json:"pricing_type,omitempty"`
	Currency      string                        `json:"currency,omitempty"`
	EstimateTable map[string]map[string]float64 `json:"estimate_table,omitempty"`
	CreditsPerCNY int                           `json:"credits_per_cny,omitempty"`
	RequiresUsage bool                          `json:"requires_usage,omitempty"`
	BillingNote   string                        `json:"billing_note,omitempty"`
}

func (s *DesignerService) GetProviders() []DesignerProviderInfo {
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
	for i, id := range order {
		cfg := s.imageCfg.Designer[id]
		if cfg == nil {
			continue
		}
		name := cfg.Alias
		if name == "" {
			name = strings.ToUpper(id[:1]) + id[1:]
		}
		routeName := "image_generation.designer." + id
		providerKey := s.designerProviderKey(id)
		providers = append(providers, DesignerProviderInfo{
			ID:           id,
			Name:         name,
			Alias:        cfg.Alias,
			Provider:     cfg.Provider,
			ProviderKey:  providerKey,
			Route:        routeName,
			Model:        cfg.Model,
			Credits:      cfg.Credits,
			Enabled:      cfg.IsEnabled(),
			Idx:          i,
			Capabilities: designerCapabilities(cfg.Provider, cfg.Model),
			Pricing:      s.designerPricing(providerKey, cfg.Model),
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

func (s *DesignerService) designerPricing(providerKey, modelName string) DesignerProviderPricing {
	if s.fullCfg == nil || providerKey == "" || modelName == "" {
		return DesignerProviderPricing{}
	}
	price, ok := s.fullCfg.ModelPrices.ImageGeneration[providerKey+"/"+modelName]
	if !ok {
		return DesignerProviderPricing{}
	}
	pricingType := strings.TrimSpace(price.PricingType)
	if pricingType == "" && strings.EqualFold(price.UnitString(), "image") {
		pricingType = srvconfig.ImagePricingTypePerImage
	}
	out := DesignerProviderPricing{
		PricingType:   pricingType,
		Currency:      strings.ToUpper(strings.TrimSpace(price.Currency)),
		CreditsPerCNY: s.fullCfg.Billing.CreditsPerCNY,
		RequiresUsage: price.RequireUsage,
	}
	if out.CreditsPerCNY <= 0 {
		out.CreditsPerCNY = 1000
	}
	if out.Currency == "" {
		out.Currency = "CNY"
	}
	if pricingType == srvconfig.ImagePricingTypeOpenAIUsage {
		out.BillingNote = "dynamic usage billing"
		out.EstimateTable = make(map[string]map[string]float64, len(price.EstimateTable))
		for size, qualities := range price.EstimateTable {
			out.EstimateTable[size] = make(map[string]float64, len(qualities))
			for quality, value := range qualities {
				out.EstimateTable[size][quality] = value.Float64()
			}
		}
	} else if pricingType == srvconfig.ImagePricingTypePerImage {
		out.BillingNote = "fixed per-image billing"
	}
	return out
}

func designerCapabilities(provider, modelName string) DesignerProviderCapabilities {
	if isDesignerGPTImageModel(modelName) {
		return DesignerProviderCapabilities{
			QualityLevels:      []string{"auto", "low", "medium", "high"},
			SizePresets:        []string{"auto", "1024x1024", "1536x1024", "1024x1536"},
			DefaultSize:        "auto",
			MaxBatch:           10,
			MaxReferenceImages: 16,
			SupportsReference:  true,
			SupportsMask:       true,
			OutputFormats:      []string{"png", "jpeg", "webp"},
			HasBackground:      true,
			HasCompression:     true,
		}
	}

	switch provider {
	case "volcengine":
		return DesignerProviderCapabilities{
			SizePresets:        []string{"1:1", "3:4", "4:3", "16:9", "9:16"},
			DefaultSize:        "1:1",
			MaxBatch:           1,
			MaxReferenceImages: 1,
			SupportsReference:  true,
			OutputFormats:      []string{"png", "jpeg"},
			Watermark:          true,
		}
	case "gemini":
		return DesignerProviderCapabilities{
			SizePresets:        []string{"1:1", "3:4", "4:3", "16:9"},
			DefaultSize:        "1:1",
			MaxBatch:           1,
			MaxReferenceImages: 10,
			SupportsReference:  true,
			OutputFormats:      []string{"png"},
		}
	}
	return DesignerProviderCapabilities{MaxBatch: 1}
}

func isDesignerGPTImageModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.HasPrefix(modelName, "gpt-image-") || modelName == "chatgpt-image-latest"
}

// resolveCredits returns the per-image credit cost for the given provider+model combination.
func (s *DesignerService) resolveCredits(provider, modelName string) int {
	if s.imageCfg == nil || s.imageCfg.Designer == nil {
		return 0
	}
	for _, cfg := range s.imageCfg.Designer {
		if cfg != nil && cfg.Provider == provider && cfg.Model == modelName {
			return cfg.Credits
		}
	}
	return 0
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
	return s.imageCfg.Designer[id]
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

func (s *DesignerService) resolveFilePath(fileID string) (string, error) {
	pattern := filepath.Join(os.TempDir(), fmt.Sprintf("anban-creator_ref_%s_*", fileID))
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("reference file not found: %s", fileID)
	}
	return matches[0], nil
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
