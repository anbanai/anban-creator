package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

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
	imageCfg       *srvconfig.ImageAPIConfig
	storage        storage.Provider
	repo           repository.Repository
	creditSvc      *CreditService
	modelConfigSvc *ModelConfigService
	logger         *zerolog.Logger
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

// SetModelConfigService sets the model config service for per-user AI model overrides.
func (s *ImageService) SetModelConfigService(svc *ModelConfigService) {
	s.modelConfigSvc = svc
}

// shouldSkipImageCredits returns true if the user has a fully configured image model
// (provider + api_key + model) and should not be charged credits.
func (s *ImageService) shouldSkipImageCredits(ctx context.Context, userID string) bool {
	if s.modelConfigSvc != nil {
		return s.modelConfigSvc.HasCompleteImageOverride(ctx, userID)
	}
	return false
}

// resolveToLocalFile downloads a remote URL or decodes a data URL to a temp file.
func (s *ImageService) resolveToLocalFile(rawURL string) (string, error) {
	if strings.HasPrefix(rawURL, "data:") {
		return s.dataURLToTempFile(rawURL)
	}
	return s.downloadURLToTempFile(rawURL)
}

// resolveAppImageAPI extracts the ImageAPI config from a channel-aware appCfg
// (built by BuildAppConfig) based on platform and image type.
func resolveAppImageAPI(appCfg *appconfig.Config, platform, imageType string) *appconfig.ImageAPI {
	switch platform {
	case model.ScopeArticle:
		if imageType == "cover" {
			return &appCfg.Wechat.Article.Cover.Image
		}
		return &appCfg.Wechat.Article.Content.Image
	case model.ScopeXls:
		if imageType == "cover" {
			return &appCfg.Wechat.Xls.Cover.Image
		}
		return &appCfg.Wechat.Xls.Content.Image
	case model.ScopeRednote:
		if appCfg.Rednote == nil {
			return nil
		}
		if imageType == "cover" {
			return &appCfg.Rednote.Cover.Image
		}
		return &appCfg.Rednote.Content.Image
	}
	return nil
}

// buildProcessor creates a new image.Processor for the given channel and image type.
func (s *ImageService) buildProcessor(ctx context.Context, ch *model.Channel, imageType string) (*image.Processor, error) {
	// Use per-user image config if available, otherwise fall back to server config.
	effectiveCfg := s.imageCfg
	if s.modelConfigSvc != nil {
		if userCfg := s.modelConfigSvc.GetEffectiveImageConfig(ctx, ch.UserID); userCfg != nil {
			effectiveCfg = userCfg
		}
	}

	appCfg, err := agent.BuildAppConfig(ch, effectiveCfg, "")
	if err != nil {
		return nil, fmt.Errorf("build app config: %w", err)
	}

	apiCfg := resolveAppImageAPI(appCfg, ch.Platform, imageType)
	if apiCfg == nil {
		return nil, fmt.Errorf("no image API config available for type %q", imageType)
	}

	return image.NewProcessor(appCfg, apiCfg, s.logger), nil
}

// GenerateImage generates a single image using the channel's image provider.
// Returns the download URL (remote CDN URL or data URL) for the agent to download.
// If outputPath is provided, also saves the image to that path and returns file_path.
func (s *ImageService) GenerateImage(
	ctx context.Context,
	userID, channelID, prompt, imageType, outputPath, refPath, taskID, size string,
) (*ImageResult, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}

	processor, err := s.buildProcessor(ctx, ch, imageType)
	if err != nil {
		return nil, err
	}

	if refPath != "" {
		processor.SetRefImage(refPath)
	}

	// Deduct credits before generation (skip if user has own image model config).
	if s.creditSvc != nil && !s.shouldSkipImageCredits(ctx, userID) {
		if _, err := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeImageGen, 1); err != nil {
			return nil, fmt.Errorf("deduct credits: %w", err)
		}
	}

	var rawResult *image.GenerateRawResult
	if size != "" {
		rawResult, err = processor.GenerateRawWithSize(prompt, size)
	} else {
		rawResult, err = processor.GenerateRaw(prompt)
	}
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}

	result := &ImageResult{
		DownloadURL: rawResult.URL,
		Size:        rawResult.Size,
	}

	// If outputPath provided, download and save the image there.
	if outputPath != "" {
		localPath, dlErr := s.resolveToLocalFile(rawResult.URL)
		if dlErr != nil {
			return nil, fmt.Errorf("download generated image: %w", dlErr)
		}
		defer os.RemoveAll(filepath.Dir(localPath))

		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return nil, fmt.Errorf("create output directory: %w", err)
		}

		data, err := os.ReadFile(localPath)
		if err != nil {
			return nil, fmt.Errorf("read downloaded image: %w", err)
		}
		if err := os.WriteFile(outputPath, data, 0644); err != nil {
			return nil, fmt.Errorf("save image to %s: %w", outputPath, err)
		}
		result.FilePath = outputPath
	}

	return result, nil
}

