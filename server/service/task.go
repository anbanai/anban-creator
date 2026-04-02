package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// TaskEnqueuer abstracts the async task enqueue mechanism (Asynq, in-process, etc.).
type TaskEnqueuer interface {
	Enqueue(taskType string, payload []byte) error
}

// TypeContentGenerate is the Asynq task type for content generation.
const TypeContentGenerate = "content:generate"

// TaskService handles task CRUD, manual creation, and execution orchestration.
type TaskService struct {
	repo     repository.Repository
	executor *agent.Executor
	logger   *zerolog.Logger
	enqueuer TaskEnqueuer
}

// NewTaskService creates a new TaskService.
func NewTaskService(
	repo repository.Repository,
	executor *agent.Executor,
	enqueuer TaskEnqueuer,
	logger *zerolog.Logger,
) *TaskService {
	return &TaskService{
		repo:     repo,
		executor: executor,
		logger:   logger,
		enqueuer: enqueuer,
	}
}

// CreateManual creates a task without a plan and enqueues it for execution.
func (s *TaskService) CreateManual(ctx context.Context, userID, taskType, topic string) (*model.Task, error) {
	if taskType == "" {
		return nil, fmt.Errorf("type is required")
	}

	taskID := generateTaskID()

	task := &model.Task{
		ID:        taskID,
		UserID:    userID,
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
		tasks, err = s.repo.Tasks().FindByUserIDAndStatus(ctx, userID, status, offset, limit)
	} else {
		tasks, err = s.repo.Tasks().FindByUserID(ctx, userID, offset, limit)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}

	var total int64
	if status != "" {
		total, err = s.repo.Tasks().CountByUserIDAndStatus(ctx, userID, status)
	} else {
		total, err = s.repo.Tasks().CountByUserID(ctx, userID)
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
func (s *TaskService) EnqueueExecution(ctx context.Context, task *model.Task, userConfig *model.UserConfig) error {
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
	go s.HandleExecution(context.Background(), task, userConfig)
	return nil
}

// HandleExecution is called by the async worker to execute a task.
// It calls the agent executor and updates status in the DB.
func (s *TaskService) HandleExecution(ctx context.Context, task *model.Task, userConfig *model.UserConfig) error {
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

	// Load user config if not provided.
	if userConfig == nil {
		cfg, err := s.repo.UserConfigs().FindByUserAndScope(ctx, userID, task.Type)
		if err != nil {
			s.logger.Warn().Err(err).
				Str("task_id", taskID).
				Str("scope", task.Type).
				Msg("no user config found for task type, proceeding without config")
		} else {
			userConfig = cfg
		}
	}

	// Execute via agent.
	result, execErr := s.executor.Execute(ctx, &agent.ExecutionOptions{
		Task:       task,
		UserConfig: userConfig,
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
		_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, execErr.Error())
		_ = s.repo.Tasks().SetCompletedAt(ctx, taskID)
		return execErr
	}

	if !result.Success {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "execution returned unsuccessful result"
		}
		s.logger.Error().Str("task_id", taskID).Str("error", errMsg).Msg("task execution returned failure")
		_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, errMsg)
		_ = s.repo.Tasks().SetCompletedAt(ctx, taskID)
		return nil
	}

	// Success.
	s.logger.Info().Str("task_id", taskID).Str("work_dir", result.WorkDir).Msg("task completed successfully")
	_ = s.repo.Tasks().UpdateStatus(ctx, taskID, model.TaskStatusCompleted)
	_ = s.repo.Tasks().SetCompletedAt(ctx, taskID)

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
