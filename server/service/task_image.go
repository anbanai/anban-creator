package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	appimage "github.com/anbanai/anban-creator/server/app/image"
	"github.com/anbanai/anban-creator/server/model"
)

const taskImageSettlementScope = "mcp-image-settlement"

type GenerateTaskImageRequest struct {
	UserID         string
	ExecutionID    string
	TaskID         string
	ProjectID      string
	Prompt         string
	ImageType      string
	OutputPath     string
	AspectRatio    string
	ReferencePaths []string
	Watermark      *bool
}

type TaskImageAsset struct {
	TaskFileID  string `json:"task_file_id"`
	FilePath    string `json:"file_path"`
	DownloadURL string `json:"download_url"`
	MimeType    string `json:"mime_type"`
	FileSize    int64  `json:"file_size"`
	ContentHash string `json:"content_hash"`
}

type TaskImageModelResolver interface {
	ResolveFrozenImageModelForGeneration(context.Context, string, model.ImageCapabilitySnapshot, string, int) (*ResolvedImageModel, error)
}

type TaskImageGenerator interface {
	GenerateImage(context.Context, string, string, string, string, string, string, []string, string, string, *ResolvedImageModel, *bool) (*ImageResult, error)
}

type TaskImageService struct {
	tasks     *TaskService
	resolver  TaskImageModelResolver
	generator TaskImageGenerator
	catalog   *BillingCatalogService
	logger    *zerolog.Logger
}

type ImageRatioNotAllowedError struct {
	RequestedRatio     string   `json:"requested_ratio"`
	AllowedImageRatios []string `json:"allowed_image_ratios"`
	TaskImageRatio     string   `json:"task_image_ratio"`
}

type ImageCapabilityBillingSKUError struct {
	BillingSKU        string `json:"billing_sku"`
	Operation         string `json:"operation"`
	Route             string `json:"route"`
	ExpectedOperation string `json:"expected_operation"`
	ExpectedRoute     string `json:"expected_route"`
}

func (e *ImageCapabilityBillingSKUError) Error() string {
	return fmt.Sprintf("image capability billing_sku %q resolves to %s/%s, want %s/%s", e.BillingSKU, e.Operation, e.Route, e.ExpectedOperation, e.ExpectedRoute)
}

func (e *ImageRatioNotAllowedError) Error() string {
	return fmt.Sprintf("requested aspect_ratio %q is not allowed for this task", e.RequestedRatio)
}

type ImageRatioMismatchError struct {
	RequestedRatio string `json:"requested_ratio"`
	ActualWidth    int    `json:"actual_width"`
	ActualHeight   int    `json:"actual_height"`
	CapabilityKey  string `json:"capability_key"`
}

func (e *ImageRatioMismatchError) Error() string {
	return fmt.Sprintf("image_ratio_mismatch: requested %s, got %dx%d", e.RequestedRatio, e.ActualWidth, e.ActualHeight)
}

func NewTaskImageService(tasks *TaskService, resolver TaskImageModelResolver, generator TaskImageGenerator, catalog *BillingCatalogService, logger *zerolog.Logger) *TaskImageService {
	return &TaskImageService{tasks: tasks, resolver: resolver, generator: generator, catalog: catalog, logger: logger}
}

