package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	Stream           bool     `json:"stream,omitempty"`
}

type DesignerGenerateResult struct {
	GenerationID  string                  `json:"generation_id"`
	Images        []DesignerGenerateImage `json:"images"`
	RevisedPrompt string                  `json:"revised_prompt,omitempty"`
	Usage         *DesignerGenerateUsage  `json:"usage,omitempty"`
}

type DesignerGenerateImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Index  int    `json:"index"`
}

type DesignerGenerateUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (s *DesignerService) Generate(ctx context.Context, userID string, req DesignerGenerateRequest, streamCB image.StreamCallback) (*DesignerGenerateResult, error) {
	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
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

	// Normalize provider aliases
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

	refPaths := make([]string, 0)
	for _, fileID := range req.ReferenceFileIDs {
		path, err := s.resolveFilePath(fileID)
		if err != nil {
			return nil, fmt.Errorf("resolve reference file %s: %w", fileID, err)
		}
		refPaths = append(refPaths, path)
	}

	maskPath := ""
	if req.MaskFileID != "" {
		path, err := s.resolveFilePath(req.MaskFileID)
		if err != nil {
			return nil, fmt.Errorf("resolve mask file %s: %w", req.MaskFileID, err)
		}
		maskPath = path
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
		return nil, fmt.Errorf("create generation record: %w", err)
	}

	apiCfg := &config.ImageAPI{
		Key:      s.resolveAPIKey(provider),
		BaseURL:  s.resolveBaseURL(provider),
		Provider: provider,
		Model:    modelName,
		Size:     req.Size,
	}

	log := zerolog.Nop()
	providerInst, err := image.NewProvider(apiCfg, &log)
	if err != nil {
		s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed, err.Error())
		return nil, fmt.Errorf("create image provider: %w", err)
	}

	genOpts := &image.GenerateOptions{
		Quality:       req.Quality,
		OutputFormat:  req.OutputFormat,
		N:             req.N,
		Size:          req.Size,
		StreamCB:      streamCB,
		RefImagePaths: refPaths,
		MaskPath:      maskPath,
	}

	result, err := providerInst.Generate(ctx, req.Prompt, genOpts)
	if err != nil {
		s.updateGenerationStatus(genID, model.ImageGenerationStatusFailed, err.Error())
		return nil, fmt.Errorf("generate image: %w", err)
	}

	images := s.processResults(genID, result)

	s.db.Model(&model.ImageGeneration{}).Where("id = ?", genID).Updates(map[string]any{
		"status":         model.ImageGenerationStatusCompleted,
		"revised_prompt": result.RevisedPrompt,
	})

	return &DesignerGenerateResult{
		GenerationID:  genID,
		Images:        images,
		RevisedPrompt: result.RevisedPrompt,
	}, nil
}

func (s *DesignerService) processResults(genID string, result *image.GenerateResult) []DesignerGenerateImage {
	var images []DesignerGenerateImage

	if len(result.Images) > 0 {
		for _, img := range result.Images {
			dbResult := model.ImageGenerationResult{
				GenerationID: genID,
				ImagePath:    img.URL,
				Index:        img.Index,
			}
			s.db.Create(&dbResult)
			images = append(images, DesignerGenerateImage{
				URL:   img.URL,
				Index: img.Index,
			})
		}
		return images
	}

	if result.URL != "" {
		dbResult := model.ImageGenerationResult{
			GenerationID: genID,
			ImagePath:    result.URL,
			Index:        0,
		}
		s.db.Create(&dbResult)
		images = append(images, DesignerGenerateImage{
			URL:   result.URL,
			Index: 0,
		})
	}

	return images
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

	return generations, total, nil
}

func (s *DesignerService) GetGeneration(ctx context.Context, userID, generationID string) (*model.ImageGeneration, error) {
	var gen model.ImageGeneration
	if err := s.db.Where("id = ? AND user_id = ?", generationID, userID).
		Preload("Results").First(&gen).Error; err != nil {
		return nil, err
	}
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
