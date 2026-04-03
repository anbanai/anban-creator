package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/storage"
)

// TaskEnqueuer abstracts the async task enqueue mechanism (Asynq, in-process, etc.).
type TaskEnqueuer interface {
	Enqueue(taskType string, payload []byte) error
	EnqueueIn(taskType string, payload []byte, delay time.Duration) error
}

// TypeContentGenerate is the Asynq task type for content generation.
const TypeContentGenerate = "content:generate"

// TaskService handles task CRUD, manual creation, and execution orchestration.
type TaskService struct {
	repo     repository.Repository
	executor *agent.Executor
	logger   *zerolog.Logger
	enqueuer TaskEnqueuer
	store    storage.Provider
}

// NewTaskService creates a new TaskService.
func NewTaskService(
	repo repository.Repository,
	executor *agent.Executor,
	enqueuer TaskEnqueuer,
	store storage.Provider,
	logger *zerolog.Logger,
) *TaskService {
	return &TaskService{
		repo:     repo,
		executor: executor,
		logger:   logger,
		enqueuer: enqueuer,
		store:    store,
	}
}

// CreateManual creates a task without a plan and enqueues it for execution.
func (s *TaskService) CreateManual(ctx context.Context, userID, channelID, topic string) (*model.Task, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel_id is required")
	}

	// Load and validate channel.
	channel, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}
	if channel.UserID != userID {
		return nil, fmt.Errorf("channel not owned by user")
	}
	if channel.Status != model.ChannelStatusActive {
		return nil, fmt.Errorf("channel is not active")
	}

	taskID := generateTaskID()
	taskType := channel.Platform

	task := &model.Task{
		ID:        taskID,
		UserID:    userID,
		ChannelID: channelID,
		Type:      taskType,
		Status:    model.TaskStatusPending,
		Topic:     topic,
	}

	if err := s.repo.Tasks().Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	// Enqueue for async execution.
	if err := s.EnqueueExecution(ctx, task, nil); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to enqueue task, marking as failed")
		_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, "failed to enqueue: "+err.Error())
		return task, nil
	}

	return task, nil
}

// CreateFromPlan creates a task linked to a plan and enqueues it for execution.
func (s *TaskService) CreateFromPlan(ctx context.Context, plan *model.Plan) (*model.Task, error) {
	taskID := generateTaskID()

	topic := plan.TopicHint
	if topic == "" {
		topic = plan.Title
	}

	// Derive task type from the channel if ChannelID is set.
	taskType := plan.Type
	if plan.ChannelID != "" {
		ch, err := s.repo.Channels().FindByID(ctx, plan.ChannelID)
		if err == nil {
			taskType = ch.Platform
		}
	}

	task := &model.Task{
		ID:        taskID,
		UserID:    plan.UserID,
		ChannelID: plan.ChannelID,
		Type:      taskType,
		Status:    model.TaskStatusPending,
		Topic:     topic,
	}

	if err := s.repo.Tasks().Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create task from plan: %w", err)
	}

	if err := s.EnqueueExecution(ctx, task, nil); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Str("plan_id", plan.ID).Msg("failed to enqueue plan task")
		_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, "failed to enqueue: "+err.Error())
		return task, nil
	}

	return task, nil
}

// GetByID returns a task by its ID.
func (s *TaskService) GetByID(ctx context.Context, id string) (*model.Task, error) {
	task, err := s.repo.Tasks().FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}
	return task, nil
}

// List returns tasks for a user with optional status filter and pagination.
func (s *TaskService) List(ctx context.Context, userID string, offset, limit int, status string) ([]*model.Task, int64, error) {
	var tasks []*model.Task
	var err error

	if status != "" {
		tasks, err = s.repo.Tasks().FindByUserIDAndStatus(ctx, userID, status, "", offset, limit)
	} else {
		tasks, err = s.repo.Tasks().FindByUserID(ctx, userID, "", offset, limit)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}

	var total int64
	if status != "" {
		total, err = s.repo.Tasks().CountByUserIDAndStatus(ctx, userID, status, "")
	} else {
		total, err = s.repo.Tasks().CountByUserID(ctx, userID, "")
	}
	if err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}

	return tasks, total, nil
}

