package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	stdimage "image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
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
	FilePath        string `json:"file_path"`
	DownloadURL     string `json:"download_url,omitempty"`
	Size            string `json:"size"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	ImageType       string `json:"image_type,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Model           string `json:"model,omitempty"`
	RevisedPrompt   string `json:"revised_prompt,omitempty"`
	ResponseType    string `json:"response_type,omitempty"`
	ResponsePreview string `json:"response_preview,omitempty"`
	OutputMIME      string `json:"output_mime,omitempty"`
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

// ImageService handles image generation, upload, and compression
// for server-side MCP tool use. It wraps the app/image package.
type ImageService struct {
	imageCfg       *srvconfig.ImageAPIConfig
	storage        storage.Provider
	repo           repository.Repository
	modelConfigSvc *ModelConfigService
	logger         *zerolog.Logger
}

// NewImageService creates a new ImageService.
func NewImageService(
	imageCfg *srvconfig.ImageAPIConfig,
	store storage.Provider,
	repo repository.Repository,
	logger *zerolog.Logger,
) *ImageService {
	return &ImageService{
		imageCfg: imageCfg,
		storage:  store,
		repo:     repo,
		logger:   logger,
	}
}

// SetModelConfigService sets the model config service for per-user AI model overrides.
func (s *ImageService) SetModelConfigService(svc *ModelConfigService) {
	s.modelConfigSvc = svc
}

// resolveToLocalFile downloads a remote URL or decodes a data URL to a temp file.
func (s *ImageService) resolveToLocalFile(rawURL string) (string, error) {
	if strings.HasPrefix(rawURL, "data:") {
		return s.dataURLToTempFile(rawURL)
	}
	return s.downloadURLToTempFile(rawURL)
}

func buildImageResult(rawResult *image.GenerateRawResult, imageType string) *ImageResult {
	return &ImageResult{
		DownloadURL:     rawResult.URL,
		Size:            rawResult.Size,
		Prompt:          rawResult.Prompt,
		ImageType:       imageType,
		Provider:        rawResult.Provider,
		Model:           rawResult.Model,
		RevisedPrompt:   rawResult.RevisedPrompt,
		ResponseType:    rawResult.ResponseType,
		ResponsePreview: rawResult.ResponsePreview,
		OutputMIME:      rawResult.OutputMIME,
	}
}

func saveGeneratedImageBytes(outputPath string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	outputMIME := http.DetectContentType(data)
	if strings.EqualFold(filepath.Ext(outputPath), ".png") && outputMIME != "image/png" {
		img, _, err := stdimage.Decode(bytes.NewReader(data))
		if err != nil {
			return "", fmt.Errorf("decode image for png output: %w", err)
		}
		var pngBuf bytes.Buffer
		if err := png.Encode(&pngBuf, img); err != nil {
			return "", fmt.Errorf("encode png output: %w", err)
		}
		data = pngBuf.Bytes()
		outputMIME = "image/png"
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return "", fmt.Errorf("save image to %s: %w", outputPath, err)
	}
	return outputMIME, nil
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
	case model.ScopeSeednote:
		if appCfg.Seednote == nil {
			return nil
		}
		if imageType == "cover" {
			return &appCfg.Seednote.Cover.Image
		}
		return &appCfg.Seednote.Content.Image
	}
	return nil
}

// buildProcessor creates a new image.Processor for the given channel and image type.
func (s *ImageService) buildProcessor(ctx context.Context, ch *model.Channel, imageType string) (*image.Processor, error) {
	// Use per-user image config if available, otherwise fall back to server config.
	effectiveCfg := s.imageCfg
	if s.modelConfigSvc != nil {
		if userCfg := s.modelConfigSvc.GetEffectiveImageConfig(ctx, ch.UserID); userCfg != nil {
			s.logger.Info().
				Str("user_id", ch.UserID).
				Str("image_type", imageType).
				Msg("using user custom image config")
			effectiveCfg = userCfg
		}
	}

	appCfg, err := agent.BuildAppConfig(ch, effectiveCfg, "", false)
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

	var rawResult *image.GenerateRawResult
	if size != "" {
		rawResult, err = processor.GenerateRawWithSize(prompt, size)
	} else {
		rawResult, err = processor.GenerateRaw(prompt)
	}
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}

	result := buildImageResult(rawResult, imageType)

	// If outputPath provided, download and save the image there.
	if outputPath != "" {
		localPath, dlErr := s.resolveToLocalFile(rawResult.URL)
		if dlErr != nil {
			return nil, fmt.Errorf("download generated image: %w", dlErr)
		}
		defer os.RemoveAll(filepath.Dir(localPath))

		data, err := os.ReadFile(localPath)
		if err != nil {
			return nil, fmt.Errorf("read downloaded image: %w", err)
		}
		outputMIME, err := saveGeneratedImageBytes(outputPath, data)
		if err != nil {
			return nil, err
		}
		result.FilePath = outputPath
		result.OutputMIME = outputMIME
	}

	return result, nil
}

// UploadImage uploads a local image. For WeChat platforms (article), uploads
// to WeChat CDN. For other platforms (seednote), uploads to the configured storage provider.
func (s *ImageService) UploadImage(
	ctx context.Context,
	userID, channelID, filePath string,
) (*UploadImageResult, error) {
	ch, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}

	// Non-WeChat platforms: upload to storage provider (local/OSS).
	if ch.Platform != model.PlatformArticle {
		return s.uploadToStorage(ctx, filePath)
	}

	// WeChat platforms: upload to WeChat CDN.
	processor, err := s.buildProcessor(ctx, ch, "content")
	if err != nil {
		return nil, err
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

	mimeType := DetectTaskFileMIME(filePath)
	result, err := s.storage.Upload(ctx, key, file, mimeType)
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
		if ch.Platform == model.PlatformArticle {
			processor, err := s.buildProcessor(ctx, ch, "content")
			if err != nil {
				return nil, err
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
