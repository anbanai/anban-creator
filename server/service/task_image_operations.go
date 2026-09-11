package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	_ "golang.org/x/image/webp"

	appimage "github.com/anbanai/anban-creator/app/image"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
)

const (
	maxAnalyzedTaskImageBytes        = 10 << 20
	maxCompressedTaskImageInputBytes = 50 << 20
	maxTaskImageDimension            = 16384
	maxTaskImageDecodedPixels        = 40_000_000
)

var (
	ErrTaskImageOperationOwnership        = errors.New("task does not belong to user")
	ErrTaskImageOperationProjectMismatch  = errors.New("task does not belong to the requested project")
	ErrTaskImageOperationTaskNotFound     = errors.New("task not found")
	ErrTaskImageOperationTaskUnavailable  = errors.New("task service not available")
	ErrTaskImageOperationProjectNotFound  = errors.New("project not found")
	ErrTaskImageOperationProjectOwnership = errors.New("project does not belong to user")
	ErrTaskImageOperationProjectInactive  = errors.New("project is not active")
	ErrTaskImageOperationProjectRequired  = errors.New("project_id is required")
	ErrTaskImageOperationFileRequired     = errors.New("file_path is required")
	ErrTaskImageOperationPromptRequired   = errors.New("prompt is required")
	ErrTaskImageOperationSourceRequired   = errors.New("either image_url or file_path is required")
	ErrImageUnderstandingUnavailable      = errors.New("image understanding model is not configured")
)

type UploadTaskImageRequest struct {
	UserID, ExecutionID, ProjectID, TaskID, FilePath string
}

type CompressTaskImageRequest struct {
	UserID, ExecutionID, TaskID, InputPath, OutputPath string
	MaxWidth                                           int
}

type CompressTaskImageResult struct {
	TaskImageAsset
	Compressed bool `json:"compressed"`
}

type DownloadTaskImageRequest struct {
	UserID, ExecutionID, ProjectID, TaskID, URL, OutputPath string
}

type CropTaskImageRequest struct {
	UserID, ExecutionID, TaskID, InputPath, OutputPath, Anchor string
	TargetWidth, TargetHeight                                  int
}

type CropTaskImageResult struct {
	TaskImageAsset
	Width  int `json:"width"`
	Height int `json:"height"`
}

type AnalyzeTaskImageRequest struct {
	UserID, ExecutionID, ProjectID, TaskID, ImageURL, FilePath, Prompt string
}

type AnalyzeTaskImageResult struct {
	Analysis string               `json:"analysis"`
	Usage    srvconfig.TokenUsage `json:"usage"`
}

type TaskImageOperationsConfig struct {
	UnderstandingProvider string
	UnderstandingModel    string
}

type taskImageOperationsImage interface {
	UploadImage(context.Context, string, string, string) (*UploadImageResult, error)
	CompressImage(string, int) (string, bool, error)
}

type ImageUnderstandingClient interface {
	CompleteWithImageResult(context.Context, string, string, string) (*LLMResult, error)
}

type UnderstandingCostRecorder interface {
	CatalogID() string
	RecordProviderTokenUsage(context.Context, RecordProviderTokenCostRequest) (*model.BillingProviderCostEvent, error)
	RecordMediaUnreconciled(context.Context, RecordMediaUnreconciledRequest) (*model.BillingProviderCostEvent, error)
}

type TaskImageOperationsService struct {
	tasks                 *TaskService
	images                taskImageOperationsImage
	understanding         ImageUnderstandingClient
	costs                 UnderstandingCostRecorder
	config                TaskImageOperationsConfig
	logger                *zerolog.Logger
	downloadAnalysisImage func(context.Context, string, int64) ([]byte, error)
}

func NewTaskImageOperationsService(tasks *TaskService, images taskImageOperationsImage, understanding ImageUnderstandingClient, costs UnderstandingCostRecorder, cfg TaskImageOperationsConfig, logger *zerolog.Logger) *TaskImageOperationsService {
	return &TaskImageOperationsService{
		tasks: tasks, images: images, understanding: understanding, costs: costs, config: cfg, logger: logger,
		downloadAnalysisImage: downloadTaskAnalysisImage,
	}
}