// Cancel sets a task's status to "cancelled".
func (s *TaskService) Cancel(ctx context.Context, id string) error {
	if err := s.repo.Tasks().UpdateStatus(ctx, id, model.TaskStatusCancelled); err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	return nil
}

// GetFiles returns files associated with a task.
func (s *TaskService) GetFiles(ctx context.Context, taskID string) ([]*model.TaskFile, error) {
	files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get task files: %w", err)
	}
	return files, nil
}

// EnqueueExecution enqueues a task for async execution.
// If no enqueuer is available (nil), it runs synchronously in a goroutine.
func (s *TaskService) EnqueueExecution(ctx context.Context, task *model.Task, channel *model.Channel) error {
	if s.enqueuer != nil {
		payload, err := json.Marshal(map[string]string{
			"task_id": task.ID,
			"user_id": task.UserID,
		})
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}

		if err := s.enqueuer.Enqueue(TypeContentGenerate, payload); err != nil {
			return fmt.Errorf("enqueue task: %w", err)
		}

		s.logger.Info().Str("task_id", task.ID).Msg("task enqueued for async execution")
		return nil
	}

	// Fallback: run in goroutine if no enqueuer.
	s.logger.Warn().Str("task_id", task.ID).Msg("no enqueuer available, running task in goroutine")
	go s.HandleExecution(context.Background(), task, channel)
	return nil
}

// HandleExecution is called by the async worker to execute a task.
// It calls the agent executor and updates status in the DB.
func (s *TaskService) HandleExecution(ctx context.Context, task *model.Task, channel *model.Channel) error {
	taskID := task.ID
	userID := task.UserID

	s.logger.Info().Str("task_id", taskID).Msg("starting task execution")

	// Set status to running.
	if err := s.repo.Tasks().UpdateStatus(ctx, taskID, model.TaskStatusRunning); err != nil {
		return fmt.Errorf("set running status: %w", err)
	}
	if err := s.repo.Tasks().SetStartedAt(ctx, taskID); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set started_at")
	}

	// Load channel if not provided.
	if channel == nil {
		if task.ChannelID == "" {
			return fmt.Errorf("task has no channel_id, cannot execute")
		}
		ch, err := s.repo.Channels().FindByID(ctx, task.ChannelID)
		if err != nil {
			s.logger.Error().Err(err).
				Str("task_id", taskID).
				Str("channel_id", task.ChannelID).
				Msg("failed to load channel for task")
			return fmt.Errorf("load channel: %w", err)
		}
		if ch.UserID != userID {
			return fmt.Errorf("channel not owned by user")
		}
		channel = ch
	}

	// Execute via agent.
	result, execErr := s.executor.Execute(ctx, &agent.ExecutionOptions{
		Task:    task,
		Channel: channel,
		OnProgress: func(id string, message string) {
			current, err := s.repo.Tasks().FindByID(ctx, id)
			if err != nil {
				s.logger.Error().Err(err).Str("task_id", id).Msg("failed to read task for progress update")
				return
			}
			newLog := current.ProgressLog + message + "\n"
			if err := s.repo.Tasks().UpdateProgressLog(ctx, id, newLog); err != nil {
				s.logger.Error().Err(err).Str("task_id", id).Msg("failed to update progress log")
			}
		},
	})

	// Store result.
	resultJSON, _ := json.Marshal(result)
	_ = s.repo.Tasks().UpdateResult(ctx, taskID, string(resultJSON))

	if execErr != nil {
		s.logger.Error().Err(execErr).Str("task_id", taskID).Msg("task execution failed")
		return s.HandleExecutionFailure(ctx, task, execErr)
	}

	if !result.Success {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "execution returned unsuccessful result"
		}
		s.logger.Error().Str("task_id", taskID).Str("error", errMsg).Msg("task execution returned failure")
		return s.HandleExecutionFailure(ctx, task, fmt.Errorf("%s", errMsg))
	}

	// Success.
	s.logger.Info().Str("task_id", taskID).Str("work_dir", result.WorkDir).Msg("task completed successfully")
	_ = s.repo.Tasks().UpdateStatus(ctx, taskID, model.TaskStatusCompleted)
	_ = s.repo.Tasks().SetCompletedAt(ctx, taskID)

	// Upload generated files to storage.
	if result.WorkDir != "" {
		if err := s.UploadTaskFiles(ctx, taskID, userID, result.WorkDir); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("file upload failed")
		}
	}

	return nil
}