func (s *TaskImageService) Generate(ctx context.Context, req GenerateTaskImageRequest) (*TaskImageAsset, error) {
	if s == nil || s.tasks == nil || s.resolver == nil || s.generator == nil || s.catalog == nil {
		return nil, errors.New("task image service is not available")
	}
	if strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New("task_id and project_id are required")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	req.AspectRatio = strings.TrimSpace(req.AspectRatio)
	if req.AspectRatio == "" || req.AspectRatio == model.ImageRatioAuto || strings.Contains(strings.ToLower(req.AspectRatio), "x") {
		return nil, errors.New("aspect_ratio must be a concrete business ratio, not auto or a pixel size")
	}
	outputPath, err := CleanTaskFileRelativePath(req.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("output_path: %w", err)
	}
	req.OutputPath = filepath.ToSlash(outputPath)
	if req.ImageType == "" {
		req.ImageType = "content"
	}
	req.ReferencePaths = append([]string(nil), req.ReferencePaths...)

	task, err := s.tasks.GetByID(ctx, req.TaskID)
	if err != nil || task == nil || (req.UserID != "" && task.UserID != req.UserID) {
		return nil, errors.New("task not found")
	}
	if task.ProjectID != req.ProjectID {
		return nil, errors.New("task does not belong to the requested project")
	}
	platform := task.Type
	if snapshotPlatform := strings.TrimSpace(task.ProjectSnapshot.Data().Platform); snapshotPlatform != "" {
		platform = snapshotPlatform
	}
	allowedRatios := model.SupportedImageRatios(platform)
	if !model.IsBusinessImageRatioAllowed(platform, req.AspectRatio) ||
		(strings.TrimSpace(task.ImageRatio) != "" && task.ImageRatio != model.ImageRatioAuto && task.ImageRatio != req.AspectRatio) {
		return nil, &ImageRatioNotAllowedError{
			RequestedRatio: req.AspectRatio, AllowedImageRatios: allowedRatios, TaskImageRatio: task.ImageRatio,
		}
	}
	if req.ExecutionID == "" {
		return nil, errors.New("current execution identity is required for fixed-SKU image settlement")
	}
	if req.Watermark == nil && task.Watermark {
		watermark := true
		req.Watermark = &watermark
	}
	if err := s.tasks.ValidateAgentExecutionAccess(ctx, req.UserID, req.ProjectID, req.TaskID, req.ExecutionID); err != nil {
		return nil, fmt.Errorf("authorize image generation execution: %w", err)
	}
	if strings.TrimSpace(task.ImageCapabilityKey) == "" {
		return nil, ErrTaskImageCapabilityMissing
	}

	snapshot := task.ImageCapabilitySnapshot.Data()
	if snapshot.Key != strings.TrimSpace(task.ImageCapabilityKey) {
		return nil, ErrTaskImageCapabilityInvalid
	}
	resolved, err := s.resolver.ResolveFrozenImageModelForGeneration(ctx, req.UserID, snapshot, req.ImageType, len(req.ReferencePaths))
	if err != nil {
		return nil, fmt.Errorf("image model unavailable: %w", err)
	}
	if resolved == nil {
		return nil, errors.New("image model unavailable: resolver returned no descriptor")
	}
	var pricing *ResolvedSKUPrice
	tier := model.Tier(task.BillingPricingTier)
	if task.BillingPricingTier == "" {
		pricing, err = s.catalog.ResolvePriceBySKUIDForUser(ctx, req.UserID, task.BillingCatalogID, snapshot.BillingSKU)
	} else {
		pricing, err = s.catalog.ResolvePriceBySKUID(ctx, task.BillingCatalogID, snapshot.BillingSKU, tier)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve fixed image SKU: %w", err)
	}
	expectedOperation := "image.generate"
	expectedRoute := "image_generation.capabilities." + resolved.Key
	if pricing == nil || pricing.SKU == nil || pricing.SKU.Operation != expectedOperation || pricing.SKU.Route != expectedRoute {
		mismatch := &ImageCapabilityBillingSKUError{
			BillingSKU: resolved.BillingSKU, ExpectedOperation: expectedOperation, ExpectedRoute: expectedRoute,
		}
		if pricing != nil && pricing.SKU != nil {
			mismatch.BillingSKU = pricing.SKU.SKUID
			mismatch.Operation = pricing.SKU.Operation
			mismatch.Route = pricing.SKU.Route
		}
		return nil, mismatch
	}
	operationID, fingerprint, err := taskImageOperationIdentity(req)
	if err != nil {
		return nil, fmt.Errorf("derive image operation identity: %w", err)
	}

	if file, snapshot, replayErr := s.tasks.FindExecutionTaskFileSettlement(ctx, req.TaskID, req.ExecutionID, operationID, fingerprint, "image", taskImageSettlementScope); replayErr != nil {
		return nil, fmt.Errorf("replay fixed image operation: %w", replayErr)
	} else if len(snapshot) > 0 {
		return s.replayAsset(ctx, file, snapshot)
	}

	referencePaths, cleanupReferences, err := s.resolveReadablePaths(ctx, task, req, req.ReferencePaths)
	if err != nil {
		return nil, fmt.Errorf("resolve reference images: %w", err)
	}
	if cleanupReferences != nil {
		defer cleanupReferences()
	}

	req.Prompt = appendStrictImageRatioRequirement(req.Prompt, req.AspectRatio)
	result, err := s.generator.GenerateImage(ctx, req.UserID, req.ProjectID, req.Prompt, req.ImageType, req.OutputPath, "", referencePaths, req.TaskID, req.AspectRatio, resolved, req.Watermark)
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}
	if result == nil {
		return nil, errors.New("generate image: image generator returned no result")
	}
	defer result.CleanupLocalFile()
	if result.FilePath == "" {
		result.FilePath = req.OutputPath
	}
	actualWidth, actualHeight, err := appimage.GetImageDimensions(result.SavedFilePath())
	if err != nil {
		return nil, fmt.Errorf("read generated image dimensions: %w", err)
	}
	result.Width, result.Height = actualWidth, actualHeight
	if !imageDimensionsMatchRatio(actualWidth, actualHeight, req.AspectRatio) {
		actualWidth, actualHeight, err = normalizeGeneratedTaskImageRatio(result, req.AspectRatio)
	}
	if err != nil || !imageDimensionsMatchRatio(actualWidth, actualHeight, req.AspectRatio) {
		discardGeneratedTaskImage(result)
		if err != nil {
			return nil, fmt.Errorf("normalize generated image to %s: %w", req.AspectRatio, err)
		}
		return nil, &ImageRatioMismatchError{
			RequestedRatio: req.AspectRatio, ActualWidth: actualWidth, ActualHeight: actualHeight, CapabilityKey: resolved.Key,
		}
	}
	result.Width, result.Height = actualWidth, actualHeight

	asset, err := s.persist(ctx, req, operationID, fingerprint, pricing, result)
	if err != nil {
		return nil, err
	}
	if s.logger != nil {
		s.logger.Info().
			Str("task_id", req.TaskID).
			Str("execution_id", req.ExecutionID).
			Str("operation_id", operationID).
			Str("provider", resolved.Provider).
			Str("model", resolved.Model).
			Str("file_path", asset.FilePath).
			Msg("task image generated and registered")
	}
	return asset, nil
}

