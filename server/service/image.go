package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.uber.org/zap"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/converter"
	"github.com/royalrick/anbanwriter/app/image"
	"github.com/royalrick/anbanwriter/server/agent"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/storage"
)

// ImageResult is the response for single image generation.
type ImageResult struct {
	FilePath    string `json:"file_path"`
	DownloadURL string `json:"download_url,omitempty"`
	Size        string `json:"size"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// BatchImageResultItem is a single item in batch image generation results.
type BatchImageResultItem struct {
	FilePath    string `json:"file_path"`
	DownloadURL string `json:"download_url,omitempty"`
	Size        string `json:"size"`
	Index       int    `json:"index"`
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

// DownloadImageResult is the response for image download (optionally with upload).
type DownloadImageResult struct {
	FilePath  string `json:"file_path,omitempty"`
	URL       string `json:"url,omitempty"`
	MediaID   string `json:"media_id,omitempty"`
	WechatURL string `json:"wechat_url,omitempty"`
}

// BatchMarkdownImageItem is a single item in batch markdown image generation results.
type BatchMarkdownImageItem struct {
	Index       int    `json:"index"`
	Prompt      string `json:"prompt"`
	FilePath    string `json:"file_path,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	URL         string `json:"url,omitempty"`
}

// BatchMarkdownResult is the response for batch image generation from markdown.
type BatchMarkdownResult struct {
	Count   int                      `json:"count"`
	Results []BatchMarkdownImageItem `json:"results"`
}

// ImageService handles image generation, upload, and compression
// for server-side MCP tool use. It wraps the app/image package.
type ImageService struct {
	imageCfg     *srvconfig.ImageAPIConfig
	storage      storage.Provider
	repo         repository.Repository
	creditSvc    *CreditService
	logger       *zerolog.Logger
	agentBaseURL string
}

// NewImageService creates a new ImageService.
func NewImageService(
	imageCfg *srvconfig.ImageAPIConfig,
	store storage.Provider,
	repo repository.Repository,
	creditSvc *CreditService,
	agentBaseURL string,
	logger *zerolog.Logger,
) *ImageService {
	return &ImageService{
		imageCfg:     imageCfg,
		storage:      store,
		repo:         repo,
		creditSvc:    creditSvc,
		logger:       logger,
		agentBaseURL: agentBaseURL,
	}
}

const agentTempStoragePrefix = "agent-temp"

// AgentTempStorageKey returns the storage key used for a temporary agent-downloadable file.
func AgentTempStorageKey(id, fileName string) string {
	return filepath.ToSlash(filepath.Join(agentTempStoragePrefix, id, filepath.Base(fileName)))
}

// AgentTempDownloadPath returns the HTTP path for downloading a temporary agent file.
func AgentTempDownloadPath(id, fileName string) string {
	return "/api/v1/agent/temp/" + id + "/" + filepath.Base(fileName)
}

func buildAgentTempDownloadURL(baseURL, id, fileName string) string {
	path := AgentTempDownloadPath(id, fileName)
	if baseURL == "" {
		return path
	}
	return strings.TrimRight(baseURL, "/") + path
}

