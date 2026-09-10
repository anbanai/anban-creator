package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	stdimage "image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	appconfig "github.com/anbanai/anban-creator/app/config"
	"github.com/anbanai/anban-creator/app/image"
	"github.com/anbanai/anban-creator/server/agent"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

// ImageResult is the response for single image generation.
type ImageResult struct {
	ProviderRequestID    string                      `json:"-"`
	ProviderOutputWidth  int                         `json:"-"`
	ProviderOutputHeight int                         `json:"-"`
	FilePath             string                      `json:"file_path"`
	LocalFilePath        string                      `json:"-"`
	DownloadURL          string                      `json:"download_url,omitempty"`
	Size                 string                      `json:"size"`
	Width                int                         `json:"width,omitempty"`
	Height               int                         `json:"height,omitempty"`
	Prompt               string                      `json:"prompt,omitempty"`
	ImageType            string                      `json:"image_type,omitempty"`
	Provider             string                      `json:"provider,omitempty"`
	Model                string                      `json:"model,omitempty"`
	SelectionReason      string                      `json:"selection_reason,omitempty"`
	SupportsReference    bool                        `json:"supports_reference"`
	MaxReferenceImages   int                         `json:"max_reference_images"`
	RevisedPrompt        string                      `json:"revised_prompt,omitempty"`
	ResponseType         string                      `json:"response_type,omitempty"`
	ResponsePreview      string                      `json:"response_preview,omitempty"`
	OutputMIME           string                      `json:"output_mime,omitempty"`
	Usage                *image.ImageGenerationUsage `json:"usage,omitempty"`
	localCleanup         func()
}

// UploadImageResult is the response for image upload.
type UploadImageResult struct {
	URL       string `json:"url"`
	MediaID   string `json:"media_id,omitempty"`
	WechatURL string `json:"wechat_url,omitempty"`
}

// DownloadImageResult is the response for a server-local image download.
type DownloadImageResult struct {
	FilePath string `json:"file_path,omitempty"`
	URL      string `json:"url,omitempty"`
}