func discardGeneratedTaskImage(result *ImageResult) {
	if result == nil {
		return
	}
	if path := strings.TrimSpace(result.SavedFilePath()); path != "" {
		_ = os.Remove(path)
	}
	result.CleanupLocalFile()
}

func normalizeGeneratedTaskImageRatio(result *ImageResult, ratio string) (int, int, error) {
	if result == nil || strings.TrimSpace(result.SavedFilePath()) == "" {
		return 0, 0, errors.New("saved image path is required")
	}
	width, height, ok := largestExactRatioDimensions(result.Width, result.Height, ratio)
	if !ok {
		return result.Width, result.Height, nil
	}
	if err := appimage.CropToSizeWithAnchor(result.SavedFilePath(), result.SavedFilePath(), width, height, "center"); err != nil {
		return result.Width, result.Height, err
	}
	actualWidth, actualHeight, err := appimage.GetImageDimensions(result.SavedFilePath())
	if err != nil {
		return 0, 0, err
	}
	return actualWidth, actualHeight, nil
}

func largestExactRatioDimensions(width, height int, ratio string) (int, int, bool) {
	parts := strings.Split(ratio, ":")
	if width <= 0 || height <= 0 || len(parts) != 2 {
		return 0, 0, false
	}
	ratioWidth, errWidth := strconv.Atoi(parts[0])
	ratioHeight, errHeight := strconv.Atoi(parts[1])
	if errWidth != nil || errHeight != nil || ratioWidth <= 0 || ratioHeight <= 0 {
		return 0, 0, false
	}
	scale := min(width/ratioWidth, height/ratioHeight)
	if scale <= 0 {
		return 0, 0, false
	}
	return ratioWidth * scale, ratioHeight * scale, true
}

func taskImageOperationIdentity(req GenerateTaskImageRequest) (string, string, error) {
	canonical, err := json.Marshal(struct {
		ExecutionID    string
		TaskID         string
		ProjectID      string
		Prompt         string
		ImageType      string
		OutputPath     string
		AspectRatio    string
		ReferencePaths []string
		Watermark      *bool
	}{
		ExecutionID: req.ExecutionID, TaskID: req.TaskID, ProjectID: req.ProjectID,
		Prompt: req.Prompt, ImageType: req.ImageType, OutputPath: req.OutputPath, AspectRatio: req.AspectRatio,
		ReferencePaths: append([]string(nil), req.ReferencePaths...), Watermark: req.Watermark,
	})
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(canonical)
	fingerprint := hex.EncodeToString(sum[:])
	return "image:" + fingerprint[:32], fingerprint, nil
}

func appendStrictImageRatioRequirement(prompt, ratio string) string {
	forbidden := make([]string, 0, 5)
	for _, fallbackRatio := range []string{"2:3", "9:16"} {
		if fallbackRatio != ratio {
			forbidden = append(forbidden, fallbackRatio)
		}
	}
	forbidden = append(forbidden, "近似比例", "留白边框", "内嵌画布")
	forbiddenText := strings.Join(forbidden[:len(forbidden)-1], "、") + "或" + forbidden[len(forbidden)-1]
	return strings.TrimSpace(prompt) + "\n\n" + fmt.Sprintf(`输出规格：最终图片画布宽高比必须严格为 %s。
该比例是交付要求，不是构图建议。
不得输出 %s。`, ratio, forbiddenText)
}

func imageDimensionsMatchRatio(width, height int, ratio string) bool {
	parts := strings.Split(ratio, ":")
	if width <= 0 || height <= 0 || len(parts) != 2 {
		return false
	}
	ratioWidth, errWidth := strconv.Atoi(parts[0])
	ratioHeight, errHeight := strconv.Atoi(parts[1])
	if errWidth != nil || errHeight != nil || ratioWidth <= 0 || ratioHeight <= 0 {
		return false
	}
	return int64(width)*int64(ratioHeight) == int64(height)*int64(ratioWidth)
}

