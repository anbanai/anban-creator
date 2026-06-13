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

	"github.com/google/uuid"
	"github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/image"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/storage"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// ErrURLNotOwned is returned by UploadReferenceFromURL when the supplied URL
// does not point at this backend's storage or is not owned by the calling user.
// Handlers should map this to a 4xx response.
var ErrURLNotOwned = errors.New("url not allowed")

type DesignerService struct {
	db        *gorm.DB
	imageSvc  *ImageService
	creditSvc *CreditService
	imageCfg  *srvconfig.ImageAPIConfig
	storage   storage.Provider
	logger    *zerolog.Logger
}

func NewDesignerService(
	db *gorm.DB,
	imageSvc *ImageService,
	creditSvc *CreditService,
	imageCfg *srvconfig.ImageAPIConfig,
	store storage.Provider,
	logger *zerolog.Logger,
) *DesignerService {
	return &DesignerService{
		db:        db,
		imageSvc:  imageSvc,
		creditSvc: creditSvc,
		imageCfg:  imageCfg,
		storage:   store,
		logger:    logger,
	}
}

type DesignerGenerateRequest struct {
	ChannelID         string   `json:"channel_id"`
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

// CreateGenerationRecord validates the request, resolves config, and creates
// a generation record with "generating" status. Returns the generation ID.
func (s *DesignerService) CreateGenerationRecord(ctx context.Context, userID string, req DesignerGenerateRequest) (string, error) {
	if req.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if req.ChannelID == "" {
		req.ChannelID = "default"
	}
	if req.N < 1 {
		req.N = 1
	}
	if req.N > 10 {
		req.N = 10
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
	if req.ProviderID != "" {
		if cfg := s.findDesignerConfigByID(req.ProviderID); cfg != nil {
			totalCost = cfg.Credits * req.N
		}
	}
	if totalCost == 0 {
		if unitCost := s.resolveCredits(provider, modelName); unitCost > 0 {
			totalCost = unitCost * req.N
		}
	}

	genID := uuid.New().String()

	if totalCost > 0 {
		if _, err := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeImageGen, totalCost, genID); err != nil {
			return "", fmt.Errorf("deduct credits: %w", err)
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
		ChannelID:         req.ChannelID,
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
	}

	if err := s.db.Create(gen).Error; err != nil {
		// Refund on DB create failure to avoid losing credits.
		if totalCost > 0 && s.creditSvc != nil {
			if refundErr := s.creditSvc.RefundForOperation(ctx, userID, model.CreditTypeImageGen, totalCost, "生成记录创建失败退还", genID); refundErr != nil {
				s.logger.Error().Err(refundErr).Int("cost", totalCost).Msg("failed to refund after DB create failure")
			}
		}
		return "", fmt.Errorf("create generation record: %w", err)
	}

	return genID, nil
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

	providerInst, err := image.NewProvider(apiCfg, s.logger)
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

	f, err := os.CreateTemp("", fmt.Sprintf("anbanwriter_download_%d_*%s", index, ext))
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
// "anbanwriter_ref_{fileID}_{filename}" naming (which resolveFilePath globs
// against), mirrors them to remote storage when configured, and returns the fileID.
func (s *DesignerService) registerReferenceFile(ctx context.Context, userID, filename string, data []byte) (string, error) {
	fileID := uuid.New().String()

	// Always save locally so providers can read file paths
	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("anbanwriter_ref_%s_%s", fileID, filename))
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

func (s *DesignerService) GetHistory(ctx context.Context, userID, channelID string, page, pageSize int) ([]model.ImageGeneration, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}

	var total int64
	var generations []model.ImageGeneration

	query := s.db.Where("user_id = ?", userID)
	if channelID != "" {
		query = query.Where("channel_id = ?", channelID)
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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Credits  int    `json:"credits"`
	Enabled  bool   `json:"enabled"`
}

func (s *DesignerService) GetProviders() []DesignerProviderInfo {
	if s.imageCfg == nil || s.imageCfg.Designer == nil {
		return nil
	}
	var providers []DesignerProviderInfo
	for id, cfg := range s.imageCfg.Designer {
		if cfg == nil {
			continue
		}
		name := cfg.Alias
		if name == "" {
			name = strings.ToUpper(id[:1]) + id[1:]
		}
		providers = append(providers, DesignerProviderInfo{
			ID:       id,
			Name:     name,
			Provider: cfg.Provider,
			Model:    cfg.Model,
			Credits:  cfg.Credits,
			Enabled:  cfg.IsEnabled(),
		})
	}
	return providers
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
	pattern := filepath.Join(os.TempDir(), fmt.Sprintf("anbanwriter_ref_%s_*", fileID))
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