func (s *TaskImageOperationsService) Upload(ctx context.Context, req UploadTaskImageRequest) (*UploadImageResult, error) {
	if req.ProjectID == "" {
		return nil, ErrTaskImageOperationProjectRequired
	}
	if req.FilePath == "" {
		return nil, ErrTaskImageOperationFileRequired
	}
	if s == nil || s.images == nil {
		return nil, errors.New("image service not available")
	}
	if err := s.validateTask(ctx, req.UserID, req.TaskID, req.ProjectID); err != nil {
		return nil, err
	}
	filePath, cleanup, err := s.resolveUploadPath(ctx, req)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	result, err := s.images.UploadImage(ctx, req.UserID, req.ProjectID, filePath)
	if err != nil {
		return nil, fmt.Errorf("upload image: %w", err)
	}
	return result, nil
}

func (s *TaskImageOperationsService) resolveUploadPath(ctx context.Context, req UploadTaskImageRequest) (string, func(), error) {
	filePath := strings.TrimSpace(req.FilePath)
	if filepath.IsAbs(filePath) {
		return "", nil, errors.New("file_path must be a task-relative image path from the current execution")
	}
	taskFile, cleanPath, err := s.findCurrentExecutionTaskImageFile(ctx, req.UserID, req.ExecutionID, req.ProjectID, req.TaskID, filePath)
	if err != nil {
		return "", nil, err
	}
	return s.tasks.materializeTaskImageFile(ctx, taskFile, cleanPath, maxTaskImageReferenceBytes)
}

func (s *TaskImageOperationsService) findCurrentExecutionTaskImageFile(ctx context.Context, userID, executionID, projectID, taskID, filePath string) (*model.TaskFile, string, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(executionID) == "" {
		return nil, "", errors.New("task_id and current execution identity are required for a task-relative file_path")
	}
	if s == nil || s.tasks == nil || s.tasks.Repository() == nil || s.tasks.Storage() == nil {
		return nil, "", errors.New("task image storage is not available")
	}
	if err := s.tasks.ValidateAgentExecutionAccess(ctx, userID, projectID, taskID, executionID); err != nil {
		return nil, "", fmt.Errorf("authorize task image execution: %w", err)
	}
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return nil, "", errors.New("file_path must not contain directory traversal")
		}
	}
	cleanPath, err := CleanTaskFileRelativePath(normalized)
	if err != nil {
		return nil, "", fmt.Errorf("file_path: %w", err)
	}
	cleanPath = filepath.ToSlash(cleanPath)
	files, err := s.tasks.Repository().TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		return nil, "", fmt.Errorf("find current execution task files: %w", err)
	}
	var taskFile *model.TaskFile
	for _, file := range files {
		if file != nil && file.TaskID == taskID && file.FilePath == cleanPath && strings.TrimSpace(file.OSSKey) != "" {
			taskFile = file
			break
		}
	}
	if taskFile == nil {
		return nil, "", errors.New("task image file not found in current execution")
	}
	return taskFile, cleanPath, nil
}

