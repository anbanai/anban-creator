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
	"strings"

	"github.com/rs/zerolog"

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
	Size           string
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
	ResolveImageModelForGeneration(context.Context, string, string, string, int) (*ResolvedImageModel, error)
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
	if req.ExecutionID == "" && task.CurrentExecutionID != nil {
		req.ExecutionID = strings.TrimSpace(*task.CurrentExecutionID)
	}
	if req.ExecutionID == "" {
		return nil, errors.New("current execution identity is required for fixed-SKU image settlement")
	}
	if req.Watermark == nil && task.Watermark {
		watermark := true
		req.Watermark = &watermark
	}

	resolved, err := s.resolver.ResolveImageModelForGeneration(ctx, req.UserID, task.ImageModelKey, req.ImageType, len(req.ReferencePaths))
	if err != nil {
		return nil, fmt.Errorf("image model unavailable: %w", err)
	}
	if resolved == nil {
		return nil, errors.New("image model unavailable: resolver returned no descriptor")
	}
	var pricing *ResolvedSKUPrice
	if task.BillingPricingTier != "" {
		pricing, err = s.catalog.ResolvePriceForTier(ctx, task.BillingCatalogID, "mcp.generate_image", "image_generation."+req.ImageType, model.Tier(task.BillingPricingTier))
	} else {
		// Tasks admitted before tier pricing did not persist a pricing tier. Their
		// immutable flat catalog remains authoritative; the user tier only labels
		// the frozen-price evidence for the operation.
		pricing, err = s.catalog.ResolvePrice(ctx, req.UserID, task.BillingCatalogID, "mcp.generate_image", "image_generation."+req.ImageType)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve fixed image SKU: %w", err)
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

	referencePaths, cleanupReferences, err := s.resolveReadablePaths(ctx, req.TaskID, req.ReferencePaths)
	if err != nil {
		return nil, fmt.Errorf("resolve reference images: %w", err)
	}
	if cleanupReferences != nil {
		defer cleanupReferences()
	}

	result, err := s.generator.GenerateImage(ctx, req.UserID, req.ProjectID, req.Prompt, req.ImageType, req.OutputPath, "", referencePaths, req.TaskID, req.Size, resolved, req.Watermark)
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

func taskImageOperationIdentity(req GenerateTaskImageRequest) (string, string, error) {
	canonical, err := json.Marshal(struct {
		ExecutionID    string
		TaskID         string
		ProjectID      string
		Prompt         string
		ImageType      string
		OutputPath     string
		Size           string
		ReferencePaths []string
		Watermark      *bool
	}{
		ExecutionID: req.ExecutionID, TaskID: req.TaskID, ProjectID: req.ProjectID,
		Prompt: req.Prompt, ImageType: req.ImageType, OutputPath: req.OutputPath, Size: req.Size,
		ReferencePaths: append([]string(nil), req.ReferencePaths...), Watermark: req.Watermark,
	})
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(canonical)
	fingerprint := hex.EncodeToString(sum[:])
	return "image:" + fingerprint[:32], fingerprint, nil
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

func (s *TaskImageService) resolveReadablePaths(ctx context.Context, taskID string, paths []string) ([]string, func(), error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}
	result := make([]string, 0, len(paths))
	cleanups := make([]func(), 0)
	for _, path := range paths {
		resolved, cleanup, err := s.resolveReadablePath(ctx, taskID, path)
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

func (s *TaskImageService) resolveReadablePath(ctx context.Context, taskID, path string) (string, func(), error) {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return path, nil, nil
	}
	if s.tasks.Repository() == nil || s.tasks.Storage() == nil {
		return path, nil, nil
	}
	cleanPath, err := CleanTaskFileRelativePath(path)
	if err != nil {
		return path, nil, nil
	}
	taskFile, err := s.tasks.Repository().TaskFiles().FindExisting(ctx, taskID, cleanPath)
	if err != nil {
		return "", nil, fmt.Errorf("find task file %s: %w", cleanPath, err)
	}
	if taskFile == nil || strings.TrimSpace(taskFile.OSSKey) == "" {
		return path, nil, nil
	}
	data, err := s.tasks.Storage().Read(ctx, taskFile.OSSKey)
	if err != nil {
		return "", nil, fmt.Errorf("read task file %s: %w", cleanPath, err)
	}
	return writeTaskImageTemp(data, cleanPath)
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
