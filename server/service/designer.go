package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/image"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/storage"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type DesignerService struct {
	db       *gorm.DB
	imageSvc *ImageService
	creditSvc *CreditService
	imageCfg *srvconfig.ImageAPIConfig
	storage  storage.Provider
	logger   *zerolog.Logger
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
	ChannelID        string   `json:"channel_id"`
	Prompt           string   `json:"prompt"`
	Provider         string   `json:"provider"`
	Model            string   `json:"model"`
	Quality          string   `json:"quality,omitempty"`
	Size             string   `json:"size,omitempty"`
	N                int      `json:"n,omitempty"`
	OutputFormat     string   `json:"output_format,omitempty"`
	ReferenceFileIDs []string `json:"reference_file_ids,omitempty"`
	MaskFileID       string   `json:"mask_file_id,omitempty"`
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

	genID := uuid.New().String()
	refFilesJSON, _ := json.Marshal(req.ReferenceFileIDs)
	gen := &model.ImageGeneration{
		ID:             genID,
		UserID:         userID,
		ChannelID:      req.ChannelID,
		Prompt:         req.Prompt,
		Provider:       provider,
		Model:          modelName,
		Quality:        req.Quality,
		Size:           req.Size,
		N:              req.N,
		OutputFormat:   req.OutputFormat,
		Status:         model.ImageGenerationStatusGenerating,
		ReferenceFiles: string(refFilesJSON),
		MaskFileID:     req.MaskFileID,
	}

	if err := s.db.Create(gen).Error; err != nil {
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
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error().Str("gen_id", genID).Any("panic", r).Msg("generation panicked")
			s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed, fmt.Sprintf("internal error: %v", r))
		}
	}()

	var gen model.ImageGeneration
	if err := s.db.Where("id = ?", genID).First(&gen).Error; err != nil {
		s.logger.Error().Err(err).Str("gen_id", genID).Msg("generation record not found")
		return
	}

	provider := gen.Provider
	modelName := gen.Model

	apiKey := s.resolveAPIKey(provider)
	baseURL := s.resolveBaseURL(provider)

	keyPreview := ""
	if len(apiKey) > 4 {
		keyPreview = apiKey[:4] + "..."
	} else if apiKey != "" {
		keyPreview = "**"
	}

	s.logger.Info().
		Str("provider", provider).
		Str("model", modelName).
		Str("base_url", baseURL).
		Str("key_preview", keyPreview).
		Str("size", gen.Size).
		Int("n", gen.N).
		Msg("designer: starting image generation")

	apiCfg := &config.ImageAPI{
		Key:      apiKey,
		BaseURL:  baseURL,
		Provider: provider,
		Model:    modelName,
		Size:     gen.Size,
	}

	providerInst, err := image.NewProvider(apiCfg, s.logger)
	if err != nil {
		s.logger.Error().Err(err).Str("provider", provider).Msg("designer: failed to create image provider")
		s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed, err.Error())
		return
	}

	var refFileIDs []string
	if gen.ReferenceFiles != "" {
		_ = json.Unmarshal([]byte(gen.ReferenceFiles), &refFileIDs)
	}
	refPaths := make([]string, 0, len(refFileIDs))
	for _, fileID := range refFileIDs {
		path, err := s.resolveFilePath(fileID)
		if err != nil {
			s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed,
				fmt.Sprintf("resolve reference file %s: %v", fileID, err))
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
			return
		}
		maskPath = path
	}

	genOpts := &image.GenerateOptions{
		Quality:       gen.Quality,
		OutputFormat:  gen.OutputFormat,
		N:             gen.N,
		Size:          gen.Size,
		RefImagePaths: refPaths,
		MaskPath:      maskPath,
	}

	result, err := providerInst.Generate(ctx, gen.Prompt, genOpts)
	if err != nil {
		s.logger.Error().Err(err).
			Str("provider", provider).
			Str("model", modelName).
			Str("prompt_preview", truncate(gen.Prompt, 100)).
			Msg("designer: image generation failed")
		s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed, err.Error())
		return
	}

	s.logger.Info().
		Str("provider", provider).
		Str("model", modelName).
		Str("result_type", result.ResponseType).
		Int("image_count", len(result.Images)).
		Str("url_preview", truncate(result.URL, 80)).
		Msg("designer: image generation completed")

	s.processResults(ctx, gen.UserID, genID, result)

	s.db.Model(&model.ImageGeneration{}).Where("id = ?", genID).Updates(map[string]any{
		"status":         model.ImageGenerationStatusCompleted,
		"revised_prompt": result.RevisedPrompt,
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
	s.db.Model(&model.ImageGeneration{}).Where("id = ?", genID).Updates(updates)
}

type DesignerProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
}

func (s *DesignerService) GetProviders() []DesignerProviderInfo {
	if s.imageCfg == nil || s.imageCfg.Designer == nil {
		return nil
	}
	var providers []DesignerProviderInfo
	for id, cfg := range s.imageCfg.Designer {
		if cfg == nil || !cfg.IsEnabled() {
			continue
		}
		name := cfg.Alias
		if name == "" {
			name = strings.ToUpper(id[:1]) + id[1:]
		}
		providers = append(providers, DesignerProviderInfo{
			ID:          id,
			Name:        name,
			Provider:    cfg.Provider,
			Model:       cfg.Model,

		})
	}
	return providers
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
