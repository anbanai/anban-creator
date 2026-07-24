package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

const maxAnalyzedTaskImageBytes = 10 << 20

var (
	ErrTaskImageOperationOwnership       = errors.New("task does not belong to user")
	ErrTaskImageOperationProjectMismatch = errors.New("task does not belong to the requested project")
	ErrTaskImageOperationTaskNotFound    = errors.New("task not found")
	ErrTaskImageOperationTaskUnavailable = errors.New("task service not available")
	ErrTaskImageOperationProjectRequired = errors.New("project_id is required")
	ErrTaskImageOperationFileRequired    = errors.New("file_path is required")
	ErrTaskImageOperationPromptRequired  = errors.New("prompt is required")
	ErrTaskImageOperationSourceRequired  = errors.New("either image_url or file_path is required")
)

type UploadTaskImageRequest struct {
	UserID, ProjectID, TaskID, FilePath string
}

type CompressTaskImageRequest struct {
	UserID, TaskID, FilePath string
	MaxWidth                 int
}

type CompressTaskImageResult struct {
	FilePath   string `json:"file_path"`
	Compressed bool   `json:"compressed"`
}

type AnalyzeTaskImageRequest struct {
	UserID, ProjectID, TaskID, ImageURL, FilePath, Prompt string
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

type taskImageOperationsWriting interface {
	AnalyzeImageDetailed(context.Context, string, string, string) (*LLMResult, error)
}

type taskImageOperationsCost interface {
	CatalogID() string
	RecordProviderTokenUsage(context.Context, RecordProviderTokenCostRequest) (*model.BillingProviderCostEvent, error)
	RecordMediaUnreconciled(context.Context, RecordMediaUnreconciledRequest) (*model.BillingProviderCostEvent, error)
}

type TaskImageOperationsService struct {
	tasks                 *TaskService
	images                taskImageOperationsImage
	writing               taskImageOperationsWriting
	costs                 taskImageOperationsCost
	config                TaskImageOperationsConfig
	logger                *zerolog.Logger
	downloadAnalysisImage func(context.Context, string, int64) ([]byte, error)
}

func NewTaskImageOperationsService(tasks *TaskService, images taskImageOperationsImage, writing taskImageOperationsWriting, costs taskImageOperationsCost, cfg TaskImageOperationsConfig, logger *zerolog.Logger) *TaskImageOperationsService {
	return &TaskImageOperationsService{
		tasks: tasks, images: images, writing: writing, costs: costs, config: cfg, logger: logger,
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
	result, err := s.images.UploadImage(ctx, req.UserID, req.ProjectID, req.FilePath)
	if err != nil {
		return nil, fmt.Errorf("upload image: %w", err)
	}
	return result, nil
}

func (s *TaskImageOperationsService) Compress(ctx context.Context, req CompressTaskImageRequest) (*CompressTaskImageResult, error) {
	if req.FilePath == "" {
		return nil, ErrTaskImageOperationFileRequired
	}
	if s == nil || s.images == nil {
		return nil, errors.New("image service not available")
	}
	if err := s.validateTask(ctx, req.UserID, req.TaskID, ""); err != nil {
		return nil, err
	}
	filePath, compressed, err := s.images.CompressImage(req.FilePath, req.MaxWidth)
	if err != nil {
		return nil, fmt.Errorf("compress image: %w", err)
	}
	return &CompressTaskImageResult{FilePath: filePath, Compressed: compressed}, nil
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
	if s == nil || s.writing == nil {
		return nil, errors.New("writing/vision service not available")
	}
	if err := s.validateTask(ctx, req.UserID, req.TaskID, req.ProjectID); err != nil {
		return nil, err
	}
	imageSource, err := s.loadAnalysisSource(ctx, req.ImageURL, req.FilePath)
	if err != nil {
		return nil, err
	}
	providerRequestID := "internal:understanding:" + model.OperationImageUnderstanding + ":" + uuid.NewString()
	result, err := s.writing.AnalyzeImageDetailed(ctx, req.UserID, imageSource, req.Prompt)
	if err != nil {
		s.recordAnalysisCost(ctx, req.TaskID, providerRequestID, nil)
		return nil, fmt.Errorf("analyze image: %w", err)
	}
	s.recordAnalysisCost(ctx, req.TaskID, providerRequestID, &result.Usage)
	return &AnalyzeTaskImageResult{Analysis: result.Text, Usage: result.Usage}, nil
}

func (s *TaskImageOperationsService) validateTask(ctx context.Context, userID, taskID, projectID string) error {
	if taskID == "" {
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
		info, err := os.Stat(filePath)
		if err != nil {
			return "", fmt.Errorf("read image file: %w", err)
		}
		if info.Size() > maxAnalyzedTaskImageBytes {
			return "", errors.New("image file is too large for analysis (max 10MB)")
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read image file: %w", err)
		}
		mimeType := http.DetectContentType(data)
		if !strings.HasPrefix(mimeType, "image/") {
			return "", errors.New("file is not an image")
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
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", errors.New("downloaded file is not an image")
	}
	return imageDataURL(mimeType, data), nil
}

func imageDataURL(mimeType string, data []byte) string {
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
}

func (s *TaskImageOperationsService) recordAnalysisCost(ctx context.Context, taskID, providerRequestID string, usage *srvconfig.TokenUsage) {
	if s == nil || s.costs == nil || s.config.UnderstandingProvider == "" || s.config.UnderstandingModel == "" {
		return
	}
	var err error
	if usage == nil || usage.TotalTokens <= 0 {
		_, err = s.costs.RecordMediaUnreconciled(ctx, RecordMediaUnreconciledRequest{
			TaskID: taskID, Provider: s.config.UnderstandingProvider, Model: s.config.UnderstandingModel,
			ProviderRequestID: providerRequestID, MediaKind: "image", ReasonCode: model.BillingExecutionCostReasonMissingProviderUsage,
		})
	} else {
		cacheRead := usage.CacheReadInputTokens
		if cacheRead == 0 {
			cacheRead = usage.CachedInputTokens
		}
		_, err = s.costs.RecordProviderTokenUsage(ctx, RecordProviderTokenCostRequest{
			TaskID: taskID, Provider: s.config.UnderstandingProvider, Model: s.config.UnderstandingModel,
			ProviderRequestID: providerRequestID, CatalogID: s.costs.CatalogID(), IdempotencyKey: providerRequestID,
			Usage:  TokenUsage{Input: usage.InputTokens, CacheRead: cacheRead, CacheCreation: usage.CacheCreationInputTokens, Output: usage.OutputTokens},
			Source: string(model.BillingProviderCostSourceProviderResponse),
		})
	}
	if err != nil && s.logger != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Str("provider_request_id", providerRequestID).Msg("record understanding provider cost; operation result remains valid")
	}
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