func (s *TaskImageOperationsService) Compress(ctx context.Context, req CompressTaskImageRequest) (*CompressTaskImageResult, error) {
	if strings.TrimSpace(req.InputPath) == "" || strings.TrimSpace(req.OutputPath) == "" {
		return nil, ErrTaskImageOperationFileRequired
	}
	if s == nil || s.images == nil || s.tasks == nil || s.tasks.Repository() == nil || s.tasks.Storage() == nil {
		return nil, errors.New("task image service not available")
	}
	if strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("task_id and current execution identity are required")
	}
	if req.MaxWidth < 0 || req.MaxWidth > 8192 {
		return nil, errors.New("max_width must be between 0 and 8192")
	}
	inputPath, err := cleanAuthorizedTaskImagePath(req.InputPath)
	if err != nil {
		return nil, fmt.Errorf("input_path: %w", err)
	}
	outputPath, err := cleanAuthorizedTaskImagePath(req.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("output_path: %w", err)
	}
	if inputPath == outputPath {
		return nil, errors.New("output_path must differ from input_path")
	}
	task, err := s.tasks.GetByID(ctx, req.TaskID)
	if err != nil || task == nil {
		return nil, ErrTaskImageOperationTaskNotFound
	}
	localInput, cleanup, err := s.tasks.materializeAuthorizedTaskImageReference(
		ctx, task, req.UserID, task.ProjectID, req.ExecutionID, inputPath, taskImageReferenceTransform, maxCompressedTaskImageInputBytes,
	)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if _, _, err := validateTaskRasterImageFile(localInput, inputPath, maxCompressedTaskImageInputBytes); err != nil {
		return nil, err
	}
	compressedPath, compressed, err := s.images.CompressImage(localInput, req.MaxWidth)
	if err != nil {
		return nil, fmt.Errorf("compress image: %w", err)
	}
	resultPath := localInput
	if compressed {
		if strings.TrimSpace(compressedPath) == "" {
			return nil, errors.New("compress image returned no output")
		}
		resultPath = compressedPath
		if resultPath != localInput {
			defer os.Remove(resultPath)
		}
	}
	data, err := readBoundedTaskImageFile(resultPath, maxAnalyzedTaskImageBytes)
	if err != nil {
		return nil, fmt.Errorf("read compressed image: %w", err)
	}
	asset, err := s.persistTaskImageBytes(ctx, req.UserID, req.ExecutionID, req.TaskID, outputPath, data)
	if err != nil {
		return nil, fmt.Errorf("register compressed task image: %w", err)
	}
	return &CompressTaskImageResult{TaskImageAsset: *asset, Compressed: compressed}, nil
}

func (s *TaskImageOperationsService) Download(ctx context.Context, req DownloadTaskImageRequest) (*TaskImageAsset, error) {
	if s == nil || s.tasks == nil || s.tasks.Repository() == nil || s.tasks.Storage() == nil {
		return nil, errors.New("task image service not available")
	}
	if strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("project_id, task_id, and current execution identity are required")
	}
	if strings.TrimSpace(req.URL) == "" {
		return nil, errors.New("url is required")
	}
	outputPath, err := cleanAuthorizedTaskImagePath(req.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("output_path: %w", err)
	}
	if err := s.tasks.ValidateAgentExecutionAccess(ctx, req.UserID, req.ProjectID, req.TaskID, req.ExecutionID); err != nil {
		return nil, fmt.Errorf("authorize image download execution: %w", err)
	}
	downloader := s.downloadAnalysisImage
	if downloader == nil {
		downloader = downloadTaskAnalysisImage
	}
	data, err := downloader(ctx, strings.TrimSpace(req.URL), maxAnalyzedTaskImageBytes)
	if err != nil {
		return nil, fmt.Errorf("download image: %w", err)
	}
	asset, err := s.persistTaskImageBytes(ctx, req.UserID, req.ExecutionID, req.TaskID, outputPath, data)
	if err != nil {
		return nil, fmt.Errorf("register downloaded task image: %w", err)
	}
	return asset, nil
}

func (s *TaskImageOperationsService) persistTaskImageBytes(ctx context.Context, userID, executionID, taskID, outputPath string, data []byte) (*TaskImageAsset, error) {
	mimeType, _, err := validateTaskRasterImageBytes(data, outputPath)
	if err != nil {
		return nil, err
	}
	taskFile, err := s.tasks.UploadContentAddressedExecutionTaskFileFromReader(
		ctx, taskID, userID, executionID, filepath.ToSlash(outputPath), bytes.NewReader(data), mimeType, int64(len(data)),
	)
	if err != nil {
		return nil, err
	}
	s.tasks.EnrichFilesWithURLs(ctx, []*model.TaskFile{taskFile})
	asset := &TaskImageAsset{
		TaskFileID: taskFile.ID, FilePath: taskFile.FilePath, DownloadURL: firstTaskFileURL(taskFile),
		MimeType: taskFile.MimeType, FileSize: taskFile.FileSize, ContentHash: taskFile.ContentHash,
	}
	if asset.DownloadURL == "" {
		return nil, errors.New("registered task image has no fetchable URL")
	}
	return asset, nil
}