// HandleExecutionFromPayload is a convenience method that loads the task from the DB
// by ID and delegates to HandleExecution.
func (s *TaskService) HandleExecutionFromPayload(ctx context.Context, taskID, userID string) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task %s: %w", taskID, err)
	}

	if task.Status == model.TaskStatusRunning {
		s.logger.Warn().Str("task_id", taskID).Msg("task already running, skipping")
		return nil
	}

	if task.Status != model.TaskStatusPending {
		s.logger.Warn().Str("task_id", taskID).Str("status", task.Status).Msg("task not in pending state, skipping")
		return nil
	}

	return s.HandleExecution(ctx, task, nil)
}

// generateTaskID generates a unique task ID using UUID v4.
func generateTaskID() string {
	return uuid.New().String()
}

// retryBackoffs defines exponential backoff delays for retries.
var retryBackoffs = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

// HandleExecutionFailure handles task execution failures with retry logic.
// If the task has not exceeded max retries, it schedules a delayed retry.
// Otherwise, it marks the task as permanently failed.
func (s *TaskService) HandleExecutionFailure(ctx context.Context, task *model.Task, execErr error) error {
	taskID := task.ID

	// Set defaults for retry configuration.
	if task.MaxRetries <= 0 {
		task.MaxRetries = model.DefaultRetries
	}

	// Check if retry is possible.
	if task.RetryCount >= task.MaxRetries {
		s.logger.Error().
			Err(execErr).
			Str("task_id", taskID).
			Int("retry_count", task.RetryCount).
			Int("max_retries", task.MaxRetries).
			Msg("task permanently failed after max retries")
		_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, execErr.Error())
		_ = s.repo.Tasks().SetCompletedAt(ctx, taskID)
		return execErr
	}

	// Increment retry count.
	task.RetryCount++
	_ = s.repo.Tasks().Update(ctx, task)

	// Calculate backoff delay.
	idx := task.RetryCount - 1
	if idx >= len(retryBackoffs) {
		idx = len(retryBackoffs) - 1
	}
	delay := retryBackoffs[idx]

	s.logger.Info().
		Err(execErr).
		Str("task_id", taskID).
		Int("retry_count", task.RetryCount).
		Dur("backoff", delay).
		Msg("scheduling task retry")

	// Schedule retry via enqueuer with delay.
	if s.enqueuer != nil {
		payload, _ := json.Marshal(map[string]string{
			"task_id": task.ID,
			"user_id": task.UserID,
		})
		_ = s.enqueuer.EnqueueIn(TypeContentGenerate, payload, delay)
	}

	// Update status back to pending so it will be picked up.
	return s.repo.Tasks().UpdateStatus(ctx, taskID, model.TaskStatusPending)
}