// ImageService handles image generation, upload, and compression
// for server-side MCP tool use. It wraps the app/image package.
type ImageService struct {
	imageCfg           *srvconfig.ImageAPIConfig
	storage            storage.Provider
	repo               repository.Repository
	capabilityResolver *ImageCapabilityResolver
	logger             *zerolog.Logger
	providerCostSvc    *ProviderCostService
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

func (s *ImageService) SetImageCapabilityResolver(resolver *ImageCapabilityResolver) {
	s.capabilityResolver = resolver
}

func (s *ImageService) SetProviderCostService(svc *ProviderCostService) {
	if s != nil {
		s.providerCostSvc = svc
	}
}

// resolveToLocalFile downloads a remote URL or decodes a data URL to a temp file.
func (s *ImageService) resolveToLocalFile(rawURL string) (string, error) {
	if strings.HasPrefix(rawURL, "data:") {
		return s.dataURLToTempFile(rawURL)
	}
	return s.downloadURLToTempFile(rawURL)
}

// buildImageResult maps the provider's raw result onto the MCP-facing
// ImageResult. DownloadURL starts as rawResult.URL, which for OpenAI/Gemini is
// a multi-MB base64 data URL (Processor.resolveRawURL inlines the provider temp
// file). When generate_image runs with a task context, the MCP handler rewrites
// DownloadURL to the image's fetchable storage URL (see
// registerGeneratedImageTaskFile in server/mcp/image_tools.go), so no inline
// base64 reaches the LLM on the task path. The ad-hoc path (no task_id) keeps
// the raw value.
func buildImageResult(rawResult *image.GenerateRawResult, imageType string) *ImageResult {
	return &ImageResult{
		ProviderRequestID:    rawResult.ProviderRequestID,
		ProviderOutputWidth:  rawResult.OutputWidth,
		ProviderOutputHeight: rawResult.OutputHeight,
		Width:                rawResult.OutputWidth,
		Height:               rawResult.OutputHeight,
		DownloadURL:          rawResult.URL,
		Size:                 rawResult.Size,
		Prompt:               rawResult.Prompt,
		ImageType:            imageType,
		Provider:             rawResult.Provider,
		Model:                rawResult.Model,
		RevisedPrompt:        rawResult.RevisedPrompt,
		ResponseType:         rawResult.ResponseType,
		ResponsePreview:      rawResult.ResponsePreview,
		OutputMIME:           rawResult.OutputMIME,
		Usage:                rawResult.Usage,
	}
}

// SavedFilePath returns the server-local path where generated bytes were saved.
// FilePath is the logical task path returned to agents; LocalFilePath is the
// physical path used when task workspaces are not mounted in the API server.
func (r *ImageResult) SavedFilePath() string {
	if r == nil {
		return ""
	}
	if r.LocalFilePath != "" {
		return r.LocalFilePath
	}
	return r.FilePath
}

// CleanupLocalFile removes a temporary generated image, when one was created.
func (r *ImageResult) CleanupLocalFile() {
	if r == nil || r.localCleanup == nil {
		return
	}
	r.localCleanup()
	r.localCleanup = nil
}

// HasTemporaryLocalFile reports whether LocalFilePath is owned by this result
// and should disappear after CleanupLocalFile.
func (r *ImageResult) HasTemporaryLocalFile() bool {
	return r != nil && r.localCleanup != nil
}

func generatedImageTaskOutputPath(outputPath string) (string, func(), error) {
	ext := strings.ToLower(filepath.Ext(outputPath))
	if ext == "" {
		ext = ".img"
	}
	dir, err := os.MkdirTemp("", "anban-generated-image-*")
	if err != nil {
		return "", nil, fmt.Errorf("create generated image temp directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	return filepath.Join(dir, "image"+ext), cleanup, nil
}

func saveGeneratedImageForOutput(outputPath string, data []byte, taskID string) (string, string, func(), error) {
	if taskID != "" && !filepath.IsAbs(outputPath) {
		return saveGeneratedImageToTaskTemp(outputPath, data)
	}
	outputMIME, err := saveGeneratedImageBytes(outputPath, data)
	if err == nil || taskID == "" || !isGeneratedImageSavePathError(err) {
		return outputPath, outputMIME, nil, err
	}
	return saveGeneratedImageToTaskTemp(outputPath, data)
}

func saveGeneratedImageToTaskTemp(outputPath string, data []byte) (string, string, func(), error) {
	savePath, cleanup, err := generatedImageTaskOutputPath(outputPath)
	if err != nil {
		return "", "", nil, err
	}
	outputMIME, err := saveGeneratedImageBytes(savePath, data)
	if err != nil {
		cleanup()
		return "", "", nil, err
	}
	return savePath, outputMIME, cleanup, nil
}

func isGeneratedImageSavePathError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "create output directory") || strings.Contains(msg, "save image to")
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

// buildProcessor creates a new image.Processor for the given project and image type.
// imageCapabilityKey routes through the server-owned capability catalog.
func (s *ImageService) buildProcessor(ctx context.Context, ch *model.Project, imageType, imageCapabilityKey string) (*image.Processor, error) {
	effectiveCfg := s.imageCfg
	if s.capabilityResolver != nil {
		resolved, source, err := s.capabilityResolver.ResolveImageConfigForTaskKey(ctx, ch.UserID, imageCapabilityKey)
		if err != nil {
			return nil, err
		}
		if resolved != nil {
			s.logger.Info().
				Str("user_id", ch.UserID).
				Str("image_type", imageType).
				Str("image_capability_key", imageCapabilityKey).
				Str("source", source).
				Msg("using task-selected image config")
			effectiveCfg = resolved
		}
	}

	// BuildAppConfig only needs the image-API/sizing slots here; the style
	// dimensions are irrelevant for provider resolution but the signature requires
	// a resolved set, so pass the project-only resolution (no task).
	appCfg, err := agent.BuildAppConfig(ch, ResolveStyle(ch, nil), effectiveCfg, "", false)
	if err != nil {
		return nil, fmt.Errorf("build app config: %w", err)
	}

	var apiCfg *appconfig.ImageAPI
	if effectiveCfg != nil {
		apiCfg = effectiveCfg.API
	}
	if apiCfg == nil {
		return nil, fmt.Errorf("no image API config available for type %q", imageType)
	}

	return image.NewProcessor(appCfg, apiCfg, s.logger), nil
}

// buildProcessorForResolved builds the generation processor from an immutable
// descriptor selected before billing. Image type is an artifact role and never
// changes the capability provider route.
func (s *ImageService) buildProcessorForResolved(
	ch *model.Project,
	imageType string,
	resolved *ResolvedImageModel,
) (*image.Processor, error) {
	if ch == nil {
		return nil, fmt.Errorf("project is required")
	}
	imageType, err := normalizeGenerationImageType(imageType)
	if err != nil {
		return nil, err
	}
	if resolved == nil || resolved.Config == nil || strings.TrimSpace(resolved.Provider) == "" || strings.TrimSpace(resolved.Model) == "" {
		return nil, fmt.Errorf("resolved image model is incomplete")
	}

	appCfg, err := agent.BuildAppConfig(ch, ResolveStyle(ch, nil), resolved.Config, "", false)
	if err != nil {
		return nil, fmt.Errorf("build app config: %w", err)
	}
	apiCfg := resolved.Config.API
	if apiCfg == nil {
		return nil, fmt.Errorf("no image API config available for type %q", imageType)
	}

	actualProvider := imageProviderKind(apiCfg.Provider)
	actualModel := strings.TrimSpace(apiCfg.Model)
	expectedProvider := imageProviderKind(resolved.Provider)
	expectedModel := strings.TrimSpace(resolved.Model)
	if actualProvider != expectedProvider || actualModel != expectedModel {
		return nil, fmt.Errorf("resolved image model does not match image type configuration")
	}

	return image.NewProcessor(appCfg, apiCfg, s.logger), nil
}

// generateProviderImage bounds one provider call. Agent and Skill workflows own
// every retry and stop/continue decision above this application capability.
func (s *ImageService) generateProviderImage(
	ctx context.Context,
	attemptTimeout time.Duration,
	gen func(context.Context) (*image.GenerateRawResult, error),
	_ string,
) (*image.GenerateRawResult, error) {
	if attemptTimeout <= 0 {
		attemptTimeout = 5 * time.Minute
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	result, err := gen(attemptCtx)
	attemptErr := attemptCtx.Err()
	cancel()
	if err == nil {
		return result, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if attemptErr != nil {
		err = fmt.Errorf("provider attempt timed out after %s: %w", attemptTimeout, attemptErr)
	}
	return nil, fmt.Errorf("generate image: %w", err)
}

func providerAttemptTimeout(resolved *ResolvedImageModel, imageType string) time.Duration {
	const fallback = 5 * time.Minute
	if resolved == nil || resolved.Config == nil {
		return fallback
	}
	apiCfg := resolved.Config.API
	if apiCfg == nil || apiCfg.TimeoutSec <= 0 {
		return fallback
	}
	return time.Duration(apiCfg.TimeoutSec) * time.Second
}

// GenerateImage generates a single image using the project's image provider.
// Returns the download URL (remote CDN URL or data URL) for the agent to download.
// If outputPath is provided, also saves the image to that path and returns file_path.
func (s *ImageService) GenerateImage(
	ctx context.Context,
	userID, projectID, prompt, imageType, outputPath, refPath string, refPaths []string, taskID string,
	aspectRatio string,
	resolved *ResolvedImageModel,
	watermark *bool,
) (*ImageResult, error) {
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}

	processor, err := s.buildProcessorForResolved(ch, imageType, resolved)
	if err != nil {
		return nil, err
	}

	if refPath != "" {
		processor.SetRefImage(refPath)
	}
	if len(refPaths) > 0 {
		processor.SetRefImages(refPaths)
	}
	processor.SetWatermark(watermark)

	// Invoke the selected provider exactly once. A stochastic retry is a new
	// workflow decision and therefore belongs to the calling Agent or Skill.
	providerRequestID := "internal:image:" + uuid.NewString()
	rawResult, err := s.generateProviderImage(ctx, providerAttemptTimeout(resolved, imageType), func(attemptCtx context.Context) (*image.GenerateRawResult, error) {
		return processor.GenerateRawWithSize(attemptCtx, prompt, aspectRatio)
	}, imageType)
	if err != nil {
		s.recordFailedImageProviderCost(ctx, taskID, providerRequestID, resolved)
		return nil, err
	}
	if strings.TrimSpace(rawResult.ProviderRequestID) == "" {
		rawResult.ProviderRequestID = providerRequestID
	}

	result := buildImageResult(rawResult, imageType)
	defer s.recordImageProviderCost(ctx, taskID, result)
	if resolved != nil {
		if imageProviderKind(result.Provider) != imageProviderKind(resolved.Provider) || strings.TrimSpace(result.Model) != strings.TrimSpace(resolved.Model) {
			return nil, fmt.Errorf("generated image provider/model does not match resolved image model")
		}
		result.Provider = resolved.Provider
		result.Model = resolved.Model
		result.SelectionReason = resolved.SelectionReason
		result.SupportsReference = resolved.SupportsReference
		result.MaxReferenceImages = resolved.MaxReferenceImages
	}

	s.logger.Info().
		Str("project_id", projectID).
		Str("provider", result.Provider).
		Str("model", result.Model).
		Str("size", result.Size).
		Str("response_type", result.ResponseType).
		Str("response_preview", result.ResponsePreview).
		Msg("image generated")

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
		savePath, outputMIME, cleanup, err := saveGeneratedImageForOutput(outputPath, data, taskID)
		if err != nil {
			return nil, err
		}
		result.FilePath = outputPath
		if savePath != outputPath {
			result.LocalFilePath = savePath
		}
		result.OutputMIME = outputMIME
		result.localCleanup = cleanup
		if err := populateImageResultDimensions(result); err != nil {
			result.CleanupLocalFile()
			return nil, fmt.Errorf("read generated image dimensions: %w", err)
		}
	}

	return result, nil
}

func (s *ImageService) recordFailedImageProviderCost(ctx context.Context, taskID, providerRequestID string, resolved *ResolvedImageModel) {
	if s == nil || s.providerCostSvc == nil || resolved == nil {
		return
	}
	if _, err := s.providerCostSvc.RecordMediaUnreconciled(ctx, RecordMediaUnreconciledRequest{
		TaskID: taskID, Provider: resolved.Provider, Model: resolved.Model, ProviderRequestID: providerRequestID,
		MediaKind: "image", ReasonCode: model.BillingExecutionCostReasonMissingProviderUsage,
	}); err != nil && s.logger != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Str("provider_request_id", providerRequestID).Msg("record failed image provider cost evidence")
	}
}

func (s *ImageService) recordImageProviderCost(ctx context.Context, taskID string, result *ImageResult) {
	if s == nil || s.providerCostSvc == nil || result == nil {
		return
	}
	var usage *OpenAIImageUsage
	if result.Usage != nil {
		usage = &OpenAIImageUsage{
			TextInput: result.Usage.TextInputTokens, TextCachedInput: result.Usage.TextCachedInputTokens,
			ImageInput: result.Usage.ImageInputTokens, ImageCachedInput: result.Usage.ImageCachedInputTokens,
			ImageOutput: result.Usage.ImageOutputTokens,
		}
	}
	_, err := s.providerCostSvc.RecordImageGenerationCost(ctx, RecordImageGenerationCostRequest{
		TaskID: taskID, Provider: result.Provider, Model: result.Model, ProviderRequestID: result.ProviderRequestID,
		Width: int64(result.ProviderOutputWidth), Height: int64(result.ProviderOutputHeight), Usage: usage,
	})
	if err != nil && s.logger != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Str("provider_request_id", result.ProviderRequestID).Msg("record image provider cost; generated output remains valid")
	}
}