func (s *ImageService) publishAgentTempFile(ctx context.Context, filePath string) (string, error) {
	if s.storage == nil {
		return "", fmt.Errorf("storage provider is required for agent temp downloads")
	}

	fileName := filepath.Base(filePath)
	tempID := uuid.NewString()
	key := AgentTempStorageKey(tempID, fileName)
	if _, err := s.storage.UploadFile(ctx, key, filePath, DetectTaskFileMIME(filePath)); err != nil {
		return "", fmt.Errorf("upload temp file: %w", err)
	}
	return buildAgentTempDownloadURL(s.agentBaseURL, tempID, fileName), nil
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

	downloadURL, err := s.publishAgentTempFile(ctx, result.FilePath)
	if err != nil {
		return nil, err
	}

	return &ImageResult{
		FilePath:    result.FilePath,
		DownloadURL: downloadURL,
		Size:        result.Size,
		Width:       width,
		Height:      height,
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
		downloadURL, err := s.publishAgentTempFile(ctx, r.FilePath)
		if err != nil {
			return nil, err
		}
		items = append(items, BatchImageResultItem{
			FilePath:    r.FilePath,
			DownloadURL: downloadURL,
			Size:        r.Size,
			Index:       r.Index,
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
	// Prefer Content config as standalone compression is more commonly used for content images.
	var maxSize int64 = 5 * 1024 * 1024
	if s.imageCfg != nil {
		if cfg := s.imageCfg.Content; cfg != nil && cfg.MaxSizeMB > 0 {
			maxSize = cfg.MaxSizeBytes()
		} else if cfg := s.imageCfg.Cover; cfg != nil && cfg.MaxSizeMB > 0 {
			maxSize = cfg.MaxSizeBytes()
		}
	}

	if maxWidth <= 0 {
		// Try to get max width from config.
		if s.imageCfg != nil {
			if cfg := s.imageCfg.Content; cfg != nil && cfg.MaxWidth > 0 {
				maxWidth = cfg.MaxWidth
			} else if cfg := s.imageCfg.Cover; cfg != nil && cfg.MaxWidth > 0 {
				maxWidth = cfg.MaxWidth
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

// DownloadImage downloads an image from a URL and optionally uploads it to WeChat CDN.
// If upload is "true" or "wechat", the image is uploaded after download.
// Otherwise the image is saved to a temp directory.
func (s *ImageService) DownloadImage(
	ctx context.Context,
	userID, channelID, url, upload string,
) (*DownloadImageResult, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}

	processor, err := s.buildProcessor(ch, "content")
	if err != nil {
		return nil, err
	}

	if strings.EqualFold(upload, "true") || strings.EqualFold(upload, "wechat") {
		result, err := processor.DownloadAndUpload(url)
		if err != nil {
			return nil, fmt.Errorf("download and upload: %w", err)
		}

		// Deduct credits for the upload.
		if _, creditErr := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeImageUpload, 1); creditErr != nil {
			s.logger.Warn().Err(creditErr).
				Str("user_id", userID).
				Str("channel_id", channelID).
				Msg("failed to deduct image download+upload credits")
		}

		return &DownloadImageResult{
			URL:       url,
			MediaID:   result.MediaID,
			WechatURL: result.WechatURL,
		}, nil
	}

	// Download only.
	tmpDir, err := os.MkdirTemp("", "abw-dl-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	outputPath := filepath.Join(tmpDir, "downloaded.png")

	result, err := processor.DownloadOnly(url, outputPath)
	if err != nil {
		return nil, fmt.Errorf("download image: %w", err)
	}

	return &DownloadImageResult{
		FilePath: result.FilePath,
		URL:      url,
	}, nil
}

// BatchGenerateFromMarkdown extracts AI image placeholders from Markdown content,
// generates all images, and optionally uploads them.
func (s *ImageService) BatchGenerateFromMarkdown(
	ctx context.Context,
	userID, channelID, markdown, imageType, stylePrompt string,
	upload bool,
) (*BatchMarkdownResult, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}

	processor, err := s.buildProcessor(ch, imageType)
	if err != nil {
		return nil, err
	}

	// Extract AI image references from markdown.
	conv := converter.NewConverter(zap.NewNop())
	refs := conv.ExtractImages(markdown)

	// Filter to AI-only images.
	var aiRefs []converter.ImageRef
	for _, ref := range refs {
		if ref.Type == converter.ImageTypeAI {
			aiRefs = append(aiRefs, ref)
		}
	}

	if len(aiRefs) == 0 {
		return &BatchMarkdownResult{
			Count:   0,
			Results: []BatchMarkdownImageItem{},
		}, nil
	}

	// Create temp directory for generated images.
	tmpDir, err := os.MkdirTemp("", "abw-md-img-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	results := make([]BatchMarkdownImageItem, 0, len(aiRefs))
	for _, ref := range aiRefs {
		prompt := ref.AIPrompt
		if stylePrompt != "" {
			prompt = stylePrompt + "\n\n" + prompt
		}

		outputPath := filepath.Join(tmpDir, fmt.Sprintf("img-%d.png", ref.Index))

		genResult, err := processor.GenerateOnly(prompt, outputPath)
		if err != nil {
			s.logger.Warn().Err(err).
				Int("index", ref.Index).
				Str("prompt", prompt).
				Msg("failed to generate image from markdown, skipping")
			results = append(results, BatchMarkdownImageItem{
				Index:  ref.Index,
				Prompt: prompt,
			})
			continue
		}

		item := BatchMarkdownImageItem{
			Index:    ref.Index,
			Prompt:   prompt,
			FilePath: genResult.FilePath,
		}

		if downloadURL, err := s.publishAgentTempFile(ctx, genResult.FilePath); err == nil {
			item.DownloadURL = downloadURL
		} else {
			s.logger.Warn().Err(err).
				Int("index", ref.Index).
				Str("file_path", genResult.FilePath).
				Msg("failed to publish generated markdown image for agent download")
		}

		// Optionally upload the generated image.
		if upload {
			uploadResult, uploadErr := processor.UploadLocalImage(genResult.FilePath)
			if uploadErr != nil {
				s.logger.Warn().Err(uploadErr).
					Int("index", ref.Index).
					Str("file_path", genResult.FilePath).
					Msg("failed to upload generated image")
			} else {
				item.URL = uploadResult.WechatURL
			}
		}

		results = append(results, item)
	}

	// Deduct credits for all successfully generated images.
	successCount := 0
	for _, r := range results {
		if r.FilePath != "" {
			successCount++
		}
	}
	if successCount > 0 {
		if _, creditErr := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeImageGen, successCount); creditErr != nil {
			s.logger.Warn().Err(creditErr).
				Str("user_id", userID).
				Str("channel_id", channelID).
				Int("count", successCount).
				Msg("failed to deduct batch markdown image generation credits")
		}
	}

	return &BatchMarkdownResult{
		Count:   len(results),
		Results: results,
	}, nil
}