// CleanupExpiredWorkspaces cleans up workspace directories for completed/failed
// tasks that are older than the specified threshold.
func (s *TaskService) CleanupExpiredWorkspaces(ctx context.Context) error {
	threshold := time.Now().Add(-1 * time.Hour)
	tasks, err := s.repo.Tasks().FindCompletedOlderThan(ctx, threshold)
	if err != nil {
		return fmt.Errorf("find completed tasks for cleanup: %w", err)
	}

	s.logger.Info().Int("count", len(tasks)).Msg("found tasks eligible for cleanup")

	for _, task := range tasks {
		workDir := fmt.Sprintf("/tmp/anbanwriter/%s", task.ID)
		if err := os.RemoveAll(workDir); err != nil {
			s.logger.Warn().Err(err).Str("task_id", task.ID).Str("path", workDir).Msg("failed to remove workspace directory")
			continue
		}

		// Also delete uploaded storage objects for this task.
		if s.store != nil {
			files, err := s.repo.TaskFiles().FindByTaskID(ctx, task.ID)
			if err != nil {
				s.logger.Warn().Err(err).Str("task_id", task.ID).Msg("failed to list task files for storage cleanup")
			} else {
				for _, f := range files {
					if err := s.store.Delete(ctx, f.OSSKey); err != nil {
						s.logger.Warn().Err(err).Str("task_id", task.ID).Str("oss_key", f.OSSKey).Msg("failed to delete storage object")
					}
				}
			}
		}

		now := time.Now()
		if err := s.repo.Tasks().UpdateCleanedUpAt(ctx, task.ID, now); err != nil {
			s.logger.Error().Err(err).Str("task_id", task.ID).Msg("failed to update cleaned_up_at")
			continue
		}

		s.logger.Info().Str("task_id", task.ID).Str("path", workDir).Msg("cleaned up workspace directory")
	}

	return nil
}

// mimeTypes maps file extensions to MIME types.
var mimeTypes = map[string]string{
	".html":     "text/html",
	".htm":      "text/html",
	".css":      "text/css",
	".js":       "application/javascript",
	".json":     "application/json",
	".md":       "text/markdown",
	".markdown": "text/markdown",
	".png":      "image/png",
	".jpg":      "image/jpeg",
	".jpeg":     "image/jpeg",
	".gif":      "image/gif",
	".webp":     "image/webp",
	".svg":      "image/svg+xml",
	".mp4":      "video/mp4",
	".pdf":      "application/pdf",
	".zip":      "application/zip",
}

// detectMimeType returns the MIME type for a file based on its extension.
// Falls back to net/http.DetectContentType by reading the first 512 bytes.
func detectMimeType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if mime, ok := mimeTypes[ext]; ok {
		return mime
	}

	f, err := os.Open(filePath)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return http.DetectContentType(buf[:n])
}

// determineFileRole returns the role for a file based on its name and MIME type.
func determineFileRole(filename, mimeType string) string {
	base := strings.ToLower(filepath.Base(filename))
	if strings.HasPrefix(base, "cover") {
		return model.FileRoleCover
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".html" || ext == ".htm" {
		return model.FileRoleHTML
	}
	if ext == ".md" || ext == ".markdown" {
		return model.FileRoleMarkdown
	}
	if strings.HasPrefix(mimeType, "image/") {
		return model.FileRoleImage
	}
	return model.FileRoleOther
}

// UploadTaskFiles uploads all files from a task's work directory to storage.
// Individual upload failures are logged but do not abort the remaining uploads.
// Returns an error only if the work directory cannot be read at all.
func (s *TaskService) UploadTaskFiles(ctx context.Context, taskID, userID, workDir string) error {
	if s.store == nil {
		s.logger.Warn().Msg("no storage provider configured, skipping file upload")
		return nil
	}

	entries, err := os.ReadDir(workDir)
	if err != nil {
		return fmt.Errorf("read work directory %s: %w", workDir, err)
	}

	var batch []*model.TaskFile
	providerName := s.store.Name()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		filePath := filepath.Join(workDir, filename)

		info, err := entry.Info()
		if err != nil {
			s.logger.Warn().Err(err).Str("file", filename).Msg("failed to get file info, skipping")
			continue
		}

		mimeType := detectMimeType(filePath)
		role := determineFileRole(filename, mimeType)
		ossKey := fmt.Sprintf("%s/%s/%s", userID, taskID, filename)

		uploadResult, err := s.store.UploadFile(ctx, ossKey, filePath, mimeType)
		if err != nil {
			s.logger.Error().Err(err).
				Str("file", filename).
				Str("key", ossKey).
				Msg("failed to upload file, skipping")
			continue
		}

		fileSize := info.Size()
		if uploadResult.Size > 0 {
			fileSize = uploadResult.Size
		}

		batch = append(batch, &model.TaskFile{
			TaskID:          taskID,
			Role:            role,
			FileName:        filename,
			MimeType:        mimeType,
			FileSize:        fileSize,
			OSSKey:          ossKey,
			OSSURL:          uploadResult.URL,
			StorageProvider: providerName,
			FilePath:        filePath,
		})
	}

	if len(batch) > 0 {
		if err := s.repo.TaskFiles().BatchCreate(ctx, batch); err != nil {
			s.logger.Error().Err(err).
				Str("task_id", taskID).
				Int("count", len(batch)).
				Msg("failed to persist task file records")
		} else {
			s.logger.Info().
				Str("task_id", taskID).
				Int("count", len(batch)).
				Msg("task files uploaded to storage")
		}
	}

	return nil
}