type taskImageOperationSnapshot struct {
	Asset TaskImageAsset `json:"asset"`
}

func (s *TaskImageService) replayAsset(ctx context.Context, file *model.TaskFile, snapshot []byte) (*TaskImageAsset, error) {
	if file == nil {
		return nil, errors.New("replay fixed image operation: linked task file is missing")
	}
	var stored taskImageOperationSnapshot
	if err := json.Unmarshal(snapshot, &stored); err != nil {
		return nil, fmt.Errorf("decode fixed image operation replay: %w", err)
	}
	s.tasks.EnrichFilesWithURLs(ctx, []*model.TaskFile{file})
	stored.Asset.TaskFileID = file.ID
	stored.Asset.FilePath = file.FilePath
	stored.Asset.DownloadURL = firstTaskFileURL(file)
	stored.Asset.MimeType = file.MimeType
	stored.Asset.FileSize = file.FileSize
	stored.Asset.ContentHash = file.ContentHash
	if stored.Asset.DownloadURL == "" {
		return nil, errors.New("replayed task image has no fetchable URL")
	}
	return &stored.Asset, nil
}

func (s *TaskImageService) persist(ctx context.Context, req GenerateTaskImageRequest, operationID, fingerprint string, pricing *ResolvedSKUPrice, result *ImageResult) (*TaskImageAsset, error) {
	sourcePath := result.SavedFilePath()
	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("stat generated image: %w", err)
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("open generated image: %w", err)
	}
	defer file.Close()
	mimeType := result.OutputMIME
	if mimeType == "" {
		mimeType = DetectTaskFileMIME(sourcePath)
	}
	asset := TaskImageAsset{
		FilePath: req.OutputPath,
	}
	snapshot, err := json.Marshal(taskImageOperationSnapshot{Asset: asset})
	if err != nil {
		return nil, fmt.Errorf("marshal image operation snapshot: %w", err)
	}
	taskFile, err := s.tasks.UploadExecutionTaskFileWithOperationSettlementFromReader(
		ctx, req.TaskID, req.UserID, req.ExecutionID, req.OutputPath, file, mimeType, info.Size(),
		GenericTaskFileOperationSettlement{
			ResourceType: "image", IdempotencyScope: taskImageSettlementScope,
			CatalogID: pricing.SKU.CatalogID, SKUID: pricing.SKU.SKUID, PriceCredits: pricing.PriceCredits,
			OperationID: operationID, RequestFingerprint: fingerprint, ResultSnapshot: snapshot,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("register task image: %w", err)
	}
	s.tasks.EnrichFilesWithURLs(ctx, []*model.TaskFile{taskFile})
	asset.TaskFileID, asset.FilePath = taskFile.ID, taskFile.FilePath
	asset.DownloadURL = firstTaskFileURL(taskFile)
	asset.MimeType, asset.FileSize, asset.ContentHash = taskFile.MimeType, taskFile.FileSize, taskFile.ContentHash
	if asset.DownloadURL == "" {
		return nil, errors.New("registered task image has no fetchable URL")
	}
	return &asset, nil
}

func firstTaskFileURL(file *model.TaskFile) string {
	if file == nil {
		return ""
	}
	if file.URL != "" {
		return file.URL
	}
	return file.OSSURL
}

func (s *TaskImageService) resolveReadablePaths(ctx context.Context, task *model.Task, req GenerateTaskImageRequest, paths []string) ([]string, func(), error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}
	result := make([]string, 0, len(paths))
	cleanups := make([]func(), 0)
	for _, path := range paths {
		resolved, cleanup, err := s.tasks.materializeAuthorizedTaskImageReference(
			ctx, task, req.UserID, req.ProjectID, req.ExecutionID, path, taskImageReferenceGeneration, maxTaskImageReferenceBytes,
		)
		if err != nil {
			for _, cleanup := range cleanups {
				cleanup()
			}
			return nil, nil, err
		}
		result = append(result, resolved)
		if cleanup != nil {
			cleanups = append(cleanups, cleanup)
		}
	}
	if len(cleanups) == 0 {
		return result, nil, nil
	}
	return result, func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}, nil
}

func writeTaskImageTemp(data []byte, logicalPath string) (string, func(), error) {
	ext := strings.ToLower(filepath.Ext(logicalPath))
	if ext == "" {
		ext = ".bin"
	}
	file, err := os.CreateTemp("", "anban-task-image-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("create task image temp: %w", err)
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("write task image temp: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("close task image temp: %w", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}
