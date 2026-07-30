package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

var (
	ErrVideoUnderstandingUnavailable       = errors.New("video understanding model is not configured")
	ErrVideoUnderstandingNativeUnsupported = errors.New("video understanding model does not support native video input")
)

type AnalyzeTaskVideoRequest struct {
	UserID, ProjectID, TaskID, TaskFileID, VideoURL, Prompt string
}

type AnalyzeTaskVideoResult struct {
	Analysis string               `json:"analysis"`
	Usage    srvconfig.TokenUsage `json:"usage"`
}

type NativeVideoUnderstandingClient interface {
	CompleteWithVideoURLResult(context.Context, string, string, string) (*LLMResult, error)
}

type TaskVideoOperationsConfig struct {
	UnderstandingProvider string
	UnderstandingModel    string
}

type TaskVideoOperationsService struct {
	repo   repository.Repository
	store  storage.Provider
	client LLMClient
	costs  UnderstandingCostRecorder
	config TaskVideoOperationsConfig
	logger *zerolog.Logger
}

func NewTaskVideoOperationsService(repo repository.Repository, store storage.Provider, client LLMClient, costs UnderstandingCostRecorder, cfg TaskVideoOperationsConfig, logger *zerolog.Logger) *TaskVideoOperationsService {
	return &TaskVideoOperationsService{repo: repo, store: store, client: client, costs: costs, config: cfg, logger: logger}
}

func (s *TaskVideoOperationsService) Analyze(ctx context.Context, req AnalyzeTaskVideoRequest) (*AnalyzeTaskVideoResult, error) {
	if strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New("project_id is required")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	if (strings.TrimSpace(req.VideoURL) == "") == (strings.TrimSpace(req.TaskFileID) == "") {
		return nil, errors.New("exactly one of video_url or task_file_id is required")
	}
	if s == nil || s.repo == nil {
		return nil, errors.New("video understanding repository is not available")
	}
	project, err := s.repo.Projects().FindByID(ctx, req.ProjectID)
	if err != nil || project == nil {
		return nil, errors.New("project not found")
	}
	if req.UserID == "" || project.UserID != req.UserID {
		return nil, errors.New("project does not belong to user")
	}
	if project.Status != model.ProjectStatusActive {
		return nil, errors.New("project is not active")
	}
	if s.client == nil {
		return nil, ErrVideoUnderstandingUnavailable
	}
	native, ok := s.client.(NativeVideoUnderstandingClient)
	if !ok {
		return nil, ErrVideoUnderstandingNativeUnsupported
	}

	videoURL, taskID, err := s.resolveVideoSource(ctx, req)
	if err != nil {
		return nil, err
	}
	providerRequestID := "internal:understanding:" + model.OperationVideoUnderstanding + ":" + uuid.NewString()
	result, err := native.CompleteWithVideoURLResult(ctx, "Analyze the complete authorized video natively. Do not substitute frames, audio, or text.", req.Prompt, videoURL)
	if err != nil {
		s.recordAnalysisCost(ctx, taskID, providerRequestID, nil)
		return nil, fmt.Errorf("analyze video: %w", err)
	}
	s.recordAnalysisCost(ctx, taskID, providerRequestID, &result.Usage)
	return &AnalyzeTaskVideoResult{Analysis: strings.TrimSpace(result.Text), Usage: result.Usage}, nil
}