// GetFileStream returns a ReadCloser for a task file's content and the TaskFile metadata.
func (s *TaskService) GetFileStream(ctx context.Context, fileID string) (io.ReadCloser, *model.TaskFile, error) {
	file, err := s.repo.TaskFiles().FindByID(ctx, fileID)
	if err != nil {
		return nil, nil, fmt.Errorf("find task file: %w", err)
	}

	data, err := s.getFileContent(ctx, file)
	if err != nil {
		return nil, file, fmt.Errorf("read file content: %w", err)
	}

	return io.NopCloser(bytes.NewReader(data)), file, nil
}

// DownloadZip creates a ZIP archive of all files belonging to a task.
// Returns the ZIP buffer and the suggested download filename.
func (s *TaskService) DownloadZip(ctx context.Context, taskID string) (*bytes.Buffer, string, error) {
	files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, "", fmt.Errorf("find task files: %w", err)
	}
	if len(files) == 0 {
		return nil, "", fmt.Errorf("no files found for task %s", taskID)
	}

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	for _, file := range files {
		data, err := s.getFileContent(ctx, file)
		if err != nil {
			s.logger.Warn().Err(err).
				Str("file_name", file.FileName).
				Str("oss_key", file.OSSKey).
				Msg("failed to read file for zip, skipping")
			continue
		}

		w, err := zipWriter.Create(file.FileName)
		if err != nil {
			s.logger.Warn().Err(err).
				Str("file_name", file.FileName).
				Msg("failed to create zip entry, skipping")
			continue
		}

		if _, err := w.Write(data); err != nil {
			s.logger.Warn().Err(err).
				Str("file_name", file.FileName).
				Msg("failed to write file to zip, skipping")
			continue
		}
	}

	if err := zipWriter.Close(); err != nil {
		return nil, "", fmt.Errorf("close zip writer: %w", err)
	}

	zipName := fmt.Sprintf("task_%s_files.zip", taskID)
	return &buf, zipName, nil
}

// getFileContent reads the full content of a task file based on its storage provider.
func (s *TaskService) getFileContent(ctx context.Context, file *model.TaskFile) ([]byte, error) {
	if s.store == nil {
		return nil, fmt.Errorf("no storage provider configured")
	}

	if s.store.Name() == "local" {
		return os.ReadFile(file.FilePath)
	}

	// For OSS and other remote providers, use a signed URL.
	signedURL, err := s.store.DownloadURL(ctx, file.OSSKey, 3600)
	if err != nil {
		return nil, fmt.Errorf("get download URL for %s: %w", file.OSSKey, err)
	}

	resp, err := http.Get(signedURL)
	if err != nil {
		return nil, fmt.Errorf("download file from %s: %w", signedURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download file from %s: unexpected status %d", signedURL, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