func populateImageResultDimensions(result *ImageResult) error {
	if result == nil || result.SavedFilePath() == "" {
		return fmt.Errorf("saved image path is required")
	}
	w, h, err := image.GetImageDimensions(result.SavedFilePath())
	if err != nil {
		return err
	}
	result.Width = w
	result.Height = h
	if result.ProviderOutputWidth == 0 && result.ProviderOutputHeight == 0 {
		result.ProviderOutputWidth = w
		result.ProviderOutputHeight = h
	}
	return nil
}

// UploadImage uploads a local image. For WeChat platforms (article), uploads
// to WeChat CDN. For other platforms (seednote), uploads to the configured storage provider.
func (s *ImageService) UploadImage(
	ctx context.Context,
	userID, projectID, filePath string,
) (*UploadImageResult, error) {
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}

	// Non-WeChat platforms: upload to storage provider (local/OSS).
	if ch.Platform != model.PlatformArticle {
		return s.uploadToStorage(ctx, filePath)
	}

	// WeChat platforms: upload to WeChat CDN.
	processor, err := s.buildProcessor(ctx, ch, "content", "")
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
	if s.imageCfg != nil && s.imageCfg.API != nil && s.imageCfg.API.MaxSizeMB > 0 {
		maxSize = s.imageCfg.API.MaxSizeBytes()
	}

	if maxWidth <= 0 {
		// Try to get max width from config.
		if s.imageCfg != nil && s.imageCfg.API != nil && s.imageCfg.API.MaxWidth > 0 {
			maxWidth = s.imageCfg.API.MaxWidth
		}
		// Ultimate fallback.
		if maxWidth <= 0 {
			maxWidth = 1920
		}
	}

	compressor := image.NewCompressor(s.logger, maxWidth, maxSize)

	return compressor.CompressImage(filePath)
}

// DownloadImage downloads an image from a URL to a server-local temporary file.
func (s *ImageService) DownloadImage(ctx context.Context, projectID, url string) (*DownloadImageResult, error) {
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	processor, err := s.buildProcessor(ctx, ch, "content", "")
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