func (s *TaskVideoOperationsService) resolveVideoSource(ctx context.Context, req AnalyzeTaskVideoRequest) (string, string, error) {
	if req.TaskFileID != "" {
		file, err := s.repo.TaskFiles().FindByID(ctx, req.TaskFileID)
		if err != nil || file == nil {
			return "", "", errors.New("task_file_id not found")
		}
		if req.TaskID != "" && req.TaskID != file.TaskID {
			return "", "", errors.New("task_file_id does not belong to task_id")
		}
		task, err := s.validateTask(ctx, req.UserID, req.ProjectID, file.TaskID)
		if err != nil {
			return "", "", err
		}
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(file.MimeType)), "video/") {
			return "", "", errors.New("task file is not a video")
		}
		resolved, err := ResolveMediaSource(ctx, s.store, s.logger, MediaSourceRequest{
			Key: file.OSSKey, RawURL: firstNonEmpty(file.OSSURL, file.URL), ContentType: file.MimeType,
		})
		if err != nil {
			return "", "", fmt.Errorf("resolve task video: %w", err)
		}
		return resolved.URL, task.ID, nil
	}

	if req.TaskID != "" {
		if _, err := s.validateTask(ctx, req.UserID, req.ProjectID, req.TaskID); err != nil {
			return "", "", err
		}
	}
	parsed, err := url.Parse(req.VideoURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", "", errors.New("video_url must be an HTTPS URL with a host")
	}
	if parsed.User != nil {
		return "", "", errors.New("video_url must not contain userinfo")
	}
	resolved, err := ResolveMediaSource(ctx, s.store, s.logger, MediaSourceRequest{RawURL: req.VideoURL, ContentType: "video/*"})
	if err != nil {
		return "", "", fmt.Errorf("resolve video_url: %w", err)
	}
	return resolved.URL, req.TaskID, nil
}

func (s *TaskVideoOperationsService) validateTask(ctx context.Context, userID, projectID, taskID string) (*model.Task, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil || task == nil {
		return nil, errors.New("task not found")
	}
	if userID == "" || task.UserID != userID {
		return nil, errors.New("task does not belong to user")
	}
	if task.ProjectID != projectID {
		return nil, errors.New("task does not belong to the requested project")
	}
	return task, nil
}

func (s *TaskVideoOperationsService) recordAnalysisCost(ctx context.Context, taskID, providerRequestID string, usage *srvconfig.TokenUsage) {
	recordUnderstandingCost(ctx, s.costs, s.logger, understandingCostRequest{
		TaskID: taskID, Provider: s.config.UnderstandingProvider, Model: s.config.UnderstandingModel,
		ProviderRequestID: providerRequestID, MediaKind: "video", Usage: usage,
	})
}

type understandingCostRequest struct {
	TaskID, Provider, Model, ProviderRequestID, MediaKind string
	Usage                                                 *srvconfig.TokenUsage
}

func recordUnderstandingCost(ctx context.Context, costs UnderstandingCostRecorder, logger *zerolog.Logger, req understandingCostRequest) {
	if costs == nil || req.Provider == "" || req.Model == "" {
		return
	}
	var err error
	if req.Usage == nil || (req.Usage.InputTokens <= 0 && req.Usage.CachedInputTokens <= 0 && req.Usage.CacheReadInputTokens <= 0 && req.Usage.CacheCreationInputTokens <= 0 && req.Usage.OutputTokens <= 0) {
		_, err = costs.RecordMediaUnreconciled(ctx, RecordMediaUnreconciledRequest{
			TaskID: req.TaskID, Provider: req.Provider, Model: req.Model,
			ProviderRequestID: req.ProviderRequestID, MediaKind: req.MediaKind,
			ReasonCode: model.BillingExecutionCostReasonMissingProviderUsage,
		})
	} else {
		cacheRead := req.Usage.CacheReadInputTokens
		if cacheRead == 0 {
			cacheRead = req.Usage.CachedInputTokens
		}
		_, err = costs.RecordProviderTokenUsage(ctx, RecordProviderTokenCostRequest{
			TaskID: req.TaskID, Provider: req.Provider, Model: req.Model,
			ProviderRequestID: req.ProviderRequestID, CatalogID: costs.CatalogID(), IdempotencyKey: req.ProviderRequestID,
			Usage:  TokenUsage{Input: req.Usage.InputTokens, CacheRead: cacheRead, CacheCreation: req.Usage.CacheCreationInputTokens, Output: req.Usage.OutputTokens},
			Source: string(model.BillingProviderCostSourceProviderResponse),
		})
	}
	if err != nil && logger != nil {
		logger.Error().Err(err).Str("task_id", req.TaskID).Str("provider_request_id", req.ProviderRequestID).Msg("record understanding provider cost; operation result remains valid")
	}
}