// GenerateBatch generates multiple images using the channel's image provider.
// Returns an array of download URLs (remote CDN URLs or data URLs) for the agent to download.
// If outputDir is provided, also saves each image to that directory and returns file_path for each.
func (s *ImageService) GenerateBatch(
	ctx context.Context,
	userID, channelID, prompt, imageType string,
	count int,
	outputDir, refPath, taskID, size string,
) (*BatchImageResult, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}

	processor, err := s.buildProcessor(ctx, ch, imageType)
	if err != nil {
		return nil, err
	}

	if refPath != "" {
		processor.SetRefImage(refPath)
	}

	// Deduct credits before generation (skip if user has own image model config).
	if s.creditSvc != nil && !s.shouldSkipImageCredits(ctx, userID) {
		if _, err := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeImageGen, count); err != nil {
			return nil, fmt.Errorf("deduct credits: %w", err)
		}
	}

	rawResults, err := processor.GenerateBatchRaw(prompt, count, size)
	if err != nil {
		return nil, fmt.Errorf("batch generate: %w", err)
	}

	items := make([]BatchImageResultItem, 0, len(rawResults))
	for _, r := range rawResults {
		item := BatchImageResultItem{
			DownloadURL: r.URL,
			Size:        r.Size,
			Index:       r.Index,
		}

		if outputDir != "" {
			localPath, dlErr := s.resolveToLocalFile(r.URL)
			if dlErr != nil {
				return nil, fmt.Errorf("download batch image %d: %w", r.Index, dlErr)
			}
			defer os.RemoveAll(filepath.Dir(localPath))

			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return nil, fmt.Errorf("create output directory: %w", err)
			}

			destPath := filepath.Join(outputDir, fmt.Sprintf("image_%02d.png", r.Index))
			data, err := os.ReadFile(localPath)
			if err != nil {
				return nil, fmt.Errorf("read batch image %d: %w", r.Index, err)
			}
			if err := os.WriteFile(destPath, data, 0644); err != nil {
				return nil, fmt.Errorf("save batch image %d: %w", r.Index, err)
			}
			item.FilePath = destPath
		}

		items = append(items, item)
	}

	return &BatchImageResult{
		Images: items,
		Count:  len(items),
	}, nil
}

// UploadImage uploads a local image. For WeChat platforms (article/xls), uploads
// to WeChat CDN. For other platforms (rednote), uploads to the configured storage provider.
func (s *ImageService) UploadImage(
	ctx context.Context,
	userID, channelID, filePath string,
) (*UploadImageResult, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}

	// Non-WeChat platforms: upload to storage provider (local/OSS).
	if ch.Platform != model.PlatformArticle && ch.Platform != model.PlatformXLS {
		return s.uploadToStorage(ctx, filePath)
	}

	// WeChat platforms: upload to WeChat CDN.
	processor, err := s.buildProcessor(ctx, ch, "content")
	if err != nil {
		return nil, err
	}

	// Deduct credits before upload.
	if s.creditSvc != nil {
		if _, err := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeImageUpload, 1); err != nil {
			return nil, fmt.Errorf("deduct credits: %w", err)
		}
	}

	result, err := processor.UploadLocalImage(filePath)
	if err != nil {
		return nil, fmt.Errorf("upload image: %w", err)
	}

	return &UploadImageResult{
		URL:       result.WechatURL,
		MediaID:   result.MediaID,
		WechatURL: result.WechatURL,
	}, nil
}