func readBoundedTaskImageFile(filePath string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, storage.ErrObjectExceedsMaxSize
	}
	return data, nil
}

func validateTaskRasterImageFile(filePath, logicalPath string, maxBytes int64) (string, stdimage.Config, error) {
	data, err := readBoundedTaskImageFile(filePath, maxBytes)
	if err != nil {
		return "", stdimage.Config{}, err
	}
	return validateTaskRasterImageBytes(data, logicalPath)
}

func validateTaskRasterImageBytes(data []byte, logicalPath string) (string, stdimage.Config, error) {
	mimeType, config, err := validateRasterImageSafety(data, maxCompressedTaskImageInputBytes)
	if err != nil {
		return "", stdimage.Config{}, err
	}
	expected := normalizedImageContentType(contentTypeForUploadExt(strings.ToLower(filepath.Ext(logicalPath))))
	if expected == "" || expected == "application/octet-stream" || expected != mimeType {
		return "", stdimage.Config{}, fmt.Errorf("image content type %s does not match output path %q", mimeType, logicalPath)
	}
	return mimeType, config, nil
}

func validateRasterImageSafety(data []byte, maxBytes int64) (string, stdimage.Config, error) {
	if len(data) == 0 {
		return "", stdimage.Config{}, errors.New("image data is empty")
	}
	if maxBytes <= 0 || int64(len(data)) > maxBytes {
		return "", stdimage.Config{}, errors.New("image exceeds the processing size limit")
	}
	mimeType := normalizedImageContentType(http.DetectContentType(data))
	switch mimeType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return "", stdimage.Config{}, errors.New("file is not a supported raster image")
	}
	config, _, err := stdimage.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", stdimage.Config{}, fmt.Errorf("decode image dimensions: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxTaskImageDimension || config.Height > maxTaskImageDimension ||
		int64(config.Width)*int64(config.Height) > maxTaskImageDecodedPixels {
		return "", stdimage.Config{}, errors.New("image dimensions exceed safety limits")
	}
	return mimeType, config, nil
}

func (s *TaskImageOperationsService) Crop(ctx context.Context, req CropTaskImageRequest) (*CropTaskImageResult, error) {
	if s == nil || s.tasks == nil || s.tasks.Repository() == nil || s.tasks.Storage() == nil {
		return nil, errors.New("task image storage is not available")
	}
	if strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("task_id and current execution identity are required")
	}
	inputPath, err := cleanAuthorizedTaskImagePath(req.InputPath)
	if err != nil {
		return nil, fmt.Errorf("input_path: %w", err)
	}
	outputPath, err := cleanAuthorizedTaskImagePath(req.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("output_path: %w", err)
	}
	if inputPath == outputPath {
		return nil, errors.New("output_path must differ from input_path")
	}
	if req.TargetWidth <= 0 || req.TargetHeight <= 0 || req.TargetWidth > 8192 || req.TargetHeight > 8192 {
		return nil, errors.New("target_width and target_height must be between 1 and 8192")
	}
	task, err := s.tasks.GetByID(ctx, req.TaskID)
	if err != nil || task == nil {
		return nil, ErrTaskImageOperationTaskNotFound
	}
	if err := s.tasks.ValidateAgentExecutionAccess(ctx, req.UserID, task.ProjectID, req.TaskID, req.ExecutionID); err != nil {
		return nil, fmt.Errorf("authorize image crop execution: %w", err)
	}
	localInput, cleanup, err := s.tasks.materializeAuthorizedTaskImageReference(
		ctx, task, req.UserID, task.ProjectID, req.ExecutionID, filepath.ToSlash(inputPath), taskImageReferenceTransform, maxTaskImageReferenceBytes,
	)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if _, _, err := validateTaskRasterImageFile(localInput, inputPath, maxTaskImageReferenceBytes); err != nil {
		return nil, err
	}
	localOutput := filepath.Join(filepath.Dir(localInput), "cropped"+strings.ToLower(filepath.Ext(outputPath)))
	defer os.Remove(localOutput)
	if err := appimage.CropToSizeWithAnchor(localInput, localOutput, req.TargetWidth, req.TargetHeight, req.Anchor); err != nil {
		return nil, fmt.Errorf("crop image: %w", err)
	}
	data, err := readBoundedTaskImageFile(localOutput, maxAnalyzedTaskImageBytes)
	if err != nil {
		return nil, fmt.Errorf("read cropped image: %w", err)
	}
	asset, err := s.persistTaskImageBytes(ctx, req.UserID, req.ExecutionID, req.TaskID, filepath.ToSlash(outputPath), data)
	if err != nil {
		return nil, fmt.Errorf("register cropped task image: %w", err)
	}
	return &CropTaskImageResult{
		TaskImageAsset: *asset,
		Width:          req.TargetWidth, Height: req.TargetHeight,
	}, nil
}

