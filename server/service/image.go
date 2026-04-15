package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"github.com/rs/zerolog"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/image"
	"github.com/royalrick/anbanwriter/server/agent"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/storage"
)

// ImageResult is the response for single image generation.
type ImageResult struct {
	FilePath string `json:"file_path"`
	Size     string `json:"size"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

// BatchImageResultItem is a single item in batch image generation results.
type BatchImageResultItem struct {
	FilePath string `json:"file_path"`
	Size     string `json:"size"`
	Index    int    `json:"index"`
}

// BatchImageResult is the response for batch image generation.
type BatchImageResult struct {
	Images []BatchImageResultItem `json:"images"`
	Count  int                    `json:"count"`
}

// UploadImageResult is the response for image upload.
type UploadImageResult struct {
	URL       string `json:"url"`
	MediaID   string `json:"media_id,omitempty"`
	WechatURL string `json:"wechat_url,omitempty"`
}

// ImageService handles image generation, upload, and compression
// for server-side MCP tool use. It wraps the app/image package.
type ImageService struct {
	imageCfg  *srvconfig.ImageAPIConfig
	storage   storage.Provider
	repo      repository.Repository
	creditSvc *CreditService
	logger    *zerolog.Logger
}

// NewImageService creates a new ImageService.
func NewImageService(
	imageCfg *srvconfig.ImageAPIConfig,
	store storage.Provider,
	repo repository.Repository,
	creditSvc *CreditService,
	logger *zerolog.Logger,
) *ImageService {
	return &ImageService{
		imageCfg:  imageCfg,
		storage:   store,
		repo:      repo,
		creditSvc: creditSvc,
		logger:    logger,
	}
}

// resolveImageAPI returns the appropriate ImageAPI config based on image_type.
// For "cover" images, uses the Cover config; for all other types, uses Content.
func (s *ImageService) resolveImageAPI(imageType string) *appconfig.ImageAPI {
	if imageType == "cover" && s.imageCfg != nil && s.imageCfg.Cover != nil {
		return s.imageCfg.Cover
	}
	if s.imageCfg != nil && s.imageCfg.Content != nil {
		return s.imageCfg.Content
	}
	return nil
}

// buildProcessor creates a new image.Processor for the given channel and image type.
func (s *ImageService) buildProcessor(ch *model.Channel, imageType string) (*image.Processor, error) {
	appCfg, err := agent.BuildAppConfig(ch, s.imageCfg)
	if err != nil {
		return nil, fmt.Errorf("build app config: %w", err)
	}

	apiCfg := s.resolveImageAPI(imageType)
	if apiCfg == nil {
		return nil, fmt.Errorf("no image API config available for type %q", imageType)
	}

	// Convert zerolog to zap logger for the processor.
	zapLog, err := zap.NewProduction()
	if err != nil {
		zapLog = zap.NewNop()
	}

	return image.NewProcessor(appCfg, apiCfg, zapLog), nil
}

// GenerateImage generates a single image using the channel's image provider.
// If outputPath is empty, the image is saved to a system temp directory.
func (s *ImageService) GenerateImage(
	ctx context.Context,
	userID, channelID, prompt, imageType, outputPath, refPath string,
) (*ImageResult, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}

	processor, err := s.buildProcessor(ch, imageType)
	if err != nil {
		return nil, err
	}

	if refPath != "" {
		processor.SetRefImage(refPath)
	}

	// If no output path specified, generate to a temp directory.
	if outputPath == "" {
		tmpDir, err := os.MkdirTemp("", "abw-img-")
		if err != nil {
			return nil, fmt.Errorf("create temp dir: %w", err)
		}
		outputPath = filepath.Join(tmpDir, "generated.png")
	}

	// Ensure output directory exists.
	if dir := filepath.Dir(outputPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create output dir: %w", err)
		}
	}

	result, err := processor.GenerateOnly(prompt, outputPath)
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}

	// Deduct credits for the generation.
	if _, creditErr := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeImageGen, 1); creditErr != nil {
		s.logger.Warn().Err(creditErr).
			Str("user_id", userID).
			Str("channel_id", channelID).
			Msg("failed to deduct image generation credits")
	}

	// Read image dimensions if possible.
	width, height := 0, 0
	if info, dimErr := image.GetImageInfo(result.FilePath); dimErr == nil {
		width, height = info.Width, info.Height
	}

	return &ImageResult{
		FilePath: result.FilePath,
		Size:     result.Size,
		Width:    width,
		Height:   height,
	}, nil
}

// GenerateBatch generates multiple images using the channel's image provider.
// All images are saved to outputDir. If outputDir is empty, a temp directory is created.
func (s *ImageService) GenerateBatch(
	ctx context.Context,
	userID, channelID, prompt, imageType string,
	count int,
	outputDir, refPath string,
) (*BatchImageResult, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}

	processor, err := s.buildProcessor(ch, imageType)
	if err != nil {
		return nil, err
	}

	if refPath != "" {
		processor.SetRefImage(refPath)
	}

	// If no output dir specified, create a temp directory.
	if outputDir == "" {
		tmpDir, err := os.MkdirTemp("", "abw-batch-")
		if err != nil {
			return nil, fmt.Errorf("create temp dir: %w", err)
		}
		outputDir = tmpDir
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	results, err := processor.GenerateBatchOnly(prompt, count, outputDir)
	if err != nil {
		return nil, fmt.Errorf("batch generate: %w", err)
	}

	// Deduct credits for all generated images.
	if _, creditErr := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeImageGen, count); creditErr != nil {
		s.logger.Warn().Err(creditErr).
			Str("user_id", userID).
			Str("channel_id", channelID).
			Int("count", count).
			Msg("failed to deduct batch image generation credits")
	}

	items := make([]BatchImageResultItem, 0, len(results))
	for _, r := range results {
		items = append(items, BatchImageResultItem{
			FilePath: r.FilePath,
			Size:     r.Size,
			Index:    r.Index,
		})
	}

	return &BatchImageResult{
		Images: items,
		Count:  len(items),
	}, nil
}

// UploadImage uploads a local image to the WeChat CDN via the channel's credentials.
func (s *ImageService) UploadImage(
	ctx context.Context,
	userID, channelID, filePath string,
) (*UploadImageResult, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}

	processor, err := s.buildProcessor(ch, "content")
	if err != nil {
		return nil, err
	}

	result, err := processor.UploadLocalImage(filePath)
	if err != nil {
		return nil, fmt.Errorf("upload image: %w", err)
	}

	// Deduct credits for the upload.
	if _, creditErr := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeImageUpload, 1); creditErr != nil {
		s.logger.Warn().Err(creditErr).
			Str("user_id", userID).
			Str("channel_id", channelID).
			Msg("failed to deduct image upload credits")
	}

	return &UploadImageResult{
		URL:       result.WechatURL,
		MediaID:   result.MediaID,
		WechatURL: result.WechatURL,
	}, nil
}

// CompressImage compresses a local image file.
// maxWidth is the maximum width in pixels (0 = use server default from config).
// Returns the path to the compressed file (may be a temp file) and whether
// compression was actually performed.
func (s *ImageService) CompressImage(filePath string, maxWidth int) (string, bool, error) {
	// Determine max size from config; fall back to 5MB if not configured.
	var maxSize int64 = 5 * 1024 * 1024
	if s.imageCfg != nil {
		for _, apiCfg := range []*appconfig.ImageAPI{s.imageCfg.Cover, s.imageCfg.Content} {
			if apiCfg != nil && apiCfg.MaxSizeMB > 0 {
				maxSize = apiCfg.MaxSizeBytes()
				break
			}
		}
	}

	if maxWidth <= 0 {
		// Try to get max width from config.
		if s.imageCfg != nil {
			for _, apiCfg := range []*appconfig.ImageAPI{s.imageCfg.Cover, s.imageCfg.Content} {
				if apiCfg != nil && apiCfg.MaxWidth > 0 {
					maxWidth = apiCfg.MaxWidth
					break
				}
			}
		}
		// Ultimate fallback.
		if maxWidth <= 0 {
			maxWidth = 1920
		}
	}

	zapLog, _ := zap.NewProduction()
	compressor := image.NewCompressor(zapLog, maxWidth, maxSize)

	return compressor.CompressImage(filePath)
}