// uploadToStorage uploads a file to the configured storage provider (local or OSS).
func (s *ImageService) uploadToStorage(ctx context.Context, filePath string) (*UploadImageResult, error) {
	if s.storage == nil {
		return nil, fmt.Errorf("storage provider not available")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(filePath))
	key := fmt.Sprintf("uploads/images/%s%s", uuid.New().String(), ext)

	result, err := s.storage.Upload(ctx, key, file, "image/jpeg")
	if err != nil {
		return nil, fmt.Errorf("upload to storage: %w", err)
	}

	return &UploadImageResult{
		URL: result.URL,
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

	compressor := image.NewCompressor(s.logger, maxWidth, maxSize)

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

	if strings.EqualFold(upload, "true") || strings.EqualFold(upload, "wechat") {
		// WeChat platforms: download and upload to WeChat CDN.
		if ch.Platform == model.PlatformArticle || ch.Platform == model.PlatformXLS {
			processor, err := s.buildProcessor(ctx, ch, "content")
			if err != nil {
				return nil, err
			}

			// Deduct credits before download+upload.
			if s.creditSvc != nil {
				if _, err := s.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeImageUpload, 1); err != nil {
					return nil, fmt.Errorf("deduct credits: %w", err)
				}
			}

			result, err := processor.DownloadAndUpload(url)
			if err != nil {
				return nil, fmt.Errorf("download and upload: %w", err)
			}

			return &DownloadImageResult{
				URL:       url,
				MediaID:   result.MediaID,
				WechatURL: result.WechatURL,
			}, nil
		}

		// Non-WeChat platforms: download and upload to storage provider.
		return s.downloadAndUploadToStorage(ctx, url)
	}

	// Download only (all platforms).
	processor, err := s.buildProcessor(ctx, ch, "content")
	if err != nil {
		return nil, err
	}

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

// downloadAndUploadToStorage downloads an image from URL and uploads it to the storage provider.
func (s *ImageService) downloadAndUploadToStorage(ctx context.Context, imageURL string) (*DownloadImageResult, error) {
	if s.storage == nil {
		return nil, fmt.Errorf("storage provider not available")
	}

	tmpDir, err := os.MkdirTemp("", "abw-dl-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	outputPath := filepath.Join(tmpDir, "downloaded.png")

	// Download using http.Get directly (no WeChat dependency needed).
	resp, err := http.Get(imageURL)
	if err != nil {
		return nil, fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download image: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return nil, fmt.Errorf("write temp file: %w", err)
	}
	f.Close()

	uploadResult, err := s.uploadToStorage(ctx, outputPath)
	if err != nil {
		return nil, err
	}

	return &DownloadImageResult{
		URL: uploadResult.URL,
	}, nil
}

// BatchGenerateFromMarkdown extracts AI image placeholders from Markdown content,
// generates all images, and optionally uploads them.
func (s *ImageService) BatchGenerateFromMarkdown(
	ctx context.Context,
	userID, channelID, markdown, imageType, stylePrompt string,
	upload bool,
	taskID string,
) (*BatchMarkdownResult, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required for batch markdown image generation")
	}
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}

	processor, err := s.buildProcessor(ctx, ch, imageType)
	if err != nil {
		return nil, err
	}

	// Extract AI image references from markdown.
	nopLog := zerolog.Nop()
	conv := converter.NewConverter(&nopLog)
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

	results := make([]BatchMarkdownImageItem, 0, len(aiRefs))
	for _, ref := range aiRefs {
		prompt := ref.AIPrompt
		if stylePrompt != "" {
			prompt = stylePrompt + "\n\n" + prompt
		}

		rawResult, err := processor.GenerateRaw(prompt)
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
			Index:       ref.Index,
			Prompt:      prompt,
			DownloadURL: rawResult.URL,
		}

		// Optionally upload the generated image to WeChat CDN.
		if upload {
			uploadResult, uploadErr := s.uploadFromRawURL(ctx, processor, rawResult.URL)
			if uploadErr != nil {
				s.logger.Warn().Err(uploadErr).
					Int("index", ref.Index).
					Str("url", rawResult.URL).
					Msg("failed to upload generated image")
			} else {
				item.URL = uploadResult.WechatURL
			}
		}

		results = append(results, item)
	}

	return &BatchMarkdownResult{
		Count:   len(results),
		Results: results,
	}, nil
}

// uploadFromRawURL downloads an image from a remote URL or data URL to a temp file,
// then uploads it to WeChat CDN via the processor.
func (s *ImageService) uploadFromRawURL(ctx context.Context, processor *image.Processor, rawURL string) (*image.UploadResult, error) {
	var localPath string
	var err error

	if strings.HasPrefix(rawURL, "data:") {
		localPath, err = s.dataURLToTempFile(rawURL)
	} else {
		localPath, err = s.downloadURLToTempFile(rawURL)
	}
	if err != nil {
		return nil, fmt.Errorf("prepare image for upload: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(localPath))

	return processor.UploadLocalImage(localPath)
}

// dataURLToTempFile decodes a data URL and writes the content to a temp file.
func (s *ImageService) dataURLToTempFile(dataURL string) (string, error) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", fmt.Errorf("invalid data URL")
	}
	parts := strings.SplitN(dataURL[5:], ",", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid data URL format")
	}
	if !strings.HasSuffix(parts[0], ";base64") {
		return "", fmt.Errorf("only base64 data URLs are supported")
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	tmpDir, err := os.MkdirTemp("", "abw-upload-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	ext := ".png"
	if strings.Contains(parts[0], "jpeg") || strings.Contains(parts[0], "jpg") {
		ext = ".jpg"
	} else if strings.Contains(parts[0], "webp") {
		ext = ".webp"
	} else if strings.Contains(parts[0], "gif") {
		ext = ".gif"
	}
	localPath := filepath.Join(tmpDir, "upload"+ext)
	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}
	return localPath, nil
}

// downloadURLToTempFile downloads a remote URL to a temp file.
func (s *ImageService) downloadURLToTempFile(url string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "abw-upload-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	localPath := filepath.Join(tmpDir, "downloaded.png")

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download image: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	return localPath, nil
}