func (s *TaskImageOperationsService) Analyze(ctx context.Context, req AnalyzeTaskImageRequest) (*AnalyzeTaskImageResult, error) {
	if req.ProjectID == "" {
		return nil, ErrTaskImageOperationProjectRequired
	}
	if req.Prompt == "" {
		return nil, ErrTaskImageOperationPromptRequired
	}
	if req.ImageURL == "" && req.FilePath == "" {
		return nil, ErrTaskImageOperationSourceRequired
	}
	if s == nil || s.understanding == nil {
		return nil, ErrImageUnderstandingUnavailable
	}
	if err := s.validateTask(ctx, req.UserID, req.TaskID, req.ProjectID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.TaskID) != "" {
		if err := s.tasks.ValidateAgentExecutionAccess(ctx, req.UserID, req.ProjectID, req.TaskID, req.ExecutionID); err != nil {
			return nil, fmt.Errorf("authorize image analysis execution: %w", err)
		}
	}
	analysisPath, cleanup, err := s.resolveAnalysisFilePath(ctx, req)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	imageSource, err := s.loadAnalysisSource(ctx, req.ImageURL, analysisPath)
	if err != nil {
		return nil, err
	}
	providerRequestID := "internal:understanding:" + model.OperationImageUnderstanding + ":" + uuid.NewString()
	result, err := s.understanding.CompleteWithImageResult(ctx, "Analyze the authorized image accurately and answer only the user's request.", req.Prompt, imageSource)
	if err != nil {
		s.recordAnalysisCost(ctx, req.TaskID, providerRequestID, nil)
		return nil, fmt.Errorf("analyze image: %w", err)
	}
	s.recordAnalysisCost(ctx, req.TaskID, providerRequestID, &result.Usage)
	return &AnalyzeTaskImageResult{Analysis: strings.TrimSpace(result.Text), Usage: result.Usage}, nil
}

func (s *TaskImageOperationsService) resolveAnalysisFilePath(ctx context.Context, req AnalyzeTaskImageRequest) (string, func(), error) {
	filePath := strings.TrimSpace(req.FilePath)
	if filePath == "" {
		return "", nil, nil
	}
	if _, err := cleanAuthorizedTaskImagePath(filePath); err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.ExecutionID) == "" {
		return "", nil, errors.New("task_id and current execution identity are required for a task-relative file_path")
	}
	task, err := s.tasks.GetByID(ctx, req.TaskID)
	if err != nil || task == nil {
		return "", nil, ErrTaskImageOperationTaskNotFound
	}
	return s.tasks.materializeAuthorizedTaskImageReference(
		ctx, task, req.UserID, req.ProjectID, req.ExecutionID, filePath, taskImageReferenceAnalysis, maxAnalyzedTaskImageBytes,
	)
}

func (s *TaskImageOperationsService) validateTask(ctx context.Context, userID, taskID, projectID string) error {
	if taskID == "" {
		if projectID == "" {
			return nil
		}
		if s == nil || s.tasks == nil || s.tasks.repo == nil {
			return ErrTaskImageOperationTaskUnavailable
		}
		project, err := s.tasks.repo.Projects().FindByID(ctx, projectID)
		if err != nil || project == nil {
			return ErrTaskImageOperationProjectNotFound
		}
		if userID == "" || project.UserID != userID {
			return ErrTaskImageOperationProjectOwnership
		}
		if project.Status != model.ProjectStatusActive {
			return ErrTaskImageOperationProjectInactive
		}
		return nil
	}
	if s == nil || s.tasks == nil {
		return ErrTaskImageOperationTaskUnavailable
	}
	task, err := s.tasks.GetByID(ctx, taskID)
	if err != nil || task == nil {
		return ErrTaskImageOperationTaskNotFound
	}
	if userID != "" && task.UserID != userID {
		return ErrTaskImageOperationOwnership
	}
	if projectID != "" && task.ProjectID != projectID {
		return ErrTaskImageOperationProjectMismatch
	}
	return nil
}

func (s *TaskImageOperationsService) loadAnalysisSource(ctx context.Context, imageURL, filePath string) (string, error) {
	if filePath != "" {
		data, err := readBoundedTaskImageFile(filePath, maxAnalyzedTaskImageBytes)
		if err != nil {
			return "", fmt.Errorf("read image file: %w", err)
		}
		mimeType, _, err := validateRasterImageSafety(data, maxAnalyzedTaskImageBytes)
		if err != nil {
			return "", fmt.Errorf("validate image file: %w", err)
		}
		return imageDataURL(mimeType, data), nil
	}
	if !strings.HasPrefix(imageURL, "https://") {
		return "", errors.New("image_url must be an HTTPS URL")
	}
	downloader := s.downloadAnalysisImage
	if downloader == nil {
		downloader = downloadTaskAnalysisImage
	}
	data, err := downloader(ctx, imageURL, maxAnalyzedTaskImageBytes)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	mimeType, _, err := validateRasterImageSafety(data, maxAnalyzedTaskImageBytes)
	if err != nil {
		return "", fmt.Errorf("validate downloaded image: %w", err)
	}
	return imageDataURL(mimeType, data), nil
}

func imageDataURL(mimeType string, data []byte) string {
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
}

func (s *TaskImageOperationsService) recordAnalysisCost(ctx context.Context, taskID, providerRequestID string, usage *srvconfig.TokenUsage) {
	recordUnderstandingCost(ctx, s.costs, s.logger, understandingCostRequest{
		TaskID: taskID, Provider: s.config.UnderstandingProvider, Model: s.config.UnderstandingModel,
		ProviderRequestID: providerRequestID, MediaKind: "image", Usage: usage,
	})
}

func downloadTaskAnalysisImage(ctx context.Context, imageURL string, maxSize int64) ([]byte, error) {
	client := &http.Client{
		Timeout:       15 * time.Second,
		Transport:     &http.Transport{DialContext: publicTaskAnalysisDialContext},
		CheckRedirect: validateTaskAnalysisRedirect,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if req.URL.Scheme != "https" || req.URL.Hostname() == "" || req.URL.User != nil {
		return nil, errors.New("invalid external image URL")
	}
	req.Header.Set("Accept", "image/*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download external image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download external image: unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read external image: %w", err)
	}
	if int64(len(data)) > maxSize {
		return nil, errors.New("external image exceeds max size")
	}
	return data, nil
}

func validateTaskAnalysisRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return errors.New("too many redirects")
	}
	if req == nil || req.URL == nil || req.URL.Scheme != "https" {
		return errors.New("external image redirects must use https")
	}
	return nil
}

func publicTaskAnalysisDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host: %w", err)
	}
	if len(ips) == 0 {
		return nil, errors.New("host did not resolve")
	}
	for _, address := range ips {
		if !isPublicTaskAnalysisIP(address.IP) {
			return nil, errors.New("external image host resolves to a non-public address")
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

func isPublicTaskAnalysisIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range taskAnalysisSpecialUsePrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

var taskAnalysisSpecialUsePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}
